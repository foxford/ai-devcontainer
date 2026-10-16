// Reuse an existing React 17 / react-intl / TypeScript / jsdom toolchain via NODE_PATH.
const { test, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const ts = require('typescript');
require.extensions['.tsx'] = require.extensions['.ts'] = (module, file) => {
  module._compile(ts.transpileModule(fs.readFileSync(file, 'utf8'), { compilerOptions: {
    jsx: ts.JsxEmit.React, module: ts.ModuleKind.CommonJS, esModuleInterop: true,
  } }).outputText, file);
};
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', { pretendToBeVisual: true });
global.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
global.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
after(() => dom.window.close());
global.window = dom.window; global.document = dom.window.document; global.navigator = dom.window.navigator;
// React 17 scheduler otherwise keeps Node's MessageChannel ports alive.
global.MessageChannel = undefined;
const React = require('react');
const ReactDOM = require('react-dom');
const { act } = require('react-dom/test-utils');
const { IntlProvider } = require('react-intl');

for (const variant of ['before', 'after']) {
  test(variant + ': combined imports, extraction, i18n and VM subscriptions', () => {
    const { Screen } = require('./' + variant + '/client/Screen.tsx');
    const { New } = require('./' + variant + '/client/New.tsx');
    const { make, setDoubled, finish } = require('./' + variant + '/client/model.cjs');
    let destroyed = 0;
    const vm = make(() => destroyed++), root = document.createElement('div'); document.body.append(root);
    const selected = [], a = { id: 'a', name: 'Alpha' }, b = { id: 'b', name: 'Beta' };
    const render = (items, factor = 2, locale = 'en') => act(() => { ReactDOM.render(
      React.createElement(IntlProvider, { locale, messages: locale === 'fr' ? { heading: 'Bonjour {name}', search: 'Chercher', new: 'Nouveau' } : {} },
        React.createElement(React.Fragment, null, React.createElement(Screen, { vm, factor, items, onSelect: id => selected.push(id) }), React.createElement(New))), root); });
    render([a, b]);
    assert.equal(root.querySelector('h2').textContent, 'Hello Ada');
    assert.equal(root.querySelector('input').placeholder, 'Search');
    assert.equal(root.querySelector('output').textContent, '4');
    const buttonA = root.querySelector('[data-id="item-a"] button');
    act(() => { buttonA.click(); }); assert.deepEqual(selected, ['a']);
    render([b, { ...a, name: 'Updated' }], 3, 'fr');
    assert.equal(root.querySelector('h2').textContent, 'Bonjour Ada');
    assert.equal(root.querySelector('input').placeholder, 'Chercher');
    assert.equal(root.querySelector('aside').textContent, 'Nouveau');
    assert.equal(root.querySelector('[data-id="item-a"] button'), buttonA);
    assert.equal(buttonA.textContent, 'Updated');
    assert.deepEqual([...root.querySelectorAll('li')].map(el => el.dataset.id), ['item-b', 'item-a']);
    assert.equal(root.querySelector('output').textContent, '6');
    act(() => { vm.state.count.set(4); }); assert.equal(root.querySelector('output').textContent, '12');
    const save = root.querySelector('section > .kitButton');
    if (variant === 'before') {
      assert.equal(save.disabled, true, 'negative: signal is a truthy React element');
      assert.throws(() => setDoubled(vm, 10), /Cannot set value on mapped reactive value/);
    } else {
      assert.equal(save.disabled, false);
      act(() => { vm.state.busy.set(true); }); assert.equal(save.disabled, true);
      act(() => { vm.state.busy.set(false); }); assert.equal(save.disabled, false);
      const seen = []; const off = vm.state.count.subscribe(n => seen.push(n));
      act(() => { setDoubled(vm, 10); }); assert.equal(vm.state.count.get(), 5);
      assert.equal(root.querySelector('output').textContent, '15'); assert(seen.includes(5)); off();
    }
    render([]); assert.equal(root.querySelector('section > p').textContent, 'Empty');
    act(() => { ReactDOM.unmountComponentAtNode(root); }); root.remove();
    finish(vm);
    assert.equal(destroyed, variant === 'after' ? 1 : 0, 'negative: state.destroy bypasses VM callbacks');
    vm.destroy(); assert.equal(destroyed, 1);
  });
}
