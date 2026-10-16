// Reuse existing TypeScript/React toolchain. No dependency installation.
const ts = require('typescript');
const path = require('node:path');
const assert = require('node:assert/strict');
const modules = process.env.NODE_PATH;
assert(modules, 'Set NODE_PATH to an existing compatible node_modules');
const options = { strict: true, noEmit: true, skipLibCheck: true, jsx: ts.JsxEmit.React,
  esModuleInterop: true, target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS,
  moduleResolution: ts.ModuleResolutionKind.NodeJs, types: [],
  baseUrl: __dirname, paths: { '@foxford/vm': [path.join(modules, '@foxford/vm/index.d.ts')],
    react: [path.join(modules, '@types/react/index.d.ts')] } };
for (const variant of ['before', 'after']) {
  const files = ['Text', 'Total', 'Controls', 'Owner', 'Pure'].map(n => path.join(__dirname, variant, 'client', n + '.tsx'));
  const program = ts.createProgram(files, options);
  const diagnostics = ts.getPreEmitDiagnostics(program);
  if (variant === 'after') assert.equal(diagnostics.length, 0, ts.formatDiagnosticsWithColorAndContext(diagnostics, {
    getCanonicalFileName: p => p, getCurrentDirectory: () => __dirname, getNewLine: () => '\n' }));
  else assert(diagnostics.some(d => d.code === 2322), 'before: signal cannot be assigned to disabled');
  console.log(variant + ': ' + diagnostics.length + ' diagnostics (expected invalid before, clean after)');
}
const file = path.join(__dirname, 'cases/ambiguous/Write.ts');
const diagnostics = ts.getPreEmitDiagnostics(ts.createProgram([file], options));
assert(diagnostics.some(d => d.code === 2339 && ts.flattenDiagnosticMessageText(d.messageText, ' ').includes('set')));
assert.equal(diagnostics.length, 1);
console.log('readonly: set rejected by actual MappedState declaration');
