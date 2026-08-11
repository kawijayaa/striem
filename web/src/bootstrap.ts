type Readiness = { status?: string; error?: string; challengeName?: string };

const loadingScreen = document.querySelector<HTMLElement>('#ingestion-screen');
const loadingTitle = document.querySelector<HTMLElement>('#ingestion-title');
const loadingDetail = document.querySelector<HTMLElement>('#ingestion-detail');
const challengeName = document.querySelector<HTMLElement>('#ingestion-challenge-name');
const workspace = document.querySelector<HTMLElement>('.app-shell');

function showFailure(message: string): void {
  loadingScreen?.classList.add('ingestion-failed');
  if (loadingTitle) loadingTitle.textContent = 'Ingestion could not complete';
  if (loadingDetail) loadingDetail.textContent = message;
}

function showChallengeName(value?: string): void {
  const name = value?.trim();
  if (!name || !challengeName) return;
  challengeName.textContent = name;
  challengeName.classList.remove('hidden');
}

async function waitUntilReady(): Promise<void> {
  for (;;) {
    try {
      const response = await fetch('/api/ready', { cache: 'no-store' });
      const state = await response.json() as Readiness;
      showChallengeName(state.challengeName);
      if (response.ok) return;
      if (state.status === 'error') {
        showFailure(state.error || 'Check the server logs for details.');
        return new Promise<void>(() => undefined);
      }
    } catch {
      // The listener may still be starting. Keep the loading screen in place.
    }
    await new Promise(resolve => window.setTimeout(resolve, 1000));
  }
}

async function start(): Promise<void> {
  await waitUntilReady();
  await import('./app');
  workspace?.classList.remove('workspace-pending');
  workspace?.removeAttribute('inert');
  workspace?.removeAttribute('aria-hidden');
  loadingScreen?.remove();
}

void start().catch(() => {
  showFailure('The workspace could not be initialized. Reload the page or check the server logs.');
});
