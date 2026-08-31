import { describe, expect, it } from 'vitest'

import { typescript } from '../layers/typescript.js'

describe('typescript layer', () => {
  it('exports an array', () => {
    expect(Array.isArray(typescript)).toBe(true)
  })

  it('has multiple entries (strict preset entries + foxford configs)', () => {
    expect(typescript.length).toBeGreaterThanOrEqual(2)
  })

  it('contains a named foxford/typescript config', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript')
    expect(config).toBeDefined()
  })

  it('contains a named foxford/typescript/declarations config', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript/declarations')
    expect(config).toBeDefined()
  })

  it('foxford/typescript targets TS files', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript')
    expect(config?.files).toContain('**/*.ts')
    expect(config?.files).toContain('**/*.tsx')
    expect(config?.files).toContain('**/*.mts')
    expect(config?.files).toContain('**/*.cts')
  })

  it('foxford/typescript/declarations targets .d.ts files', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript/declarations')
    expect(config?.files).toContain('**/*.d.ts')
  })

  it('enforces consistent-type-imports', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript')!
    const rules = config.rules!
    expect(rules['@typescript-eslint/consistent-type-imports']).toBe('error')
  })

  it('enforces consistent-type-exports', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript')!
    const rules = config.rules!
    expect(rules['@typescript-eslint/consistent-type-exports']).toBe('error')
  })

  it('enforces no-import-type-side-effects', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript')!
    const rules = config.rules!
    expect(rules['@typescript-eslint/no-import-type-side-effects']).toBe('error')
  })

  it('overrides no-unused-vars with @typescript-eslint version', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript')!
    const rules = config.rules!
    expect(rules['@typescript-eslint/no-unused-vars']).toEqual([
      'error',
      { argsIgnorePattern: '^_', ignoreRestSiblings: true },
    ])
  })

  it('disables base class-methods-use-this in favor of TS version', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript')!
    const rules = config.rules!
    expect(rules['class-methods-use-this']).toBe(0)
  })

  it('declarations config relaxes no-namespace to allow declarations', () => {
    const config = typescript.find((c) => c.name === 'foxford/typescript/declarations')!
    const rules = config.rules!
    expect(rules['@typescript-eslint/no-namespace']).toEqual([
      'error',
      { allowDeclarations: true, allowDefinitionFiles: true },
    ])
  })
})
