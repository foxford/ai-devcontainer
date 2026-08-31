import { RuleTester } from 'eslint'
import * as tseslint from 'typescript-eslint'
import { describe, it } from 'vitest'

import ruleModule from '../import-types-independently.js'

import type { Rule } from 'eslint'

const rule: Rule.RuleModule = ruleModule

const ruleTester = new RuleTester({
  languageOptions: {
    parser: tseslint.parser,
    sourceType: 'module',
  },
})

describe('import-types-independently rule', () => {
  it('passes valid cases and reports invalid cases', () => {
    ruleTester.run('import-types-independently', rule, {
      invalid: [
        {
          // type specifier mixed with value specifier
          code: "import { foo, type Bar } from './module'",
          errors: [{ messageId: 'separateTypeImport' }],
          output: "import { foo } from './module'\nimport type { Bar } from './module'",
        },
        {
          // multiple types mixed with values
          code: "import { foo, type Bar, type Baz } from './module'",
          errors: [{ messageId: 'separateTypeImport' }],
          output: "import { foo } from './module'\nimport type { Bar, Baz } from './module'",
        },
        {
          // default import + type specifier
          code: "import Foo, { type Bar } from './module'",
          errors: [{ messageId: 'separateTypeImport' }],
          output: "import Foo from './module'\nimport type { Bar } from './module'",
        },
      ],
      valid: [
        // Pure type-only import is fine
        { code: "import type { Foo } from './module'" },
        // Pure value import is fine
        { code: "import { foo, bar } from './module'" },
        // Default import is fine
        { code: "import Foo from './module'" },
        // Mixed default + named values (no types) is fine
        { code: "import Foo, { bar } from './module'" },
        // Namespace import is fine
        { code: "import * as Mod from './module'" },
      ],
    })
  })

  it('converts all-inline-type specifiers to a top-level type import', () => {
    ruleTester.run('import-types-independently (all inline types)', rule, {
      invalid: [
        {
          // All specifiers are type — convert the whole import to `import type { ... }`
          code: "import { type Foo, type Bar } from './module'",
          errors: [{ messageId: 'separateTypeImport' }],
          output: "import type { Foo, Bar } from './module'",
        },
      ],
      valid: [],
    })
  })

  it('handles aliased type specifiers in the autofix', () => {
    ruleTester.run('import-types-independently (aliased type specifier)', rule, {
      invalid: [
        {
          code: "import { foo, type Bar as B } from './module'",
          errors: [{ messageId: 'separateTypeImport' }],
          output: "import { foo } from './module'\nimport type { Bar as B } from './module'",
        },
      ],
      valid: [],
    })
  })
})
