import * as jsoncParser from 'jsonc-eslint-parser'
import config from '@foxford/eslint-config/node'

export default [
  { ignores: ['build/**'] },
  ...config,
  // automation uses composite tsconfig (files/include: []) — override project to explicit refs
  {
    files: ['**/*.ts', '**/*.tsx', '**/*.mts', '**/*.cts'],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.lib.json', './tsconfig.spec.json', './tsconfig.json'],
      },
    },
  },
  {
    files: ['package.json', 'project.json'],
    languageOptions: { parser: jsoncParser },
  },
]
