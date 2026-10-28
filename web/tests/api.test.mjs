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
    text: async () => { throw error; },
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

test('integers beyond 2^53 inside dynamic values keep their exact digits', async t => {
  const text = '{"rows":[{"d":{"id":9223372036854775807,"small":5,"real":1.5e300,"list":[-9007199254740993]}}]}';
  t.mock.method(globalThis, 'fetch', async () => new Response(text));
  const body = await request('/api/query');
  assert.equal(JSON.stringify(body.rows[0].d), '{"id":9223372036854775807,"small":5,"real":1.5e+300,"list":[-9007199254740993]}');
  assert.equal(body.rows[0].d.small, 5);
});

test('without JSON.rawJSON, integers beyond 2^53 keep their digits as strings', async () => {
  const { quoteUnsafeIntegers } = await import('../src/api.ts');
  const text = '{"a":9223372036854775807,"s":"9223372036854775807 \\" 123456789012345678","n":[-9007199254740993,9007199254740991,1.5e300,12345678901234567890.5]}';
  const parsed = JSON.parse(quoteUnsafeIntegers(text));
  assert.equal(parsed.a, '9223372036854775807');
  assert.equal(parsed.s, '9223372036854775807 " 123456789012345678');
  assert.deepEqual(parsed.n, ['-9007199254740993', 9007199254740991, 1.5e300, 12345678901234567890.5]);
});
