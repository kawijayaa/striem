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
  const mobileMedia = window.matchMedia('(max-width: 900px)');

  let current: MobileWorkspaceView = 'query';
  let queryRunning = false;

  function updateRunButton(): void {
    runButton.classList.toggle('hidden', current !== 'query' && !queryRunning);
  }

  function show(view: MobileWorkspaceView): void {
    current = view;
    panels.forEach(panel => {
      const mobile = mobileMedia.matches;
      const selected = panel.dataset.workspaceView === current;
      panel.classList.toggle('mobile-view-hidden', mobile && !selected);
      if (mobile) {
        panel.setAttribute('role', 'tabpanel');
        panel.setAttribute('aria-labelledby', `mobile-${panel.dataset.workspaceView}-tab`);
      } else {
        panel.removeAttribute('role');
        panel.removeAttribute('aria-labelledby');
      }
    });
    tabs.forEach(button => {
      const selected = button.dataset.mobileView === current;
      button.classList.toggle('selected', selected);
      button.setAttribute('aria-selected', String(selected));
      button.tabIndex = selected ? 0 : -1;
    });
    updateRunButton();
  }

  tabs.forEach(button => {
    button.addEventListener('click', () => {
      const view = button.dataset.mobileView;
      if (view === 'query' || view === 'results' || view === 'investigate') show(view);
    });
  });
  root.querySelector('.mobile-workspace-nav')?.addEventListener('keydown', event => {
    if (!(event instanceof KeyboardEvent)) return;
    const currentIndex = tabs.indexOf(document.activeElement as HTMLButtonElement);
    if (currentIndex < 0) return;
    let next: number | undefined;
    if (event.key === 'ArrowRight') next = (currentIndex + 1) % tabs.length;
    if (event.key === 'ArrowLeft') next = (currentIndex - 1 + tabs.length) % tabs.length;
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = tabs.length - 1;
    if (next === undefined) return;
    event.preventDefault();
    tabs[next]?.click();
    tabs[next]?.focus();
  });
  mobileMedia.addEventListener('change', () => show(current));
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
