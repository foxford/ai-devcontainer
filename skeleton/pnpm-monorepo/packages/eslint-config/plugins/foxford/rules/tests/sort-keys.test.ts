import { RuleTester } from 'eslint'
import { describe, it } from 'vitest'

import ruleModule from '../sort-keys.js'

import type { Rule } from 'eslint'

const rule: Rule.RuleModule = ruleModule

const ruleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2021,
    sourceType: 'module',
  },
})

describe('sort-keys rule', () => {
  it('passes valid cases and reports invalid cases', () => {
    ruleTester.run('sort-keys', rule, {
      invalid: [
        {
          code: 'const x = { b: 1, a: 2 }',
          errors: [{ messageId: 'sortKeys' }],
          output: 'const x = { a: 2, b: 1 }',
        },
        {
          code: 'const x = { c: 3, a: 1, b: 2 }',
          errors: [{ messageId: 'sortKeys' }],
          output: 'const x = { a: 1, c: 3, b: 2 }',
        },
        {
          code: 'const x = { z: 1, a: 2 }',
          errors: [{ messageId: 'sortKeys' }],
          output: 'const x = { a: 2, z: 1 }',
        },
      ],
      valid: [
        { code: 'const x = { a: 1, b: 2, c: 3 }' },
        { code: 'const x = { a: 1 }' },
        { code: 'const x = {}' },
        // Effector methods are excluded
        { code: 'sample({ source: a, target: b, clock: c })' },
        { code: 'guard({ source: a, target: b, filter: fn })' },
        { code: 'forward({ from: a, to: b })' },
        { code: 'attach({ source: a, effect: fx })' },
        // Patronum methods are excluded
        { code: 'condition({ source: a, if: pred, then: b, else: c })' },
        { code: 'debounce({ source: a, timeout: 300 })' },
        // Destructuring patterns are excluded
        { code: 'const { b, a } = obj' },
        // Single spread
        { code: 'const x = { ...rest, a: 1 }' },
      ],
    })
  })

  it('sorts in desc order when configured', () => {
    const descTester = new RuleTester({
      languageOptions: { ecmaVersion: 2021, sourceType: 'module' },
    })
    descTester.run('sort-keys', rule, {
      invalid: [
        {
          code: 'const x = { a: 1, b: 2 }',
          errors: [{ messageId: 'sortKeys' }],
          options: ['desc'],
          output: 'const x = { b: 2, a: 1 }',
        },
      ],
      valid: [{ code: 'const x = { b: 2, a: 1 }', options: ['desc'] }],
    })
  })

  it('caseSensitive: false treats upper and lower case as equal for ordering', () => {
    const insensitiveTester = new RuleTester({
      languageOptions: { ecmaVersion: 2021, sourceType: 'module' },
    })
    insensitiveTester.run('sort-keys (caseSensitive: false)', rule, {
      invalid: [
        {
          code: 'const x = { b: 2, A: 1 }',
          errors: [{ messageId: 'sortKeys' }],
          options: ['asc', { caseSensitive: false }],
          output: 'const x = { A: 1, b: 2 }',
        },
      ],
      valid: [
        { code: 'const x = { a: 1, B: 2 }', options: ['asc', { caseSensitive: false }] },
        { code: 'const x = { A: 1, b: 2 }', options: ['asc', { caseSensitive: false }] },
      ],
    })
  })

  it('natural: true compares numbers within strings naturally', () => {
    const naturalTester = new RuleTester({
      languageOptions: { ecmaVersion: 2021, sourceType: 'module' },
    })
    naturalTester.run('sort-keys (natural: true)', rule, {
      invalid: [
        {
          code: 'const x = { item10: 1, item9: 2 }',
          errors: [{ messageId: 'sortKeys' }],
          options: ['asc', { natural: true }],
          output: 'const x = { item9: 2, item10: 1 }',
        },
      ],
      valid: [{ code: 'const x = { item2: 1, item10: 2 }', options: ['asc', { natural: true }] }],
    })
  })

  it('falls back to identifier name for computed properties when comparing order', () => {
    // getPropertyName falls back to node.key.name for computed identifiers —
    // the key is compared using the variable's name, not its runtime value.
    ruleTester.run('sort-keys (computed)', rule, {
      invalid: [
        {
          // [z] is compared as 'z', 'z' > 'a' → ordering error
          code: 'const x = { [z]: 1, a: 2 }',
          errors: [{ messageId: 'sortKeys' }],
          output: 'const x = { a: 2, [z]: 1 }',
        },
      ],
      valid: [
        // [a] is compared as 'a', 'a' <= 'b' → no error
        { code: 'const x = { [a]: 1, b: 2 }' },
      ],
    })
  })

  it('supports string literal keys', () => {
    ruleTester.run('sort-keys (string literal keys)', rule, {
      invalid: [
        {
          code: "const x = { 'b': 1, 'a': 2 }",
          errors: [{ messageId: 'sortKeys' }],
          output: "const x = { 'a': 2, 'b': 1 }",
        },
      ],
      valid: [{ code: "const x = { 'a': 1, 'b': 2 }" }],
    })
  })

  it('excludes split effector method', () => {
    // The exclusion only applies to the direct argument object of split().
    // Nested objects (like cases: {}) are still subject to sorting.
    ruleTester.run('sort-keys (split)', rule, {
      invalid: [],
      valid: [{ code: 'split({ source: a, match: fn })' }],
    })
  })

  it('excludes snapshot patronum method', () => {
    ruleTester.run('sort-keys (snapshot)', rule, {
      invalid: [],
      valid: [{ code: 'snapshot({ source: a, clock: b })' }],
    })
  })

  it('excludes or patronum method', () => {
    ruleTester.run('sort-keys (or)', rule, {
      invalid: [],
      valid: [{ code: 'or({ source: a, filter: b })' }],
    })
  })

  it('excludes not patronum method', () => {
    ruleTester.run('sort-keys (not)', rule, {
      invalid: [],
      valid: [{ code: 'not({ source: a, filter: b })' }],
    })
  })
})
