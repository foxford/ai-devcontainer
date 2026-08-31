import { describe, expect, it } from 'vitest'

import { node } from '../layers/node.js'

describe('node layer', () => {
  it('exports an array', () => {
    expect(Array.isArray(node)).toBe(true)
  })

  it('has at least 3 entries (n/recommended + foxford config + foxford/typescript override)', () => {
    expect(node.length).toBeGreaterThanOrEqual(3)
  })

  it('contains a named foxford/node config', () => {
    const config = node.find((c) => c.name === 'foxford/node')
    expect(config).toBeDefined()
  })

  it('provides node globals', () => {
    const config = node.find((c) => c.name === 'foxford/node')
    const nodeGlobals = config?.languageOptions?.globals
    expect(nodeGlobals).toBeDefined()
    expect(nodeGlobals).toHaveProperty('process')
    expect(nodeGlobals).toHaveProperty('__dirname')
  })

  it('disables no-console for node environment', () => {
    const config = node.find((c) => c.name === 'foxford/node')!
    const rules = config.rules!
    expect(rules['no-console']).toBe('off')
  })

  it('disables no-await-in-loop', () => {
    const config = node.find((c) => c.name === 'foxford/node')!
    const rules = config.rules!
    expect(rules['no-await-in-loop']).toBe('off')
  })

  it('disables overly strict n/no-extraneous-require', () => {
    const config = node.find((c) => c.name === 'foxford/node')!
    const rules = config.rules!
    expect(rules['n/no-extraneous-require']).toBe('off')
  })

  it('disables n/no-unpublished-require', () => {
    const config = node.find((c) => c.name === 'foxford/node')!
    const rules = config.rules!
    expect(rules['n/no-unpublished-require']).toBe(0)
  })

  it('disables unsupported feature checks (monorepo setup)', () => {
    const config = node.find((c) => c.name === 'foxford/node')!
    const rules = config.rules!
    expect(rules['n/no-unsupported-features/es-builtins']).toBe(0)
    expect(rules['n/no-unsupported-features/es-syntax']).toBe(0)
    expect(rules['n/no-unsupported-features/node-builtins']).toBe(0)
  })

  it('contains a named foxford/node/typescript config', () => {
    const config = node.find((c) => c.name === 'foxford/node/typescript')
    expect(config).toBeDefined()
  })

  it('foxford/node/typescript targets TS files', () => {
    const config = node.find((c) => c.name === 'foxford/node/typescript')
    expect(config?.files).toContain('**/*.ts')
    expect(config?.files).toContain('**/*.tsx')
    expect(config?.files).toContain('**/*.mts')
    expect(config?.files).toContain('**/*.cts')
  })

  it('foxford/node/typescript disables n/no-missing-import for TS files', () => {
    const config = node.find((c) => c.name === 'foxford/node/typescript')!
    const rules = config.rules!
    expect(rules['n/no-missing-import']).toBe('off')
    expect(rules['n/no-missing-require']).toBe('off')
    expect(rules['n/no-unpublished-import']).toBe('off')
  })

  it('allows param reassignment without prop restriction', () => {
    const config = node.find((c) => c.name === 'foxford/node')!
    const rules = config.rules!
    expect(rules['no-param-reassign']).toEqual(['error', { props: false }])
  })
})
