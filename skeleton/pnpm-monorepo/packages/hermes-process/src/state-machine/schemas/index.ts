/**
 * Schemas для валидации handoff metadata в kanban_complete.
 *
 * Хук pre_tool_call перехватывает kanban_complete, извлекает metadata,
 * валидирует по схеме узла (определяется по metadata.node, которое
 * подставляет state machine при kanban_create). Если не сходится —
 * блокирует tool call и worker переделывает.
 *
 * Это и есть «контракт» для каждой роли.
 */
import { z } from 'zod'

import type { NodeName } from '../pipeline.ts'

const NonEmpty = z.string().min(1)
const ShaSchema = z.string().regex(/^[0-9a-f]{7,40}$/, 'must be a git SHA')

// ────────────────────────────────────────────────────────────
// FEATURE TEMPLATE
// ────────────────────────────────────────────────────────────

/** Tech-lead закончил план. */
const planSchema = z.object({
  acceptance_criteria: z.array(NonEmpty).min(1),
  estimated_subtasks: z.number().int().min(1),
  plan_doc_path: NonEmpty.describe('relative path to plan doc inside worktree'),
  technical_approach: NonEmpty,
})

/** Team-lead декомпозировал. Должен вернуть id созданных impl-subtasks. */
const decomposeSchema = z.object({
  rationale: NonEmpty.describe('brief justification of split'),
  subtasks: z.array(NonEmpty).min(1).describe('hermes task ids of created impl-subtasks (t_xxx)'),
})

/** Developer закончил импл (или fix-цикл). */
const implSchema = z.object({
  branch: NonEmpty,
  changed_files: z.array(NonEmpty).min(1),
  commit_sha: ShaSchema,
  quality_gates: z.object({
    lint: z.literal('green'),
    tests: z.literal('green'),
    typecheck: z.literal('green'),
  }),
  summary: NonEmpty,
  tests_added: z.array(NonEmpty),
})

/** Tech-lead закончил review. */
const reviewSchema = z.discriminatedUnion('outcome', [
  z.object({
    outcome: z.literal('approved'),
    review_notes: z.string(),
  }),
  z.object({
    findings: z
      .array(
        z.object({
          description: NonEmpty,
          file: z.string().optional(),
          severity: z.enum(['nit', 'minor', 'major', 'blocker']),
        })
      )
      .min(1),
    outcome: z.literal('changes-requested'),
  }),
])

/** QA закончил тесты. */
const qaSchema = z.discriminatedUnion('outcome', [
  z.object({
    coverage_notes: z.string().optional(),
    outcome: z.literal('green'),
    test_run_command: NonEmpty,
    tests_added: z.array(NonEmpty).min(1).describe('e2e/integration test files'),
  }),
  z.object({
    failure_log_path: z.string().optional(),
    failures: z
      .array(
        z.object({
          summary: NonEmpty,
          test_name: NonEmpty,
        })
      )
      .min(1),
    outcome: z.literal('red'),
  }),
])

/** Tech-lead решил что дальше (security/docs?). */
const routePostQaSchema = z.object({
  doc_impact: z.boolean(),
  needs_security: z.boolean(),
  rationale: NonEmpty,
})

/** Security audit. */
const securitySchema = z.discriminatedUnion('outcome', [
  z.object({
    audit_notes: z.string(),
    outcome: z.literal('clean'),
    owasp_categories_checked: z.array(NonEmpty),
  }),
  z.object({
    findings: z
      .array(
        z.object({
          category: NonEmpty.describe('e.g. injection, auth, secrets'),
          description: NonEmpty,
          severity: z.enum(['low', 'medium', 'high', 'critical']),
        })
      )
      .min(1),
    outcome: z.literal('findings'),
  }),
])

/** Documenter — обновил доки. */
const docsSchema = z.object({
  changed_files: z.array(NonEmpty).min(1),
  commit_sha: ShaSchema,
  summary: NonEmpty,
})

/** Финальный verify. */
const finalVerifySchema = z.discriminatedUnion('outcome', [
  z.object({
    notes: z.string(),
    outcome: z.literal('approved'),
  }),
  z.object({
    outcome: z.literal('rejected'),
    reason: NonEmpty,
  }),
])

// ────────────────────────────────────────────────────────────
// DOC-ONLY TEMPLATE
// ────────────────────────────────────────────────────────────

const docOnlyImplSchema = z.object({
  changed_files: z.array(NonEmpty).min(1),
  commit_sha: ShaSchema,
  summary: NonEmpty,
})

const docOnlyVerifySchema = finalVerifySchema

// ────────────────────────────────────────────────────────────
// BUGFIX TEMPLATE
// ────────────────────────────────────────────────────────────

const bugfixImplSchema = implSchema
const bugfixReviewSchema = reviewSchema
const bugfixQaSchema = qaSchema

// ────────────────────────────────────────────────────────────
// REGISTRY
// ────────────────────────────────────────────────────────────

export const NODE_SCHEMAS: Record<NodeName, z.ZodTypeAny> = {
  'bugfix-impl': bugfixImplSchema,
  'bugfix-qa': bugfixQaSchema,
  'bugfix-review': bugfixReviewSchema,
  decompose: decomposeSchema,
  'doc-only-impl': docOnlyImplSchema,
  'doc-only-verify': docOnlyVerifySchema,
  docs: docsSchema,
  'final-verify': finalVerifySchema,
  impl: implSchema,
  plan: planSchema,
  qa: qaSchema,
  review: reviewSchema,
  'route-post-qa': routePostQaSchema,
  security: securitySchema,
}

/** Удобно для отладки: показать человеку, что узел ожидает. */
export function describeSchema(node: NodeName): string {
  // Zod не даёт чистый JSON-Schema без зависимостей; для hooks достаточно
  // вернуть текстовое описание из .description() полей. Здесь — заглушка.
  return `Schema for node "${node}" — see src/schemas/index.ts`
}
