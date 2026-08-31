// Rule files are CommonJS modules — imported via ESM interop
import importTypesIndependently from './rules/import-types-independently.js'
import noReactDefaultImport from './rules/no-react-default-import.js'
import noRestrictedPaths from './rules/no-restricted-paths/index.js'
import sortKeys from './rules/sort-keys.js'

/**
 * ESLint plugin with Foxford custom rules.
 * Compatible with ESLint 9 flat config format.
 *
 * Usage in eslint.config.ts:
 * ```ts
 * import { foxfordPlugin } from '@foxford/eslint-config/plugins/foxford'
 *
 * export default [
 *   {
 *     plugins: { foxford: foxfordPlugin },
 *     rules: {
 *       'foxford/sort-keys': 'error',
 *     },
 *   },
 * ]
 * ```
 *
 * @type {import('eslint').ESLint.Plugin}
 */
const foxfordPlugin = {
  name: 'foxford',
  rules: {
    'import-types-independently': importTypesIndependently,
    'no-react-default-import': noReactDefaultImport,
    'no-restricted-paths': noRestrictedPaths,
    'sort-keys': sortKeys,
  },
}

export { foxfordPlugin }
