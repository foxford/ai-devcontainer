import { RuleTester } from 'eslint'
import * as tseslint from 'typescript-eslint'
import { describe, it } from 'vitest'

import ruleModule from '../no-react-default-import.js'

import type { Rule } from 'eslint'

const rule: Rule.RuleModule = ruleModule

const ruleTester = new RuleTester({
  languageOptions: {
    parser: tseslint.parser,
    parserOptions: { ecmaFeatures: { jsx: true } },
    sourceType: 'module',
  },
})

describe('no-react-default-import rule', () => {
  it('passes valid cases and reports invalid cases', () => {
    ruleTester.run('no-react-default-import', rule, {
      invalid: [
        {
          code: "import React from 'react'",
          errors: [{ messageId: 'noDefaultImport' }],
          output: "import * as React from 'react'",
        },
        {
          // Mixing default + named — no autofix
          code: "import React, { useState } from 'react'",
          errors: [{ messageId: 'noDefaultImportMixed' }],
          output: null,
        },
      ],
      valid: [
        { code: "import * as React from 'react'" },
        { code: "import { useState } from 'react'" },
        // Non-react package default import is fine
        { code: "import ReactDOM from 'react-dom'" },
        // Type-only import is not a value import — must not trigger the rule
        { code: "import type React from 'react'" },
        // Named-only imports from react are fine
        { code: "import { createElement, Fragment } from 'react'" },
      ],
    })
  })
})
