// Evaluation harness, not an ADC runtime test. Reuses an existing toolchain via NODE_PATH.
const { test, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const ts = require('typescript');
require.extensions['.tsx'] = (module, file) => module._compile(ts.transpileModule(
  fs.readFileSync(file, 'utf8'), { compilerOptions: { jsx: ts.JsxEmit.React,
    module: ts.ModuleKind.CommonJS, esModuleInterop: true } }).outputText, file);
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', { pretendToBeVisual: true });
global.window = dom.window; global.document = dom.window.document; global.navigator = dom.window.navigator;
global.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
global.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
global.MessageChannel = undefined;
after(() => dom.window.close());
const React = require('react');
const ReactDOM = require('react-dom');
const { act } = require('react-dom/test-utils');
const { createReactiveValue } = require('@foxford/vm');
const { make, setDoubled, finish } = require('./after/model.cjs');
function mount(Component, props) {
  const root = document.createElement('div'); document.body.appendChild(root);
  const render = next => act(() => { ReactDOM.render(React.createElement(Component, next), root); });
  render(props);
  return { root, render, close() { act(() => { ReactDOM.unmountComponentAtNode(root); }); root.remove(); } };
}
test('mapper: rerenders, dependency refresh and source updates', () => {
  const { Total } = require('./after/client/Total.tsx');
  const vm = make(); const app = mount(Total, { vm, factor: 2 });
  assert.equal(app.root.textContent, '4');
  app.render({ vm, factor: 2 }); assert.equal(app.root.textContent, '4');
  app.render({ vm, factor: 3 }); assert.equal(app.root.textContent, '6');
  act(() => { vm.state.count.set(4); }); assert.equal(app.root.textContent, '12');
  app.close(); vm.destroy();
  // Measure the underlying cache, not React Proxy identity.
  const raw = createReactiveValue(2); let calls = 0;
  const mapper = n => { calls++; return n * 2; };
  const mapped = raw.map(mapper); const initial = calls;
  assert.equal(raw.map(mapper), mapped); assert.equal(calls, initial);
  raw.set(3); assert.equal(mapped.get(), 6); raw.destroy();
});
test('signal updates text; use updates branch and boolean attribute', () => {
  const { Text } = require('./after/client/Text.tsx');
  const { Controls } = require('./after/client/Controls.tsx');
  const vm = make(); const text = mount(Text, { vm }); const controls = mount(Controls, { vm });
  act(() => { vm.state.count.set(8); }); assert.equal(text.root.textContent, '8');
  assert.equal(controls.root.querySelector('button').disabled, false);
  act(() => { vm.state.busy.set(true); }); assert.equal(controls.root.querySelector('button').disabled, true);
  act(() => { vm.state.visible.set(false); }); assert.equal(controls.root.querySelector('button'), null);
  act(() => { vm.state.visible.set(true); }); assert.equal(controls.root.querySelector('button').disabled, true);
  text.close(); controls.close(); vm.destroy();
});
test('readonly action updates source, derived result and subscribers', () => {
  const vm = make(); const derived = vm.state.count.map(n => n * 2); const seen = [];
  const unsubscribe = derived.subscribe(n => seen.push(n));
  assert.throws(() => derived.set(10), /mapped/i);
  setDoubled(vm, 10); assert.equal(vm.state.count.get(), 5); assert.equal(derived.get(), 10);
  assert.equal(seen.at(-1), 10); unsubscribe(); vm.destroy();
});
test('owner destroy calls onDestroy once and makes state unusable', () => {
  let destroyed = 0; const vm = make(() => destroyed++);
  finish(vm); finish(vm); assert.equal(vm.isDestroyed, true); assert.equal(destroyed, 1);
  assert.throws(() => vm.state.get(), /destroyed/i);
});
test('auto lifecycle: shared VM survives without permission; enabled unmount destroys', () => {
  const { Owner } = require('./after/client/Owner.tsx');
  for (const enabled of [false, true]) {
    let destroyed = 0; const vm = make(() => destroyed++); const app = mount(Owner, { vm });
    if (enabled) act(() => app.root.querySelector('button').click());
    assert.equal(vm.isDestroyed, false); app.close();
    assert.equal(vm.isDestroyed, enabled); assert.equal(destroyed, enabled ? 1 : 0);
    vm.destroy();
  }
  const vm = make(); const app = mount(Owner, { vm });
  vm.enableAutoDestroy(); vm.disableAutoDestroy(); app.close();
  assert.equal(vm.isDestroyed, false); vm.destroy();
});
test('2.0.2 caveat: rerender after permission cleans up before unmount', () => {
  const { Owner } = require('./after/client/Owner.tsx'); const vm = make();
  const app = mount(Owner, { vm }); vm.enableAutoDestroy(); app.render({ vm });
  assert.equal(vm.isDestroyed, true); app.close();
});
test('negative fixtures expose stale closure, wrong branch, mapped write and partial cleanup', () => {
  const { Total } = require('./before/client/Total.tsx');
  const { Controls } = require('./before/client/Controls.tsx'); const before = require('./before/model.cjs');
  const vm = before.make(); const app = mount(Total, { vm, factor: 2 });
  app.render({ vm, factor: 3 }); assert.equal(app.root.textContent, '4'); app.close();
  vm.state.visible.set(false); const controls = mount(Controls, { vm });
  assert.notEqual(controls.root.querySelector('button'), null); controls.close();
  assert.throws(() => before.setDoubled(vm, 10), /mapped/i);
  before.finish(vm); assert.equal(vm.isDestroyed, false); vm.destroy();
});

test('pure mapper output remains reactive after repeated renders', () => {
  const { Pure } = require('./after/client/Pure.tsx');
  const vm = make(); const app = mount(Pure, { vm });
  app.render({ vm }); assert.equal(app.root.textContent, '4');
  act(() => { vm.state.count.set(3); }); assert.equal(app.root.textContent, '6');
  app.close(); vm.destroy();
});
