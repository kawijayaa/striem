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
import { Compartment, EditorState } from '@codemirror/state';
import {
  drawSelection,
  dropCursor,
  EditorView,
  keymap,
  lineNumbers,
} from '@codemirror/view';
import { vim } from '@replit/codemirror-vim';
import { tags } from '@lezer/highlight';
import './style.css';

const $ = (selector) => document.querySelector(selector);
const availableTables = new Set(['Events']);

const operators = new Set([
  'let', 'where', 'project', 'extend', 'summarize', 'distinct', 'order', 'sort',
  'top', 'take', 'limit', 'count', 'union', 'join', 'kind', 'inner', 'leftouter',
  'on', 'by', 'asc', 'desc',
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
      if (availableTables.has(word)) return 'className';
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
  ['union', 'Combine compatible table rows'], ['join', 'Correlate rows by matching columns'],
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

let commonFields = [];
let fieldGroups = [];
let fieldCompletions = [];
let tableCompletions = [{ label: 'Events', type: 'class', detail: 'All datasets' }];
let availableTableMetadata = [];
let selectedTable = 'Events';
const storageKeys = {
  history: 'striem.queryHistory',
  saved: 'striem.savedQueries',
  bookmarks: 'striem.bookmarks',
};

function readStored(key) {
  try {
    const value = JSON.parse(localStorage.getItem(key));
    return Array.isArray(value) ? value : [];
  } catch {
    return [];
  }
}

function writeStored(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Storage may be unavailable in private browsing or restricted contexts.
  }
}

let queryHistory = readStored(storageKeys.history);
let savedQueries = readStored(storageKeys.saved);
let bookmarks = readStored(storageKeys.bookmarks);
let queryLibraryView = 'saved';
let resultsPanelView = 'results';
const sharedQuery = new URL(window.location.href).searchParams.get('q');

function kqlCompletionSource(context) {
  const word = context.matchBefore(/[A-Za-z_][A-Za-z0-9_.\[\]"]*/);
  if (!context.explicit && (!word || word.from === word.to)) return null;
  return {
    from: word ? word.from : context.pos,
    options: [...tableCompletions, ...operatorCompletions, ...functionCompletions, ...fieldCompletions],
    validFor: /^[A-Za-z_][A-Za-z0-9_.\[\]"]*$/,
  };
}

const vimCompartment = new Compartment();
let vimEnabled = false;

const editor = new EditorView({
  state: EditorState.create({
    doc: sharedQuery || 'Events\n| order by TimeGenerated desc\n| take 100',
    extensions: [
      vimCompartment.of([]),
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
        { key: 'Mod-Enter', run: () => { runQuery(); return true; } },
        ...closeBracketsKeymap,
        ...defaultKeymap,
        ...historyKeymap,
      ]),
      EditorView.lineWrapping,
    ],
  }),
  parent: $('#query'),
});

function toggleVim() {
  vimEnabled = !vimEnabled;
  const btn = $('#vim-toggle');
  btn.textContent = vimEnabled ? 'Vim' : 'Normal';
  btn.classList.toggle('active', vimEnabled);
  editor.dispatch({ effects: vimCompartment.reconfigure(vimEnabled ? vim() : []) });
}



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
  const query = editor.state.doc.toString();
  errorBox.classList.add('hidden');
  clearDiagnostics();
  runButton.disabled = true;
  resultsPanelView = 'results';
  renderResultsPanelView();
  recordQuery(query);
  try {
    const result = await request('/api/query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query }),
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
  const actionHeader = document.createElement('th');
  actionHeader.className = 'row-action';
  actionHeader.setAttribute('aria-label', 'Bookmark');
  headerRow.append(actionHeader);
  result.columns.forEach(column => {
    const th = document.createElement('th');
    th.textContent = column;
    headerRow.append(th);
  });
  head.append(headerRow);
  result.rows.forEach(row => {
    const tr = document.createElement('tr');
    const action = document.createElement('td');
    action.className = 'row-action';
    const bookmark = document.createElement('button');
    const rowKey = JSON.stringify(row);
    const saved = bookmarks.some(item => item.rowKey === rowKey);
    bookmark.className = 'bookmark-toggle';
    bookmark.rowKey = rowKey;
    bookmark.textContent = saved ? 'Saved' : 'Save';
    bookmark.title = saved ? 'Remove bookmark' : 'Bookmark event';
    bookmark.addEventListener('click', () => toggleBookmark(row, rowKey, bookmark));
    action.append(bookmark);
    tr.append(action);
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
  renderTimeline(result.rows);
}

function renderTimeline(rows) {
  const timeline = $('#result-timeline');
  const points = rows
    .map(row => new Date(row.TimeGenerated))
    .filter(value => !Number.isNaN(value.getTime()))
    .map(value => value.getTime());
  timeline.classList.toggle('hidden', points.length === 0);
  if (!points.length) return;
  const minimum = Math.min(...points);
  const maximum = Math.max(...points);
  const bucketCount = Math.min(24, Math.max(1, Math.ceil(Math.sqrt(points.length))));
  const width = Math.max(1, maximum - minimum + 1);
  const buckets = Array(bucketCount).fill(0);
  points.forEach(value => {
    const index = Math.min(bucketCount - 1, Math.floor(((value - minimum) / width) * bucketCount));
    buckets[index]++;
  });
  const peak = Math.max(...buckets);
  const bars = $('#timeline-bars');
  bars.replaceChildren();
  buckets.forEach((count, index) => {
    const bar = document.createElement('span');
    bar.className = 'timeline-bar';
    bar.style.height = count ? `${Math.max(4, (count / peak) * 100)}%` : '0';
    const start = new Date(minimum + (width * index) / bucketCount);
    bar.title = `${count} event${count === 1 ? '' : 's'} from ${start.toLocaleString()}`;
    bars.append(bar);
  });
  $('#timeline-summary').textContent = `${points.length.toLocaleString()} timestamped rows`;
  $('#timeline-start').textContent = new Date(minimum).toLocaleString();
  $('#timeline-end').textContent = new Date(maximum).toLocaleString();
}

function renderResultsPanelView() {
  const showingQueries = resultsPanelView === 'queries';
  $('#results-content').classList.toggle('hidden', showingQueries);
  $('#queries-pane').classList.toggle('hidden', !showingQueries);
  $('#results-view').classList.toggle('selected', !showingQueries);
  $('#results-view').setAttribute('aria-selected', String(!showingQueries));
  $('#queries-view').classList.toggle('selected', showingQueries);
  $('#queries-view').setAttribute('aria-selected', String(showingQueries));
}

function showRaw(value) {
  $('#raw-json').textContent = JSON.stringify(value, null, 2);
  $('#raw-dialog').showModal();
}

function queryLabel(query) {
  const line = query.split('\n').map(value => value.trim()).find(Boolean) || 'Query';
  return line.length > 42 ? `${line.slice(0, 39)}...` : line;
}

function querySource(query) {
  return query.match(/^(?:\s*)([A-Za-z_][A-Za-z0-9_]*)(?=\s*(?:\||$))/m)?.[1];
}

function recordQuery(query) {
  const normalized = query.trim();
  if (!normalized) return;
  queryHistory = [
    { query: normalized, runAt: new Date().toISOString() },
    ...queryHistory.filter(item => item.query !== normalized),
  ].slice(0, 10);
  writeStored(storageKeys.history, queryHistory);
  renderQueryLibrary();
}

function saveCurrentQuery() {
  const query = editor.state.doc.toString().trim();
  if (!query) return;
  const name = window.prompt('Name this query', queryLabel(query));
  if (!name?.trim()) return;
  savedQueries = [{
    id: crypto.randomUUID?.() || `${Date.now()}-${Math.random()}`,
    name: name.trim(),
    query,
    savedAt: new Date().toISOString(),
  }, ...savedQueries];
  writeStored(storageKeys.saved, savedQueries);
  renderQueryLibrary();
}

async function shareCurrentQuery() {
  const url = new URL(window.location.href);
  url.searchParams.set('q', editor.state.doc.toString());
  window.history.replaceState(null, '', url);
  try {
    await navigator.clipboard.writeText(url.toString());
    $('#query-stats').textContent = 'Share link copied';
  } catch {
    $('#query-stats').textContent = 'Share link added to address bar';
  }
}

function createQueryListItem(item, saved) {
  const container = document.createElement('div');
  container.className = 'compact-row';
  const open = document.createElement('button');
  open.className = 'compact-main';
  const title = document.createElement('strong');
  title.textContent = saved ? item.name : new Date(item.runAt).toLocaleString();
  const detail = document.createElement('code');
  detail.className = 'query-preview';
  detail.textContent = item.query;
  open.append(title, detail);
  open.addEventListener('click', () => replaceQuery(item.query));
  container.append(open);
  if (saved) {
    const remove = document.createElement('button');
    remove.className = 'compact-action';
    remove.textContent = 'Remove';
    remove.addEventListener('click', () => {
      savedQueries = savedQueries.filter(query => query.id !== item.id);
      writeStored(storageKeys.saved, savedQueries);
      renderQueryLibrary();
    });
    container.append(remove);
  } else {
    container.classList.add('history-row');
  }
  return container;
}

function renderQueryLibrary() {
  const savedList = $('#saved-query-list');
  const historyList = $('#query-history');
  savedList.replaceChildren();
  historyList.replaceChildren();
  $('#saved-count').textContent = savedQueries.length;
  $('#history-count').textContent = queryHistory.length;
  const showingHistory = queryLibraryView === 'history';
  savedList.classList.toggle('hidden', showingHistory);
  historyList.classList.toggle('hidden', !showingHistory);
  $('#saved-view').classList.toggle('selected', !showingHistory);
  $('#saved-view').setAttribute('aria-selected', String(!showingHistory));
  $('#history-view').classList.toggle('selected', showingHistory);
  $('#history-view').setAttribute('aria-selected', String(showingHistory));
  $('#clear-history').classList.toggle('hidden', !showingHistory || queryHistory.length === 0);
  if (!savedQueries.length) savedList.innerHTML = '<span class="muted">No saved queries.</span>';
  if (!queryHistory.length) historyList.innerHTML = '<span class="muted">No query history.</span>';
  savedQueries.forEach(item => savedList.append(createQueryListItem(item, true)));
  queryHistory.forEach(item => historyList.append(createQueryListItem(item, false)));
}

function toggleBookmark(row, rowKey, button) {
  const existing = bookmarks.find(item => item.rowKey === rowKey);
  if (existing) {
    bookmarks = bookmarks.filter(item => item.rowKey !== rowKey);
    button.textContent = 'Save';
    button.title = 'Bookmark event';
  } else {
    bookmarks = [{
      id: crypto.randomUUID?.() || `${Date.now()}-${Math.random()}`,
      rowKey,
      row,
      query: editor.state.doc.toString(),
      table: selectedTable,
      note: '',
      createdAt: new Date().toISOString(),
    }, ...bookmarks];
    button.textContent = 'Saved';
    button.title = 'Remove bookmark';
  }
  writeStored(storageKeys.bookmarks, bookmarks);
  renderBookmarks();
  refreshBookmarkButtons();
}

function refreshBookmarkButtons() {
  document.querySelectorAll('.bookmark-toggle').forEach(button => {
    const saved = bookmarks.some(item => item.rowKey === button.rowKey);
    button.textContent = saved ? 'Saved' : 'Save';
    button.title = saved ? 'Remove bookmark' : 'Bookmark event';
  });
}

function bookmarkLabel(bookmark) {
  const row = bookmark.row;
  return String(row.EventType || row.Message || row.User || row.Host || 'Bookmarked event');
}

function renderBookmarks() {
  const list = $('#bookmark-list');
  list.replaceChildren();
  $('#bookmark-count').textContent = bookmarks.length;
  if (!bookmarks.length) {
    list.innerHTML = '<span class="muted">No bookmarked events.</span>';
    return;
  }
  bookmarks.forEach(bookmark => {
    const container = document.createElement('div');
    container.className = 'compact-row bookmark-row';
    const open = document.createElement('button');
    open.className = 'compact-main';
    const title = document.createElement('strong');
    title.textContent = bookmarkLabel(bookmark);
    const detail = document.createElement('small');
    detail.textContent = bookmark.note || bookmark.row.TimeGenerated || bookmark.table;
    open.append(title, detail);
    open.addEventListener('click', () => showRaw(bookmark.row));
    const actions = document.createElement('span');
    actions.className = 'bookmark-actions';
    const note = document.createElement('button');
    note.className = 'compact-action';
    note.textContent = bookmark.note ? 'Edit note' : 'Add note';
    note.addEventListener('click', () => {
      const value = window.prompt('Bookmark note', bookmark.note || '');
      if (value === null) return;
      bookmark.note = value.trim();
      writeStored(storageKeys.bookmarks, bookmarks);
      renderBookmarks();
    });
    const remove = document.createElement('button');
    remove.className = 'compact-action';
    remove.textContent = 'Remove';
    remove.addEventListener('click', () => {
      bookmarks = bookmarks.filter(item => item.id !== bookmark.id);
      writeStored(storageKeys.bookmarks, bookmarks);
      renderBookmarks();
      refreshBookmarkButtons();
    });
    actions.append(note, remove);
    container.append(open, actions);
    list.append(container);
  });
}

function replaceQuery(query) {
  editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: query } });
  clearDiagnostics();
  const source = querySource(query);
  if (source && availableTableMetadata.some(table => table.name === source)) {
    selectedTable = source;
    renderTables();
    renderFields($('#field-search').value);
  }
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

function selectTable(name) {
  selectedTable = name;
  const query = editor.state.doc.toString();
  const sourceLine = /^(\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*)(?=\||$)/m;
  if (sourceLine.test(query)) {
    replaceQuery(query.replace(sourceLine, (_, leading, _source, trailing) => `${leading}${name}${trailing}`));
  } else {
    replaceQuery(`${name}\n| take 100`);
  }
  renderTables();
  renderFields($('#field-search').value);
}

function renderTables() {
  const list = $('#table-list');
  list.replaceChildren();
  availableTableMetadata.forEach(table => {
    const button = document.createElement('button');
    button.className = 'table-row';
    button.classList.toggle('selected', table.name === selectedTable);
    button.setAttribute('aria-pressed', String(table.name === selectedTable));
    const identity = document.createElement('span');
    const name = document.createElement('strong');
    name.textContent = table.name;
    const description = document.createElement('small');
    description.textContent = table.description;
    identity.append(name, description);
    const count = document.createElement('span');
    count.className = 'table-count';
    count.textContent = Number(table.eventCount).toLocaleString();
    button.append(identity, count);
    button.addEventListener('click', () => selectTable(table.name));
    list.append(button);
  });
}

function renderFields(filter = '') {
  const list = $('#field-list');
  const normalized = filter.trim().toLowerCase();
  list.replaceChildren();
  const selectedGroups = selectedTable === 'Events'
    ? fieldGroups
    : fieldGroups.filter(group => group.table === selectedTable);
  const groups = [{ table: 'Common', fields: commonFields }, ...selectedGroups]
    .map(group => ({
      ...group,
      fields: group.fields.filter(field => field.path.toLowerCase().includes(normalized)
        || group.table.toLowerCase().includes(normalized)),
    }))
    .filter(group => group.fields.length > 0);
  const fieldCount = groups.reduce((count, group) => count + group.fields.length, 0);
  $('#field-count').textContent = `${fieldCount} fields`;
  if (!groups.length) {
    const empty = document.createElement('span');
    empty.className = 'muted';
    empty.textContent = 'No matching fields.';
    list.append(empty);
    return;
  }
  groups.forEach(group => {
    const heading = document.createElement('div');
    heading.className = 'field-group';
    heading.textContent = group.table;
    list.append(heading);
    group.fields.forEach(field => {
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
  });
}

async function loadFields() {
  try {
    const [result, schema] = await Promise.all([request('/api/fields'), request('/api/schema')]);
    commonFields = result.common;
    fieldGroups = result.tables;
    availableTableMetadata = schema.tables;
    const source = querySource(editor.state.doc.toString());
    if (availableTableMetadata.some(table => table.name === source)) selectedTable = source;
    fieldCompletions = [
      ...commonFields.map(field => ({ ...field, table: 'Common' })),
      ...fieldGroups.flatMap(group => group.fields.map(field => ({ ...field, table: group.table }))),
    ].map(field => ({
      label: field.path,
      type: field.type === 'dynamic' ? 'property' : 'variable',
      detail: `${field.type} · ${field.table}`,
    }));
    tableCompletions = [
      { label: 'Events', type: 'class', detail: 'All datasets' },
      ...fieldGroups.map(group => ({ label: group.table, type: 'class', detail: 'Dataset table' })),
    ];
    fieldGroups.forEach(group => availableTables.add(group.table));
    renderTables();
    renderFields();
  } catch {
    $('#field-count').textContent = 'Unavailable';
    $('#table-list').innerHTML = '<span class="muted">Could not load tables.</span>';
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
document.addEventListener('keydown', event => {
  const isRunShortcut = event.key === 'Enter' && (event.ctrlKey || event.metaKey)
    && !event.altKey && !event.shiftKey;
  const isUnrelatedInput = event.target.closest?.('input, textarea, select, [contenteditable="true"]')
    && !event.target.closest('#query');
  if (event.defaultPrevented || !isRunShortcut || rawDialog.open || isUnrelatedInput) return;
  event.preventDefault();
  runQuery();
});
$('#run-query').addEventListener('click', runQuery);
$('#save-query').addEventListener('click', saveCurrentQuery);
$('#share-query').addEventListener('click', shareCurrentQuery);
$('#vim-toggle').addEventListener('click', toggleVim);
$('#results-view').addEventListener('click', () => {
  resultsPanelView = 'results';
  renderResultsPanelView();
});
$('#queries-view').addEventListener('click', () => {
  resultsPanelView = 'queries';
  renderResultsPanelView();
});
$('#saved-view').addEventListener('click', () => {
  queryLibraryView = 'saved';
  renderQueryLibrary();
});
$('#history-view').addEventListener('click', () => {
  queryLibraryView = 'history';
  renderQueryLibrary();
});
$('#clear-history').addEventListener('click', () => {
  queryHistory = [];
  writeStored(storageKeys.history, queryHistory);
  renderQueryLibrary();
});
$('#field-search').addEventListener('input', event => renderFields(event.target.value));
document.querySelectorAll('.example').forEach(button => {
  button.addEventListener('click', () => replaceQuery(button.dataset.query));
});

renderQueryLibrary();
renderBookmarks();
renderResultsPanelView();
loadFields();
