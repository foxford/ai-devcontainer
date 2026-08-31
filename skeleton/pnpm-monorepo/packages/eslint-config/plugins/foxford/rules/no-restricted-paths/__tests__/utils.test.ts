import path from 'path'

import { describe, expect, it } from 'vitest'

import { isTargetContainsFile } from '../utils.js'

describe('isTargetContainsFile', () => {
  it('returns true when filepath equals the target', () => {
    const dir = '/project/src/features/auth'
    expect(isTargetContainsFile(dir, dir)).toBe(true)
  })

  it('returns true when filepath is a child of target directory', () => {
    const target = '/project/src/features/auth'
    const file = '/project/src/features/auth/model.ts'
    expect(isTargetContainsFile(file, target)).toBe(true)
  })

  it('returns true for deeply nested child', () => {
    const target = '/project/src'
    const file = '/project/src/features/auth/components/LoginForm.tsx'
    expect(isTargetContainsFile(file, target)).toBe(true)
  })

  it('returns false when filepath is outside of target', () => {
    const target = '/project/src/features/auth'
    const file = '/project/src/features/payments/model.ts'
    expect(isTargetContainsFile(file, target)).toBe(false)
  })

  it('returns false when target is a child of filepath', () => {
    const target = '/project/src/features/auth/model'
    const file = '/project/src/features/auth'
    expect(isTargetContainsFile(file, target)).toBe(false)
  })

  it('supports glob patterns in target', () => {
    expect(isTargetContainsFile('/project/src/features/auth/model.ts', '/project/src/**/*.ts')).toBe(true)
  })

  it('glob pattern that does not match returns false', () => {
    expect(isTargetContainsFile('/project/src/features/auth/model.js', '/project/src/**/*.ts')).toBe(false)
  })

  it('handles platform path separators correctly', () => {
    const target = path.resolve('/project/src/features')
    const file = path.resolve('/project/src/features/auth/model.ts')
    expect(isTargetContainsFile(file, target)).toBe(true)
  })
})
