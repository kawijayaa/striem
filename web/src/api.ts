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
// they become strings of their exact digits.
export function parseExactJSON(text: string): unknown {
  const json = JSON as unknown as Partial<RawJSON>;
  if (typeof json.rawJSON !== 'function') return JSON.parse(quoteUnsafeIntegers(text));
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

// quoteUnsafeIntegers rewrites integer literals beyond 2^53, outside JSON
// strings, as strings of the same digits.
export function quoteUnsafeIntegers(text: string): string {
  let result = '';
  let start = 0;
  for (let index = 0; index < text.length; index++) {
    const character = text.charAt(index);
    if (character === '"') {
      for (index++; index < text.length && text.charAt(index) !== '"'; index++) {
        if (text.charAt(index) === '\\') index++;
      }
      continue;
    }
    if (character !== '-' && (character < '0' || character > '9')) continue;
    let end = index + 1;
    while (end < text.length && /[0-9eE.+-]/.test(text.charAt(end))) end++;
    const literal = text.slice(index, end);
    if (/^-?\d{16,}$/.test(literal) && !Number.isSafeInteger(Number(literal))) {
      result += text.slice(start, index) + '"' + literal + '"';
      start = end;
    }
    index = end - 1;
  }
  return result + text.slice(start);
}
