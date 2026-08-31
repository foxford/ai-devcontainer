import path from 'path'
import { fileURLToPath } from 'url'

import { RuleTester } from 'eslint'
import { describe, it } from 'vitest'

import ruleModule from '../no-restricted-paths/index.js'

import type { Rule } from 'eslint'

const rule: Rule.RuleModule = ruleModule

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const ruleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: 'module',
  },
})

// Package root — used to resolve real files on disk for the import resolver
const PKG_ROOT = path.resolve(__dirname, '../../../../../..')

describe('no-restricted-paths rule', () => {
  it('allows all imports when no zones configured', () => {
    ruleTester.run('no-restricted-paths (no zones)', rule, {
      invalid: [],
      valid: [
        {
          code: "import { something } from 'some-package'",
          options: [{}],
        },
        {
          code: "import { something } from './local/file'",
          options: [{}],
        },
      ],
    })
  })

  it('validates basePath and zones options schema', () => {
    ruleTester.run('no-restricted-paths (schema)', rule, {
      invalid: [],
      valid: [
        {
          code: 'const x = 1',
          options: [
            {
              basePath: '/some/path',
              zones: [
                {
                  from: 'src/feature-b',
                  message: 'Cannot import from feature-b',
                  target: 'src/feature-a',
                },
              ],
            },
          ],
        },
        {
          code: 'const x = 1',
          options: [
            {
              basePath: '/some/path',
              zones: [
                {
                  except: ['src/feature-b/public-api'],
                  from: 'src/feature-b',
                  target: ['src/feature-a', 'src/feature-c'],
                },
              ],
            },
          ],
        },
      ],
    })
  })

  it('skips non-resolvable imports (node_modules)', () => {
    ruleTester.run('no-restricted-paths (non-resolvable)', rule, {
      invalid: [],
      valid: [
        {
          code: "import { foo } from 'some-external-package'",
          filename: path.join(process.cwd(), 'src/feature-a/index.js'),
          options: [
            {
              basePath: process.cwd(),
              zones: [
                {
                  from: 'src/feature-b',
                  target: 'src/feature-a',
                },
              ],
            },
          ],
        },
      ],
    })
  })

  it('validates require() calls in addition to imports', () => {
    ruleTester.run('no-restricted-paths (require)', rule, {
      invalid: [],
      valid: [
        {
          code: "const x = require('external-package')",
          options: [{}],
        },
      ],
    })
  })

  it('supports exclude option to skip validation for certain files', () => {
    ruleTester.run('no-restricted-paths (exclude)', rule, {
      invalid: [],
      valid: [
        {
          code: "import { foo } from './restricted'",
          filename: '/workspaces/frontend-platform/src/excluded-file.js',
          options: [
            {
              basePath: '/workspaces/frontend-platform',
              exclude: ['/workspaces/frontend-platform/src/excluded-file.js'],
              zones: [
                {
                  from: 'src',
                  target: 'src',
                },
              ],
            },
          ],
        },
      ],
    })
  })

  it('also checks ExportNamedDeclaration with source', () => {
    ruleTester.run('no-restricted-paths (export named)', rule, {
      invalid: [],
      valid: [
        {
          code: 'export const foo = 1',
          options: [{}],
        },
        {
          code: "export { foo } from 'some-external-package'",
          options: [{}],
        },
      ],
    })
  })

  it('also checks ExportAllDeclaration', () => {
    ruleTester.run('no-restricted-paths (export all)', rule, {
      invalid: [],
      valid: [
        {
          code: "export * from 'some-external-package'",
          options: [{}],
        },
      ],
    })
  })

  it('reports error when file in target zone imports from restricted from zone', () => {
    ruleTester.run('no-restricted-paths (invalid: basic)', rule, {
      invalid: [
        {
          code: "const x = require('../../rules/no-restricted-paths/utils.js')",
          errors: [{ messageId: 'restrictedPath' }],
          filename: path.join(PKG_ROOT, 'src/plugins/foxford/__tests__/rules/sample.js'),
          options: [
            {
              basePath: PKG_ROOT,
              zones: [
                {
                  from: 'src/plugins/foxford/rules/no-restricted-paths',
                  target: 'src/plugins/foxford/__tests__',
                },
              ],
            },
          ],
        },
      ],
      valid: [],
    })
  })

  it('reports error with custom message when zone has message configured', () => {
    ruleTester.run('no-restricted-paths (invalid: custom message)', rule, {
      invalid: [
        {
          code: "import utils from '../../rules/no-restricted-paths/utils.js'",
          errors: [{ messageId: 'restrictedPath' }],
          filename: path.join(PKG_ROOT, 'src/plugins/foxford/__tests__/rules/sample.js'),
          options: [
            {
              basePath: PKG_ROOT,
              zones: [
                {
                  from: 'src/plugins/foxford/rules/no-restricted-paths',
                  message: 'Do not import implementation details from tests.',
                  target: 'src/plugins/foxford/__tests__',
                },
              ],
            },
          ],
        },
      ],
      valid: [],
    })
  })

  it('reports error for ExportNamedDeclaration importing from restricted zone', () => {
    ruleTester.run('no-restricted-paths (invalid: export named)', rule, {
      invalid: [
        {
          code: "export { isTargetContainsFile } from '../../rules/no-restricted-paths/utils.js'",
          errors: [{ messageId: 'restrictedPath' }],
          filename: path.join(PKG_ROOT, 'src/plugins/foxford/__tests__/rules/sample.js'),
          options: [
            {
              basePath: PKG_ROOT,
              zones: [
                {
                  from: 'src/plugins/foxford/rules/no-restricted-paths',
                  target: 'src/plugins/foxford/__tests__',
                },
              ],
            },
          ],
        },
      ],
      valid: [],
    })
  })

  it('reports error for ExportAllDeclaration importing from restricted zone', () => {
    ruleTester.run('no-restricted-paths (invalid: export all)', rule, {
      invalid: [
        {
          code: "export * from '../../rules/no-restricted-paths/utils.js'",
          errors: [{ messageId: 'restrictedPath' }],
          filename: path.join(PKG_ROOT, 'src/plugins/foxford/__tests__/rules/sample.js'),
          options: [
            {
              basePath: PKG_ROOT,
              zones: [
                {
                  from: 'src/plugins/foxford/rules/no-restricted-paths',
                  target: 'src/plugins/foxford/__tests__',
                },
              ],
            },
          ],
        },
      ],
      valid: [],
    })
  })

  it('allows import when path matches except list', () => {
    ruleTester.run('no-restricted-paths (valid: except)', rule, {
      invalid: [],
      valid: [
        {
          code: "const x = require('../../rules/no-restricted-paths/utils.js')",
          filename: path.join(PKG_ROOT, 'src/plugins/foxford/__tests__/rules/sample.js'),
          options: [
            {
              basePath: PKG_ROOT,
              zones: [
                {
                  except: ['utils.js'],
                  from: 'src/plugins/foxford/rules/no-restricted-paths',
                  target: 'src/plugins/foxford/__tests__',
                },
              ],
            },
          ],
        },
      ],
    })
  })

  it('reports error when zone targets multiple directories and file matches one', () => {
    ruleTester.run('no-restricted-paths (invalid: multi-target)', rule, {
      invalid: [
        {
          code: "const x = require('../../rules/no-restricted-paths/utils.js')",
          errors: [{ messageId: 'restrictedPath' }],
          filename: path.join(PKG_ROOT, 'src/plugins/foxford/__tests__/rules/sample.js'),
          options: [
            {
              basePath: PKG_ROOT,
              zones: [
                {
                  from: 'src/plugins/foxford/rules/no-restricted-paths',
                  target: ['src/plugins/foxford/__tests__', 'src/layers'],
                },
              ],
            },
          ],
        },
      ],
      valid: [],
    })
  })
})
