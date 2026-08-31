import { describe, expect, it } from 'vitest'

import { javascript } from '../layers/javascript.js'

describe('javascript layer', () => {
  it('exports an array', () => {
    expect(Array.isArray(javascript)).toBe(true)
  })

  it('has at least 2 entries (recommended + foxford config)', () => {
    expect(javascript.length).toBeGreaterThanOrEqual(2)
  })

  it('contains a named foxford/javascript config', () => {
    const foxfordConfig = javascript.find((c) => c.name === 'foxford/javascript')
    expect(foxfordConfig).toBeDefined()
  })

  it('foxford/javascript targets JS/JSX files', () => {
    const foxfordConfig = javascript.find((c) => c.name === 'foxford/javascript')
    expect(foxfordConfig?.files).toContain('**/*.js')
    expect(foxfordConfig?.files).toContain('**/*.jsx')
    expect(foxfordConfig?.files).toContain('**/*.mjs')
    expect(foxfordConfig?.files).toContain('**/*.cjs')
  })

  it('foxford/javascript provides browser, node, and es2021 globals', () => {
    const foxfordConfig = javascript.find((c) => c.name === 'foxford/javascript')
    const globals = foxfordConfig?.languageOptions?.globals
    expect(globals).toBeDefined()
    // browser globals include 'window'
    expect(globals).toHaveProperty('window')
    // node globals include 'process'
    expect(globals).toHaveProperty('process')
  })

  it('defines expected custom rules', () => {
    const foxfordConfig = javascript.find((c) => c.name === 'foxford/javascript')
    const rules = foxfordConfig?.rules
    expect(rules).toHaveProperty('complexity')
    expect(rules).toHaveProperty('max-lines')
    expect(rules).toHaveProperty('max-lines-per-function')
    expect(rules).toHaveProperty('no-unused-vars')
    expect(rules).toHaveProperty('no-irregular-whitespace')
  })

  it('complexity rule is set to 12', () => {
    const foxfordConfig = javascript.find((c) => c.name === 'foxford/javascript')!
    const rules = foxfordConfig.rules!
    expect(rules['complexity']).toEqual(['error', 12])
  })

  it('max-lines rule allows up to 200 lines', () => {
    const foxfordConfig = javascript.find((c) => c.name === 'foxford/javascript')!
    const rules = foxfordConfig.rules!
    expect(rules['max-lines']).toEqual(['error', { max: 200, skipComments: true }])
  })

  it('no-unused-vars ignores _-prefixed args and rest siblings', () => {
    const foxfordConfig = javascript.find((c) => c.name === 'foxford/javascript')!
    const rules = foxfordConfig.rules!
    expect(rules['no-unused-vars']).toEqual(['error', { argsIgnorePattern: '^_', ignoreRestSiblings: true }])
  })
})
