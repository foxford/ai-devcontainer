import { createTypeScriptImportResolver } from 'eslint-import-resolver-typescript'
import * as importX from 'eslint-plugin-import-x'

/**
 * Import layer — eslint-plugin-import-x with resolver and ordering rules.
 * Applies to: all JS and TS files.
 * @type {import('eslint').Linter.Config[]}
 */
const imports = [
  importX.flatConfigs.recommended,
  importX.flatConfigs.typescript,
  {
    name: 'foxford/imports',
    rules: {
      /**
       * Rules that don't work well with TypeScript parser (disabled):
       * import-x/no-cycle, import-x/no-named-as-default, import-x/no-unused-modules
       * @see https://github.com/typescript-eslint/typescript-eslint/blob/f335c504bcf75623d2d671e2e784b047e5e186b9/docs/getting-started/linting/FAQ.md#eslint-plugin-import
       */
      'import-x/consistent-type-specifier-style': ['error', 'prefer-top-level'],
      'import-x/no-deprecated': 2,
      'import-x/no-extraneous-dependencies': 'off',
      'import-x/no-unresolved': 2,
      'import-x/no-useless-path-segments': 1,
      'import-x/order': [
        'error',
        {
          alphabetize: {
            caseInsensitive: false,
            order: 'asc',
          },
          distinctGroup: false,
          groups: ['builtin', 'external', 'internal', ['parent', 'sibling', 'index'], 'type'],
          'newlines-between': 'always',
          pathGroups: [
            {
              group: 'external',
              pattern: 'node_modules',
              position: 'before',
            },
            {
              group: 'external',
              pattern: '@foxford/**',
              position: 'after',
            },
            {
              group: 'internal',
              pattern: `~/**`,
              position: 'after',
            },
            {
              group: 'sibling',
              pattern: '{.,..}/**/*.{sass,scss,css}',
              position: 'after',
            },
          ],
          pathGroupsExcludedImportTypes: ['type'],
        },
      ],
      'import-x/prefer-default-export': 'off',
    },
    settings: {
      'import-x/resolver-next': [
        createTypeScriptImportResolver({
          alwaysTryTypes: true,
          extensions: ['.ts', '.tsx', '.d.ts', '.js', '.jsx', '.mjs', '.cjs', '.json'],
          project: [
            'tsconfig?(.*).json',
            'jsconfig?(.*).json',
            '**/packages/*/tsconfig?(.*).json',
            '**/packages/*/jsconfig?(.*).json',
            '**/*/tsconfig?(.*).json',
            '*/tsconfig?(.*).json',
          ],
        }),
        importX.createNodeResolver(),
      ],
    },
  },
  {
    // TypeScript's compiler already validates import resolution for .ts files
    // Disabling here prevents false positives for peer/optional deps not installed locally
    files: ['**/*.ts', '**/*.tsx', '**/*.mts', '**/*.cts'],
    name: 'foxford/imports/typescript',
    rules: {
      'import-x/no-unresolved': 'off',
    },
  },
]

export { imports }
