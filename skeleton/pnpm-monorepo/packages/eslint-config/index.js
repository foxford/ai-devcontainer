import { globalIgnores } from './layers/ignores.js'
import { imports } from './layers/imports.js'
import { javascript } from './layers/javascript.js'
import { prettier } from './layers/prettier.js'
import { testing } from './layers/testing.js'
import { typescript } from './layers/typescript.js'
import { createFoxfordPluginConfig } from './plugins/foxford/config.js'

/** @type {import('eslint').Linter.Config[]} */
const config = [
  globalIgnores,
  ...javascript,
  ...typescript,
  ...imports,
  createFoxfordPluginConfig(),
  ...testing,
  ...prettier,
]

export default config
