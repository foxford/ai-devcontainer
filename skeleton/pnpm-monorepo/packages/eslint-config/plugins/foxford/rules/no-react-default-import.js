/**
 * @fileoverview Rule to enforce namespace React import instead of default import
 */

/** @type {import('eslint').Rule.RuleModule} */
const rule = {
  create(context) {
    return {
      ImportDeclaration(node) {
        if (
          node.source.value === 'react' &&
          // Only look for value imports of react itself, not `import type`
          node.importKind === 'value'
        ) {
          // If there's only one default import, we can make an autofix
          if (node.specifiers.length === 1 && node.specifiers[0].type === 'ImportDefaultSpecifier') {
            context.report({
              fix(fixer) {
                return fixer.replaceText(node.specifiers[0], '* as React')
              },
              messageId: 'noDefaultImport',
              node,
            })
          }
          // Mixing default and named imports, cannot autofix
          else if (node.specifiers.some((specifier) => specifier.type === 'ImportDefaultSpecifier')) {
            context.report({
              messageId: 'noDefaultImportMixed',
              node,
            })
          }
        }
      },
    }
  },

  meta: {
    fixable: 'code',
    messages: {
      noDefaultImport: 'Do not use the default React import, use named or type import instead.',
      noDefaultImportMixed: 'Do not use the default React import, use named or namespace import instead.',
    },
    schema: [],
    type: 'suggestion',
  },
}

export default rule
