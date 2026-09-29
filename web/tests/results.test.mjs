import assert from 'node:assert/strict';
import { test } from 'node:test';
import { sortResultRows, nextResultSort, resultRowKey, resultIdentity, resultContext, resultTime, resultValue } from '../src/features/results.ts';

test('sorting toggles direction and starts new columns ascending', () => {
  let sort = nextResultSort({ column: null, direction: 'asc' }, 'count');
  assert.deepEqual(sort, { column: 'count', direction: 'asc' });
  sort = nextResultSort(sort, 'count');
  assert.deepEqual(sort, { column: 'count', direction: 'desc' });
  assert.deepEqual(nextResultSort(sort, 'User'), { column: 'User', direction: 'asc' });
});

test('numeric and natural string sorting do not mutate the original rows', () => {
  const result = { rows: [{ value: 10 }, { value: 2 }, { value: -1 }] };
  assert.deepEqual(sortResultRows(result, { column: 'value', direction: 'asc' }).map(r => r.value), [-1, 2, 10]);
  assert.deepEqual(sortResultRows(result, { column: 'value', direction: 'desc' }).map(r => r.value), [10, 2, -1]);
  assert.deepEqual(result.rows.map(r => r.value), [10, 2, -1]);
  assert.equal(sortResultRows(result, { column: null, direction: 'asc' }), result.rows);
  const strings = { rows: [{ value: 'host10' }, { value: 'Host2' }] };
  assert.equal(sortResultRows(strings, { column: 'value', direction: 'asc' })[0].value, 'Host2');
});

test('null and missing values preserve relative order as equivalent empty cells', () => {
  const emptyRows = [{ value: null, id: 1 }, { id: 2 }, { value: null, id: 3 }, { id: 4 }];
  const result = { rows: [...emptyRows, { value: 1, id: 5 }] };
  assert.deepEqual(sortResultRows(result, { column: 'value', direction: 'asc' }).map(r => r.id), [5, 1, 2, 3, 4]);
  assert.deepEqual(sortResultRows(result, { column: 'value', direction: 'desc' }).map(r => r.id), [1, 2, 3, 4, 5]);
});

test('duplicate result rows have distinct selection identities', () => {
  const result = { rows: [{ User: 'same' }, { User: 'same' }] };
  assert.notEqual(resultRowKey(result, result.rows[0]), resultRowKey(result, result.rows[1]));
});

test('mobile result summaries retain false/zero values and skip raw objects', () => {
  const row = { TimeGenerated: '2026-01-01T00:00:00Z', Source: 'Audit', RawData: {}, User: 'alice', Enabled: false, Count: 0 };
  assert.equal(resultIdentity(row), 'alice');
  assert.equal(resultContext(row), 'Audit · false · 0');
  assert.equal(resultIdentity({ Source: 'Audit', RawData: {} }), 'Audit');
  assert.equal(resultIdentity({}), 'Security event');
  assert.equal(resultTime({ TimeGenerated: 'invalid time' }), 'invalid time');
  assert.equal(resultTime({}), undefined);
  assert.equal(resultTime({ TimeGenerated: 0 }), new Date(0).toLocaleString());
  assert.equal(resultValue(false), 'false');
  assert.equal(resultValue(1000), (1000).toLocaleString());
});
