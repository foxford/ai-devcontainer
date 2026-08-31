/**
 * Global ignores — files and directories excluded from linting across all packages.
 * @type {import('eslint').Linter.Config}
 */
const globalIgnores = {
  ignores: ['**/build/**', '**/dist/**', '**/out/**', '**/node_modules/**', '**/docs/**', '**/*.bundled_*.mjs'],
  name: 'foxford/global-ignores',
}

export { globalIgnores }
