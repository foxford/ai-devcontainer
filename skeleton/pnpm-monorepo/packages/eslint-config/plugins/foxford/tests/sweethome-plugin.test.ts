import { describe, expect, it } from 'vitest'

import { foxfordPlugin } from '../index.js'

const EXPECTED_RULES = ['import-types-independently', 'no-react-default-import', 'sort-keys', 'no-restricted-paths']

const DELETED_RULES = [
  'pure-analytics-call',
  'effector-units-leftward-type-declaration',
  'export-type-annotation',
  'pure-url-build',
  'i18n-string-literal-id',
  'import-effector-types-independently',
  'no-cookie',
  'no-published-package-imports',
]

describe('foxfordPlugin', () => {
  it('has the correct plugin name', () => {
    expect(foxfordPlugin.name).toBe('foxford')
  })

  it('exports a rules object', () => {
    expect(foxfordPlugin.rules).toBeDefined()
    expect(typeof foxfordPlugin.rules).toBe('object')
  })

  it('contains exactly the 4 expected rules', () => {
    const ruleNames = Object.keys(foxfordPlugin.rules ?? {})
    expect(ruleNames).toHaveLength(4)
    for (const name of EXPECTED_RULES) {
      expect(ruleNames).toContain(name)
    }
  })

  it('does not contain any deleted rules', () => {
    const ruleNames = Object.keys(foxfordPlugin.rules ?? {})
    for (const name of DELETED_RULES) {
      expect(ruleNames).not.toContain(name)
    }
  })

  describe('rule structure', () => {
    for (const ruleName of EXPECTED_RULES) {
      it(`${ruleName} has a meta object`, () => {
        const rule = foxfordPlugin.rules?.[ruleName]
        expect(rule).toBeDefined()
        expect(rule?.meta).toBeDefined()
      })

      it(`${ruleName} has meta.schema`, () => {
        const rule = foxfordPlugin.rules?.[ruleName]
        expect(rule?.meta?.schema).toBeDefined()
      })

      it(`${ruleName} has meta.messages`, () => {
        const rule = foxfordPlugin.rules?.[ruleName]
        expect(rule?.meta?.messages).toBeDefined()
        expect(typeof rule?.meta?.messages).toBe('object')
      })

      it(`${ruleName} has a create function`, () => {
        const rule = foxfordPlugin.rules?.[ruleName]
        expect(typeof rule?.create).toBe('function')
      })
    }
  })

  describe('flat config compatibility', () => {
    it('plugin can be used in a flat config plugins object', () => {
      const config = {
        plugins: { foxford: foxfordPlugin },
        rules: {
          'foxford/sort-keys': 'error',
        },
      }

      expect(config.plugins.foxford).toBe(foxfordPlugin)
      expect(config.rules['foxford/sort-keys']).toBe('error')
    })
  })
})
