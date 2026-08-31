/**
 * @fileoverview Rule to require object keys to be sorted unless called in effector methods
 */

import naturalCompare from 'natural-compare'

/**
 * Gets the static property name of a Property or MemberExpression node.
 * Returns null if the name cannot be statically determined.
 *
 * @param {import('eslint').Rule.Node} node
 * @returns {string|null}
 */
function getStaticPropertyName(node) {
  let prop

  switch (node && node.type) {
    case 'Property':
    case 'MethodDefinition':
      prop = node.key
      break

    case 'MemberExpression':
      prop = node.property
      break

    // no default
  }

  switch (prop && prop.type) {
    case 'Literal':
      return String(prop.value)

    case 'TemplateLiteral':
      if (prop.expressions.length === 0 && prop.quasis.length === 1) {
        return prop.quasis[0].value.cooked
      }
      break

    case 'Identifier':
      if (!node.computed) {
        return prop.name
      }
      break

    // no default
  }

  return null
}

/**
 * Gets the property name of the given `Property` node.
 *
 * @param {import('eslint').Rule.Node} node - The `Property` node to get.
 * @returns {string|null} The property name or null.
 */
function getPropertyName(node) {
  const staticName = getStaticPropertyName(node)

  if (staticName !== null) {
    return staticName
  }

  return node.key.name || null
}

/**
 * Functions which check that the given 2 names are in specific order.
 */
const isValidOrders = {
  asc(a, b) {
    return a <= b
  },
  ascI(a, b) {
    return a.toLowerCase() <= b.toLowerCase()
  },
  ascIN(a, b) {
    return naturalCompare(a.toLowerCase(), b.toLowerCase()) <= 0
  },
  ascN(a, b) {
    return naturalCompare(a, b) <= 0
  },
  desc(a, b) {
    return isValidOrders.asc(b, a)
  },
  descI(a, b) {
    return isValidOrders.ascI(b, a)
  },
  descIN(a, b) {
    return isValidOrders.ascIN(b, a)
  },
  descN(a, b) {
    return isValidOrders.ascN(b, a)
  },
}

const effectorMethods = ['sample', 'guard', 'forward', 'attach', 'split']
const patronumMethods = ['condition', 'debounce', 'snapshot', 'or', 'not']

/** @type {import('eslint').Rule.RuleModule} */
const rule = {
  create(context) {
    const order = context.options[0] || 'asc'
    const options = context.options[1]
    const insensitive = (options && options.caseSensitive) === false
    const natual = Boolean(options && options.natural)
    const isValidOrder = isValidOrders[order + (insensitive ? 'I' : '') + (natual ? 'N' : '')]
    const exceptions = [...effectorMethods, ...patronumMethods]

    // The stack to save the previous property's name for each object literals.
    let stack = null

    const SpreadElement = (node) => {
      if (node.parent.type === 'ObjectExpression') {
        stack.prevName = null
      }
    }

    return {
      ExperimentalSpreadProperty: SpreadElement,

      ObjectExpression() {
        stack = {
          prevName: null,
          prevNode: null,
          upper: stack,
        }
      },

      'ObjectExpression:exit'() {
        stack = stack.upper
      },

      Property(node) {
        if (node.parent.type === 'ObjectPattern') {
          return
        }

        if (
          node.parent &&
          node.parent.parent &&
          node.parent.parent.callee &&
          node.parent.parent.callee.name &&
          exceptions.includes(node.parent.parent.callee.name)
        ) {
          return
        }

        const prevName = stack.prevName
        const prevNode = stack.prevNode
        const thisName = getPropertyName(node)

        if (thisName !== null) {
          stack.prevName = thisName
          stack.prevNode = node || prevNode
        }

        if (prevName === null || thisName === null) {
          return
        }

        if (!isValidOrder(prevName, thisName)) {
          context.report({
            data: {
              insensitive: insensitive ? 'insensitive ' : '',
              natual: natual ? 'natural ' : '',
              order,
              prevName,
              thisName,
            },
            fix(fixer) {
              const fixes = []
              const sourceCode = context.sourceCode

              const moveProperty = (fromNode, toNode) => {
                const prevText = sourceCode.getText(fromNode)
                const thisComments = sourceCode.getCommentsBefore(fromNode)

                for (const thisComment of thisComments) {
                  fixes.push(fixer.insertTextBefore(toNode, `${sourceCode.getText(thisComment)}\n`))
                  fixes.push(fixer.remove(thisComment))
                }

                fixes.push(fixer.replaceText(toNode, prevText))
              }

              moveProperty(node, prevNode)
              moveProperty(prevNode, node)

              return fixes
            },
            loc: node.key.loc,
            messageId: 'sortKeys',
            node,
          })
        }
      },

      SpreadElement,
    }
  },

  meta: {
    docs: {
      category: 'Stylistic Issues',
      description: 'require object keys to be sorted',
      recommended: false,
      url: 'https://github.com/leo-buneev/eslint-plugin-sort-keys-fix',
    },
    fixable: 'code',
    messages: {
      sortKeys:
        "Expected object keys to be in {{natual}}{{insensitive}}{{order}}ending order. '{{thisName}}' should be before '{{prevName}}'.",
    },
    schema: [
      {
        enum: ['asc', 'desc'],
      },
      {
        additionalProperties: false,
        properties: {
          caseSensitive: {
            type: 'boolean',
          },
          natural: {
            type: 'boolean',
          },
        },
        type: 'object',
      },
    ],
    type: 'suggestion',
  },
}

export default rule
