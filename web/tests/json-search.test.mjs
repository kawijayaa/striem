import assert from 'node:assert/strict';
import { test } from 'node:test';
import { findJSONSearchMatches } from '../src/ui/json-search.ts';

test('JSON search finds full literal phrases across keys and values', () => {
  const json = '{\n  "action": "login",\n  "message": "LOGIN"\n}';
  const query = '"action": "login"';
  assert.deepEqual(findJSONSearchMatches(json, query), [{ start: 4, end: 21 }]);
  const matches = findJSONSearchMatches(json, 'login');
  assert.deepEqual(matches.map(match => json.slice(match.start, match.end)), ['login', 'LOGIN']);
});

test('JSON search treats regex symbols literally and preserves Unicode offsets', () => {
  const text = 'İprefixTarget [a.b]+? 🎉 target';
  for (const query of ['[a.b]+?', '🎉', 'target']) {
    const matches = findJSONSearchMatches(text, query);
    assert.equal(matches.length, query === 'target' ? 2 : 1);
    for (const match of matches) {
      assert.equal(text.slice(match.start, match.end).toLowerCase(), query.toLowerCase());
    }
  }
  assert.deepEqual(findJSONSearchMatches(text, ''), []);
  assert.deepEqual(findJSONSearchMatches(text, 'absent'), []);
});
