import { describe, expect, it } from 'vitest'

import { prettier } from '../layers/prettier.js'

describe('prettier layer', () => {
  it('exports an array', () => {
    expect(Array.isArray(prettier)).toBe(true)
  })

  it('has exactly one config entry', () => {
    expect(prettier).toHaveLength(1)
  })

  it('is named eslint-plugin-prettier/recommended', () => {
    expect(prettier.at(0)?.name).toBe('eslint-plugin-prettier/recommended')
  })

  it('disables indent rule (conflicts with Prettier)', () => {
    const rules = prettier.at(0)?.rules
    expect(rules).toHaveProperty('indent')
    // prettier turns rules off (0 or 'off' — both are valid ESLint disabled values)
    expect([0, 'off']).toContain(rules?.['indent'])
  })

  it('disables quotes rule (conflicts with Prettier)', () => {
    const rules = prettier.at(0)?.rules
    expect(rules).toHaveProperty('quotes')
    expect([0, 'off']).toContain(rules?.['quotes'])
  })

  it('disables semi rule (conflicts with Prettier)', () => {
    const rules = prettier.at(0)?.rules
    expect(rules).toHaveProperty('semi')
    expect([0, 'off']).toContain(rules?.['semi'])
  })

  it('disables comma-dangle rule (conflicts with Prettier)', () => {
    const rules = prettier.at(0)?.rules
    expect(rules).toHaveProperty('comma-dangle')
    expect([0, 'off']).toContain(rules?.['comma-dangle'])
  })
})
