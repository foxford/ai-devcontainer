import { configs as tseslintConfigs } from 'typescript-eslint'

/**
 * TypeScript layer — typescript-eslint strict + Foxford custom rules.
 * Applies to: .ts, .tsx, .mts, .cts files.
 * @type {import('eslint').Linter.Config[]}
 */
const typescript = [
  ...tseslintConfigs.strict,
  {
    files: ['**/*.ts', '**/*.tsx', '**/*.mts', '**/*.cts'],
    languageOptions: {
      parserOptions: {
        project: true,
      },
    },
    name: 'foxford/typescript',
    rules: {
      // From current/languages/typescript.js
      '@typescript-eslint/class-methods-use-this': ['error', { exceptMethods: ['render'] }],
      '@typescript-eslint/consistent-type-exports': 'error',
      '@typescript-eslint/consistent-type-imports': 'error',
      '@typescript-eslint/no-import-type-side-effects': 'error',
      '@typescript-eslint/no-invalid-void-type': ['error', { allowInGenericTypeArguments: true }],
      '@typescript-eslint/no-namespace': ['error', { allowDeclarations: false, allowDefinitionFiles: true }],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', ignoreRestSiblings: true }],
      // From current/languages/rules/eslint.js
      'arrow-parens': 0,
      // Override base rule — handled by @typescript-eslint equivalent
      'class-methods-use-this': 0,
      complexity: ['error', 12],
      'max-lines': ['error', { max: 200, skipComments: true }],
      'max-lines-per-function': ['error', 198],
      'no-irregular-whitespace': ['error', { skipComments: true, skipJSXText: true, skipTemplates: true }],
    },
  },
  {
    files: ['**/*.d.ts'],
    name: 'foxford/typescript/declarations',
    rules: {
      // Declaration files are allowed to have namespace declarations
      '@typescript-eslint/no-namespace': ['error', { allowDeclarations: true, allowDefinitionFiles: true }],
      // Declaration files can be as long as needed
      'max-lines': 'off',
    },
  },
]

export { typescript }
