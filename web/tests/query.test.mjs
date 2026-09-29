import assert from 'node:assert/strict';
import { test } from 'node:test';
import { addValueFilter } from '../src/kql/query.ts';

test('filtering empty cells tests null instead of the text "null"', () => {
  for (const value of [null, undefined]) {
    assert.match(addValueFilter('Events', 'User', value), /\| where isnull\(User\)/);
    assert.match(addValueFilter('Events', 'User', value, true), /\| where isnotnull\(User\)/);
  }
  assert.match(addValueFilter('Events', 'User', 'null'), /\| where User == "null"/);
});

test('value filters escape control characters and query delimiters inside strings', () => {
  const value = 'line one\nline two\r\t\u0000"\\ | take 0';
  const query = addValueFilter('Events', 'Message', value);
  const literal = query.split(' == ')[1].trim();
  assert.equal(JSON.parse(literal), value);
  assert.equal(literal.includes('\n'), false);
  assert.equal(literal.includes('\u0000'), false);
});

test('result filters run after the projection or aggregation defining the column', () => {
  const query = 'Events | summarize Total=count() by Source';
  assert.equal(addValueFilter(query, 'Total', 3), query + '\n| where Total == 3');
  assert.equal(addValueFilter(query, 'Total', null), query + '\n| where isnull(Total)');
});
