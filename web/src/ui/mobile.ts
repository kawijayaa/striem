export type MobileWorkspaceView = 'query' | 'results' | 'investigate';

export interface MobileWorkspaceController {
  show(view: MobileWorkspaceView): void;
  setRunning(running: boolean): void;
}

export function createMobileWorkspace(
  root: Document,
  onRun: () => void,
): MobileWorkspaceController {
  const panels = Array.from(root.querySelectorAll<HTMLElement>('[data-workspace-view]'));
  const tabs = Array.from(root.querySelectorAll<HTMLButtonElement>('.mobile-workspace-tab'));
  const candidate = root.querySelector('#mobile-run-query');
  if (!(candidate instanceof HTMLButtonElement)) throw new Error('Missing mobile run button');
  const runButton = candidate;

  let current: MobileWorkspaceView = 'query';
  let queryRunning = false;

  function updateRunButton(): void {
    runButton.classList.toggle('hidden', current !== 'query' && !queryRunning);
  }

  function show(view: MobileWorkspaceView): void {
    current = view;
    panels.forEach(panel => {
      panel.classList.toggle('mobile-view-hidden', panel.dataset.workspaceView !== current);
    });
    tabs.forEach(button => {
      const selected = button.dataset.mobileView === current;
      button.classList.toggle('selected', selected);
      button.setAttribute('aria-pressed', String(selected));
      if (selected) button.setAttribute('aria-current', 'page');
      else button.removeAttribute('aria-current');
    });
    updateRunButton();
  }

  tabs.forEach(button => {
    button.addEventListener('click', () => {
      const view = button.dataset.mobileView;
      if (view === 'query' || view === 'results' || view === 'investigate') show(view);
    });
  });
  runButton.addEventListener('click', onRun);
  show(current);

  return {
    show,
    setRunning(running) {
      queryRunning = running;
      updateRunButton();
    },
  };
}
