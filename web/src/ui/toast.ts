export interface ToastAction {
  label: string;
  handler: () => void;
}

export interface ToastController {
  show(message: string, action?: ToastAction): void;
  hide(): void;
}

export function createToast(
  root: HTMLElement,
  messageElement: HTMLElement,
  actionButton: HTMLButtonElement,
): ToastController {
  let timer: number | undefined;
  let actionHandler: (() => void) | undefined;
  let visible = false;
  let hovered = false;
  let focused = false;
  let remaining = 0;
  let expiresAt = 0;

  function pause(): void {
    if (timer === undefined) return;
    remaining = Math.max(0, expiresAt - Date.now());
    window.clearTimeout(timer);
    timer = undefined;
  }

  function resume(): void {
    if (!visible || hovered || focused || timer !== undefined) return;
    expiresAt = Date.now() + remaining;
    timer = window.setTimeout(hide, remaining);
  }

  function hide(): void {
    window.clearTimeout(timer);
    timer = undefined;
    visible = false;
    focused = false;
    root.classList.add('hidden');
    actionHandler = undefined;
  }

  function show(message: string, action?: ToastAction): void {
    window.clearTimeout(timer);
    timer = undefined;
    visible = true;
    focused = Boolean(action) && focused;
    remaining = action ? 6000 : 2800;
    messageElement.textContent = message;
    actionHandler = action?.handler;
    actionButton.textContent = action?.label ?? '';
    actionButton.classList.toggle('hidden', !action);
    root.classList.remove('hidden');
    resume();
  }

  root.addEventListener('pointerenter', () => { hovered = true; pause(); });
  root.addEventListener('pointerleave', () => { hovered = false; resume(); });
  root.addEventListener('focusin', () => { focused = true; pause(); });
  root.addEventListener('focusout', event => {
    focused = event.relatedTarget instanceof Node && root.contains(event.relatedTarget);
    resume();
  });

  actionButton.addEventListener('click', () => {
    const handler = actionHandler;
    hide();
    handler?.();
  });

  return { show, hide };
}
