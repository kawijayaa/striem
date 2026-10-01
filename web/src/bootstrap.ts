const loadingScreen = document.querySelector<HTMLElement>('#ingestion-screen');
const loadingTitle = document.querySelector<HTMLElement>('#ingestion-title');
const loadingDetail = document.querySelector<HTMLElement>('#ingestion-detail');
const challengeName = document.querySelector<HTMLElement>('#ingestion-challenge-name');
const workspace = document.querySelector<HTMLElement>('.app-shell');
const loadingStatus = document.querySelector<HTMLElement>('#ingestion-status');
const retryButton = document.querySelector<HTMLButtonElement>('#ingestion-retry');

function showFailure(message: string, title = 'Ingestion could not complete'): void {
  loadingScreen?.classList.add('ingestion-failed');
  if (loadingTitle) loadingTitle.textContent = title;
  if (loadingDetail) loadingDetail.textContent = message;
  retryButton?.classList.remove('hidden');
}

function showChallengeName(value: unknown): void {
  const name = typeof value === 'string' ? value.trim() : '';
  if (!name || !challengeName) return;
  challengeName.textContent = name;
  challengeName.classList.remove('hidden');
}

function showProgress(connected: boolean): void {
  if (loadingTitle) loadingTitle.textContent = connected ? 'Preparing investigation data' : 'Connecting to workspace';
  if (loadingDetail) loadingDetail.textContent = connected
    ? 'Events are being ingested and indexed. The workspace will open automatically when ingestion is complete.'
    : 'The workspace service is unavailable. Trying again automatically.';
  if (loadingStatus) loadingStatus.textContent = connected ? 'Ingesting deployment…' : 'Reconnecting…';
}

async function waitUntilReady(): Promise<boolean> {
  for (;;) {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 8000);
    try {
      const response = await fetch('/api/ready', { cache: 'no-store', signal: controller.signal });
      const value: unknown = await response.json();
      if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid readiness response');
      const state = value as Record<string, unknown>;
      showChallengeName(state.challengeName);
      if (response.ok && state.status === 'ok') return true;
      if (state.status === 'error') {
        showFailure(typeof state.error === 'string' && state.error ? state.error : 'Reload the workspace to try again.');
        return false;
      }
      showProgress(state.status === 'loading');
    } catch {
      showProgress(false);
    } finally {
      window.clearTimeout(timeout);
    }
    await new Promise(resolve => window.setTimeout(resolve, 1000));
  }
}

async function start(): Promise<void> {
  if (!await waitUntilReady()) return;
  await import('./app');
  workspace?.classList.remove('workspace-pending');
  workspace?.removeAttribute('inert');
  workspace?.removeAttribute('aria-hidden');
  loadingScreen?.remove();
}

retryButton?.addEventListener('click', () => window.location.reload());

void start().catch(() => {
  showFailure('Reload the workspace to try again.', 'Workspace could not open');
});
