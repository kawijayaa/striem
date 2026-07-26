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

  function hide(): void {
    root.classList.add('hidden');
    actionHandler = undefined;
  }

  function show(message: string, action?: ToastAction): void {
    window.clearTimeout(timer);
    messageElement.textContent = message;
    actionHandler = action?.handler;
    actionButton.textContent = action?.label ?? '';
    actionButton.classList.toggle('hidden', !action);
    root.classList.remove('hidden');
    timer = window.setTimeout(hide, action ? 6000 : 2800);
  }

  actionButton.addEventListener('click', () => {
    actionHandler?.();
    hide();
  });

  return { show, hide };
}
