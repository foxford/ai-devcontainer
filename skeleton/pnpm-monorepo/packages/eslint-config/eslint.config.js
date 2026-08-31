import config from './node.js'

/** @type {import('eslint').Linter.Config[]} */
const eslintConfig = [
  // Ignore generated and dependency directories
  {
    ignores: ['build/**', 'node_modules/**'],
  },
  ...config,
  // KNOWN LIMITATION: Some resolver-based rules are disabled for self-linting.
  // This package has no tsconfig.json (no TS sources), so the @helljs resolver
  // cannot perform type-aware resolution. Import correctness is enforced by
  // the Vitest test runner instead.
  {
    rules: {
      'import-x/default': 'off',
      'import-x/namespace': 'off',
      'import-x/no-deprecated': 'off',
      'import-x/no-unresolved': 'off',
      'n/no-missing-import': 'off',
      'n/no-unpublished-import': 'off',
    },
  },
  // Plugin rule files — disable complexity/max-lines for complex implementations
  {
    files: ['plugins/foxford/rules/**/*.js', 'plugins/foxford/utils/**/*.js'],
    rules: {
      complexity: 'off',
      'foxford/sort-keys': 'off',
      'max-lines': 'off',
    },
  },
  // Plugin rule tests — disable max-lines for large test suites
  {
    files: ['plugins/foxford/rules/tests/**/*.ts', 'plugins/foxford/rules/**/__tests__/**/*.ts'],
    rules: {
      'max-lines': 'off',
    },
  },
]

export default eslintConfig
