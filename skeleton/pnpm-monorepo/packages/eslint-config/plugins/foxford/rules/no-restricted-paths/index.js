/**
 * @fileoverview Rule to restrict imports by zone boundaries
 */

import { ImportValidator } from './import-validator.js'

/** @type {import('eslint').Rule.RuleModule} */
const rule = {
  create(context) {
    const options = context.options[0] || {}
    const basePath = options.basePath || process.cwd()
    // ESLint 9: use context.physicalFilename / context.filename
    const currentFilename = context.physicalFilename ?? context.filename
    const parentPackagePrefix = options.parentPackagePrefix
    const parentPackagePostfix = options.parentPackagePostfix
    const exclude = options.exclude || []

    const validator = new ImportValidator({
      basePath,
      context,
      currentFilename,
      exclude,
      parentPackagePostfix,
      parentPackagePrefix,
      zones: options.zones || [],
    })

    /**
     * Visit a source string literal node (import or require argument).
     * @param {import('eslint').Rule.Node} sourceNode
     */
    const visitSource = (sourceNode) => {
      try {
        validator.dispatch(sourceNode.value)
      } catch (e) {
        context.report({
          data: { message: e.message },
          messageId: 'restrictedPath',
          node: sourceNode,
        })
      }
    }

    return {
      CallExpression(node) {
        if (
          node.callee.type === 'Identifier' &&
          node.callee.name === 'require' &&
          node.arguments.length === 1 &&
          node.arguments[0].type === 'Literal'
        ) {
          visitSource(node.arguments[0])
        }
      },
      ExportAllDeclaration(node) {
        visitSource(node.source)
      },
      ExportNamedDeclaration(node) {
        if (node.source) {
          visitSource(node.source)
        }
      },
      ImportDeclaration(node) {
        visitSource(node.source)
      },
    }
  },

  meta: {
    docs: {
      category: 'Import Errors',
      description: 'Restrict imports by zone boundaries',
      recommended: false,
    },
    messages: {
      restrictedPath: '{{message}}',
    },
    schema: [
      {
        additionalProperties: false,
        properties: {
          basePath: { type: 'string' },
          exclude: {
            items: { type: 'string' },
            type: 'array',
          },
          parentPackagePostfix: { type: 'string' },
          parentPackagePrefix: { type: 'string' },
          zones: {
            items: {
              additionalProperties: false,
              properties: {
                except: {
                  items: { type: 'string' },
                  type: 'array',
                },
                from: { type: 'string' },
                message: { type: 'string' },
                target: {
                  oneOf: [{ type: 'string' }, { items: { type: 'string' }, type: 'array' }],
                },
              },
              required: ['target', 'from'],
              type: 'object',
            },
            type: 'array',
          },
        },
        type: 'object',
      },
    ],
    type: 'problem',
  },
}

export default rule
