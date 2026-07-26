import { autocompletion, closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
import type { Completion, CompletionContext } from '@codemirror/autocomplete';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import {
  bracketMatching,
  HighlightStyle,
  indentOnInput,
  StreamLanguage,
  syntaxHighlighting,
} from '@codemirror/language';
import { setDiagnostics } from '@codemirror/lint';
import { Compartment, EditorState } from '@codemirror/state';
import { drawSelection, dropCursor, EditorView, keymap, lineNumbers } from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { Vim, vim } from '@replit/codemirror-vim';
import type { QueryError } from '../types';

const keywords = new Set([
  'let', 'where', 'search', 'project', 'extend', 'summarize', 'distinct', 'order', 'sort',
  'top', 'take', 'limit', 'count', 'mv-expand', 'mv-apply', 'union', 'join', 'kind', 'inner', 'leftouter',
  'leftanti', 'on', 'by', 'asc', 'desc',
]);
const logicalOperators = new Set([
  'and', 'or', 'not', '=~', '!~', 'in', 'in~', '!in', '!in~', 'contains', 'contains_cs',
  'startswith', 'startswith_cs', 'endswith', 'endswith_cs', 'has', 'has_cs', 'has_any', 'has_all',
]);
const functions = new Set([
  'now', 'ago', 'datetime', 'bin', 'tostring', 'toint', 'tolower', 'toupper', 'isnull',
  'isnotnull', 'parse_json', 'count', 'countif', 'iff', 'coalesce', 'strlen', 'substring',
  'strcat', 'dcount', 'sum', 'min', 'max', 'avg', 'split', 'extract', 'trim', 'replace_string',
  'arg_max', 'arg_min', 'make_set', 'make_list', 'take_any',
]);
const literals = new Set(['true', 'false', 'null']);

const operatorCompletions = createCompletions('keyword', [
  ['let', 'Declare a scalar or tabular value'],
  ['where', 'Filter rows'], ['search', 'Search all visible columns'],
  ['project', 'Select columns'], ['extend', 'Add a calculated column'],
  ['summarize', 'Aggregate rows'], ['distinct', 'Return unique rows'],
  ['order by', 'Sort rows'], ['sort by', 'Sort rows'], ['take', 'Limit rows'],
  ['top', 'Rank and limit rows'], ['limit', 'Limit rows'], ['count', 'Count rows'],
  ['mv-expand', 'Expand a dynamic array into rows'],
  ['mv-apply', 'Aggregate a dynamic array per input row'],
  ['union', 'Combine compatible table rows'], ['join', 'Correlate rows by matching columns'],
], label => `${label} `);

const functionCompletions = createCompletions('function', [
  ['now', 'Current UTC time'], ['ago', 'Relative UTC time'], ['datetime', 'Datetime literal'],
  ['bin', 'Bucket a value'], ['tostring', 'Convert to string'], ['toint', 'Convert to integer'],
  ['tolower', 'Lowercase text'], ['toupper', 'Uppercase text'], ['isnull', 'Test for null'],
  ['isnotnull', 'Test for non-null'], ['parse_json', 'Parse JSON text'],
  ['iff', 'Conditional value'], ['coalesce', 'First non-null value'], ['strlen', 'String length'],
  ['substring', 'Extract text'], ['strcat', 'Concatenate values'],
  ['split', 'Split text into a dynamic array'], ['extract', 'Extract a regular expression group'],
  ['trim', 'Trim a regular expression'], ['replace_string', 'Replace literal text'],
  ['count', 'Count rows'], ['countif', 'Conditional count'], ['dcount', 'Distinct count'],
  ['sum', 'Sum values'], ['min', 'Minimum value'], ['max', 'Maximum value'], ['avg', 'Average value'],
  ['arg_max', 'Row with the maximum value'], ['arg_min', 'Row with the minimum value'],
  ['make_set', 'Collect distinct values'], ['make_list', 'Collect values'],
  ['take_any', 'Select a group value'],
], label => `${label}()`);

function createCompletions(
  type: string,
  definitions: [label: string, detail: string][],
  apply: (label: string) => string,
): Completion[] {
  return definitions.map(([label, detail]) => ({ label, detail, type, apply: apply(label) }));
}

interface QueryEditorOptions {
  parent: HTMLElement;
  initialDocument: string;
  availableTables: Set<string>;
  getTableCompletions: () => Completion[];
  getFieldCompletions: () => Completion[];
  onDocumentChange: () => void;
  onRun: () => void;
}

export interface QueryEditor {
  view: EditorView;
  clearDiagnostics(): void;
  showDiagnostic(error: QueryError): void;
  toggleVim(button: HTMLButtonElement): void;
}

export function createQueryEditor(options: QueryEditorOptions): QueryEditor {
  const vimCompartment = new Compartment();
  let vimEnabled = false;

  const language = createLanguage(options.availableTables);
  const completionSource = (context: CompletionContext) => {
    const word = context.matchBefore(/[A-Za-z_][A-Za-z0-9_.\[\]"]*/);
    if (!context.explicit && (!word || word.from === word.to)) return null;
    return {
      from: word?.from ?? context.pos,
      options: [
        ...options.getTableCompletions(),
        ...operatorCompletions,
        ...functionCompletions,
        ...options.getFieldCompletions(),
      ],
      validFor: /^[A-Za-z_][A-Za-z0-9_.\[\]"]*$/,
    };
  };

  const view = new EditorView({
    state: EditorState.create({
      doc: options.initialDocument,
      extensions: [
        vimCompartment.of([]),
        lineNumbers(),
        history(),
        drawSelection(),
        dropCursor(),
        indentOnInput(),
        bracketMatching(),
        closeBrackets(),
        autocompletion({ override: [completionSource], activateOnTyping: true }),
        language,
        syntaxHighlighting(highlightStyle),
        editorTheme,
        EditorView.updateListener.of(update => {
          if (!update.docChanged) return;
          options.onDocumentChange();
          update.view.dispatch(setDiagnostics(update.state, []));
        }),
        EditorView.contentAttributes.of({ 'aria-label': 'KQL query editor' }),
        keymap.of([
          { key: 'Mod-Enter', run: () => { options.onRun(); return true; } },
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...historyKeymap,
        ]),
        EditorView.lineWrapping,
      ],
    }),
    parent: options.parent,
  });

  Vim.defineEx('write', 'w', options.onRun);
  for (const mode of ['normal', 'visual']) {
    Vim.noremap('y', '"+y', mode);
    Vim.noremap('Y', '"+Y', mode);
    Vim.noremap('p', '"+p', mode);
    Vim.noremap('P', '"+P', mode);
  }

  return {
    view,
    clearDiagnostics() {
      view.dispatch(setDiagnostics(view.state, []));
    },
    showDiagnostic(error) {
      if (!error.position) return;
      const lineNumber = Math.max(1, Math.min(error.position.line, view.state.doc.lines));
      const line = view.state.doc.line(lineNumber);
      const from = Math.min(line.to, line.from + Math.max(0, error.position.column - 1));
      view.dispatch(setDiagnostics(view.state, [{
        from,
        to: Math.min(line.to, from + 1),
        severity: 'error',
        message: error.error || error.message || 'Invalid query',
      }]));
    },
    toggleVim(button) {
      vimEnabled = !vimEnabled;
      button.textContent = `Vim: ${vimEnabled ? 'On' : 'Off'}`;
      button.classList.toggle('active', vimEnabled);
      button.setAttribute('aria-pressed', String(vimEnabled));
      view.dispatch({ effects: vimCompartment.reconfigure(vimEnabled ? vim() : []) });
    },
  };
}

function createLanguage(availableTables: Set<string>) {
  return StreamLanguage.define({
    token(stream) {
      if (stream.eatSpace()) return null;
      if (stream.match('//')) {
        stream.skipToEnd();
        return 'comment';
      }
      if (stream.peek() === '"' || stream.peek() === "'") {
        const quote = stream.next();
        let escaped = false;
        while (!stream.eol()) {
          const character = stream.next();
          if (character === quote && !escaped) break;
          escaped = character === '\\' && !escaped;
          if (character !== '\\') escaped = false;
        }
        return 'string';
      }
      if (stream.match(/^\d+(?:\.\d+)?(?:ms|[smhdw])?/)) return 'number';
      if (stream.match(/^(?:==|!=|=~|!~|<=|>=|[+\-*/%<>=])/)) return 'operator';
      if (stream.match(/^(?:!in~?|in~)(?=\s|\()/)) return 'operator';
      if (stream.match(/^mv-(?:expand|apply)\b/)) return 'keyword';
      if (stream.match(/^[A-Za-z_][A-Za-z0-9_]*/)) {
        const word = stream.current();
        if (keywords.has(word)) return 'keyword';
        if (logicalOperators.has(word)) return 'operator';
        if (literals.has(word)) return 'bool';
        if (functions.has(word) && stream.match(/^\s*\(/, false)) return 'typeName';
        if (availableTables.has(word)) return 'className';
        return 'variableName';
      }
      stream.next();
      return null;
    },
  });
}

const highlightStyle = HighlightStyle.define([
  { tag: tags.keyword, color: '#6ea8fe', fontWeight: '500' },
  { tag: tags.operatorKeyword, color: '#9ac3ff' },
  { tag: tags.operator, color: '#aab5c2' },
  { tag: tags.string, color: '#9ed7ae' },
  { tag: tags.number, color: '#e8a87c' },
  { tag: tags.bool, color: '#e8a87c' },
  { tag: tags.typeName, color: '#72d6ca' },
  { tag: tags.className, color: '#f3f6fa', fontWeight: '600' },
  { tag: tags.variableName, color: '#d1d8e1' },
  { tag: tags.comment, color: '#657386', fontStyle: 'italic' },
]);

const editorTheme = EditorView.theme({
  '&': { color: '#dfe5ec', backgroundColor: 'transparent' },
  '.cm-content': { caretColor: '#6ea8fe' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: '#6ea8fe' },
  '.cm-gutters': {
    color: '#657386',
    backgroundColor: '#0a0d12',
    borderRight: '1px solid #252d39',
  },
  '.cm-activeLine': { backgroundColor: 'rgba(110, 168, 254, .045)' },
  '.cm-activeLineGutter': { color: '#6ea8fe', backgroundColor: 'rgba(110, 168, 254, .06)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: '#303d4d',
  },
  '.cm-tooltip': { color: '#f3f6fa', backgroundColor: '#171d27', border: '1px solid #354254' },
}, { dark: true });
