import resolve from 'eslint-module-utils/resolve'
import { describe, expect, it, vi } from 'vitest'

import { ImportValidator } from '../import-validator.js'

import type { Rule } from 'eslint'
import type { MockedFunction } from 'vitest'

vi.mock('eslint-module-utils/resolve', () => ({
  default: vi.fn(() => null),
}))

type ValidatorConfig = {
  basePath: string
  context: Rule.RuleContext
  currentFilename: string
  zones: Array<{ target: string | string[]; from: string; except?: string[]; message?: string }>
  parentPackagePrefix?: string
  parentPackagePostfix?: string
  exclude?: string[]
}

const makeValidator = (config: ValidatorConfig) =>
  // ImportValidator is a JS class whose constructor types are poorly inferred (e.g. `exclude` as `never[]`).
  // Using a type assertion here is intentional — the JS runtime handles optional fields correctly.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  new ImportValidator(config as any)

const BASE_PATH = '/project'
const makeContext = (): Rule.RuleContext => ({}) as unknown as Rule.RuleContext
const mockedResolve = resolve as unknown as MockedFunction<() => string | null>

describe('ImportValidator', () => {
  describe('constructor — zone filtering', () => {
    it('keeps zones where current file is inside target', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(validator.zones).toHaveLength(1)
    })

    it('discards zones where current file is outside target', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/payments/model.js',
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(validator.zones).toHaveLength(0)
    })

    it('keeps zones whose target is an array and current file matches one entry', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/cart/index.js',
        zones: [{ from: 'src/features/billing', target: ['src/features/auth', 'src/features/cart'] }],
      })

      expect(validator.zones).toHaveLength(1)
    })

    it('discards zones whose target array does not match current file', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/payments/index.js',
        zones: [{ from: 'src/features/billing', target: ['src/features/auth', 'src/features/cart'] }],
      })

      expect(validator.zones).toHaveLength(0)
    })
  })

  describe('dispatch — exclude', () => {
    it('skips validation when current file matches an exclude pattern', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        exclude: ['/project/src/features/auth/model.js'],
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      // Without exclude this would throw
      expect(() => validator.dispatch('../billing/api.js')).not.toThrow()
    })

    it('validates when current file does not match exclude pattern', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        exclude: ['/project/src/features/auth/other.js'],
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../billing/api.js')).toThrow()
    })

    it('supports glob patterns in exclude', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        exclude: ['/project/src/features/auth/**'],
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../billing/api.js')).not.toThrow()
    })
  })

  describe('dispatch — non-resolvable imports', () => {
    it('skips validation when external import cannot be resolved', () => {
      mockedResolve.mockReturnValue(null)

      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('some-external-package')).not.toThrow()
    })

    it('validates when external import resolves to a restricted path', () => {
      mockedResolve.mockReturnValue('/project/src/features/billing/api.js')

      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('@org/billing')).toThrow()
    })
  })

  describe('dispatch — relative imports', () => {
    it('throws when relative import targets a restricted zone', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../billing/api.js')).toThrow()
    })

    it('does not throw when relative import is outside any restricted zone', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../shared/utils.js')).not.toThrow()
    })

    it('error message includes the import path', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features/billing', message: 'no billing imports', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../billing/api.js')).toThrow('../billing/api.js')
    })

    it('error message includes zone message when configured', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features/billing', message: 'no billing imports', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../billing/api.js')).toThrow('no billing imports')
    })
  })

  describe('dispatch — except list', () => {
    it('allows import when it matches an except path', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ except: ['public-api.js'], from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../billing/public-api.js')).not.toThrow()
    })

    it('throws when import does not match except path', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ except: ['public-api.js'], from: 'src/features/billing', target: 'src/features/auth' }],
      })

      expect(() => validator.dispatch('../billing/internal.js')).toThrow()
    })

    it('allows import when it matches an except glob pattern (non-relative import)', () => {
      // Glob except works on the raw importPath string.
      // For relative imports starting with ".." minimatch won't match "**/..." due to dotfile rules,
      // so glob except is only effective for non-relative import strings.
      mockedResolve.mockReturnValueOnce('/project/src/features/billing/public-api.js')

      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ except: ['**/public-api.js'], from: 'src/features/billing', target: 'src/features/auth' }],
      })

      // importPath does not start with '.' → resolve() is used → returns billing path
      // minimatch('src/features/billing/public-api.js', '**/public-api.js') → true → return (no throw)
      expect(() => validator.dispatch('src/features/billing/public-api.js')).not.toThrow()
    })

    it('allows when importPath exactly equals an except entry', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [
          {
            except: ['../billing/public-api.js'],
            from: 'src/features/billing',
            target: 'src/features/auth',
          },
        ],
      })

      expect(() => validator.dispatch('../billing/public-api.js')).not.toThrow()
    })
  })

  describe('dispatch — self-feature package (parentPackagePrefix)', () => {
    it('allows imports within the same feature when parentPackagePrefix is set', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        parentPackagePrefix: 'src/features',
        zones: [{ from: 'src/features', target: 'src/features' }],
      })

      // Importing from own feature — should be allowed
      expect(() => validator.dispatch('./utils.js')).not.toThrow()
    })

    it('throws when importing from a different feature even with parentPackagePrefix', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        parentPackagePrefix: 'src/features',
        zones: [{ from: 'src/features', target: 'src/features' }],
      })

      expect(() => validator.dispatch('../billing/api.js')).toThrow()
    })

    it('returns null for self-package when parentPackagePrefix is absent', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        zones: [{ from: 'src/features', target: 'src/features' }],
      })

      // No parentPackagePrefix → no self-package exclusion → throws
      expect(() => validator.dispatch('./utils.js')).toThrow()
    })

    it('supports {name} placeholder in parentPackagePrefix', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/packages/auth/src/model.js',
        parentPackagePrefix: 'packages/{name}/src',
        zones: [{ from: 'packages', target: 'packages' }],
      })

      // Import from own package's src — allowed
      expect(() => validator.dispatch('./utils.js')).not.toThrow()
      // absoluteImportPath = /project/packages/auth/src/utils.js
      // selfFeatureAbsolutePath = /project/packages/auth/src
      // starts with → return
    })

    it('throws for import outside {name} placeholder self-package scope', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/packages/auth/src/model.js',
        parentPackagePrefix: 'packages/{name}/src',
        zones: [{ from: 'packages', target: 'packages' }],
      })

      // Import from billing — different package, should throw
      expect(() => validator.dispatch('../../billing/index.js')).toThrow()
    })

    it('returns null for self-package when filename does not match prefix pattern', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/model.js',
        parentPackagePrefix: 'packages',
        zones: [{ from: 'src', target: 'src' }],
      })

      // File doesn't contain /packages/<name>/ pattern → no self-package exclusion
      expect(() => validator.dispatch('./utils.js')).toThrow()
    })
  })

  describe('dispatch — parentPackagePostfix', () => {
    it('uses parentPackagePostfix when provided', () => {
      const validator = makeValidator({
        basePath: BASE_PATH,
        context: makeContext(),
        currentFilename: '/project/src/features/auth/model.js',
        parentPackagePostfix: '',
        parentPackagePrefix: 'src/features',
        zones: [{ from: 'src/features', target: 'src/features' }],
      })

      // Same feature — allowed
      expect(() => validator.dispatch('./utils.js')).not.toThrow()
    })
  })
})
