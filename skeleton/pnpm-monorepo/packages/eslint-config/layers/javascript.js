import js from '@eslint/js'
import globals from 'globals'

/**
 * JavaScript layer — @eslint/js recommended + Foxford custom rules.
 * Applies to: .js, .mjs, .cjs, .jsx files.
 * @type {import('eslint').Linter.Config[]}
 */
const javascript = [
  js.configs.recommended,
  {
    files: ['**/*.js', '**/*.mjs', '**/*.cjs', '**/*.jsx'],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
        ...globals.es2021,
      },
    },
    name: 'foxford/javascript',
    rules: {
      'arrow-parens': 0,
      'class-methods-use-this': ['error', { exceptMethods: ['render'] }],
      complexity: ['error', 12],
      'max-lines': ['error', { max: 200, skipComments: true }],
      'max-lines-per-function': ['error', 198],
      'no-irregular-whitespace': ['error', { skipComments: true, skipJSXText: true, skipTemplates: true }],
      'no-unused-vars': ['error', { argsIgnorePattern: '^_', ignoreRestSiblings: true }],
    },
  },
]

export { javascript }
