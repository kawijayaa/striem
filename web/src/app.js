import { autocompletion, closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import {
  bracketMatching,
  HighlightStyle,
  indentOnInput,
  StreamLanguage,
  syntaxHighlighting,
} from '@codemirror/language';
import { setDiagnostics } from '@codemirror/lint';
import { EditorState } from '@codemirror/state';
import {
  drawSelection,
  dropCursor,
  EditorView,
  keymap,
  lineNumbers,
} from '@codemirror/view';
import { tags } from '@lezer/highlight';
import './style.css';

const $ = (selector) => document.querySelector(selector);

const operators = new Set([
  'let', 'where', 'project', 'extend', 'summarize', 'distinct', 'order', 'sort',
  'top', 'take', 'limit', 'count', 'by', 'asc', 'desc',
]);
const logicalOperators = new Set([
  'and', 'or', 'not', 'in', 'contains', 'startswith', 'endswith',
]);
const functions = new Set([
  'now', 'ago', 'datetime', 'bin', 'tostring', 'toint', 'tolower',
  'toupper', 'isnull', 'isnotnull', 'parse_json', 'count', 'countif',
  'iff', 'coalesce', 'strlen', 'substring', 'strcat', 'dcount', 'sum', 'min', 'max', 'avg',
]);
const literals = new Set(['true', 'false', 'null']);

const kqlLanguage = StreamLanguage.define({
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
    if (stream.match(/^(?:==|!=|<=|>=|[+\-*/%<>=])/)) return 'operator';
    if (stream.match(/^[A-Za-z_][A-Za-z0-9_]*/)) {
      const word = stream.current();
      if (operators.has(word)) return 'keyword';
      if (logicalOperators.has(word)) return 'operator';
      if (literals.has(word)) return 'bool';
      if (functions.has(word) && stream.match(/^\s*\(/, false)) return 'typeName';
      if (word === 'Events') return 'className';
      return 'variableName';
    }
    stream.next();
    return null;
  },
});

const highlightStyle = HighlightStyle.define([
  { tag: tags.keyword, color: '#d3d5d7', fontWeight: '650' },
  { tag: tags.operatorKeyword, color: '#c2c5c8' },
  { tag: tags.operator, color: '#c2c5c8' },
  { tag: tags.string, color: '#c9b995' },
  { tag: tags.number, color: '#b5c5aa' },
  { tag: tags.bool, color: '#b5c5aa' },
  { tag: tags.typeName, color: '#c7b5d1' },
  { tag: tags.className, color: '#dedede', fontWeight: '650' },
  { tag: tags.variableName, color: '#d8dade' },
  { tag: tags.comment, color: '#767b80', fontStyle: 'italic' },
]);

const editorTheme = EditorView.theme({
  '&': {
    color: '#e0e1e2',
    backgroundColor: '#151719',
  },
  '.cm-content': {
    caretColor: '#f3f3f3',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: '#f3f3f3',
  },
  '.cm-gutters': {
    color: '#71767b',
    backgroundColor: '#131517',
    borderRight: '1px solid #303438',
  },
  '.cm-activeLine, .cm-activeLineGutter': {
    backgroundColor: 'transparent',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: '#4b5056',
  },
  '.cm-tooltip': {
    color: '#f0f1f2',
    backgroundColor: '#202326',
    border: '1px solid #44494e',
  },
}, { dark: true });

const operatorCompletions = [
  ['let', 'Declare a scalar variable'],
  ['where', 'Filter rows'], ['project', 'Select columns'], ['extend', 'Add a calculated column'],
  ['summarize', 'Aggregate rows'], ['distinct', 'Return unique rows'],
  ['order by', 'Sort rows'], ['sort by', 'Sort rows'], ['take', 'Limit rows'],
  ['top', 'Rank and limit rows'], ['limit', 'Limit rows'], ['count', 'Count rows'],
].map(([label, detail]) => ({ label, apply: `${label} `, type: 'keyword', detail }));

const functionCompletions = [
  ['now', 'Current UTC time'], ['ago', 'Relative UTC time'], ['datetime', 'Datetime literal'],
  ['bin', 'Bucket a value'], ['tostring', 'Convert to string'], ['toint', 'Convert to integer'],
  ['tolower', 'Lowercase text'], ['toupper', 'Uppercase text'], ['isnull', 'Test for null'],
  ['isnotnull', 'Test for non-null'], ['parse_json', 'Parse JSON text'],
  ['iff', 'Conditional value'], ['coalesce', 'First non-null value'], ['strlen', 'String length'],
  ['substring', 'Extract text'], ['strcat', 'Concatenate values'],
  ['count', 'Count rows'], ['countif', 'Conditional count'], ['dcount', 'Distinct count'],
  ['sum', 'Sum values'], ['min', 'Minimum value'], ['max', 'Maximum value'], ['avg', 'Average value'],
].map(([label, detail]) => ({ label, apply: `${label}()`, type: 'function', detail }));

let availableFields = [];
let fieldCompletions = [];

function kqlCompletionSource(context) {
  const word = context.matchBefore(/[A-Za-z_][A-Za-z0-9_.\[\]"]*/);
  if (!context.explicit && (!word || word.from === word.to)) return null;
  return {
    from: word ? word.from : context.pos,
    options: [...operatorCompletions, ...functionCompletions, ...fieldCompletions],
    validFor: /^[A-Za-z_][A-Za-z0-9_.\[\]"]*$/,
  };
}

const editor = new EditorView({
  state: EditorState.create({
    doc: 'Events\n| order by TimeGenerated desc\n| take 100',
    extensions: [
      lineNumbers(),
      history(),
      drawSelection(),
      dropCursor(),
      indentOnInput(),
      bracketMatching(),
      closeBrackets(),
      autocompletion({ override: [kqlCompletionSource], activateOnTyping: true }),
      kqlLanguage,
      syntaxHighlighting(highlightStyle),
      editorTheme,
      keymap.of([
        ...closeBracketsKeymap,
        ...defaultKeymap,
        ...historyKeymap,
        { key: 'Mod-Enter', run: () => { runQuery(); return true; } },
      ]),
      EditorView.lineWrapping,
    ],
  }),
  parent: $('#query'),
});

async function request(url, options = {}) {
  const response = await fetch(url, options);
  const body = response.status === 204 ? null : await response.json().catch(() => ({ error: 'Invalid server response' }));
  if (!response.ok) throw body;
  return body;
}

function clearDiagnostics() {
  editor.dispatch(setDiagnostics(editor.state, []));
}

function showDiagnostic(error) {
  if (!error.position) return;
  const lineNumber = Math.max(1, Math.min(error.position.line, editor.state.doc.lines));
  const line = editor.state.doc.line(lineNumber);
  const from = Math.min(line.to, line.from + Math.max(0, error.position.column - 1));
  const to = Math.min(line.to, from + 1);
  editor.dispatch(setDiagnostics(editor.state, [{
    from,
    to,
    severity: 'error',
    message: error.error || 'Invalid query',
  }]));
}

async function runQuery() {
  const errorBox = $('#query-error');
  const runButton = $('#run-query');
  errorBox.classList.add('hidden');
  clearDiagnostics();
  runButton.disabled = true;
  try {
    const result = await request('/api/query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: editor.state.doc.toString() }),
    });
    renderResults(result);
    $('#query-stats').textContent = `${result.rowCount} rows · ${result.durationMs} ms`;
  } catch (error) {
    const position = error.position ? `Line ${error.position.line}, column ${error.position.column}: ` : '';
    errorBox.textContent = position + (error.error || error.message || 'Query failed');
    errorBox.classList.remove('hidden');
    showDiagnostic(error);
  } finally {
    runButton.disabled = false;
  }
}

function renderResults(result) {
  const head = $('#result-head');
  const body = $('#result-body');
  head.replaceChildren();
  body.replaceChildren();
  const headerRow = document.createElement('tr');
  result.columns.forEach(column => {
    const th = document.createElement('th');
    th.textContent = column;
    headerRow.append(th);
  });
  head.append(headerRow);
  result.rows.forEach(row => {
    const tr = document.createElement('tr');
    result.columns.forEach(column => {
      const td = document.createElement('td');
      const value = row[column];
      if (value !== null && typeof value === 'object') {
        td.textContent = 'View JSON';
        td.className = 'raw';
        td.title = 'Open JSON';
        td.addEventListener('click', () => showRaw(value));
      } else {
        td.textContent = value ?? '∅';
        td.title = String(value ?? 'null');
      }
      tr.append(td);
    });
    body.append(tr);
  });
  $('#empty-results').classList.toggle('hidden', result.rows.length > 0);
  $('#empty-results').lastElementChild.textContent = result.rows.length ? '' : 'Query returned no events.';
  $('#table-wrap').classList.toggle('hidden', result.rows.length === 0);
}

function showRaw(value) {
  $('#raw-json').textContent = JSON.stringify(value, null, 2);
  $('#raw-dialog').showModal();
}

function replaceQuery(query) {
  editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: query } });
  clearDiagnostics();
  editor.focus();
}

function insertField(path) {
  const selection = editor.state.selection.main;
  const previous = selection.from > 0 ? editor.state.doc.sliceString(selection.from - 1, selection.from) : '';
  const prefix = previous && !/\s/.test(previous) ? ' ' : '';
  editor.dispatch({
    changes: { from: selection.from, to: selection.to, insert: prefix + path },
    selection: { anchor: selection.from + prefix.length + path.length },
  });
  editor.focus();
}

function renderFields(filter = '') {
  const list = $('#field-list');
  const normalized = filter.trim().toLowerCase();
  const fields = availableFields.filter(field => field.path.toLowerCase().includes(normalized));
  list.replaceChildren();
  if (!fields.length) {
    const empty = document.createElement('span');
    empty.className = 'muted';
    empty.textContent = 'No matching fields.';
    list.append(empty);
    return;
  }
  fields.forEach(field => {
    const button = document.createElement('button');
    button.className = 'field-row';
    button.title = `Insert ${field.path}`;
    const path = document.createElement('span');
    path.className = 'field-path';
    path.textContent = field.path;
    const type = document.createElement('span');
    type.className = 'field-type';
    type.textContent = field.type;
    button.append(path, type);
    button.addEventListener('click', () => insertField(field.path));
    list.append(button);
  });
}

async function loadFields() {
  try {
    const result = await request('/api/fields');
    availableFields = [...result.common, ...result.discovered];
    fieldCompletions = availableFields.map(field => ({
      label: field.path,
      type: field.type === 'dynamic' ? 'property' : 'variable',
      detail: field.type,
    }));
    $('#field-count').textContent = `${availableFields.length} fields`;
    renderFields();
  } catch {
    $('#field-count').textContent = 'Unavailable';
    $('#field-list').innerHTML = '<span class="muted">Could not load fields.</span>';
  }
}

const rawDialog = $('#raw-dialog');
$('#close-dialog').addEventListener('click', () => rawDialog.close());
rawDialog.addEventListener('click', event => {
  const bounds = rawDialog.getBoundingClientRect();
  const outside = event.clientX < bounds.left || event.clientX > bounds.right
    || event.clientY < bounds.top || event.clientY > bounds.bottom;
  if (outside) rawDialog.close();
});
$('#run-query').addEventListener('click', runQuery);
$('#field-search').addEventListener('input', event => renderFields(event.target.value));
document.querySelectorAll('.example').forEach(button => {
  button.addEventListener('click', () => replaceQuery(button.dataset.query));
});

loadFields();
