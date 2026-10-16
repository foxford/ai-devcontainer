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
  test(variant + ': props, handlers, keys, refs, lifecycle, locale and string API', () => {
    const { Dashboard } = require('./' + variant + '/Dashboard.tsx');
    const root = document.createElement('div'); document.body.appendChild(root);
    const selected = [], cleaned = [];
    const onSelect = id => selected.push(id), onCleanup = id => cleaned.push(id);
    const a = { id: 'a', name: 'Alpha' }, b = { id: 'b', name: 'Beta' };
    const render = (items, locale = 'en') => act(() => { ReactDOM.render(
      React.createElement(IntlProvider, { locale, messages: locale === 'fr' ? { greeting: 'Bonjour {name}', search: 'Chercher' } : {} },
        React.createElement(Dashboard, { items, onSelect, onCleanup })), root); });
    render([a, b]);
    assert.equal(root.querySelector('h1').innerHTML, 'Hello Ada');
    assert.equal(root.querySelector('section > input').placeholder, 'Search');
    assert.equal(root.querySelector('section').dataset.prefix, 'item-');
    const inputA = root.querySelector('[data-id="item-a"] input'); inputA.value = 'edited';
    act(() => root.querySelector('[data-id="item-a"] button').click());
    assert.deepEqual(selected, ['a']);
    render([b, { ...a, name: 'Updated' }], 'fr');
    assert.equal(root.querySelector('h1').innerHTML, 'Bonjour Ada');
    assert.equal(root.querySelector('section > input').placeholder, 'Chercher');
    assert.deepEqual([...root.querySelectorAll('li')].map(el => el.dataset.id), ['item-b', 'item-a']);
    assert.equal(root.querySelector('[data-id="item-a"] button').textContent, 'Updated:1');
    assert.equal(root.querySelector('[data-id="item-a"] input'), inputA);
    assert.equal(inputA.value, 'edited'); assert.deepEqual(cleaned, []);
    render([]); assert.equal(root.querySelector('p').textContent, 'No items');
    assert.deepEqual(cleaned.sort(), ['a', 'b']);
    act(() => { ReactDOM.unmountComponentAtNode(root); }); root.remove();
  });
}
