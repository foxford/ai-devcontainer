import { describe, expect, it } from 'vitest'

import indexConfig from '../index.js'
import nodeConfig from '../node.js'
import reactNodeConfig from '../react-node.js'
import reactConfig from '../react.js'

import type { Linter } from 'eslint'

const FOXFORD_PLUGIN_CONFIG_NAME = 'foxford/plugin'

const COMMON_FOXFORD_RULES = ['foxford/import-types-independently', 'foxford/no-restricted-paths', 'foxford/sort-keys']

const REACT_FOXFORD_RULES = [...COMMON_FOXFORD_RULES, 'foxford/no-react-default-import']

function getFoxfordPluginConfig(config: Linter.Config[]) {
  return config.find((c) => {
    return typeof c === 'object' && c !== null && c.name === FOXFORD_PLUGIN_CONFIG_NAME
  })
}

function hasLayerNamed(config: Linter.Config[], name: string) {
  return config.some((c) => typeof c === 'object' && c !== null && c.name === name)
}

describe('index export (JS+TS+imports+foxford+testing+prettier)', () => {
  it('exports an array', () => {
    expect(Array.isArray(indexConfig)).toBe(true)
  })

  it('includes global ignores', () => {
    expect(hasLayerNamed(indexConfig, 'foxford/global-ignores')).toBe(true)
  })

  it('includes javascript layer', () => {
    expect(hasLayerNamed(indexConfig, 'foxford/javascript')).toBe(true)
  })

  it('includes typescript layer', () => {
    expect(hasLayerNamed(indexConfig, 'foxford/typescript')).toBe(true)
  })

  it('includes imports layer', () => {
    expect(hasLayerNamed(indexConfig, 'foxford/imports')).toBe(true)
  })

  it('includes testing layer', () => {
    expect(hasLayerNamed(indexConfig, 'foxford/testing')).toBe(true)
  })

  it('includes prettier layer', () => {
    expect(hasLayerNamed(indexConfig, 'eslint-plugin-prettier/recommended')).toBe(true)
  })

  it('does NOT include react layer', () => {
    expect(hasLayerNamed(indexConfig, 'foxford/react')).toBe(false)
  })

  it('does NOT include node layer', () => {
    expect(hasLayerNamed(indexConfig, 'foxford/node')).toBe(false)
  })

  it('includes foxford plugin config', () => {
    expect(getFoxfordPluginConfig(indexConfig)).toBeDefined()
  })

  it('foxford plugin config has all common rules', () => {
    const pluginConfig = getFoxfordPluginConfig(indexConfig)
    const rules = pluginConfig?.rules
    for (const rule of COMMON_FOXFORD_RULES) {
      expect(rules).toHaveProperty(rule)
    }
  })

  it('foxford plugin config does NOT include no-react-default-import', () => {
    const pluginConfig = getFoxfordPluginConfig(indexConfig)
    const rules = pluginConfig?.rules
    expect(rules).not.toHaveProperty('foxford/no-react-default-import')
  })

  it('sort-keys is a warning (not error) with natural+caseSensitive options', () => {
    const pluginConfig = getFoxfordPluginConfig(indexConfig)!
    const rules = pluginConfig.rules!
    const sortKeysRule = rules['foxford/sort-keys'] as unknown[]
    expect(sortKeysRule[0]).toBe('warn')
    expect(sortKeysRule[1]).toBe('asc')
    expect(sortKeysRule[2]).toMatchObject({ caseSensitive: true, natural: true })
  })
})

describe('react export (JS+TS+imports+react+foxford+testing+prettier)', () => {
  it('exports an array', () => {
    expect(Array.isArray(reactConfig)).toBe(true)
  })

  it('includes global ignores', () => {
    expect(hasLayerNamed(reactConfig, 'foxford/global-ignores')).toBe(true)
  })

  it('includes react layer', () => {
    expect(hasLayerNamed(reactConfig, 'foxford/react')).toBe(true)
  })

  it('does NOT include node layer', () => {
    expect(hasLayerNamed(reactConfig, 'foxford/node')).toBe(false)
  })

  it('includes foxford plugin config with no-react-default-import', () => {
    const pluginConfig = getFoxfordPluginConfig(reactConfig)
    const rules = pluginConfig?.rules
    for (const rule of REACT_FOXFORD_RULES) {
      expect(rules).toHaveProperty(rule)
    }
  })
})

describe('node export (JS+TS+imports+node+foxford+testing+prettier)', () => {
  it('exports an array', () => {
    expect(Array.isArray(nodeConfig)).toBe(true)
  })

  it('includes global ignores', () => {
    expect(hasLayerNamed(nodeConfig, 'foxford/global-ignores')).toBe(true)
  })

  it('includes node layer', () => {
    expect(hasLayerNamed(nodeConfig, 'foxford/node')).toBe(true)
  })

  it('does NOT include react layer', () => {
    expect(hasLayerNamed(nodeConfig, 'foxford/react')).toBe(false)
  })

  it('includes foxford plugin config without no-react-default-import', () => {
    const pluginConfig = getFoxfordPluginConfig(nodeConfig)
    const rules = pluginConfig?.rules
    for (const rule of COMMON_FOXFORD_RULES) {
      expect(rules).toHaveProperty(rule)
    }
    expect(rules).not.toHaveProperty('foxford/no-react-default-import')
  })
})

describe('react-node export (JS+TS+imports+react+node+foxford+testing+prettier)', () => {
  it('exports an array', () => {
    expect(Array.isArray(reactNodeConfig)).toBe(true)
  })

  it('includes global ignores', () => {
    expect(hasLayerNamed(reactNodeConfig, 'foxford/global-ignores')).toBe(true)
  })

  it('includes react layer', () => {
    expect(hasLayerNamed(reactNodeConfig, 'foxford/react')).toBe(true)
  })

  it('includes node layer', () => {
    expect(hasLayerNamed(reactNodeConfig, 'foxford/node')).toBe(true)
  })

  it('includes foxford plugin config with no-react-default-import', () => {
    const pluginConfig = getFoxfordPluginConfig(reactNodeConfig)
    const rules = pluginConfig?.rules
    for (const rule of REACT_FOXFORD_RULES) {
      expect(rules).toHaveProperty(rule)
    }
  })
})
