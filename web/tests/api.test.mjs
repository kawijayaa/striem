import assert from 'node:assert/strict';
import { test } from 'node:test';
import { request } from '../src/api.ts';

test('invalid JSON in a successful response rejects the request', async t => {
  t.mock.method(globalThis, 'fetch', async () => new Response('<html>Unavailable</html>'));
  await assert.rejects(request('/api/query'), /Invalid server response/);
});

test('cancellation while reading a response remains an AbortError', async t => {
  const error = new DOMException('Canceled', 'AbortError');
  t.mock.method(globalThis, 'fetch', async () => ({
    status: 200,
    ok: true,
    json: async () => { throw error; },
  }));
  await assert.rejects(request('/api/query'), candidate => candidate === error);
});

test('valid API responses, empty validation responses, and server errors retain their behavior', async t => {
  t.mock.method(globalThis, 'fetch', async (_url, options) => {
    assert.equal(options.headers.get('X-Striem-Request'), '1');
    return Response.json({ rows: [] });
  });
  assert.deepEqual(await request('/api/query', { method: 'POST' }), { rows: [] });
  globalThis.fetch = async () => new Response(null, { status: 204 });
  assert.equal(await request('/api/query/validate'), null);
  globalThis.fetch = async () => Response.json({ error: 'Invalid query' }, { status: 400 });
  await assert.rejects(request('/api/query'), error => error.error === 'Invalid query');
});
