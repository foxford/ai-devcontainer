import { describe, expect, it } from 'vitest'

import { react } from '../layers/react.js'

describe('react layer', () => {
  it('exports an array', () => {
    expect(Array.isArray(react)).toBe(true)
  })

  it('has at least 4 entries (react recommended, react-hooks, jsx-a11y, foxford)', () => {
    expect(react.length).toBeGreaterThanOrEqual(4)
  })

  it('contains a named foxford/react config', () => {
    const config = react.find((c) => c.name === 'foxford/react')
    expect(config).toBeDefined()
  })

  it('configures react version detection', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const settings = config.settings as Record<string, Record<string, string>> | undefined
    const reactSettings = settings?.['react']
    expect(reactSettings?.['version']).toBe('detect')
  })

  it('disables react/react-in-jsx-scope (new JSX transform)', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['react/react-in-jsx-scope']).toBe('off')
  })

  it('disables react/jsx-uses-react (new JSX transform)', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['react/jsx-uses-react']).toBe('off')
  })

  it('errors on react/display-name', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['react/display-name']).toBe(2)
  })

  it('errors on react/no-danger', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['react/no-danger']).toBe(2)
  })

  it('forbids dangerouslySetInnerHTML prop', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    const forbidRule = rules['react/forbid-component-props'] as unknown[]
    const dangerousProp = (forbidRule[1] as { forbid: Array<{ propName: string }> }).forbid.find(
      (f) => f.propName === 'dangerouslySetInnerHTML'
    )
    expect(dangerousProp).toBeDefined()
  })

  it('enforces react-hooks/rules-of-hooks as error', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['react-hooks/rules-of-hooks']).toBe('error')
  })

  it('warns on react-hooks/exhaustive-deps', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['react-hooks/exhaustive-deps']).toBe('warn')
  })

  it('uses single quotes for JSX attributes', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['jsx-quotes']).toEqual(['error', 'prefer-single'])
  })

  it('enforces react/prefer-stateless-function', () => {
    const config = react.find((c) => c.name === 'foxford/react')!
    const rules = config.rules!
    expect(rules['react/prefer-stateless-function']).toEqual(['error', { ignorePureComponents: true }])
  })
})
