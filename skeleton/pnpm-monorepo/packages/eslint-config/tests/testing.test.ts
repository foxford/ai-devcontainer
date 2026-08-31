import { describe, expect, it } from 'vitest'

import { testing } from '../layers/testing.js'

describe('testing layer', () => {
  it('exports an array', () => {
    expect(Array.isArray(testing)).toBe(true)
  })

  it('has exactly one config entry', () => {
    expect(testing).toHaveLength(1)
  })

  it('is named foxford/testing', () => {
    expect(testing.at(0)?.name).toBe('foxford/testing')
  })

  it('targets spec and test files', () => {
    const files = testing.at(0)?.files
    expect(files).toContain('**/*.spec.ts')
    expect(files).toContain('**/*.spec.tsx')
    expect(files).toContain('**/*.test.ts')
    expect(files).toContain('**/*.test.tsx')
    expect(files).toContain('**/*.spec.js')
    expect(files).toContain('**/*.test.js')
  })

  it('targets ESM and TS module spec/test files', () => {
    const files = testing.at(0)?.files
    expect(files).toContain('**/*.spec.mjs')
    expect(files).toContain('**/*.test.mjs')
    expect(files).toContain('**/*.spec.mts')
    expect(files).toContain('**/*.test.mts')
  })

  it('provides vitest globals', () => {
    const globals = testing.at(0)?.languageOptions?.globals
    expect(globals).toBeDefined()
    // vitest globals include 'describe', 'it', 'expect'
    expect(globals).toHaveProperty('describe')
    expect(globals).toHaveProperty('it')
    expect(globals).toHaveProperty('expect')
  })

  it('disables max-lines-per-function in tests', () => {
    const rules = testing.at(0)?.rules
    expect(rules?.['max-lines-per-function']).toBe('off')
  })

  it('allows more lines per file in tests (300 vs 200)', () => {
    const rules = testing.at(0)?.rules
    expect(rules?.['max-lines']).toEqual(['error', { max: 300, skipComments: true }])
  })
})
