/**
 * @fileoverview Rule to require import of types independently
 */

import { getImportText, getImportTypeText, getSpecifierName } from '../utils/import-utils.js'

/** @type {import('eslint').Rule.RuleModule} */
const rule = {
  meta: {
    docs: {
      category: 'Stylistic Issues',
      description: 'Require import of types independently',
      recommended: false,
    },
    fixable: 'code',
    messages: {
      separateTypeImport: "Import of type '{{type}}' should be declared independently (on different lines)",
    },
    schema: [],
    type: 'suggestion',
  },

  create(context) {
    return {
      ImportDeclaration(node) {
        if (node.importKind === 'type') {
          return
        }

        const specifiers = node.specifiers
        const failedNode = specifiers.find((item) => item.importKind === 'type')

        if (failedNode) {
          context.report({
            data: {
              type: failedNode.imported.name,
            },
            fix(fixer) {
              const fixes = []
              const { typeSpecifiers, valuesSpecifiers, defaultSpecifier } = specifiers.reduce(
                (acc, item) => {
                  if (item.importKind === 'type') {
                    acc.typeSpecifiers.push(item)
                  } else if (item.type !== 'ImportDefaultSpecifier') {
                    acc.valuesSpecifiers.push(item)
                  } else {
                    acc.defaultSpecifier = item
                  }

                  return acc
                },
                {
                  defaultSpecifier: null,
                  typeSpecifiers: [],
                  valuesSpecifiers: [],
                }
              )

              const types = typeSpecifiers.map(getSpecifierName).join(', ')
              const values = valuesSpecifiers.map(getSpecifierName).join(', ')
              const source = node.source.raw

              fixes.push(fixer.remove(node))

              if (defaultSpecifier) {
                if (specifiers.length - 1 === typeSpecifiers.length) {
                  fixes.push(
                    fixer.insertTextAfter(
                      node,
                      getImportText.lineFeedEnded({ defaultValue: defaultSpecifier.local.name, source })
                    )
                  )
                  fixes.push(fixer.insertTextAfter(node, getImportTypeText({ source, types })))
                } else {
                  fixes.push(
                    fixer.insertTextAfter(
                      node,
                      getImportText.lineFeedEnded({ defaultValue: defaultSpecifier.local.name, source, values })
                    )
                  )
                  fixes.push(fixer.insertTextAfter(node, getImportTypeText({ source, types })))
                }
              } else if (specifiers.length === typeSpecifiers.length) {
                fixes.push(fixer.insertTextAfter(node, getImportTypeText({ source, types })))
              } else {
                fixes.push(fixer.insertTextAfter(node, getImportText.lineFeedEnded({ source, values })))
                fixes.push(fixer.insertTextAfter(node, getImportTypeText({ source, types })))
              }

              return fixes
            },
            messageId: 'separateTypeImport',
            node: failedNode,
          })
        }
      },
    }
  },
}

export default rule
