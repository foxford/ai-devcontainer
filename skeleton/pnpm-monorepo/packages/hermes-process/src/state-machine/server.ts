/* eslint-disable complexity -- validate разбирает много вариантов handoff одним ветвлением */
/**
 * HTTP-сервер для валидации kanban_complete и блокировки fan-out.
 *
 * Шелл-хуки Hermes (см. .hermes/hermes-hooks/validation/) POST-ят payload
 * сюда. Сервер валидирует Zod-схемой из pipeline.ts и возвращает
 * {action: "block"|"allow", message?}.
 */
import { createServer } from 'node:http'

import { fetchTask, fetchTaskParents } from './db.ts'
import { extractMetadataFromBody } from './kanban.ts'
import { log } from './log.ts'
import { ROLES } from './pipeline.ts'
import { NODE_SCHEMAS } from './schemas/index.ts'

import type { NodeName } from './pipeline.ts'
import type { IncomingMessage, ServerResponse } from 'node:http'

const PORT = Number(process.env.HERMES_SM_PORT || 43210)

interface HookPayload {
  hook_event_name: string
  tool_name: string | null
  tool_input: Record<string, unknown> | null
  session_id: string
  cwd: string
  extra: Record<string, unknown>
}

interface Decision {
  action?: 'block'
  message?: string
}

function readJsonBody(req: IncomingMessage): Promise<unknown> {
  return new Promise((resolve, reject) => {
    let body = ''
    req.setEncoding('utf8')
    req.on('data', (chunk) => {
      body += chunk
    })
    req.on('end', () => {
      try {
        resolve(body ? JSON.parse(body) : {})
      } catch (e) {
        reject(e)
      }
    })
    req.on('error', reject)
  })
}

function jsonResponse(res: ServerResponse, status: number, body: unknown): void {
  res.statusCode = status
  res.setHeader('Content-Type', 'application/json')
  res.end(JSON.stringify(body))
}

export function startServer(): void {
  const server = createServer(async (req, res) => {
    if (req.url === '/health' && req.method === 'GET') {
      res.statusCode = 200
      res.setHeader('Content-Type', 'text/plain')
      res.end('ok')
      return
    }
    if (req.url === '/validate' && req.method === 'POST') {
      try {
        const payload = (await readJsonBody(req)) as HookPayload
        const decision = await validate(payload)
        jsonResponse(res, 200, decision)
      } catch (e) {
        log.error('validation failed', { error: String(e) })
        // Fail-open: возвращаем пустой результат, чтобы не блокировать
        // legitimate work из-за внутренней ошибки сервера.
        jsonResponse(res, 200, {})
      }
      return
    }
    res.statusCode = 404
    res.end('not found')
  })

  server.listen(PORT, '127.0.0.1', () => {
    log.info('validation server listening', { port: PORT })
  })
}

async function validate(payload: HookPayload): Promise<Decision> {
  const tool = payload.tool_name
  const input = payload.tool_input ?? {}
  const profile = (payload.extra?.profile as string) || '(unknown)'

  // ── 1. Запрет kanban_create / kanban_link / kanban_unblock для не-team-lead ──
  if (tool === 'kanban_create' || tool === 'kanban_link' || tool === 'kanban_unblock') {
    const roleSpec = (ROLES as Record<string, { canCreateTasks: boolean }>)[profile]
    if (!roleSpec?.canCreateTasks) {
      return {
        action: 'block',
        message:
          `Profile "${profile}" is not allowed to call ${tool}. ` +
          `Only "team-lead" can create tasks, and only during decomposition. ` +
          `Use kanban_complete with your handoff metadata; the state machine will route downstream.`,
      }
    }
    return {}
  }

  // ── 2. Валидация kanban_complete metadata по schema узла ──
  if (tool === 'kanban_complete') {
    const taskId = (payload.extra?.task_id as string) || (input.task_id as string)
    if (!taskId) {
      log.warn('kanban_complete without task_id in extra; skipping validation')
      return {}
    }

    const task = fetchTask(taskId)
    if (!task) {
      log.warn('kanban_complete for unknown task; skipping', { taskId })
      return {}
    }

    const taskMeta = extractMetadataFromBody(task.body || '')
    const node = taskMeta.node as NodeName | undefined
    if (!node) {
      return {}
    }

    const schema = NODE_SCHEMAS[node]
    if (!schema) {
      log.warn('no schema for node', { node })
      return {}
    }

    const metadata = input.metadata ?? {}
    const result = schema.safeParse(metadata)
    if (!result.success) {
      const errs = result.error.errors.map((e) => `${e.path.join('.') || '<root>'}: ${e.message}`).join('; ')
      return {
        action: 'block',
        message:
          `Invalid metadata for node "${node}". Errors: ${errs}. ` +
          `Check the schema in packages/hermes-process/src/schemas/index.ts and fix the kanban_complete call.`,
      }
    }

    // Дополнительная семантическая валидация (требует SQL-доступа к kanban.db).
    if (node === 'decompose') {
      const data = result.data as { subtasks: string[]; rationale: string }
      const semErr = validateDecomposeSubtasks(taskId, data.subtasks)
      if (semErr) return { action: 'block', message: semErr }
    }

    return {}
  }

  return {}
}

/**
 * Для каждой subtask, которую team-lead заявил в metadata.subtasks:
 *   1. Таска физически существует в kanban.db.
 *   2. Assignee == "developer".
 *   3. Parents содержат сам decompose task.
 */
function validateDecomposeSubtasks(decomposeTaskId: string, subtasks: string[]): string | null {
  const errors: string[] = []

  for (const subtaskId of subtasks) {
    const task = fetchTask(subtaskId)
    if (!task) {
      errors.push(`subtask "${subtaskId}" does not exist in kanban`)
      continue
    }
    if (task.assignee !== 'developer') {
      errors.push(`subtask "${subtaskId}" has assignee="${task.assignee ?? '(none)'}" — must be "developer"`)
    }
    const parents = fetchTaskParents(subtaskId)
    if (!parents.includes(decomposeTaskId)) {
      errors.push(
        `subtask "${subtaskId}" does not list this decompose task ("${decomposeTaskId}") in its parents — ` +
          `it lists: [${parents.join(', ') || '(none)'}]`
      )
    }
  }

  if (errors.length === 0) return null

  return (
    `Invalid decomposition: ${errors.join('; ')}. ` +
    `Each subtask you create via kanban_create must (1) be assigned to "developer", ` +
    `(2) include this task id (${decomposeTaskId}) in parents=[...]. ` +
    `Re-create the bad subtasks and retry kanban_complete.`
  )
}
