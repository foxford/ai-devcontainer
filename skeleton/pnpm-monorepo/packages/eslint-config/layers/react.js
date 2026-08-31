import jsxA11yPlugin from 'eslint-plugin-jsx-a11y'
import reactPlugin from 'eslint-plugin-react'
import reactHooksPlugin from 'eslint-plugin-react-hooks'

const jsxA11yFlatRecommended = jsxA11yPlugin.flatConfigs.recommended

if (!reactPlugin.configs.flat?.recommended) {
  throw new Error(
    'eslint-plugin-react does not expose flat.recommended. Upgrade to a version that supports ESLint 9 flat config.'
  )
}
/** @type {import('eslint').Linter.Config} */
const reactFlatRecommended = reactPlugin.configs.flat.recommended

if (!reactHooksPlugin.configs.flat?.['recommended-latest'] && !reactHooksPlugin.configs.flat?.recommended) {
  throw new Error(
    'eslint-plugin-react-hooks does not expose flat config. Upgrade to a version that supports ESLint 9 flat config.'
  )
}
const reactHooksKey =
  'recommended-latest' in (reactHooksPlugin.configs.flat ?? {}) ? 'recommended-latest' : 'recommended'
/** @type {import('eslint').Linter.Config} */
const reactHooksFlatRecommended = reactHooksPlugin.configs.flat?.[reactHooksKey] ?? {}

/**
 * React layer — react, react-hooks, jsx-a11y rules.
 * Applies to: .jsx, .tsx files (and .js/.ts with JSX).
 * @type {import('eslint').Linter.Config[]}
 */
const react = [
  reactFlatRecommended,
  reactHooksFlatRecommended,
  jsxA11yFlatRecommended,
  {
    name: 'foxford/react',
    rules: {
      // jsx-a11y
      'jsx-a11y/alt-text': [
        1,
        {
          area: ['Area'],
          elements: ['img', 'object', 'area', 'input[type="image"]'],
          img: ['Image'],
          'input[type="image"]': ['InputImage'],
          object: ['Object'],
        },
      ],
      // react
      'jsx-quotes': ['error', 'prefer-single'],
      'react/destructuring-assignment': 0,
      'react/display-name': 2,
      'react/forbid-component-props': [
        2,
        {
          forbid: [
            {
              message: 'Do not use dangerouslySetInnerHTML',
              propName: 'dangerouslySetInnerHTML',
            },
          ],
        },
      ],
      'react/forbid-prop-types': ['error', { forbid: ['any'] }],
      'react/function-component-definition': 0,
      'react/jsx-closing-tag-location': 0,
      'react/jsx-curly-brace-presence': 'off',
      'react/jsx-curly-newline': 0,
      'react/jsx-filename-extension': 'off',
      'react/jsx-indent': 0,
      'react/jsx-indent-props': 0,
      'react/jsx-no-duplicate-props': [2, { ignoreCase: false }],
      'react/jsx-no-undef': [2, { allowGlobals: true }],
      'react/jsx-no-useless-fragment': 0,
      'react/jsx-pascal-case': [2, { allowAllCaps: true, allowNamespace: true }],
      'react/jsx-props-no-spreading': 0,
      'react/jsx-uses-react': 'off',
      'react/jsx-wrap-multilines': 0,
      'react/no-array-index-key': 'off',
      'react/no-danger': 2,
      'react/no-unstable-nested-components': ['error', { allowAsProps: true }],
      'react/no-unused-class-component-methods': 2,
      'react/no-unused-prop-types': 0,
      'react/prefer-es6-class': 0,
      'react/prefer-exact-props': 2,
      'react/prefer-stateless-function': ['error', { ignorePureComponents: true }],
      'react/prop-types': [2, { skipUndeclared: true }],
      'react/react-in-jsx-scope': 'off',
      'react/require-default-props': 0,
      'react/state-in-constructor': 0,
      'react/static-property-placement': 0,
      // react-hooks
      'react-hooks/exhaustive-deps': 'warn',
      'react-hooks/rules-of-hooks': 'error',
    },
    settings: {
      react: {
        defaultVersion: '17',
        version: 'detect',
      },
    },
  },
]

export { react }
