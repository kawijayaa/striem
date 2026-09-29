import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createTimeline } from '../src/features/timeline.ts';

function element() {
  return {
    children: [],
    handlers: {},
    style: { setProperty() {} },
    classList: { toggle() {} },
    append(child) { this.children.push(child); },
    replaceChildren() { this.children = []; },
    setAttribute() {},
    addEventListener(name, handler) { this.handlers[name] = handler; },
  };
}

test('each timeline bar selects precisely the timestamps it counts', t => {
  globalThis.window = { innerWidth: 1024 };
  globalThis.document = { createElement: element };
  t.after(() => {
    delete globalThis.window;
    delete globalThis.document;
  });
  // Two buckets over three milliseconds have a fractional boundary at 1.5 ms.
  for (const minimum of [0, -10, 1_700_000_000_000]) {
    const times = [minimum, minimum + 1, minimum + 2];
    const elements = { root: element(), bars: element(), start: element(), end: element() };
    const selected = [];
    const timeline = createTimeline(elements, (start, end) => {
      selected.push(times.filter(time => time >= +start && time < +end));
    });
    timeline.render(times.map(TimeGenerated => ({ TimeGenerated })));
    for (const bar of elements.bars.children) bar.handlers.click();
    assert.deepEqual(selected, [[minimum, minimum + 1], [minimum + 2]]);
  }
});
