/* eslint-disable max-lines -- весь слой доступа к sqlite (kanban.db + state.db) в одном модуле */
/**
 * Прямой доступ к sqlite:
 *   - kanban.db   — ТОЛЬКО ЧТЕНИЕ task_events и tasks. Hermes пишет туда сам.
 *   - state.db    — НАШ sqlite. Counter-ы, last_seen_event_id, idempotency.
 *
 * Запись в kanban.db делает CLI (см. kanban.ts), чтобы не ловить race с dispatcher.
 */
import { mkdirSync, existsSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

// eslint-disable-next-line import-x/no-named-as-default -- default export better-sqlite3 и есть конструктор Database
import Database from 'better-sqlite3'

import { log } from './log.ts'

const BOARD = process.env.HERMES_BOARD || 'default'

// ────────────────────────────────────────────────────────────
// kanban.db (read-only)
// ────────────────────────────────────────────────────────────

function kanbanDbPath(): string {
  if (BOARD === 'default') {
    return join(homedir(), '.hermes', 'kanban.db')
  }
  return join(homedir(), '.hermes', 'kanban', 'boards', BOARD, 'kanban.db')
}

let kanbanDb: Database.Database | null = null

function getKanbanDb(): Database.Database {
  if (!kanbanDb) {
    const path = kanbanDbPath()
    if (!existsSync(path)) {
      throw new Error(`kanban.db not found at ${path}. Run 'hermes kanban init' first.`)
    }
    kanbanDb = new Database(path, { fileMustExist: true, readonly: true })
    kanbanDb.pragma('journal_mode = WAL')
  }
  return kanbanDb
}

export interface TaskEvent {
  id: number
  task_id: string
  kind: string
  payload: Record<string, unknown>
  run_id: string | null
  created_at: string
}

export function fetchEventsSince(lastSeenId: number, limit = 100): TaskEvent[] {
  const db = getKanbanDb()
  const rows = db
    .prepare(
      `
    SELECT id, task_id, kind, payload, run_id, created_at
    FROM task_events
    WHERE id > ?
    ORDER BY id ASC
    LIMIT ?
  `
    )
    .all(lastSeenId, limit) as Array<{
    id: number
    task_id: string
    kind: string
    payload: string | null
    run_id: string | null
    created_at: string
  }>

  return rows.map((r) => ({
    created_at: r.created_at,
    id: r.id,
    kind: r.kind,
    payload: r.payload ? JSON.parse(r.payload) : {},
    run_id: r.run_id,
    task_id: r.task_id,
  }))
}

export interface TaskRow {
  id: string
  title: string
  body: string
  assignee: string | null
  status: string
  tenant: string | null
  workspace: string | null
  created_at: string
}

export function fetchTask(id: string): TaskRow | null {
  const db = getKanbanDb()
  // Имена колонок в реальной схеме Hermes могут отличаться (workspace_spec и т.п.).
  // Если у тебя в БД они другие — поправь этот запрос. Это единственное место
  // где state machine знает SQL-схему Hermes.
  const row = db
    .prepare(
      `
    SELECT id, title, body, assignee, status,
           tenant, workspace_spec AS workspace, created_at
    FROM tasks WHERE id = ?
  `
    )
    .get(id) as TaskRow | undefined
  return row ?? null
}

export function fetchTaskParents(id: string): string[] {
  const db = getKanbanDb()
  const rows = db
    .prepare(
      `
    SELECT parent_id FROM task_links WHERE child_id = ?
  `
    )
    .all(id) as Array<{ parent_id: string }>
  return rows.map((r) => r.parent_id)
}

export function fetchTaskChildren(id: string): string[] {
  const db = getKanbanDb()
  const rows = db
    .prepare(
      `
    SELECT child_id FROM task_links WHERE parent_id = ?
  `
    )
    .all(id) as Array<{ child_id: string }>
  return rows.map((r) => r.child_id)
}

/** Получить последний run-row для таска (с handoff metadata от worker'а). */
export interface RunRow {
  id: string
  task_id: string
  outcome: string | null
  summary: string | null
  metadata: Record<string, unknown>
  started_at: string
  ended_at: string | null
}

export function fetchLatestRun(taskId: string): RunRow | null {
  const db = getKanbanDb()
  const row = db
    .prepare(
      `
    SELECT id, task_id, outcome, summary, metadata, started_at, ended_at
    FROM task_runs
    WHERE task_id = ?
    ORDER BY started_at DESC
    LIMIT 1
  `
    )
    .get(taskId) as
    | {
        id: string
        task_id: string
        outcome: string | null
        summary: string | null
        metadata: string | null
        started_at: string
        ended_at: string | null
      }
    | undefined
  if (!row) return null
  return {
    ...row,
    metadata: row.metadata ? JSON.parse(row.metadata) : {},
  }
}

// ────────────────────────────────────────────────────────────
// state.db (наш sqlite — read-write)
// ────────────────────────────────────────────────────────────

function stateDbPath(): string {
  const projectSlug = process.env.HERMES_PROJECT_SLUG || 'default-project'
  const dir = join(homedir(), '.hermes', 'state-machine')
  mkdirSync(dir, { recursive: true })
  return join(dir, `${projectSlug}.db`)
}

let stateDb: Database.Database | null = null

function getStateDb(): Database.Database {
  if (!stateDb) {
    stateDb = new Database(stateDbPath())
    stateDb.pragma('journal_mode = WAL')
    stateDb.exec(`
      CREATE TABLE IF NOT EXISTS kv (
        key TEXT PRIMARY KEY,
        value TEXT NOT NULL
      );
      CREATE TABLE IF NOT EXISTS task_meta (
        task_id TEXT PRIMARY KEY,
        root_id TEXT NOT NULL,
        node_name TEXT NOT NULL,
        short_id TEXT NOT NULL,
        feature_slug TEXT NOT NULL,
        template TEXT NOT NULL,
        cycle_count INTEGER NOT NULL DEFAULT 0,
        impl_subtask_id TEXT,
        created_at TEXT NOT NULL DEFAULT (datetime('now'))
      );
      CREATE INDEX IF NOT EXISTS task_meta_root ON task_meta(root_id);
      CREATE TABLE IF NOT EXISTS roots (
        root_id TEXT PRIMARY KEY,
        short_id TEXT NOT NULL,
        feature_slug TEXT NOT NULL,
        template TEXT NOT NULL,
        worktree_path TEXT,
        created_at TEXT NOT NULL DEFAULT (datetime('now')),
        closed_at TEXT
      );
      CREATE TABLE IF NOT EXISTS processed_events (
        event_id INTEGER PRIMARY KEY
      );
    `)
    log.info('state.db ready', { path: stateDbPath() })
  }
  return stateDb
}

// ────────────────────────────────────────────────────────────
// State helpers
// ────────────────────────────────────────────────────────────

export function getKv(key: string): string | null {
  const row = getStateDb().prepare('SELECT value FROM kv WHERE key = ?').get(key) as { value: string } | undefined
  return row?.value ?? null
}

export function setKv(key: string, value: string): void {
  getStateDb().prepare('INSERT OR REPLACE INTO kv (key, value) VALUES (?, ?)').run(key, value)
}

export function getLastSeenEventId(): number {
  return Number(getKv('last_seen_event_id') ?? 0)
}

export function setLastSeenEventId(id: number): void {
  setKv('last_seen_event_id', String(id))
}

/** Атомарно: получить следующий human-readable id (DEV-1, DEV-2, …). */
export function nextShortId(prefix: string): string {
  const cur = Number(getKv(`counter:${prefix}`) ?? 0)
  const next = cur + 1
  setKv(`counter:${prefix}`, String(next))
  return `${prefix}-${next}`
}

export interface TaskMeta {
  task_id: string
  root_id: string
  node_name: string
  short_id: string
  feature_slug: string
  template: string
  cycle_count: number
  impl_subtask_id: string | null
}

export function saveTaskMeta(meta: TaskMeta): void {
  getStateDb()
    .prepare(
      `
    INSERT OR REPLACE INTO task_meta
      (task_id, root_id, node_name, short_id, feature_slug, template, cycle_count, impl_subtask_id)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  `
    )
    .run(
      meta.task_id,
      meta.root_id,
      meta.node_name,
      meta.short_id,
      meta.feature_slug,
      meta.template,
      meta.cycle_count,
      meta.impl_subtask_id
    )
}

export function getTaskMeta(taskId: string): TaskMeta | null {
  const row = getStateDb().prepare('SELECT * FROM task_meta WHERE task_id = ?').get(taskId) as TaskMeta | undefined
  return row ?? null
}

export function countCyclesForRoot(rootId: string, node: string, implSubtaskId: string | null): number {
  const row = getStateDb()
    .prepare(
      `
    SELECT COUNT(*) AS n FROM task_meta
    WHERE root_id = ? AND node_name = ?
    AND ((? IS NULL AND impl_subtask_id IS NULL) OR impl_subtask_id = ?)
  `
    )
    .get(rootId, node, implSubtaskId, implSubtaskId) as { n: number }
  return row.n
}

export interface RootRecord {
  root_id: string
  short_id: string
  feature_slug: string
  template: string
  worktree_path: string | null
}

export function saveRoot(root: RootRecord): void {
  getStateDb()
    .prepare(
      `
    INSERT OR REPLACE INTO roots (root_id, short_id, feature_slug, template, worktree_path)
    VALUES (?, ?, ?, ?, ?)
  `
    )
    .run(root.root_id, root.short_id, root.feature_slug, root.template, root.worktree_path)
}

export function getRoot(rootId: string): RootRecord | null {
  const row = getStateDb()
    .prepare('SELECT root_id, short_id, feature_slug, template, worktree_path FROM roots WHERE root_id = ?')
    .get(rootId) as RootRecord | undefined
  return row ?? null
}

export function closeRoot(rootId: string): void {
  getStateDb().prepare("UPDATE roots SET closed_at = datetime('now') WHERE root_id = ?").run(rootId)
}

/**
 * Список impl-subtask id, прицепленных к root (feature template).
 */
export function getImplSubtasksForRoot(rootId: string): string[] {
  const rows = getStateDb()
    .prepare(
      `
    SELECT DISTINCT impl_subtask_id FROM task_meta
    WHERE root_id = ? AND impl_subtask_id IS NOT NULL
  `
    )
    .all(rootId) as Array<{ impl_subtask_id: string }>
  return rows.map((r) => r.impl_subtask_id)
}

/**
 * Найти outcome последнего final-verify (или doc-only-verify) для impl-subtask.
 */
export function getFinalVerifyOutcomeForImplSubtask(
  rootId: string,
  implSubtaskId: string
): 'approved' | 'rejected' | null {
  const rows = getStateDb()
    .prepare(
      `
    SELECT task_id, created_at FROM task_meta
    WHERE root_id = ?
      AND impl_subtask_id = ?
      AND node_name IN ('final-verify', 'doc-only-verify', 'bugfix-qa')
    ORDER BY created_at DESC
    LIMIT 1
  `
    )
    .all(rootId, implSubtaskId) as Array<{ task_id: string; created_at: string }>

  const first = rows[0]
  if (!first) return null

  const latestVerifyTaskId = first.task_id
  const run = fetchLatestRun(latestVerifyTaskId)
  if (!run) return null

  const outcome = run.metadata?.outcome as string | undefined
  if (outcome === 'approved' || outcome === 'green') return 'approved'
  if (outcome === 'rejected' || outcome === 'red') return 'rejected'
  return null
}

/** Was event already processed? Idempotency. */
export function isEventProcessed(eventId: number): boolean {
  const row = getStateDb().prepare('SELECT 1 FROM processed_events WHERE event_id = ?').get(eventId)
  return row !== undefined
}

export function markEventProcessed(eventId: number): void {
  getStateDb().prepare('INSERT OR IGNORE INTO processed_events (event_id) VALUES (?)').run(eventId)
}
