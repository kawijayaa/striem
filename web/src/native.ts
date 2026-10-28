import './native.css';
import { executeNative } from './features/native-request';
import type { NativeMode } from './features/native-request';

function element<T extends HTMLElement>(id: string): T {
  const value = document.getElementById(id);
  if (!value) throw new Error(`Missing ${id}`);
  return value as T;
}
const form = element<HTMLFormElement>('native-form');
const mode = element<HTMLSelectElement>('mode');
const query = element<HTMLTextAreaElement>('native-query');
const parameters = element<HTMLTextAreaElement>('parameters');
const progressive = element<HTMLInputElement>('progressive');
const run = element<HTMLButtonElement>('native-run');
const cancel = element<HTMLButtonElement>('native-cancel');
const download = element<HTMLButtonElement>('native-download');
const status = element('native-status');
const output = element('native-output');
let controller: AbortController | null = null;
let enabled = false;
let responseText: string | null = null;

function controls(): void {
  const busy = controller !== null;
  run.disabled = !enabled || busy;
  mode.disabled = !enabled || busy;
  query.disabled = busy;
  parameters.disabled = busy;
  progressive.disabled = busy || mode.value === 'management';
  cancel.disabled = !busy;
  run.textContent = mode.value === 'management' ? 'Run management command' : 'Run query';
}
mode.addEventListener('change', controls);
cancel.addEventListener('click', () => controller?.abort());
form.addEventListener('submit', async event => {
  event.preventDefault();
  if (!enabled || controller) return;
  const active = new AbortController();
  controller = active;
  responseText = null; download.disabled = true; output.textContent = '';
  status.textContent = 'Running…'; controls();
  try {
    const result = await executeNative(mode.value as NativeMode, query.value, parameters.value, progressive.checked, active.signal);
    if (active.signal.aborted) throw new DOMException('Canceled', 'AbortError');
    responseText = result; output.textContent = result; download.disabled = false;
    status.textContent = 'Complete. The response includes all returned tables and their column types.';
  } catch (error) {
    status.textContent = active.signal.aborted ? 'Canceled. The service may still be processing the request.'
      : error instanceof Error ? error.message : 'Request failed.';
  } finally { controller = null; controls(); }
});
query.addEventListener('keydown', event => {
  if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) { event.preventDefault(); form.requestSubmit(); }
});
download.addEventListener('click', () => {
  if (responseText === null) return;
  const url = URL.createObjectURL(new Blob([responseText], { type: 'application/json' }));
  const link = document.createElement('a'); link.href = url; link.download = 'kusto-response.json'; link.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
});
void (async () => {
  const connection = element('connection');
  try {
    const response = await fetch('/api/kusto/capabilities', { cache: 'no-store' });
    const value: unknown = await response.json();
    if (!response.ok || !value || typeof value !== 'object' || !('enabled' in value)
      || typeof value.enabled !== 'boolean' || !('management' in value) || typeof value.management !== 'boolean') throw new Error('Connection settings unavailable. Reload to retry.');
    enabled = value.enabled;
    const management = mode.querySelector<HTMLOptionElement>('[value="management"]');
    if (management) management.disabled = !enabled || !value.management;
    connection.textContent = !enabled ? 'A Kusto service connection has not been configured by the administrator.'
      : value.management ? 'Queries and management commands are enabled.' : 'Queries are enabled. Management commands are disabled.';
  } catch (error) { connection.textContent = error instanceof Error ? error.message : 'Connection settings unavailable.'; }
  controls();
})();
