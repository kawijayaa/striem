export interface EventDialogController {
  show(value: unknown): void;
  isOpen(): boolean;
}

type ElementConstructor<T extends Element> = new (...args: never[]) => T;

function required<T extends Element>(root:ParentNode, selector: string, constructor: ElementConstructor<T>): T {
  const element = root.querySelector(selector);
  if (!(element instanceof constructor)) throw new Error(`Missing element ${selector}`);
  return element;
}

function appendSearchText(parent: Node, text: string, query: string): number {
  if (!query) {
    parent.appendChild(document.createTextNode(text));
    return 0;
  }

  const lower = text.toLowerCase();
  const needle = query.toLowerCase();
  let cursor = 0;
  let count = 0;
  while (cursor < text.length) {
    const index = lower.indexOf(needle, cursor);
    if (index < 0) break;
    parent.appendChild(document.createTextNode(text.slice(cursor, index)));
    const mark = document.createElement('mark');
    mark.textContent = text.slice(index, index + needle.length);
    parent.appendChild(mark);
    cursor = index + needle.length;
    count++;
  }
  parent.appendChild(document.createTextNode(text.slice(cursor)));
  return count;
}

export function createEventDialog(
  root: Document,
  notify: (message: string) => void,
): EventDialogController {
  const dialog = required(root, '#raw-dialog', HTMLDialogElement);
  const closeButton = required(root, '#close-dialog', HTMLButtonElement);
  const search = required(root, '#raw-search', HTMLInputElement);
  const searchStatus = required(root, '#raw-search-status', HTMLElement);
  const copyButton = required(root, '#copy-raw', HTMLButtonElement);
  const output = required(root, '#raw-json', HTMLPreElement);
  let currentValue: unknown = null;

  function renderJson(): void {
    const query = search.value.trim();
    const json = JSON.stringify(currentValue, null, 2) ?? String(currentValue);
    const tokenPattern = /("(?:\\.|[^"\\])*"\s*:)|("(?:\\.|[^"\\])*")|\b(true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g;
    let cursor = 0;
    let matches = 0;
    output.replaceChildren();
    for (const token of json.matchAll(tokenPattern)) {
      matches += appendSearchText(output, json.slice(cursor, token.index), query);
      const span = document.createElement('span');
      span.className = token[1] ? 'json-key' : token[2] ? 'json-string' : token[3] ? 'json-literal' : 'json-number';
      matches += appendSearchText(span, token[0], query);
      output.append(span);
      cursor = token.index + token[0].length;
    }
    matches += appendSearchText(output, json.slice(cursor), query);
    searchStatus.textContent = query ? `${matches} match${matches === 1 ? '' : 'es'}` : '';
  }

  function show(value: unknown): void {
    currentValue = value;
    search.value = '';
    renderJson();
    dialog.showModal();
  }

  closeButton.addEventListener('click', () => dialog.close());
  search.addEventListener('input', renderJson);
  copyButton.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(currentValue, null, 2) ?? String(currentValue));
      notify('JSON copied');
    } catch {
      notify('Could not copy JSON');
    }
  });
  dialog.addEventListener('click', event => {
    const bounds = dialog.getBoundingClientRect();
    const outside = event.clientX < bounds.left || event.clientX > bounds.right
      || event.clientY < bounds.top || event.clientY > bounds.bottom;
    if (outside) dialog.close();
  });

  return { show, isOpen: () => dialog.open };
}
