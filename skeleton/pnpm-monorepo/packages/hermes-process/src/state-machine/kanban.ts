/**
 * Обёртка над `hermes kanban …` CLI.
 *
 * Используем CLI как стабильный API. Прямого SDK для kanban_db
 * у Hermes нет, а ходить в sqlite на запись — небезопасно (race с диспетчером).
 *
 * Чтение делаем через прямой sqlite (см. db.ts) — это безопасно
 * (WAL-режим, append-only events). Запись — только через CLI.
 */
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

import { z } from 'zod'

import { log } from './log.ts'

const execFileAsync = promisify(execFile)
const BOARD = process.env.HERMES_BOARD || 'default'

interface SpawnResult {
  stdout: string
  stderr: string
  exitCode: number
}

async function runHermes(args: string[]): Promise<SpawnResult> {
  const fullArgs = ['kanban', '--board', BOARD, ...args]
  log.debug('hermes spawn', { args: fullArgs })

  try {
    const { stdout, stderr } = await execFileAsync('hermes', fullArgs, {
      maxBuffer: 10 * 1024 * 1024, // 10 MB
    })
    return { exitCode: 0, stderr, stdout }
  } catch (e) {
    const err = e as NodeJS.ErrnoException & { stdout?: string; stderr?: string; code?: number | string }
    return {
      exitCode: typeof err.code === 'number' ? err.code : 1,
      stderr: err.stderr ?? String(err.message ?? err),
      stdout: err.stdout ?? '',
    }
  }
}

async function runHermesJson<T>(args: string[], schema: z.ZodType<T>): Promise<T> {
  const { stdout, stderr, exitCode } = await runHermes([...args, '--json'])
  if (exitCode !== 0) {
    throw new Error(`hermes kanban ${args.join(' ')} failed (exit ${exitCode}): ${stderr}`)
  }
  try {
    const parsed = JSON.parse(stdout)
    return schema.parse(parsed)
  } catch (e) {
    throw new Error(
      `Failed to parse hermes output for ${args.join(' ')}: ${e}\n--- stdout:\n${stdout}\n--- stderr:\n${stderr}`
    )
  }
}

// ────────────────────────────────────────────────────────────
// SCHEMAS for CLI responses
// ────────────────────────────────────────────────────────────

const TaskIdSchema = z.string().regex(/^t_[a-z0-9]+$/)

const CreateTaskResponseSchema = z.object({
  status: z.string(),
  task_id: TaskIdSchema,
})

// ────────────────────────────────────────────────────────────
// API
// ────────────────────────────────────────────────────────────

export type Workspace =
  | { kind: 'scratch' }
  | { kind: 'dir'; absolutePath: string }
  | { kind: 'worktree'; relativePath: string; branch?: string }

export interface CreateTaskInput {
  title: string
  assignee: string
  body?: string
  parents?: string[]
  workspace?: Workspace
  skills?: string[]
  metadata?: Record<string, unknown>
  /** Идемпотентность: дубликаты с тем же ключом возвращают существующий id. */
  idempotencyKey?: string
}

export async function createTask(input: CreateTaskInput): Promise<string> {
  const args: string[] = ['create', input.title, '--assignee', input.assignee]

  const body = composeBody(input.body, input.metadata)
  if (body) args.push('--body', body)

  for (const p of input.parents ?? []) args.push('--parent', p)

  if (input.workspace) {
    const ws = serializeWorkspace(input.workspace)
    args.push('--workspace', ws)
    if (input.workspace.kind === 'worktree' && input.workspace.branch) {
      args.push('--branch', input.workspace.branch)
    }
  }

  for (const s of input.skills ?? []) args.push('--skill', s)

  if (input.idempotencyKey) args.push('--idempotency-key', input.idempotencyKey)

  const result = await runHermesJson(args, CreateTaskResponseSchema)
  log.info('task created', { assignee: input.assignee, id: result.task_id, title: input.title })
  return result.task_id
}

function serializeWorkspace(ws: Workspace): string {
  switch (ws.kind) {
    case 'scratch':
      return 'scratch'
    case 'dir':
      return `dir:${ws.absolutePath}`
    case 'worktree':
      return `worktree:${ws.relativePath}`
  }
}

function composeBody(body: string | undefined, metadata: Record<string, unknown> | undefined): string {
  const parts: string[] = []
  if (body) parts.push(body)
  if (metadata && Object.keys(metadata).length > 0) {
    parts.push('\n──── STATE MACHINE METADATA (do not edit by hand) ────')
    parts.push('```json')
    parts.push(JSON.stringify(metadata, null, 2))
    parts.push('```')
  }
  return parts.join('\n')
}

/** Прочесть metadata из body таска (то что записал composeBody). */
export function extractMetadataFromBody(body: string): Record<string, unknown> {
  const match = body.match(/STATE MACHINE METADATA[\s\S]*?```json\s*([\s\S]*?)\s*```/)
  if (!match) return {}
  try {
    return JSON.parse(match[1] ?? '')
  } catch {
    return {}
  }
}

export async function linkTasks(parentId: string, childId: string): Promise<void> {
  const { exitCode, stderr } = await runHermes(['link', parentId, childId])
  if (exitCode !== 0) {
    throw new Error(`link ${parentId} → ${childId} failed: ${stderr}`)
  }
}

export async function commentOnTask(id: string, text: string, author = 'state-machine'): Promise<void> {
  const { exitCode, stderr } = await runHermes(['comment', id, text, '--author', author])
  if (exitCode !== 0) {
    throw new Error(`comment on ${id} failed: ${stderr}`)
  }
}

export async function blockTask(id: string, reason: string): Promise<void> {
  const { exitCode, stderr } = await runHermes(['block', id, reason])
  if (exitCode !== 0) {
    throw new Error(`block ${id} failed: ${stderr}`)
  }
}

export async function completeTaskManually(
  id: string,
  summary: string,
  metadata: Record<string, unknown>
): Promise<void> {
  const args = ['complete', id, '--summary', summary, '--metadata', JSON.stringify(metadata)]
  const { exitCode, stderr } = await runHermes(args)
  if (exitCode !== 0) {
    throw new Error(`complete ${id} failed: ${stderr}`)
  }
}
