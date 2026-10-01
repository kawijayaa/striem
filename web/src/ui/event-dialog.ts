import { findJSONSearchMatches } from './json-search';

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
    const matches = findJSONSearchMatches(json, query);
    let matchIndex = 0;
    // Walk the matches once across syntax segments, preserving highlighting
    // even when a search spans a key, punctuation, and its value.
    function appendSearchText(parent: Node, text: string, offset: number): void {
      let cursor = 0;
      const end = offset + text.length;
      while (matchIndex < matches.length) {
        const match = matches[matchIndex]!;
        if (match.start >= end) break;
        if (match.end <= offset) {
          matchIndex++;
          continue;
        }
        const start = Math.max(match.start, offset) - offset;
        const stop = Math.min(match.end, end) - offset;
        parent.appendChild(document.createTextNode(text.slice(cursor, start)));
        const mark = document.createElement('mark');
        mark.textContent = text.slice(start, stop);
        parent.appendChild(mark);
        cursor = stop;
        if (match.end > end) break;
        matchIndex++;
      }
      parent.appendChild(document.createTextNode(text.slice(cursor)));
    }
    const tokenPattern = /("(?:\\.|[^"\\])*"\s*:)|("(?:\\.|[^"\\])*")|\b(true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g;
    let cursor = 0;
    output.replaceChildren();
    for (const token of json.matchAll(tokenPattern)) {
      appendSearchText(output, json.slice(cursor, token.index), cursor);
      const span = document.createElement('span');
      span.className = token[1] ? 'json-key' : token[2] ? 'json-string' : token[3] ? 'json-literal' : 'json-number';
      appendSearchText(span, token[0], token.index);
      output.append(span);
      cursor = token.index + token[0].length;
    }
    appendSearchText(output, json.slice(cursor), cursor);
    searchStatus.textContent = query ? `${matches.length} match${matches.length === 1 ? '' : 'es'}` : '';
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
    if (event.target !== dialog) return;
    const bounds = dialog.getBoundingClientRect();
    const outside = event.clientX < bounds.left || event.clientX > bounds.right
      || event.clientY < bounds.top || event.clientY > bounds.bottom;
    if (outside) dialog.close();
  });

  return { show, isOpen: () => dialog.open };
}
