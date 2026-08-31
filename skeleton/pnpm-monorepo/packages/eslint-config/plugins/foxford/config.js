import { foxfordPlugin } from './index.js'

/**
 * @param {{ react?: boolean }} [options]
 * @returns {import('eslint').Linter.Config}
 */
export function createFoxfordPluginConfig(options) {
  return {
    name: 'foxford/plugin',
    plugins: { foxford: foxfordPlugin },
    rules: {
      'foxford/import-types-independently': 'error',
      'foxford/no-restricted-paths': 'error',
      'foxford/sort-keys': ['warn', 'asc', { caseSensitive: true, natural: true }],
      ...(options?.react ? { 'foxford/no-react-default-import': 'error' } : {}),
    },
  }
}
