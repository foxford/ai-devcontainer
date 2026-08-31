import globals from 'globals'

/**
 * Testing layer — relaxed rules for test and spec files.
 * Applies to: *.spec.*, *.test.* files.
 * @type {import('eslint').Linter.Config[]}
 */
const testing = [
  {
    files: [
      '**/*.spec.js',
      '**/*.spec.mjs',
      '**/*.spec.ts',
      '**/*.spec.tsx',
      '**/*.spec.mts',
      '**/*.test.js',
      '**/*.test.mjs',
      '**/*.test.ts',
      '**/*.test.tsx',
      '**/*.test.mts',
    ],
    languageOptions: {
      globals: {
        ...globals.vitest,
      },
    },
    name: 'foxford/testing',
    rules: {
      '@typescript-eslint/no-non-null-assertion': 'off',
      'max-lines': ['error', { max: 300, skipComments: true }],
      'max-lines-per-function': 'off',
    },
  },
]

export { testing }
