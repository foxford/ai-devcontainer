/* eslint-disable max-lines, complexity -- движок стейт-машины: длинный по своей природе граф-обработчик */
/**
 * State machine engine.
 *
 * Что делает:
 *   1. Получает task_events из kanban.db (через src/db.ts).
 *   2. Решает что делать на основе графа из src/pipeline.ts.
 *   3. Создаёт следующие задачи через src/kanban.ts.
 *   4. Хранит счётчики циклов, маппинги root↔subtasks в src/db.ts (state.db).
 *
 * Файл намеренно НЕ содержит описания процесса — оно в pipeline.ts.
 */
import { execFile } from 'node:child_process'
import { resolve } from 'node:path'
import { promisify } from 'node:util'

import * as state from './db.ts'
import * as kanban from './kanban.ts'
import { log } from './log.ts'
import { NODES, TEMPLATES, TASK_PREFIX, worktreePath, branchFor } from './pipeline.ts'

import type { Template, NodeName, CompletionContext } from './pipeline.ts'

const execFileAsync = promisify(execFile)

/**
 * Корень репо берётся из env HERMES_PROJECT_ROOT, который bootstrap.sh
 * прописывает в gateway hook handler.py.
 */
const PROJECT_ROOT = process.env.HERMES_PROJECT_ROOT ? resolve(process.env.HERMES_PROJECT_ROOT) : process.cwd()

// ────────────────────────────────────────────────────────────
// KICKOFF: усыновление root-задачи, созданной юзером в Dashboard
// ────────────────────────────────────────────────────────────
//
// Юзер создаёт задачу через Hermes Dashboard. Опционально первой строкой
// в body указывает `template: feature|doc-only|bugfix` (по умолчанию — feature).
//
// Признаки user root task (определяет onTaskCreated):
//   - kind == "created" в task_events
//   - parents = [] (наши pipeline-step tasks всегда имеют parents)
//   - в body нет нашего METADATA-блока (мы свои tasks помечаем role=root|pipeline-step)
//
// Когда state machine видит такую задачу — она:
//   1. Парсит template из body (default = "feature")
//   2. Записывает в state.db как root
//   3. Создаёт первый pipeline step как child этой root задачи
//
// Сама root task ОСТАЁТСЯ открытой в kanban. Hermes-dispatcher её к worker
// не пошлёт, потому что у неё появился child через kanban_link (dispatcher
// уважает зависимости). Root закрывается state machine'ой позже —
// в closeRootSuccessfully после прохождения всего pipeline.

const TEMPLATE_LINE_REGEX = /^\s*template\s*:\s*([a-z-]+)\s*$/im

function parseTemplateFromBody(body: string): Template {
  const match = body.match(TEMPLATE_LINE_REGEX)
  if (!match) return 'feature'
  const value = match[1]?.toLowerCase()
  if (value === 'feature' || value === 'doc-only' || value === 'bugfix') {
    return value
  }
  return 'feature'
}

function slugifyTitle(title: string): string {
  return (
    title
      .toLowerCase()
      .replace(/^\[[^\]]*\]\s*/, '') // убрать существующий [DEV-N] префикс если есть
      .replace(/[^a-z0-9\s-]/g, '')
      .trim()
      .replace(/\s+/g, '-')
      .substring(0, 50) || `task-${Date.now()}`
  )
}

async function adoptUserRootTask(taskId: string): Promise<void> {
  const task = state.fetchTask(taskId)
  if (!task) {
    log.warn('adoptUserRootTask: task not found', { taskId })
    return
  }

  const template = parseTemplateFromBody(task.body || '')
  const spec = TEMPLATES[template]
  const featureSlug = slugifyTitle(task.title)
  const shortId = state.nextShortId(TASK_PREFIX)
  const worktreeRel = spec.needsWorktree ? worktreePath(featureSlug) : null

  log.info('adopting user root task', {
    featureSlug,
    shortId,
    taskId,
    template,
  })

  // Если нужен worktree — создаём его через git worktree add на отдельной ветке.
  // Делаем это до создания первого child, чтобы child мог работать в worktree.
  if (worktreeRel) {
    await ensureWorktree(featureSlug, worktreeRel)
  }

  state.saveRoot({
    feature_slug: featureSlug,
    root_id: taskId,
    short_id: shortId,
    template,
    worktree_path: worktreeRel,
  })

  state.saveTaskMeta({
    cycle_count: 0,
    feature_slug: featureSlug,
    impl_subtask_id: null,
    node_name: 'ROOT' as NodeName,
    root_id: taskId,
    short_id: shortId,
    task_id: taskId,
    template,
  })

  // Оставляем след в самом task для следующих просмотров (чтобы повторный
  // task_events.created не вызвал второй adopt — см. isAdoptedUserRoot).
  await kanban.commentOnTask(
    taskId,
    `🤖 State machine adopted as root (${shortId}). Template: \`${template}\`. ` +
      (worktreeRel ? `Worktree: \`${worktreeRel}\`. ` : '') +
      `Pipeline started — first step is "${spec.firstStep}".`
  )

  // Создаём первый узел по template, parents=[root-task]
  await createNextNode({
    featureSlug,
    implSubtaskId: null,
    node: spec.firstStep,
    parentTaskIds: [taskId],
    rootId: taskId,
    template,
    worktreeRelative: worktreeRel,
  })
}

async function ensureWorktree(slug: string, relativePath: string): Promise<void> {
  const absPath = resolve(PROJECT_ROOT, relativePath)
  // Idempotent: если worktree уже существует — пропускаем.
  try {
    const { stdout } = await execFileAsync('git', ['worktree', 'list', '--porcelain'], {
      cwd: PROJECT_ROOT,
    })
    if (stdout.includes(absPath)) {
      log.debug('worktree already exists', { absPath })
      return
    }
  } catch (e) {
    log.warn('git worktree list failed, attempting create anyway', { error: String(e) })
  }

  log.info('creating worktree', { absPath, branch: branchFor(slug) })
  try {
    await execFileAsync('git', ['worktree', 'add', '-b', branchFor(slug), absPath], {
      cwd: PROJECT_ROOT,
    })
  } catch (e) {
    const err = e as { stderr?: string; stdout?: string; message?: string }
    log.error('git worktree add failed', {
      message: err.message,
      stderr: err.stderr,
      stdout: err.stdout,
    })
    throw new Error(`git worktree add failed: ${err.stderr ?? err.message}`)
  }
}

// ────────────────────────────────────────────────────────────
// MAIN EVENT LOOP — обрабатываем task_events
// ────────────────────────────────────────────────────────────

export async function handleEvent(event: state.TaskEvent): Promise<void> {
  if (state.isEventProcessed(event.id)) return

  try {
    switch (event.kind) {
      case 'created':
        await onTaskCreated(event)
        break
      case 'completed':
        await onTaskCompleted(event)
        break
      case 'blocked':
        await onTaskBlocked(event)
        break
      // claimed/heartbeat/etc — нас не касаются
      default:
        break
    }
    state.markEventProcessed(event.id)
    state.setLastSeenEventId(event.id)
  } catch (e) {
    log.error('event handling failed', { error: String(e), event_id: event.id, kind: event.kind })
    // НЕ отмечаем event как processed — попробуем ещё раз на следующем тике.
    // Если будет повторяться — нужен circuit-breaker (тут не реализован).
    throw e
  }
}

/**
 * Реакция на создание любой task. Большинство task создаём мы сами —
 * их мы игнорируем (они уже в state.task_meta). User-created root task
 * (через Dashboard) — это та, которой у нас в state.task_meta ещё нет
 * и у которой нет parents в kanban.
 */
async function onTaskCreated(event: state.TaskEvent): Promise<void> {
  // Если task уже в нашем state — это наша pipeline-step task, игнор.
  if (state.getTaskMeta(event.task_id)) {
    return
  }

  const parents = state.fetchTaskParents(event.task_id)
  if (parents.length > 0) {
    // Это subtask, созданная team-lead-ом во время decompose —
    // её "усыновляет" onDecomposeComplete, не здесь.
    return
  }

  // No parents + не в нашем state → user root task из Dashboard.
  log.info('user root task detected', { taskId: event.task_id })
  await adoptUserRootTask(event.task_id)
}

async function onTaskCompleted(event: state.TaskEvent): Promise<void> {
  const meta = state.getTaskMeta(event.task_id)
  if (!meta) {
    log.debug("ignoring completion of task we don't track", { id: event.task_id })
    return
  }
  if (meta.node_name === 'ROOT') {
    log.info('root completed directly — should not happen in pipeline; ignoring', { id: event.task_id })
    return
  }

  const run = state.fetchLatestRun(event.task_id)
  const handoffMetadata = run?.metadata ?? {}

  // СПЕЦИАЛЬНЫЙ КЕЙС: decompose-узел. Team-lead создал subtasks сам через
  // kanban_create. Мы должны "усыновить" их в pipeline — повесить на каждую
  // pipeline-маршрут impl→review→qa→…
  if (meta.node_name === 'decompose') {
    await onDecomposeComplete(meta, handoffMetadata)
    return
  }

  // СПЕЦИАЛЬНЫЙ КЕЙС: final-verify. Узел сам по себе означает "subtask done"
  // (для feature) либо "root done" (для doc-only/bugfix). Тут проверяем,
  // все ли subtasks root-а закрыты — и если да, закрываем root.
  const node = NODES[meta.node_name as NodeName]
  if (!node) {
    log.error('unknown node', { node: meta.node_name })
    return
  }

  const ctx: CompletionContext = {
    cycleCount: meta.cycle_count,

    // не критично здесь
    featureSlug: meta.feature_slug,
    implSubtaskId: meta.impl_subtask_id ?? undefined,
    metadata: handoffMetadata,
    rootId: meta.root_id,
    rootTitle: '',
    taskShortId: meta.short_id,
    template: meta.template as Template,
    worktreePath: TEMPLATES[meta.template as Template].needsWorktree ? worktreePath(meta.feature_slug) : null,
  }

  const decision = node.onComplete(ctx)

  if (decision === 'root-done') {
    // Для feature: один impl-subtask закрыт. Проверим, все ли закрыты.
    if (meta.template === 'feature') {
      await maybeCloseFeatureRoot(meta.root_id)
    } else {
      // doc-only / bugfix — final-verify == root close
      await closeRootSuccessfully(meta.root_id)
    }
    return
  }

  if (decision.length === 0) {
    // Пустой результат означает что лимит цикла превышен (либо namespace ошибка).
    // Блокируем root для человека.
    await escalateToHuman(meta, 'Cycle limit exceeded or empty decision in onComplete')
    return
  }

  for (const next of decision) {
    await createNextNode({
      extraMetadata: next.metadata,
      featureSlug: meta.feature_slug,
      implSubtaskId: meta.impl_subtask_id,
      node: next.node,
      parentTaskIds: [event.task_id],
      rootId: meta.root_id,
      template: meta.template as Template,
      titleOverride: next.title,
      worktreeRelative: ctx.worktreePath ? worktreePath(meta.feature_slug) : null,
    })
  }
}

async function onTaskBlocked(event: state.TaskEvent): Promise<void> {
  const meta = state.getTaskMeta(event.task_id)
  if (!meta) return
  const reason = (event.payload?.reason as string) || 'no reason given'
  log.warn('task blocked by worker', { node: meta.node_name, reason, task_id: event.task_id })
  // Пробрасываем на root — человек разберётся.
  await escalateToHuman(meta, `Worker blocked at ${meta.node_name}: ${reason}`)
}

async function onDecomposeComplete(decomposeMeta: state.TaskMeta, handoff: Record<string, unknown>): Promise<void> {
  const subtaskIds = (handoff.subtasks as string[] | undefined) || []
  if (subtaskIds.length === 0) {
    await escalateToHuman(decomposeMeta, 'Team lead decompose produced 0 subtasks')
    return
  }

  log.info('adopting subtasks into pipeline', {
    root: decomposeMeta.root_id,
    subtasks: subtaskIds,
  })

  // Для каждой subtask, созданной team-lead-ом, запускаем impl → ... pipeline.
  // Subtask таска УЖЕ существует (её создал team-lead). Мы не создаём её
  // заново — мы продолжаем с review/qa-узлов как parents эту subtask.
  //
  // НО: team-lead создал subtask assignee=developer (так велит профайл team-lead).
  // Эта subtask и есть наш impl-узел. Поэтому мы регистрируем её в state.task_meta
  // как импл-узел и она поедет дальше по completion.
  for (const subtaskId of subtaskIds) {
    state.saveTaskMeta({
      cycle_count: 0,
      feature_slug: decomposeMeta.feature_slug,
      impl_subtask_id: subtaskId,
      node_name: 'impl',
      root_id: decomposeMeta.root_id,
      short_id: state.nextShortId(TASK_PREFIX),
      task_id: subtaskId,
      template: decomposeMeta.template, // эта таска и есть subtask
    })
  }
}

// ────────────────────────────────────────────────────────────
// CREATE NEXT NODE
// ────────────────────────────────────────────────────────────

interface CreateNextNodeArgs {
  node: NodeName
  rootId: string
  featureSlug: string
  template: Template
  parentTaskIds: string[]
  implSubtaskId: string | null
  worktreeRelative: string | null
  extraMetadata?: Record<string, unknown>
  titleOverride?: string
}

async function createNextNode(args: CreateNextNodeArgs): Promise<string> {
  const nodeSpec = NODES[args.node]
  const shortId = state.nextShortId(TASK_PREFIX)

  // Циклы: считаем сколько раз этот узел уже был для этой root + impl-subtask.
  const cycleCount = state.countCyclesForRoot(args.rootId, args.node, args.implSubtaskId)

  const title = args.titleOverride ?? `[${shortId}] ${nodeTitle(args.node, args.featureSlug)}`

  const taskId = await kanban.createTask({
    assignee: nodeSpec.assignee,
    metadata: {
      cycle: cycleCount + 1,
      feature_slug: args.featureSlug,
      node: args.node,
      role: 'pipeline-step',
      root_id: args.rootId,
      short_id: shortId,
      template: args.template,
      ...(args.extraMetadata || {}),
    },
    parents: args.parentTaskIds,
    skills: nodeSpec.pinnedSkills,
    title,
    workspace: args.worktreeRelative
      ? { absolutePath: resolve(PROJECT_ROOT, args.worktreeRelative), kind: 'dir' }
      : undefined,
  })

  state.saveTaskMeta({
    cycle_count: cycleCount + 1,
    feature_slug: args.featureSlug,
    impl_subtask_id: args.implSubtaskId,
    node_name: args.node,
    root_id: args.rootId,
    short_id: shortId,
    task_id: taskId,
    template: args.template,
  })

  return taskId
}

function nodeTitle(node: NodeName, slug: string): string {
  const human: Partial<Record<NodeName, string>> = {
    'bugfix-impl': 'Fix bug',
    'bugfix-qa': 'QA the fix',
    'bugfix-review': 'Review fix',
    decompose: 'Decompose into impl subtasks',
    'doc-only-impl': 'Write documentation',
    'doc-only-verify': 'Verify documentation',
    docs: 'Update documentation',
    'final-verify': 'Final verify',
    impl: 'Implement',
    plan: 'Write spec/plan',
    qa: 'QA (e2e/integration)',
    review: 'Code review',
    'route-post-qa': 'Route post-QA (security/docs?)',
    security: 'Security audit',
  }
  return `${human[node] ?? node} — ${slug}`
}

// ────────────────────────────────────────────────────────────
// ROOT CLOSURE
// ────────────────────────────────────────────────────────────

async function maybeCloseFeatureRoot(rootId: string): Promise<void> {
  const rootRecord = state.getRoot(rootId)
  if (!rootRecord) {
    log.error('maybeCloseFeatureRoot: root not found in state.db', { rootId })
    return
  }
  if (rootRecord.template !== 'feature') {
    // doc-only / bugfix приходят сюда не должны — для них verify == root close сразу
    // (см. ветку выше в onTaskCompleted). На всякий случай — закрываем.
    await closeRootSuccessfully(rootId)
    return
  }

  const implSubtaskIds = state.getImplSubtasksForRoot(rootId)
  if (implSubtaskIds.length === 0) {
    // Невозможно для feature: decompose обязан был положить >= 1 subtask
    // и schema это валидирует. Если всё-таки случилось — эскалируем.
    log.error('feature root has 0 impl subtasks at closure time', { rootId })
    const rootMeta = state.getTaskMeta(rootId)
    if (rootMeta) await escalateToHuman(rootMeta, 'feature root has 0 impl subtasks')
    return
  }

  // Проверяем outcome каждой impl-subtask.
  const outcomes = implSubtaskIds.map((id) => ({
    id,
    outcome: state.getFinalVerifyOutcomeForImplSubtask(rootId, id),
  }))

  const pending = outcomes.filter((o) => o.outcome === null)
  const rejected = outcomes.filter((o) => o.outcome === 'rejected')
  const approved = outcomes.filter((o) => o.outcome === 'approved')

  if (pending.length > 0) {
    log.info('feature root not yet ready: subtasks still in progress', {
      approved: approved.length,
      pending: pending.map((o) => o.id),
      rootId,
      total: implSubtaskIds.length,
    })
    return
  }

  if (rejected.length > 0) {
    // final-verify=rejected означает что для этой subtask state machine уже
    // создала новый impl-цикл (см. NODES["final-verify"].onComplete).
    // То что мы тут видим rejected — артефакт того что последняя final-verify
    // была отрицательной, но обработчик уже запустил fix. Ждём.
    log.info('feature root: rejected final-verify(s) — fix cycles in flight', {
      rejected: rejected.map((o) => o.id),
      rootId,
    })
    return
  }

  // Все approved.
  log.info('all impl subtasks approved — closing feature root', {
    rootId,
    subtasks: approved.length,
  })
  await closeRootSuccessfully(rootId)
}

async function closeRootSuccessfully(rootId: string): Promise<void> {
  const meta = state.getTaskMeta(rootId)
  if (!meta) return
  log.info('closing root successfully', { rootId, shortId: meta.short_id })
  await kanban.completeTaskManually(rootId, `Pipeline finished successfully for ${meta.short_id}`, {
    closed_by: 'state-machine',
    final: true,
  })
  state.closeRoot(rootId)
}

async function escalateToHuman(meta: state.TaskMeta, reason: string): Promise<void> {
  log.warn('escalating to human', { reason, root: meta.root_id })
  await kanban.commentOnTask(
    meta.root_id,
    `🚨 State machine escalation: ${reason}\n\n` +
      `Stuck at: node=${meta.node_name}, cycles=${meta.cycle_count}, ` +
      `subtask=${meta.impl_subtask_id || 'n/a'}`
  )
  await kanban.blockTask(meta.root_id, `State machine escalation: ${reason}`)
}

// ────────────────────────────────────────────────────────────
// EVENT LOOP
// ────────────────────────────────────────────────────────────

export async function runEventLoop(intervalMs = 500): Promise<void> {
  log.info('state machine event loop starting', { intervalMs })
  while (true) {
    try {
      const lastSeen = state.getLastSeenEventId()
      const events = state.fetchEventsSince(lastSeen)
      for (const ev of events) {
        await handleEvent(ev)
      }
    } catch (e) {
      log.error('event loop iteration failed', { error: String(e) })
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
}
