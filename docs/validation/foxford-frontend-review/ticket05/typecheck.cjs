const ts = require('typescript');
const path = require('node:path');
const assert = require('node:assert/strict');
const modules = process.env.NODE_PATH;
assert(modules, 'Set NODE_PATH to an existing compatible toolchain');
const options = { strict: true, noEmit: true, skipLibCheck: true, jsx: ts.JsxEmit.React,
  esModuleInterop: true, target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS,
  moduleResolution: ts.ModuleResolutionKind.NodeJs, types: [], baseUrl: __dirname,
  paths: { '@foxford/vm': [path.join(modules, '@foxford/vm/index.d.ts')],
    react: [path.join(modules, '@types/react/index.d.ts')],
    'react-intl': [path.join(modules, 'react-intl')] } };
const program = ts.createProgram(['Screen', 'New'].map(n => path.join(__dirname, 'after/client', n + '.tsx')), options);
const diagnostics = ts.getPreEmitDiagnostics(program);
assert.equal(diagnostics.length, 0, ts.formatDiagnosticsWithColorAndContext(diagnostics, {
  getCanonicalFileName: p => p, getCurrentDirectory: () => __dirname, getNewLine: () => '\n' }));
console.log('after: 0 diagnostics, strict, real VM/React/intl declarations; no any/as bypass');
