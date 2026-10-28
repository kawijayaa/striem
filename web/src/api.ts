import type { QueryError } from './types';

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

export async function request<T>(url: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  const method = (options.method || 'GET').toUpperCase();
  if (!SAFE_METHODS.has(method)) headers.set('X-Striem-Request', '1');

  const response = await fetch(url, { ...options, headers });
  let body: T | QueryError | null = null;
  if (response.status !== 204) {
    try {
      body = parseExactJSON(await response.text()) as T | QueryError;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error;
      throw new Error('Invalid server response');
    }
  }
  if (!response.ok) throw body;
  return body as T;
}

type RawJSON = { rawJSON(text: string): unknown };

// parseExactJSON keeps integers beyond 2^53 inside dynamic values exact:
// where the browser supports JSON.parse source text and JSON.rawJSON, such
// integers keep their source digits and re-serialize unchanged. Elsewhere
// they parse as ordinary numbers.
export function parseExactJSON(text: string): unknown {
  const json = JSON as unknown as Partial<RawJSON>;
  if (typeof json.rawJSON !== 'function') return JSON.parse(text);
  const rawJSON = json.rawJSON;
  const reviver = function (this: unknown, _key: string, value: unknown, context?: { source?: string }): unknown {
    if (typeof value === 'number' && Number.isInteger(value) && !Number.isSafeInteger(value)
      && context?.source && /^-?\d+$/.test(context.source)) {
      return rawJSON(context.source);
    }
    return value;
  };
  return JSON.parse(text, reviver as (this: unknown, key: string, value: unknown) => unknown);
}
