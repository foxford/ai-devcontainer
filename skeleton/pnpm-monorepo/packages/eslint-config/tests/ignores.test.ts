import { describe, expect, it } from 'vitest'

import { globalIgnores } from '../layers/ignores.js'

describe('ignores layer (globalIgnores)', () => {
  it('is an object (not an array)', () => {
    expect(typeof globalIgnores).toBe('object')
    expect(Array.isArray(globalIgnores)).toBe(false)
  })

  it('is named foxford/global-ignores', () => {
    expect(globalIgnores.name).toBe('foxford/global-ignores')
  })

  it('has an ignores array', () => {
    expect(Array.isArray(globalIgnores.ignores)).toBe(true)
    expect(globalIgnores.ignores!.length).toBeGreaterThan(0)
  })

  it('ignores build output directories', () => {
    const ignores = globalIgnores.ignores
    expect(ignores).toContain('**/build/**')
    expect(ignores).toContain('**/dist/**')
    expect(ignores).toContain('**/out/**')
  })

  it('ignores node_modules', () => {
    const ignores = globalIgnores.ignores
    expect(ignores).toContain('**/node_modules/**')
  })

  it('ignores docs directory', () => {
    const ignores = globalIgnores.ignores
    expect(ignores).toContain('**/docs/**')
  })

  it('ignores bundled mjs files (tsup output)', () => {
    const ignores = globalIgnores.ignores
    expect(ignores).toContain('**/*.bundled_*.mjs')
  })
})
