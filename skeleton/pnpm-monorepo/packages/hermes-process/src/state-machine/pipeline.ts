/* eslint-disable max-lines -- декларативное описание графа процесса целиком в одном файле */
/**
 * pipeline.ts — единственный источник правды процесса.
 *
 * Чтобы изменить процесс — меняй ЭТОТ файл и ничего больше. Все остальные
 * модули (machine.ts, handlers, validators) — машинерия, которая исполняет
 * то, что объявлено здесь.
 *
 * Структура:
 *   1. Константы (лимиты циклов, префикс id, пути)
 *   2. Шаблоны (templates) — feature, doc-only, bugfix
 *   3. Граф feature pipeline (это самая сложная часть)
 *   4. Реестр ролей + их toolsets
 */

// ============================================================
// 1. CONSTANTS
// ============================================================

/** Префикс читаемого id задачи. DEV-1, DEV-2, … */
export const TASK_PREFIX = 'DEV'

/** dev ↔ tech-lead "changes requested" — сколько повторов до эскалации к человеку */
export const MAX_REVIEW_CYCLES = 3

/** dev ↔ qa красные тесты — сколько повторов до эскалации */
export const MAX_QA_FIX_CYCLES = 3

/** dev ↔ security findings — сколько повторов до эскалации */
export const MAX_SECURITY_FIX_CYCLES = 2

/** tech-lead final-verify "rejected" — сколько повторов dev-fix до эскалации */
export const MAX_FINAL_VERIFY_REJECTS = 2

/** Относительный путь worktree от корня репо. Один на root-фичу. */
export const WORKTREE_BASE = '.hermes/worktrees'

/** Имя ветки от feature-slug. */
export const branchFor = (slug: string) => `feature/${slug}`

/** Имя worktree-директории от feature-slug. */
export const worktreePath = (slug: string) => `${WORKTREE_BASE}/${slug}`

// ============================================================
// 2. TEMPLATES
// ============================================================

/**
 * Шаблон задачи — определяет какой именно pipeline исполняется для root-таски.
 * Team-lead (или slash-команда) указывает template при создании root-таски.
 */
export type Template = 'feature' | 'doc-only' | 'bugfix'

/** Описание шаблонов — что куда роутится сразу после создания root. */
export const TEMPLATES: Record<Template, TemplateSpec> = {
  bugfix: {
    description: 'Лёгкий путь: dev → review → qa → done. Без plan/decompose/security/docs.',
    firstStep: 'bugfix-impl',
    needsWorktree: true,
  },
  'doc-only': {
    description: 'Только документирование. Без dev, без security, лёгкий review.',
    firstStep: 'doc-only-impl',
    needsWorktree: false,
  },
  feature: {
    description: 'Полный pipeline: plan → decompose → impl × N → qa → (sec?) → (docs?) → final verify',
    firstStep: 'plan',
    needsWorktree: true,
  },
}

export interface TemplateSpec {
  description: string
  /** Имя первого шага в графе (см. NODES ниже). */
  firstStep: NodeName
  /** Создавать ли worktree на старте root-таски. */
  needsWorktree: boolean
}

// ============================================================
// 3. ROLES
// ============================================================

export type Role = 'team-lead' | 'tech-lead' | 'developer' | 'qa' | 'documenter' | 'security-auditor'

/** Все роли + что им разрешено делать с kanban-toolset. */
export const ROLES: Record<Role, RoleSpec> = {
  developer: {
    canCreateScope: 'none',
    canCreateTasks: false,
    toolsets: ['terminal', 'file'],
  },
  documenter: {
    canCreateScope: 'none',
    canCreateTasks: false,
    toolsets: ['terminal', 'file'],
  },
  qa: {
    canCreateScope: 'none',
    canCreateTasks: false,
    toolsets: ['terminal', 'file'],
  },
  'security-auditor': {
    canCreateScope: 'none',
    canCreateTasks: false,
    toolsets: ['terminal', 'file'], // read-only по сути; file даёт чтение
  },
  'team-lead': {
    // ЕДИНСТВЕННАЯ роль с правом kanban_create
    canCreateScope: 'decomposition-only',
    canCreateTasks: true, // только impl-subtasks под своим root
    toolsets: ['terminal', 'file', 'kanban'],
  },
  'tech-lead': {
    canCreateScope: 'none',
    canCreateTasks: false,
    toolsets: ['terminal', 'file'],
  },
}

export interface RoleSpec {
  canCreateTasks: boolean
  canCreateScope: 'decomposition-only' | 'none'
  toolsets: string[]
}

// ============================================================
// 4. PIPELINE GRAPH (feature template)
// ============================================================

/**
 * Узел графа = одна задача в kanban. Когда задача завершается, state machine
 * читает её handoff metadata, валидирует, и вызывает onComplete для решения
 * "что дальше".
 *
 * NodeName включает узлы всех templates — feature, doc-only, bugfix.
 */
export type NodeName =
  // feature
  | 'plan'
  | 'decompose'
  | 'impl'
  | 'review'
  | 'qa'
  | 'route-post-qa'
  | 'security'
  | 'docs'
  | 'final-verify'
  // doc-only
  | 'doc-only-impl'
  | 'doc-only-verify'
  // bugfix
  | 'bugfix-impl'
  | 'bugfix-review'
  | 'bugfix-qa'

/**
 * onComplete возвращает массив следующих задач для создания, или null если
 * это конечный узел (тогда state machine закрывает root).
 *
 * "Создать задачу" — это NextTask spec. State machine материализует его в kanban_create.
 */
export interface NextTask {
  node: NodeName
  assignee: Role
  /** Родители (для dependency resolution). Если пусто — state machine подставит текущую задачу. */
  parents?: string[]
  /** Дополнительная metadata, которая попадает в task body. */
  metadata?: Record<string, unknown>
  /** Override title. По умолчанию — на основе node name. */
  title?: string
}

export interface NodeSpec {
  assignee: Role
  /** Какие skills принудительно подгружаются на task (помимо профиля). */
  pinnedSkills?: string[]
  /**
   * Что делать при kanban_complete этого узла. Возвращает следующие задачи.
   *
   * ctx содержит: handoff metadata, root-info, попытки, и весь нужный контекст.
   */
  onComplete: (ctx: CompletionContext) => NextTask[] | 'root-done'
  /**
   * Что делать при kanban_block. По умолчанию — пропагировать на root и звать человека.
   * Можно переопределить — например для security findings, чтобы создать fix-цикл.
   */
  onBlock?: (ctx: BlockContext) => NextTask[] | 'escalate'
}

export interface CompletionContext {
  rootId: string
  rootTitle: string
  featureSlug: string
  template: Template
  worktreePath: string | null
  /** handoff metadata из kanban_complete */
  metadata: Record<string, unknown>
  /** human-readable id вроде DEV-7 */
  taskShortId: string
  /** счётчик циклов: текущий узел встречался N раз для этой root */
  cycleCount: number
  /** ссылка на impl-subtask, к которой этот шаг относится (если применимо) */
  implSubtaskId?: string
}

export interface BlockContext extends CompletionContext {
  reason: string
}

/**
 * NODES — главная таблица. Каждый узел знает, что делать после завершения.
 *
 * Читай этот блок сверху вниз, чтобы понять процесс. Если хочешь поменять
 * процесс — меняй здесь.
 */
export const NODES: Record<NodeName, NodeSpec> = {
  // ────────────────────────────────────────────────────────────
  // BUGFIX TEMPLATE
  // ────────────────────────────────────────────────────────────
  'bugfix-impl': {
    assignee: 'developer',
    onComplete: (_ctx) => [{ assignee: 'tech-lead', node: 'bugfix-review' }],
  },

  'bugfix-qa': {
    assignee: 'qa',
    onComplete: (ctx) => {
      const outcome = ctx.metadata.outcome as string
      if (outcome === 'green') return 'root-done'
      if (ctx.cycleCount >= MAX_QA_FIX_CYCLES) return []
      return [{ assignee: 'developer', node: 'bugfix-impl' }]
    },
  },

  'bugfix-review': {
    assignee: 'tech-lead',
    onComplete: (ctx) => {
      const outcome = ctx.metadata.outcome as string
      if (outcome === 'approved') {
        return [{ assignee: 'qa', node: 'bugfix-qa' }]
      }
      if (ctx.cycleCount >= MAX_REVIEW_CYCLES) return []
      return [{ assignee: 'developer', node: 'bugfix-impl' }]
    },
  },

  /**
   * Team-lead читает план (из handoff родителя) и создаёт N impl-subtasks
   * через kanban_create. State machine принимает созданные subtasks как
   * начало parallel impl веток.
   *
   * Subtasks team-lead создаёт сам — это единственная роль с canCreateTasks.
   * После завершения decompose, state machine читает metadata.subtasks и
   * "усыновляет" их в pipeline (каждая → ветка impl→review→qa→…).
   */
  decompose: {
    assignee: 'team-lead',
    onComplete: (ctx) => {
      const subtasks = (ctx.metadata.subtasks as string[]) || []
      // State machine увидит "decompose-done" и для каждой созданной
      // team-lead-ом subtask запустит pipeline через onDecomposed (см. machine.ts).
      // Здесь возвращаем пустой массив — материализация идёт отдельным путём.
      if (subtasks.length === 0) {
        // Защита: декомпозиция должна породить хотя бы одну impl-subtask.
        // Если 0 — поднимаем флаг через onBlock.
        return []
      }
      return [] // фактическая раскрутка в machine.ts:onDecomposeComplete
    },
  },

  // ────────────────────────────────────────────────────────────
  // DOC-ONLY TEMPLATE
  // ────────────────────────────────────────────────────────────
  'doc-only-impl': {
    assignee: 'documenter',
    onComplete: (_ctx) => [{ assignee: 'tech-lead', node: 'doc-only-verify' }],
  },

  'doc-only-verify': {
    assignee: 'tech-lead',
    onComplete: (ctx) => {
      const outcome = ctx.metadata.outcome as string
      if (outcome === 'approved') return 'root-done'
      if (ctx.cycleCount >= MAX_FINAL_VERIFY_REJECTS) return []
      return [{ assignee: 'documenter', metadata: { fix_reason: 'doc-only-verify-rejected' }, node: 'doc-only-impl' }]
    },
  },

  /** Documenter обновляет доки. После — на final-verify. */
  docs: {
    assignee: 'documenter',
    onComplete: (_ctx) => [{ assignee: 'tech-lead', node: 'final-verify' }],
  },

  /**
   * Финальный verify tech-lead-ом. Если approved — subtask закрыт, machine.ts
   * чекает все ли subtasks root-а готовы; если да — закрывает root.
   * Если rejected — обратно в impl (loop до MAX_FINAL_VERIFY_REJECTS).
   */
  'final-verify': {
    assignee: 'tech-lead',
    onComplete: (ctx) => {
      const outcome = ctx.metadata.outcome as string
      if (outcome === 'approved') {
        return 'root-done'
      }
      // rejected
      if (ctx.cycleCount >= MAX_FINAL_VERIFY_REJECTS) {
        return []
      }
      return [{ assignee: 'developer', metadata: { fix_reason: 'final-verify-rejected' }, node: 'impl' }]
    },
  },

  /**
   * Developer пишет код + unit/component тесты в worktree, коммитит на ветку.
   * Не пушит. После handoff — на review к tech-lead.
   */
  impl: {
    assignee: 'developer',
    onComplete: (_ctx) => [{ assignee: 'tech-lead', node: 'review' }],
  },

  // ────────────────────────────────────────────────────────────
  // FEATURE TEMPLATE
  // ────────────────────────────────────────────────────────────
  /** Tech-lead пишет план. На основе плана team-lead будет декомпозировать. */
  plan: {
    assignee: 'tech-lead',
    onComplete: (_ctx) => [{ assignee: 'team-lead', node: 'decompose' }],
  },

  /**
   * QA пишет integration/e2e против AC и прогоняет. metadata.outcome:
   *   - "green" → tech-lead routing (нужен security/docs?)
   *   - "red" → обратно dev (loop, лимит MAX_QA_FIX_CYCLES)
   */
  qa: {
    assignee: 'qa',
    onComplete: (ctx) => {
      const outcome = ctx.metadata.outcome as string
      if (outcome === 'green') {
        return [{ assignee: 'tech-lead', node: 'route-post-qa' }]
      }
      if (ctx.cycleCount >= MAX_QA_FIX_CYCLES) {
        return []
      }
      return [{ assignee: 'developer', node: 'impl' }]
    },
  },

  /**
   * Tech-lead делает code review. Возможные исходы (в metadata.outcome):
   *   - "approved" → qa
   *   - "changes-requested" → обратно к dev (loop, лимит MAX_REVIEW_CYCLES)
   */
  review: {
    assignee: 'tech-lead',
    onComplete: (ctx) => {
      const outcome = ctx.metadata.outcome as string
      if (outcome === 'approved') {
        return [{ assignee: 'qa', node: 'qa' }]
      }
      // changes-requested
      if (ctx.cycleCount >= MAX_REVIEW_CYCLES) {
        // эскалация — обрабатывается в onBlock сценарии в machine.ts
        return []
      }
      return [{ assignee: 'developer', node: 'impl' }]
    },
  },

  /**
   * Tech-lead решает что дальше:
   *   - metadata.needs_security: bool
   *   - metadata.doc_impact: bool
   * Создаёт нужные ветки. Если оба false — сразу final-verify.
   */
  'route-post-qa': {
    assignee: 'tech-lead',
    onComplete: (ctx) => {
      const needsSecurity = ctx.metadata.needs_security === true
      const docImpact = ctx.metadata.doc_impact === true
      const next: NextTask[] = []
      if (needsSecurity) next.push({ assignee: 'security-auditor', node: 'security' })
      if (docImpact) next.push({ assignee: 'documenter', node: 'docs' })
      if (next.length === 0) {
        next.push({ assignee: 'tech-lead', node: 'final-verify' })
      }
      return next
    },
  },

  /**
   * Security audit. metadata.outcome:
   *   - "clean" → docs (если нужны) или final-verify
   *   - "findings" → dev fix (loop, лимит MAX_SECURITY_FIX_CYCLES)
   */
  security: {
    assignee: 'security-auditor',
    onComplete: (ctx) => {
      const outcome = ctx.metadata.outcome as string
      if (outcome === 'clean') {
        // дальше зависит от того, был ли doc_impact на route-post-qa
        // эту инфу machine.ts хранит в state — но проще: всегда → final-verify
        // (если docs ещё нужны, route-post-qa их уже запустил параллельно,
        //  они являются parents для final-verify через kanban_link)
        return [{ assignee: 'tech-lead', node: 'final-verify' }]
      }
      if (ctx.cycleCount >= MAX_SECURITY_FIX_CYCLES) {
        return []
      }
      // findings → dev fix → security re-audit
      return [{ assignee: 'developer', metadata: { fix_reason: 'security-findings' }, node: 'impl' }]
    },
  },
}

// ============================================================
// 5. HANDOFF METADATA SCHEMAS
// ============================================================
//
// Что КАЖДАЯ роль обязана положить в metadata при kanban_complete.
// Это валидируется в pre_tool_call hook — если не сходится, kanban_complete
// блокируется и worker должен переделать.
//
// Schema-ы импортируются из ./schemas/index.ts (Zod), сюда — только маппинг.

export { NODE_SCHEMAS } from './schemas/index.ts'
