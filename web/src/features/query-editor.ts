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
  'let', 'where', 'filter', 'search', 'project', 'project-away', 'project-keep', 'project-rename', 'project-reorder',
  'extend', 'summarize', 'distinct', 'order', 'sort', 'top', 'take',
  'limit', 'sample', 'sample-distinct', 'count', 'serialize', 'as', 'mv-expand', 'mv-apply', 'union', 'join',
  'lookup', 'kind', 'inner', 'leftouter', 'rightouter', 'fullouter', 'leftsemi', 'leftanti', 'with_itemindex',
  'on', 'by', 'of', 'asc', 'desc',
]);
const logicalOperators = new Set([
  'and', 'or', 'not', '=~', '!~', 'in', 'in~', '!in', '!in~', 'between', '!between', 'contains', 'contains_cs',
  'startswith', 'startswith_cs', 'endswith', 'endswith_cs', 'has', 'has_cs', 'hasprefix', 'hasprefix_cs',
  'hassuffix', 'hassuffix_cs', 'has_any', 'has_all',
]);
const functions = new Set([
  'now', 'ago', 'datetime', 'tostring', 'toint', 'tolong', 'toreal', 'todouble', 'tolower', 'toupper', 'isnull',
  'isnotnull', 'isempty', 'isnotempty', 'parse_json', 'array_length', 'bag_keys', 'todatetime',
  'base64_decode_tostring', 'url_decode', 'bag_has_key', 'set_has_element',
  'ipv4_is_private', 'ipv4_is_in_range',
  'count', 'countif', 'sumif', 'iff', 'case', 'coalesce', 'strlen', 'substring',
  'strcat', 'sum', 'min', 'max', 'avg', 'split', 'extract', 'trim', 'replace_string',
  'make_set', 'make_list', 'take_any',
]);
const literals = new Set(['true', 'false', 'null']);
const types = new Set(['string', 'long', 'real', 'dynamic']);

const operatorCompletions = createCompletions('keyword', [
  ['let', 'Declare a scalar or tabular value'],
  ['where', 'Filter rows'], ['filter', 'Filter rows'], ['search', 'Search visible columns'],
  ['project', 'Select columns'], ['project-away', 'Remove columns'], ['project-keep', 'Keep columns'],
  ['project-rename', 'Rename columns'], ['project-reorder', 'Reorder columns'],
  ['extend', 'Add a calculated column'],
  ['summarize', 'Aggregate rows'], ['distinct', 'Return unique rows'],
  ['order by', 'Sort rows'], ['sort by', 'Sort rows'], ['take', 'Limit rows'],
  ['top', 'Rank and limit rows'], ['limit', 'Limit rows'], ['count', 'Count rows'],
  ['sample', 'Select random rows'], ['sample-distinct', 'Select random distinct values'],
  ['serialize', 'Preserve pipeline order metadata'],
  ['as', 'Alias a reusable pipeline'],
  ['mv-expand', 'Expand one dynamic array'],
  ['mv-apply', 'Apply row-wise operators to one dynamic array'],
  ['union', 'Combine compatible table rows'], ['join', 'Correlate rows by matching columns'],
  ['lookup', 'Enrich rows by matching columns'],
], label => `${label} `);

const functionCompletions = createCompletions('function', [
  ['now', 'Current UTC time'], ['ago', 'Relative UTC time'], ['datetime', 'Datetime literal'],
  ['tostring', 'Convert to string'], ['toint', 'Convert to integer'], ['tolong', 'Convert to long'],
  ['toreal', 'Convert to real'], ['todouble', 'Convert to double'],
  ['tolower', 'Lowercase text'], ['toupper', 'Uppercase text'], ['isnull', 'Test for null'],
  ['isnotnull', 'Test for non-null'], ['parse_json', 'Parse JSON text'],
  ['isempty', 'Test for null or empty text'], ['isnotempty', 'Test for non-empty text'],
  ['array_length', 'Number of array elements'], ['bag_keys', 'Keys of a dynamic object'],
  ['bag_has_key', 'Test for a dynamic object key'], ['set_has_element', 'Test dynamic array membership'],
  ['base64_decode_tostring', 'Decode Base64 text'], ['url_decode', 'Decode URL-escaped text'],
  ['ipv4_is_private', 'Test for an RFC1918 address'],
  ['ipv4_is_in_range', 'Test an address against IPv4 ranges'],
  ['todatetime', 'Convert ISO text to UTC datetime'],
  ['iff', 'Conditional value'], ['case', 'Conditional branches'], ['coalesce', 'First non-null value'], ['strlen', 'String length'],
  ['substring', 'Extract text'], ['strcat', 'Concatenate values'],
  ['split', 'Split text into a dynamic array'], ['extract', 'Extract a regular expression group'],
  ['trim', 'Trim a regular expression'], ['replace_string', 'Replace literal text'],
  ['count', 'Count rows'], ['countif', 'Conditional count'], ['sumif', 'Conditional sum'],
  ['sum', 'Sum values'], ['min', 'Minimum value'], ['max', 'Maximum value'], ['avg', 'Average value'],
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
    const word = context.matchBefore(/[A-Za-z_][A-Za-z0-9_.\[\]"-]*(?:\s+[A-Za-z_][A-Za-z0-9_-]*)?/);
    if (!context.explicit && (!word || word.from === word.to)) return null;
    return {
      from: word?.from ?? context.pos,
      options: [
        ...options.getTableCompletions(),
        ...operatorCompletions,
        ...functionCompletions,
        ...options.getFieldCompletions(),
      ],
      validFor: /^[A-Za-z_][A-Za-z0-9_.\[\]"-]*(?:\s+[A-Za-z_][A-Za-z0-9_-]*)?$/,
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
      if (stream.match(/^(?:mv-(?:expand|apply)|parse-(?:where|kv)|project-(?:away|rename|reorder))\b/)) return 'keyword';
      if (stream.match(/^[A-Za-z_][A-Za-z0-9_]*/)) {
        const word = stream.current();
        if (keywords.has(word)) return 'keyword';
        if (logicalOperators.has(word)) return 'operator';
        if (literals.has(word)) return 'bool';
        if (types.has(word)) return 'typeName';
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
  { tag: tags.keyword, color: '#75baf2', fontWeight: '600' },
  { tag: tags.operatorKeyword, color: '#c9a7eb', fontWeight: '500' },
  { tag: tags.operator, color: '#c9a7eb' },
  { tag: tags.string, color: '#8fd18f' },
  { tag: tags.number, color: '#f5b383' },
  { tag: tags.bool, color: '#f5b383' },
  { tag: tags.typeName, color: '#6fd3d6' },
  { tag: tags.className, color: '#f3f2f1', fontWeight: '600' },
  { tag: tags.variableName, color: '#d2d0ce' },
  { tag: tags.comment, color: '#a19f9d', fontStyle: 'italic' },
]);

const editorTheme = EditorView.theme({
  '&': { color: '#f3f2f1', backgroundColor: 'transparent' },
  '.cm-content': { caretColor: '#75baf2' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: '#75baf2' },
  '.cm-gutters': {
    color: '#a19f9d',
    backgroundColor: '#252423',
    borderRight: '1px solid #484644',
  },
  '.cm-activeLine': { backgroundColor: '#263746' },
  '.cm-activeLineGutter': { color: '#75baf2', backgroundColor: '#203242' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: '#384b5e',
  },
  '.cm-tooltip': { color: '#f3f2f1', backgroundColor: '#323130', border: '1px solid #5a5856' },
}, { dark: true });
