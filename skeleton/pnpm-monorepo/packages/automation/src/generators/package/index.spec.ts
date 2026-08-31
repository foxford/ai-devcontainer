import { readProjectConfiguration } from '@nx/devkit'
import { createTreeWithEmptyWorkspace } from '@nx/devkit/testing'
import { describe, beforeEach, it, expect } from 'vitest'

import { packageGenerator } from './index'

import type { PackageGeneratorSchema } from './schema'
import type { Tree } from '@nx/devkit'

describe('package generator', () => {
  let tree: Tree
  const options: PackageGeneratorSchema = {
    name: 'test',
    useTsup: true,
  }

  beforeEach(() => {
    tree = createTreeWithEmptyWorkspace()

    // Создаем базовую структуру Nx проекта
    tree.write('nx.json', JSON.stringify({ npmScope: 'foxford' }))
    tree.write('package.json', JSON.stringify({ name: 'foxford' }))
  })

  it('should run successfully', async () => {
    await packageGenerator(tree, options)
    const config = readProjectConfiguration(tree, '@foxford/test')
    expect(config).toBeDefined()
    expect(config.root).toBe('packages/test')
  })

  it('should create base package files', async () => {
    await packageGenerator(tree, options)
    expect(tree.exists('packages/test/src/index.ts')).toBe(true)
    expect(tree.exists('packages/test/package.json')).toBe(true)
    expect(tree.exists('packages/test/project.json')).toBe(true)
    expect(tree.exists('packages/test/tsconfig.json')).toBe(true)
    expect(tree.exists('packages/test/README.mdx')).toBe(true)
  })

  it('should generate tsup config when useTsup: true', async () => {
    await packageGenerator(tree, options)
    expect(tree.exists('packages/test/tsup.config.ts')).toBe(true)
  })

  it('should add build script when useTsup: true', async () => {
    await packageGenerator(tree, options)
    const pkg = JSON.parse(tree.read('packages/test/package.json', 'utf-8')!)
    expect(pkg.scripts.build).toBe('tsup')
  })

  it('should not generate tsup config when useTsup: false', async () => {
    await packageGenerator(tree, { ...options, useTsup: false })
    expect(tree.exists('packages/test/tsup.config.ts')).toBe(false)
  })

  it('should always have lintable tag', async () => {
    await packageGenerator(tree, { ...options, useTsup: false })
    const projectJson = JSON.parse(tree.read('packages/test/project.json', 'utf-8')!)
    expect(projectJson.tags).toContain('lintable')
  })

  it('should use correct project name scope', async () => {
    await packageGenerator(tree, { ...options, name: 'my-lib' })
    const config = readProjectConfiguration(tree, '@foxford/my-lib')
    expect(config.root).toBe('packages/my-lib')
    expect(config.sourceRoot).toBe('packages/my-lib/src')
  })

  it('should create eslint.config.mjs in flat config format', async () => {
    await packageGenerator(tree, options)
    expect(tree.exists('packages/test/eslint.config.mjs')).toBe(true)
  })

  it('should not create legacy .eslintrc.cjs', async () => {
    await packageGenerator(tree, options)
    expect(tree.exists('packages/test/.eslintrc.cjs')).toBe(false)
  })

  it('should generate eslint.config.mjs that imports from @foxford/eslint-config', async () => {
    await packageGenerator(tree, options)
    const content = tree.read('packages/test/eslint.config.mjs', 'utf-8')!
    expect(content).toContain('@foxford/eslint-config')
    expect(content).toContain('export default')
  })
})
