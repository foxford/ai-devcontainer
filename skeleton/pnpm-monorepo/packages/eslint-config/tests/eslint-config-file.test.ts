import { describe, expect, it } from 'vitest'

import eslintConfigRaw from '../eslint.config.js'

import type { Linter } from 'eslint'

const eslintConfig = eslintConfigRaw as unknown as Linter.Config[]

describe('eslint.config.js (self-linting config)', () => {
  it('exports an array', () => {
    expect(Array.isArray(eslintConfig)).toBe(true)
  })

  it('has more than one entry', () => {
    expect(eslintConfig.length).toBeGreaterThan(1)
  })

  it('has an ignores entry for build and node_modules', () => {
    const ignoresEntry = eslintConfig.find((c) => Array.isArray(c.ignores) && c.ignores.includes('build/**'))
    expect(ignoresEntry).toBeDefined()
    expect(ignoresEntry!.ignores).toContain('node_modules/**')
  })

  it('extends the node config (includes foxford/node layer)', () => {
    const hasNodeLayer = eslintConfig.some((c) => c.name === 'foxford/node')
    expect(hasNodeLayer).toBe(true)
  })

  it('includes global ignores from the base config', () => {
    const hasGlobalIgnores = eslintConfig.some((c) => c.name === 'foxford/global-ignores')
    expect(hasGlobalIgnores).toBe(true)
  })

  it('disables resolver-based import rules that require tsconfig', () => {
    // The self-linting override disables import-x/no-deprecated, which is unique
    // to this config entry (not set by any base layer).
    const resolverOverride = eslintConfig.find((c) => c.rules && c.rules['import-x/no-deprecated'] === 'off')
    expect(resolverOverride).toBeDefined()
    expect(resolverOverride!.rules!['import-x/no-unresolved']).toBe('off')
    expect(resolverOverride!.rules!['import-x/default']).toBe('off')
    expect(resolverOverride!.rules!['import-x/namespace']).toBe('off')
    expect(resolverOverride!.rules!['n/no-missing-import']).toBe('off')
    expect(resolverOverride!.rules!['n/no-unpublished-import']).toBe('off')
  })

  it('relaxes complexity, sort-keys, and max-lines for plugin rule files', () => {
    const pluginOverride = eslintConfig.find(
      (c) => Array.isArray(c.files) && c.files.some((f) => f.includes('plugins/foxford'))
    )
    expect(pluginOverride).toBeDefined()
    expect(pluginOverride!.rules!['complexity']).toBe('off')
    expect(pluginOverride!.rules!['foxford/sort-keys']).toBe('off')
    expect(pluginOverride!.rules!['max-lines']).toBe('off')
  })

  it('plugin file override targets both rules and utils directories', () => {
    const pluginOverride = eslintConfig.find(
      (c) => Array.isArray(c.files) && c.files.some((f) => f.includes('plugins/foxford'))
    )
    expect(pluginOverride!.files).toContain('plugins/foxford/rules/**/*.js')
    expect(pluginOverride!.files).toContain('plugins/foxford/utils/**/*.js')
  })

  it('does not include react layer (uses node config, not react-node)', () => {
    const hasReactLayer = eslintConfig.some((c) => c.name === 'foxford/react')
    expect(hasReactLayer).toBe(false)
  })
})
