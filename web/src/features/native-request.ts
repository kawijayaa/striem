export type NativeMode = 'query' | 'management';

export function nativeRequest(mode: NativeMode, query: string, parametersText: string, progressive: boolean) {
  if (!query.trim()) throw new Error('Enter a query or management command.');
  if (new TextEncoder().encode(query).length > 32 * 1024) throw new Error('KQL exceeds 32 KiB.');
  const parameters: unknown = JSON.parse(parametersText);
  if (!parameters || typeof parameters !== 'object' || Array.isArray(parameters)
    || Object.values(parameters).some(value => typeof value !== 'string')) {
    throw new Error('Parameters must be a JSON object with string values.');
  }
  const body = JSON.stringify({ query, parameters, ...(mode === 'query' ? { progressive } : {}) });
  if (new TextEncoder().encode(body).length > 64 * 1024) throw new Error('Request exceeds 64 KiB.');
  return { url: `/api/kusto/${mode}`, body };
}

// Keep native cells as raw JSON text: parsing long/decimal values into browser
// numbers would round them before display or download.
export async function executeNative(mode: NativeMode, query: string, parameters: string, progressive: boolean, signal: AbortSignal) {
  const spec = nativeRequest(mode, query, parameters, progressive);
  const response = await fetch(spec.url, {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Striem-Request': '1' },
    body: spec.body, signal, cache: 'no-store',
  });
  const text = await response.text();
  if (!response.ok) throw new Error(`Request failed (${response.status}): ${text}`);
  return text;
}
