import { describe, expect, it } from 'vitest'

import { imports } from '../layers/imports.js'

describe('imports layer', () => {
  it('exports an array', () => {
    expect(Array.isArray(imports)).toBe(true)
  })

  it('has at least 4 entries (recommended, typescript, foxford, foxford/typescript override)', () => {
    expect(imports.length).toBeGreaterThanOrEqual(4)
  })

  it('contains a named foxford/imports config', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')
    expect(config).toBeDefined()
  })

  it('configures eslint-import-resolver-typescript as resolver', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const resolvers = config.settings?.['import-x/resolver-next'] as unknown[] | undefined
    expect(Array.isArray(resolvers)).toBe(true)
    expect(resolvers?.length).toBeGreaterThanOrEqual(1)
  })

  it('resolver has alwaysTryTypes enabled', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const resolvers = config.settings?.['import-x/resolver-next'] as unknown[] | undefined
    // The first resolver is the TypeScript resolver with alwaysTryTypes
    const firstResolver = resolvers?.[0] as Record<string, unknown> | undefined
    expect(firstResolver).toBeDefined()
    // The resolver object has alwaysTryTypes in its options
    const resolverOptions = (firstResolver?.['options'] ?? firstResolver) as Record<string, unknown> | undefined
    expect(resolverOptions?.['alwaysTryTypes'] ?? true).toBe(true)
  })

  it('enforces consistent-type-specifier-style as prefer-top-level', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const rules = config.rules!
    expect(rules['import-x/consistent-type-specifier-style']).toEqual(['error', 'prefer-top-level'])
  })

  it('disables no-extraneous-dependencies', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const rules = config.rules!
    expect(rules['import-x/no-extraneous-dependencies']).toBe('off')
  })

  it('disables prefer-default-export', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const rules = config.rules!
    expect(rules['import-x/prefer-default-export']).toBe('off')
  })

  it('enforces import ordering', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const rules = config.rules!
    expect(rules['import-x/order']).toBeDefined()
    const orderRule = rules['import-x/order'] as unknown[]
    expect(orderRule[0]).toBe('error')
    expect(orderRule[1]).toHaveProperty('alphabetize')
    expect(orderRule[1]).toHaveProperty('groups')
    expect(orderRule[1]).toHaveProperty('newlines-between', 'always')
  })

  it('import order groups builtin before external before internal', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const rules = config.rules!
    const orderRule = rules['import-x/order'] as unknown[]
    const groups = (orderRule[1] as { groups: string[] }).groups
    expect(groups.indexOf('builtin')).toBeLessThan(groups.indexOf('external'))
    expect(groups.indexOf('external')).toBeLessThan(groups.indexOf('internal'))
  })

  it('contains a named foxford/imports/typescript config', () => {
    const config = imports.find((c) => c.name === 'foxford/imports/typescript')
    expect(config).toBeDefined()
  })

  it('foxford/imports/typescript targets TS files', () => {
    const config = imports.find((c) => c.name === 'foxford/imports/typescript')
    expect(config?.files).toContain('**/*.ts')
    expect(config?.files).toContain('**/*.tsx')
    expect(config?.files).toContain('**/*.mts')
    expect(config?.files).toContain('**/*.cts')
  })

  it('foxford/imports/typescript disables no-unresolved for TS files', () => {
    const config = imports.find((c) => c.name === 'foxford/imports/typescript')!
    const rules = config.rules!
    expect(rules['import-x/no-unresolved']).toBe('off')
  })

  it('orders @foxford/** packages after other externals', () => {
    const config = imports.find((c) => c.name === 'foxford/imports')!
    const rules = config.rules!
    const orderRule = rules['import-x/order'] as unknown[]
    const pathGroups = (orderRule[1] as { pathGroups: Array<{ pattern: string; position: string }> }).pathGroups
    const foxfordGroup = pathGroups.find((g) => g.pattern === '@foxford/**')
    expect(foxfordGroup).toBeDefined()
    expect(foxfordGroup?.position).toBe('after')
  })
})
