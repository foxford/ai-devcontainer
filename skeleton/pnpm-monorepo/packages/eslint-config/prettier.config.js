export default {
  arrowParens: 'always',
  bracketSpacing: true,
  disableLanguages: ['sass'],
  jsxBracketSameLine: true,
  jsxSingleQuote: true,
  overrides: [
    {
      files: ['*.js', '*.js.flow'],
      options: { parser: 'flow' },
    },
  ],
  printWidth: 120,
  semi: false,
  singleQuote: true,
  tabWidth: 2,
  trailingComma: 'es5',
}
