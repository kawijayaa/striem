import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseFieldsResponse, parseSchemaResponse } from '../src/features/schema.ts';

test('field catalog validation accepts empty catalogs and rejects malformed nested fields', () => {
  const empty = { common: [], tables: [] };
  assert.deepEqual(parseFieldsResponse(empty), empty);
  const valid = { common: [{ path: 'RawData', type: 'dynamic' }], tables: [{ table: 'Events', fields: [{ path: 'host', type: 'string' }] }] };
  assert.deepEqual(parseFieldsResponse(valid), valid);
  for (const value of [null, [], {}, { common: [], tables: null }, { common: [null], tables: [] }, { common: [], tables: [{ table: 'Events', fields: [1] }] }, { common: [], tables: [{ table: 1, fields: [] }] }]) {
    assert.throws(() => parseFieldsResponse(value), /Invalid field catalog/);
  }
});

test('schema validation checks displayed metadata and permits missing optional fields', () => {
  assert.deepEqual(parseSchemaResponse({}), {});
  const valid = { challengeName: 'Hunt', tables: [{ name: 'Events', description: 'Telemetry' }], functions: ['now'] };
  assert.deepEqual(parseSchemaResponse(valid), valid);
  for (const value of [null, [], { challengeName: 1 }, { tables: null }, { tables: [null] }, { tables: [{ name: 'Events', description: 1 }] }, { functions: ['now', 1] }]) {
    assert.throws(() => parseSchemaResponse(value), /Invalid schema/);
  }
});
