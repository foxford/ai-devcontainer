/**
 * @fileoverview Utils for generating import text during AST fixes.
 */

const getImportTypeText = ({ types, source }) => `import type { ${types} } from ${source}`

const getImportText = ({ defaultValue, values, source }) => {
  const importingValues =
    defaultValue && values ? `${defaultValue}, { ${values} }` : values ? `{ ${values} }` : defaultValue

  return `import ${importingValues} from ${source}`
}

function lineFeedEnded(args) {
  if (typeof this !== 'function') {
    throw new TypeError('This method must be applied only to functions which return strings')
  }

  return `${this(args)}\n`
}

function lineFeedStarted(args) {
  if (typeof this !== 'function') {
    throw new TypeError('This method must be applied only to functions which return strings')
  }

  return `\n${this(args)}`
}

getImportTypeText.lineFeedEnded = lineFeedEnded
getImportTypeText.lineFeedStarted = lineFeedStarted

getImportText.lineFeedEnded = lineFeedEnded
getImportText.lineFeedStarted = lineFeedStarted

function getSpecifierName(specifier) {
  if (specifier.imported.name === specifier.local.name) {
    return specifier.imported.name
  }

  return `${specifier.imported.name} as ${specifier.local.name}`
}

export { getImportText, getImportTypeText, getSpecifierName }
