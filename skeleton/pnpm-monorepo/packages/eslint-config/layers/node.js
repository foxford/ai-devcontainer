import nPlugin from 'eslint-plugin-n'
import globals from 'globals'

/** @type {import('eslint').Linter.Config} */
const nFlatRecommended = nPlugin.configs['flat/recommended']

/**
 * Node.js layer — eslint-plugin-n rules + node globals.
 * Adds Node.js-specific rules and globals.
 * @type {import('eslint').Linter.Config[]}
 */
const node = [
  nFlatRecommended,
  {
    languageOptions: {
      globals: {
        ...globals.node,
      },
    },
    name: 'foxford/node',
    rules: {
      // eslint-plugin-n rules — disable overly strict checks for monorepo setup
      'n/no-extraneous-require': 'off',
      'n/no-unpublished-import': 0,
      'n/no-unpublished-require': 0,
      'n/no-unsupported-features/es-builtins': 0,
      'n/no-unsupported-features/es-syntax': 0,
      'n/no-unsupported-features/node-builtins': 0,
      // core rule overrides for node context
      'no-await-in-loop': 'off',
      'no-console': 'off',
      'no-lonely-if': 'off',
      'no-param-reassign': ['error', { props: false }],
      'no-restricted-syntax': 'off',
    },
  },
  {
    // TypeScript files — n plugin uses Node.js resolution which doesn't understand TS modules.
    // TypeScript compiler already validates imports, so these checks are false positives.
    files: ['**/*.ts', '**/*.tsx', '**/*.mts', '**/*.cts'],
    name: 'foxford/node/typescript',
    rules: {
      'n/hashbang': 'off',
      'n/no-missing-import': 'off',
      'n/no-missing-require': 'off',
      'n/no-unpublished-import': 'off',
    },
  },
]

export { node }
