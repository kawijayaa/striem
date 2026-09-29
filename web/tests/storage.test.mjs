import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createBrowserStorage, readQuestionDrafts, readQueryHistory, readSavedQueries, storageKeys } from '../src/storage.ts';

function setup(t) {
  const values = new Map();
  let errors = 0;
  globalThis.window = { localStorage: {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  } };
  t.after(() => { delete globalThis.window; });
  return { values, storage: createBrowserStorage(() => { errors++; }), errors: () => errors };
}

test('browser storage tolerates missing, corrupt and non-array data', t => {
  const { values, storage } = setup(t);
  for (const invalid of [undefined, '{', 'null', '{}', '42', '"text"']) {
    values.set('key', invalid);
    assert.deepEqual(storage.readArray('key'), []);
  }
  assert.equal(storage.write('key', [{ value: 'persisted' }]), true);
  assert.deepEqual(storage.readArray('key'), [{ value: 'persisted' }]);
});

test('saved queries, history and answer drafts discard malformed entries', t => {
  const { storage } = setup(t);
  const invalid = [null, [], 1, {}, { id: 2, value: 'answer' }];
  const history = { query: 'Events | count', runAt: '2026-01-01' };
  const saved = { id: 'hunt', name: '<script>literal</script>', query: 'Events', savedAt: '2026-01-01' };
  storage.write(storageKeys.history, [...invalid, history]);
  storage.write(storageKeys.saved, [...invalid, saved]);
  storage.write(storageKeys.questionDrafts, [...invalid, { id: '__proto__', value: 'answer' }]);
  assert.deepEqual(readQueryHistory(storage), [history]);
  assert.deepEqual(readSavedQueries(storage), [saved]);
  assert.deepEqual([...readQuestionDrafts(storage)], [['__proto__', 'answer']]);
});

test('unavailable storage reads safely and reports failed writes', t => {
  const { storage, errors } = setup(t);
  Object.defineProperty(window, 'localStorage', { get() { throw new Error('Blocked'); } });
  assert.deepEqual(storage.readArray('key'), []);
  assert.equal(storage.write('key', ['draft']), false);
  assert.equal(errors(), 1);
});

test('quota failures leave the previously saved value intact', t => {
  const { storage, values, errors } = setup(t);
  storage.write('key', ['original']);
  window.localStorage.setItem = () => { throw new Error('Quota exceeded'); };
  assert.equal(storage.write('key', ['replacement']), false);
  assert.equal(values.get('key'), '["original"]');
  assert.equal(errors(), 1);
});
