import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createToast } from '../src/ui/toast.ts';
import { renderTabSelection, enableTabKeyboardNavigation } from '../src/ui/tabs.ts';

function element() {
  const classes = new Set();
  const attributes = new Map();
  return {
    attributes,
    handlers: {},
    classList: {
      add: name => classes.add(name),
      remove: name => classes.delete(name),
      contains: name => classes.has(name),
      toggle: (name, force) => force ? classes.add(name) : classes.delete(name),
    },
    setAttribute: (name, value) => attributes.set(name, value),
    addEventListener(name, callback) { this.handlers[name] = callback; },
    click() { this.handlers.click?.(); },
    focus() { document.activeElement = this; },
  };
}

test('tab selection keeps visibility, focusability and accessibility state consistent', () => {
  const tabs = ['fields', 'questions', 'hunts'].map(name => ({ name, tab: element(), panel: element() }));
  renderTabSelection('questions', tabs);
  for (const { name, tab, panel } of tabs) {
    const selected = name === 'questions';
    assert.equal(panel.classList.contains('hidden'), !selected);
    assert.equal(tab.classList.contains('selected'), selected);
    assert.equal(tab.attributes.get('aria-selected'), String(selected));
    assert.equal(tab.tabIndex, selected ? 0 : -1);
  }
});

test('tab keyboard navigation wraps and skips disabled/hidden tabs', t => {
  globalThis.document = { activeElement: null };
  globalThis.KeyboardEvent = class {
    constructor(key) { this.key = key; }
    preventDefault() { this.prevented = true; }
  };
  t.after(() => { delete globalThis.document; delete globalThis.KeyboardEvent; });
  const tabs = Array.from({ length: 4 }, element);
  tabs[1].disabled = true;
  tabs[2].classList.add('hidden');
  const root = element();
  root.querySelectorAll = () => tabs;
  enableTabKeyboardNavigation(root, '.tab');
  let activated = -1;
  tabs.forEach((tab, index) => tab.addEventListener('click', () => { activated = index; }));
  tabs[0].focus();
  for (const [key, expected] of [['ArrowRight', 3], ['ArrowRight', 0], ['ArrowLeft', 3], ['Home', 0], ['End', 3], ['ArrowDown', 0], ['ArrowUp', 3]]) {
    const event = new KeyboardEvent(key);
    root.handlers.keydown(event);
    assert.equal(activated, expected);
    assert.equal(document.activeElement, tabs[expected]);
    assert.equal(event.prevented, true);
  }
  const ignored = new KeyboardEvent('a');
  root.handlers.keydown(ignored);
  assert.equal(ignored.prevented, undefined);
});

test('toast actions can display a subsequent toast and old timeouts cannot hide it', t => {
  const timers = new Map();
  let next = 0;
  globalThis.window = {
    clearTimeout: id => timers.delete(id),
    setTimeout: callback => { const id = ++next; timers.set(id, callback); return id; },
  };
  t.after(() => { delete globalThis.window; });
  const root = element();
  const message = element();
  const button = element();
  const toast = createToast(root, message, button);
  toast.show('Removed', { label: 'Undo', handler: () => toast.show('Storage unavailable') });
  assert.equal(button.textContent, 'Undo');
  button.click();
  assert.equal(message.textContent, 'Storage unavailable');
  assert.equal(root.classList.contains('hidden'), false);
  assert.equal(button.classList.contains('hidden'), true);
  assert.equal(timers.size, 1);
  toast.hide();
  assert.equal(timers.size, 0);
  assert.equal(root.classList.contains('hidden'), true);
});

test('toast actions run only once and replaced messages discard previous actions', t => {
  globalThis.window = { clearTimeout() {}, setTimeout() { return 1; } };
  t.after(() => { delete globalThis.window; });
  const button = element();
  const toast = createToast(element(), element(), button);
  let actions = 0;
  toast.show('Removed', { label: 'Undo', handler: () => { actions++; } });
  button.click();
  button.click();
  assert.equal(actions, 1);
  toast.show('Removed', { label: 'Undo', handler: () => { actions++; } });
  toast.show('Copied');
  button.click();
  assert.equal(actions, 1);
});
