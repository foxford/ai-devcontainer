import { globalIgnores } from './layers/ignores.js'
import { imports } from './layers/imports.js'
import { javascript } from './layers/javascript.js'
import { node } from './layers/node.js'
import { prettier } from './layers/prettier.js'
import { react } from './layers/react.js'
import { testing } from './layers/testing.js'
import { typescript } from './layers/typescript.js'
import { createFoxfordPluginConfig } from './plugins/foxford/config.js'

/** @type {import('eslint').Linter.Config[]} */
const config = [
  globalIgnores,
  ...javascript,
  ...typescript,
  ...imports,
  ...react,
  ...node,
  createFoxfordPluginConfig({ react: true }),
  ...testing,
  ...prettier,
]

export default config
