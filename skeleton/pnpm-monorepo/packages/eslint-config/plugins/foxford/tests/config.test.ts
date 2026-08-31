import { describe, expect, it } from 'vitest'

import { createFoxfordPluginConfig } from '../config.js'
import { foxfordPlugin } from '../index.js'

const COMMON_RULES = ['foxford/import-types-independently', 'foxford/no-restricted-paths', 'foxford/sort-keys']

describe('createFoxfordPluginConfig', () => {
  describe('structure', () => {
    it('returns an object with name foxford/plugin', () => {
      const config = createFoxfordPluginConfig()
      expect(config.name).toBe('foxford/plugin')
    })

    it('includes foxford plugin in plugins', () => {
      const config = createFoxfordPluginConfig()
      expect(config.plugins).toHaveProperty('foxford')
      expect(config.plugins!['foxford']).toBe(foxfordPlugin)
    })

    it('returns a rules object', () => {
      const config = createFoxfordPluginConfig()
      expect(config.rules).toBeDefined()
      expect(typeof config.rules).toBe('object')
    })
  })

  describe('without options (default)', () => {
    it('includes all 3 common rules', () => {
      const config = createFoxfordPluginConfig()
      const rules = config.rules
      for (const rule of COMMON_RULES) {
        expect(rules).toHaveProperty(rule)
      }
    })

    it('does NOT include foxford/no-react-default-import', () => {
      const config = createFoxfordPluginConfig()
      const rules = config.rules
      expect(rules).not.toHaveProperty('foxford/no-react-default-import')
    })

    it('all common rules (except sort-keys) are set to error', () => {
      const config = createFoxfordPluginConfig()
      const rules = config.rules!
      const errorRules = COMMON_RULES.filter((r) => r !== 'foxford/sort-keys')
      for (const rule of errorRules) {
        expect(rules[rule]).toBe('error')
      }
    })

    it('sort-keys is warn with asc + caseSensitive + natural options', () => {
      const config = createFoxfordPluginConfig()
      const rules = config.rules!
      const sortKeysRule = rules['foxford/sort-keys'] as unknown[]
      expect(sortKeysRule[0]).toBe('warn')
      expect(sortKeysRule[1]).toBe('asc')
      expect(sortKeysRule[2]).toMatchObject({ caseSensitive: true, natural: true })
    })
  })

  describe('with react: true', () => {
    it('includes foxford/no-react-default-import as error', () => {
      const config = createFoxfordPluginConfig({ react: true })
      const rules = config.rules
      expect(rules).toHaveProperty('foxford/no-react-default-import', 'error')
    })

    it('still includes all common rules', () => {
      const config = createFoxfordPluginConfig({ react: true })
      const rules = config.rules
      for (const rule of COMMON_RULES) {
        expect(rules).toHaveProperty(rule)
      }
    })
  })

  describe('with react: false', () => {
    it('does NOT include foxford/no-react-default-import', () => {
      const config = createFoxfordPluginConfig({ react: false })
      const rules = config.rules
      expect(rules).not.toHaveProperty('foxford/no-react-default-import')
    })
  })
})
