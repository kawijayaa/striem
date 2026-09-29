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
      body = await response.json();
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error;
      throw new Error('Invalid server response');
    }
  }
  if (!response.ok) throw body;
  return body as T;
}
