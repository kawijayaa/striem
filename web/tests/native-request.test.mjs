import assert from 'node:assert/strict';
import { test } from 'node:test';
import { nativeRequest, executeNative } from '../src/features/native-request.ts';

test('native requests keep encoded string parameters and explicit execution mode', () => {
  const query = nativeRequest('query', 'print x=1', '{"p":"9223372036854775807","__proto__":"literal"}', true);
  assert.equal(query.url, '/api/kusto/query');
  const value = JSON.parse(query.body);
  assert.equal(value.parameters.p, '9223372036854775807');
  assert.equal(value.parameters.__proto__, 'literal');
  assert.equal(value.progressive, true);
  const command = nativeRequest('management', '.show tables', '{}', true);
  assert.equal(command.url, '/api/kusto/management');
  assert.equal('progressive' in JSON.parse(command.body), false);
});

test('native request input validation rejects wrong types and oversized UTF-8', () => {
  for (const parameters of ['null', '[]', '{"p":1}', '{"p":null}', '{"p":{}}', '{']) {
    assert.throws(() => nativeRequest('query', 'print x=1', parameters, false));
  }
  assert.throws(() => nativeRequest('query', ' ', '{}', false));
  assert.throws(() => nativeRequest('query', '😀'.repeat(8193), '{}', false), /32 KiB/);
  assert.throws(() => nativeRequest('query', 'print x=1', JSON.stringify({ p: 'x'.repeat(65536) }), false), /64 KiB/);
});

test('native execution preserves exact numeric JSON and supplies origin headers', async t => {
  const raw = '{"tables":[{"rows":[[9223372036854775807,9007199254740993,1.234567890123456789]]}]}';
  const controller = new AbortController();
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    assert.equal(url, '/api/kusto/query');
    assert.equal(options.headers['X-Striem-Request'], '1');
    assert.equal(options.headers['Content-Type'], 'application/json');
    assert.equal(options.signal, controller.signal);
    return new Response(raw);
  });
  assert.equal(await executeNative('query', 'print x=1', '{}', false, controller.signal), raw);
});

test('native execution propagates errors and cancellation without returning results', async t => {
  const signal = new AbortController().signal;
  t.mock.method(globalThis, 'fetch', async () => new Response('{"error":"disabled"}', { status: 403 }));
  await assert.rejects(executeNative('management', '.show tables', '{}', false, signal), /403.*disabled/);
  const error = new DOMException('Canceled', 'AbortError');
  globalThis.fetch = async () => ({ ok: true, text: async () => { throw error; } });
  await assert.rejects(executeNative('query', 'print x=1', '{}', false, signal), candidate => candidate === error);
});
