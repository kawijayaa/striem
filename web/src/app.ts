import type { Completion } from '@codemirror/autocomplete';
import { request } from './api';
import { createQueryEditor } from './features/query-editor';
import {
  createResultsCsv,
  nextResultSort,
  resultContext,
  resultIdentity,
  resultRowKey,
  resultTime,
  resultValue,
  sortResultRows,
} from './features/results';
import type { ResultSort } from './features/results';
import { createTimeline } from './features/timeline';
import { addTimeRangeFilter, addValueFilter, querySource, replaceQuerySource } from './kql/query';
import {
  createBrowserStorage,
  readBookmarks,
  readQueryHistory,
  readSavedQueries,
  storageKeys,
} from './storage';
import type {
  AnswerResponse,
  Bookmark,
  ChallengeState,
  EventRow,
  FieldGroup,
  FieldMetadata,
  FieldsResponse,
  QueryError,
  QueryHistoryItem,
  QueryResult,
  SavedQuery,
  SchemaResponse,
  SchemaTable,
} from './types';
import { createEventDialog } from './ui/event-dialog';
import { createMobileWorkspace } from './ui/mobile';
import { enableTabKeyboardNavigation, renderTabSelection } from './ui/tabs';
import { createToast } from './ui/toast';
import type { ToastAction } from './ui/toast';

function $<T extends Element = HTMLElement>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) throw new Error(`Missing element ${selector}`);
  return element;
}

const ui = {
  columnSort: 'column-sort',
  rowIconAction: 'row-icon-action',
  rawValue: 'raw-value',
  compactRow: 'compact-row',
  compactMain: 'compact-main',
  compactAction: 'compact-action',
  fieldGroup: 'field-group',
  fieldRow: 'field-row',
  questionCard: 'question-card',
  questionHead: 'question-card-head',
  questionPrompt: 'question-prompt',
  questionForm: 'question-form',
  muted: 'muted',
} as const;
const toast = createToast($('#toast'), $('#toast-message'), $('#toast-action'));
const showToast = (message: string, action?: ToastAction) => toast.show(message, action);
const browserStorage = createBrowserStorage(() => {
  showToast('Browser storage is unavailable. Changes will not persist.');
});
const writeStored = <T>(key: string, value: T) => browserStorage.write(key, value);
const onboardingDialog = $<HTMLDialogElement>('#onboarding-dialog');
let onboardingSeen = browserStorage.readBoolean(storageKeys.onboarding);
$('#show-onboarding').addEventListener('click', () => onboardingDialog.showModal());
onboardingDialog.addEventListener('close', () => {
  if (onboardingSeen) return;
  onboardingSeen = true;
  writeStored(storageKeys.onboarding, true);
});
const eventDialog = createEventDialog(document, showToast);
const showRaw = (value: unknown) => eventDialog.show(value);
const mobileWorkspace = createMobileWorkspace(document, () => runQuery());
const timeline = createTimeline({
  root: $('#result-timeline'),
  bars: $('#timeline-bars'),
  start: $('#timeline-start'),
  end: $('#timeline-end'),
}, addTimelineFilter);
const availableTables = new Set(['Events']);

let commonFields: FieldMetadata[] = [];
let fieldGroups: FieldGroup[] = [];
let dataSources: SchemaTable[] = [];
let fieldCompletions: Completion[] = [];
let tableCompletions: Completion[] = [{ label: 'Events', type: 'class' }];
let activeTable = 'Events';

let queryHistory = readQueryHistory(browserStorage);
let savedQueries = readSavedQueries(browserStorage);
let bookmarks = readBookmarks(browserStorage);
let queryLibraryView: 'saved' | 'history' = 'saved';
let resultsPanelView: 'results' | 'queries' = 'results';
let sidePanelView: 'fields' | 'questions' | 'bookmarks' = 'fields';
let lastResult: QueryResult | null = null;
let resultSort: ResultSort = { column: null, direction: 'asc' };
const resultColumnWidths = new Map<string, number>();
let selectedResultKey: string | null = null;
let queryRunning = false;
let queryController: AbortController | null = null;
let validationTimer: number | undefined;
let validationController: AbortController | null = null;
let validationGeneration = 0;
let activeBookmarkId: string | null = null;
let challengeState: ChallengeState = { questions: [], solvedQuestions: 0, totalQuestions: 0, completed: false };
const questionFeedback = new Map<string, string>();
const questionDrafts = new Map<string, string>();
const questionCooldowns = new Map<string, number>();
const questionCooldownTimers = new Map<string, number>();
let questionRequestGeneration = 0;
let questionsLoaded = false;
let questionSubmitting = false;
const sharedQuery = new URL(window.location.href).searchParams.get('q');

const queryEditor = createQueryEditor({
  parent: $('#query'),
  initialDocument: sharedQuery || 'Events\n| order by TimeGenerated desc\n| take 100',
  availableTables,
  getTableCompletions: () => tableCompletions,
  getFieldCompletions: () => fieldCompletions,
  onDocumentChange: () => {
    queryController?.abort();
    syncActiveTable();
    $('#query-error').classList.add('hidden');
    scheduleQueryValidation(editor.state.doc.toString());
  },
  onRun: () => void runQuery(),
});
const editor = queryEditor.view;

function asQueryError(error: unknown): QueryError {
  if (!error || typeof error !== 'object') return { message: String(error) };
  const candidate = error as Record<string, unknown>;
  const queryError: QueryError = {};
  if (typeof candidate.error === 'string') queryError.error = candidate.error;
  if (typeof candidate.message === 'string') queryError.message = candidate.message;
  if (typeof candidate.retryAfterMs === 'number') queryError.retryAfterMs = candidate.retryAfterMs;
  const position = candidate.position;
  if (position && typeof position === 'object') {
    const coordinates = position as Record<string, unknown>;
    if (typeof coordinates.line === 'number' && typeof coordinates.column === 'number') {
      queryError.position = { line: coordinates.line, column: coordinates.column };
    }
  }
  return queryError;
}

function cancelQueryValidation(): void {
  window.clearTimeout(validationTimer);
  validationTimer = undefined;
  validationController?.abort();
  validationController = null;
  validationGeneration++;
}

function scheduleQueryValidation(query: string): void {
  cancelQueryValidation();
  if (!query.trim()) return;
  const generation = validationGeneration;
  validationTimer = window.setTimeout(async () => {
    const controller = new AbortController();
    validationController = controller;
    try {
      await request<void>('/api/query/validate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query }),
        signal: controller.signal,
      });
    } catch (error) {
      if (controller.signal.aborted || generation !== validationGeneration) return;
      const queryError = asQueryError(error);
      if (queryError.position) queryEditor.showDiagnostic(queryError);
    } finally {
      if (validationController === controller) validationController = null;
    }
  }, 400);
}

async function runQuery() {
  if (queryRunning) {
    queryController?.abort();
    return;
  }
  cancelQueryValidation();
  queryRunning = true;
  mobileWorkspace.setRunning(true);
  const controller = new AbortController();
  queryController = controller;
  const errorBox = $('#query-error');
  const runButton = $<HTMLButtonElement>('#run-query');
  const mobileRunButton = $<HTMLButtonElement>('#mobile-run-query');
  const query = editor.state.doc.toString();
  errorBox.classList.add('hidden');
  queryEditor.clearDiagnostics();
  runButton.classList.add('loading');
  runButton.setAttribute('aria-label', 'Cancel running query');
  mobileRunButton.setAttribute('aria-label', 'Cancel running query');
  $('#run-label').textContent = 'Cancel';
  $('#mobile-run-label').textContent = 'Cancel';
  $('#query-stats').textContent = 'Running query';
  $('#results-content').classList.toggle('query-stale', lastResult !== null);
  $('#results-content').setAttribute('aria-busy', 'true');
  resultsPanelView = 'results';
  renderResultsPanelView();
  recordQuery(query);
  const started = performance.now();
  const elapsedTimer = window.setInterval(() => {
    const elapsed = Math.max(1, Math.floor((performance.now() - started) / 1000));
    $('#query-stats').textContent = `Running query · ${elapsed} s`;
  }, 1000);
  try {
    const result = await request<QueryResult>('/api/query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query }),
      signal: controller.signal,
    });
    renderResults(result);
    if (window.matchMedia('(max-width: 900px)').matches) {
      mobileWorkspace.show('results');
    }
    $('#query-stats').textContent = `${result.rowCount} rows · ${result.durationMs} ms`;
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      $('#query-stats').textContent = 'Query canceled';
      return;
    }
    const queryError = asQueryError(error);
    const position = queryError.position ? `Line ${queryError.position.line}, column ${queryError.position.column}: ` : '';
    const serverMessage = queryError.error || queryError.message;
    const message = !serverMessage || /failed to fetch|invalid server response/i.test(serverMessage)
      ? 'The query service did not respond. Your query is unchanged. Try again.'
      : serverMessage;
    errorBox.textContent = position + message;
    errorBox.classList.remove('hidden');
    $('#query-stats').textContent = 'Query failed';
    queryEditor.showDiagnostic(queryError);
  } finally {
    window.clearInterval(elapsedTimer);
    queryRunning = false;
    mobileWorkspace.setRunning(false);
    if (queryController === controller) queryController = null;
    runButton.classList.remove('loading');
    runButton.removeAttribute('aria-label');
    mobileRunButton.setAttribute('aria-label', 'Run query');
    $('#run-label').textContent = 'Run query';
    $('#mobile-run-label').textContent = 'Run';
    $('#results-content').classList.remove('query-stale');
    $('#results-content').removeAttribute('aria-busy');
  }
}

function sortedResultRows(result: QueryResult): EventRow[] {
  return sortResultRows(result, resultSort);
}

function sortResults(column: string): void {
  resultSort = nextResultSort(resultSort, column);
  if (lastResult) renderResults(lastResult, false);
  document.querySelector<HTMLButtonElement>(`.column-sort[data-column="${CSS.escape(column)}"]`)?.focus();
}

function addColumnResizer(header: HTMLTableCellElement, column: string): void {
  const resizer = document.createElement('span');
  resizer.className = 'column-resizer';
  resizer.setAttribute('role', 'separator');
  resizer.setAttribute('aria-orientation', 'vertical');
  resizer.setAttribute('aria-label', `Resize ${column} column`);
  resizer.setAttribute('aria-valuemin', '96');
  resizer.tabIndex = 0;
  const resize = (width: number) => {
    const next = Math.max(96, width);
    resultColumnWidths.set(column, next);
    header.style.width = `${next}px`;
    header.style.minWidth = `${next}px`;
    header.style.maxWidth = `${next}px`;
    resizer.setAttribute('aria-valuenow', String(Math.round(next)));
    resizer.setAttribute('aria-valuetext', `${Math.round(next)} pixels wide`);
  };
  resizer.addEventListener('pointerdown', event => {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = header.getBoundingClientRect().width;
    resizer.setPointerCapture(event.pointerId);
    const move = (moveEvent: PointerEvent) => resize(startWidth + moveEvent.clientX - startX);
    const stop = () => {
      resizer.removeEventListener('pointermove', move);
      resizer.removeEventListener('pointerup', stop);
    };
    resizer.addEventListener('pointermove', move);
    resizer.addEventListener('pointerup', stop);
  });
  resizer.addEventListener('keydown', event => {
    if (!['ArrowLeft', 'ArrowRight'].includes(event.key)) return;
    event.preventDefault();
    resize(header.getBoundingClientRect().width + (event.key === 'ArrowRight' ? 16 : -16));
  });
  const storedWidth = resultColumnWidths.get(column);
  if (storedWidth) resize(storedWidth);
  const initialWidth = storedWidth || 96;
  resizer.setAttribute('aria-valuenow', String(Math.round(initialWidth)));
  resizer.setAttribute('aria-valuetext', `${Math.round(initialWidth)} pixels wide`);
  header.append(resizer);
  if (!storedWidth) window.requestAnimationFrame(() => {
    if (!header.isConnected) return;
    const renderedWidth = Math.max(96, header.getBoundingClientRect().width);
    resizer.setAttribute('aria-valuenow', String(Math.round(renderedWidth)));
    resizer.setAttribute('aria-valuetext', `${Math.round(renderedWidth)} pixels wide`);
  });
}

function createRowAction(
  label: string,
  iconPath: string,
  handler: (button: HTMLButtonElement) => void,
  active = false,
): HTMLButtonElement {
  const button = document.createElement('button');
  button.className = `${ui.rowIconAction}${active ? ' active' : ''}`;
  button.setAttribute('aria-label', label);
  button.title = label;
  button.innerHTML = `<svg viewBox="0 0 16 16" aria-hidden="true"><path d="${iconPath}"/></svg>`;
  button.addEventListener('click', event => {
    event.stopPropagation();
    handler(button);
  });
  return button;
}

function renderMobileResult(row: EventRow, rowKey: string, columns: string[]): HTMLElement {
  const card = document.createElement('article');
  card.className = 'mobile-result-card';
  const open = document.createElement('button');
  open.className = 'mobile-result-card-main';
  open.setAttribute('aria-label', `Open ${resultIdentity(row)} event details`);
  const title = document.createElement('strong');
  title.textContent = resultIdentity(row);
  open.append(title);
  const contextValue = resultContext(row);
  if (contextValue) {
    const context = document.createElement('span');
    context.textContent = contextValue;
    open.append(context);
  }
  const timeValue = resultTime(row);
  if (timeValue) {
    const time = document.createElement('time');
    time.textContent = timeValue;
    time.dateTime = String(row.TimeGenerated);
    open.append(time);
  }
  const standardColumns = new Set(['TimeGenerated', 'Source', 'EventType', 'Host', 'User', 'Message', 'RawData']);
  const details = columns.filter(column => !standardColumns.has(column)
    && row[column] !== null && row[column] !== undefined && typeof row[column] !== 'object').slice(0, 3);
  if (details.length) {
    const detailList = document.createElement('dl');
    detailList.className = 'mobile-result-details';
    details.forEach(column => {
      const item = document.createElement('div');
      const term = document.createElement('dt');
      term.textContent = column;
      const value = document.createElement('dd');
      value.textContent = resultValue(row[column]);
      value.title = value.textContent;
      item.append(term, value);
      detailList.append(item);
    });
    open.append(detailList);
  }
  open.addEventListener('click', () => showRaw(row));
  const actions = document.createElement('div');
  actions.className = 'mobile-result-card-actions';
  const saved = bookmarks.some(item => item.rowKey === rowKey);
  const bookmark = createRowAction(saved ? 'Remove bookmark' : 'Bookmark event', 'M4 2.5h8v11L8 10.8 4 13.5z', button => toggleBookmark(row, rowKey, button), saved) as HTMLButtonElement & { rowKey: string };
  bookmark.classList.add('bookmark-toggle');
  bookmark.rowKey = rowKey;
  actions.append(bookmark);
  card.append(open, actions);
  return card;
}

function migrateLegacyBookmarkKeys(result: QueryResult): void {
  const claimed = new Set<number>();
  let changed = false;
  bookmarks.forEach(bookmark => {
    if (bookmark.rowKey.startsWith('v2:') || bookmark.query !== editor.state.doc.toString()) return;
    const index = result.rows.findIndex((row, candidate) => !claimed.has(candidate)
      && JSON.stringify(row) === bookmark.rowKey);
    if (index < 0) return;
    claimed.add(index);
    bookmark.rowKey = `v2:${index}:${bookmark.rowKey}`;
    changed = true;
  });
  if (changed) writeStored(storageKeys.bookmarks, bookmarks);
}

function renderResults(result: QueryResult, resetSort = true): void {
  lastResult = result;
  migrateLegacyBookmarkKeys(result);
  if (resetSort) resultSort = { column: null, direction: 'asc' };
  const head = $('#result-head');
  const body = $('#result-body');
  const mobileList = $('#mobile-result-list');
  head.replaceChildren();
  body.replaceChildren();
  mobileList.replaceChildren();
  const headerRow = document.createElement('tr');
  const actionHeader = document.createElement('th');
  actionHeader.className = 'row-action';
  actionHeader.setAttribute('aria-label', 'Row actions');
  headerRow.append(actionHeader);
  result.columns.forEach(column => {
    const th = document.createElement('th');
    const sort = document.createElement('button');
    sort.className = ui.columnSort;
    sort.dataset.column = column;
    sort.textContent = column;
    sort.addEventListener('click', () => sortResults(column));
    if (resultSort.column === column) {
      th.setAttribute('aria-sort', resultSort.direction === 'asc' ? 'ascending' : 'descending');
      const indicator = document.createElement('span');
      indicator.className = 'sort-indicator';
      indicator.textContent = resultSort.direction === 'asc' ? 'up' : 'down';
      sort.append(indicator);
    } else {
      th.setAttribute('aria-sort', 'none');
    }
    th.append(sort);
    addColumnResizer(th, column);
    headerRow.append(th);
  });
  head.append(headerRow);
  sortedResultRows(result).forEach((row, rowIndex) => {
    const tr = document.createElement('tr');
    tr.style.setProperty('--row', String(Math.min(rowIndex, 16)));
    const rowKey = resultRowKey(result, row);
    tr.classList.toggle('selected', rowKey === selectedResultKey);
    tr.setAttribute('aria-selected', String(rowKey === selectedResultKey));
    const selectRow = () => {
      selectedResultKey = rowKey;
      body.querySelectorAll('tr').forEach(candidate => {
        const selected = candidate === tr;
        candidate.classList.toggle('selected', selected);
        candidate.setAttribute('aria-selected', String(selected));
      });
    };
    tr.addEventListener('click', selectRow);
    const action = document.createElement('td');
    action.className = 'row-action';
    const saved = bookmarks.some(item => item.rowKey === rowKey);
    const view = createRowAction('View event details', 'M2.5 3.5h11v9h-11zM5 6h6M5 8.5h6M5 11h3', () => showRaw(row));
    const bookmark = createRowAction(saved ? 'Remove bookmark' : 'Bookmark event', 'M4 2.5h8v11L8 10.8 4 13.5z', button => toggleBookmark(row, rowKey, button), saved) as HTMLButtonElement & { rowKey: string };
    view.classList.add('result-row-action');
    bookmark.classList.add('result-row-action');
    view.tabIndex = rowIndex === 0 ? 0 : -1;
    bookmark.tabIndex = -1;
    bookmark.classList.add('bookmark-toggle');
    bookmark.rowKey = rowKey;
    action.append(view, bookmark);
    tr.append(action);
    result.columns.forEach(column => {
      const td = document.createElement('td');
      const value = row[column];
      if (value !== null && typeof value === 'object') {
        td.className = 'raw';
        const dynamic = document.createElement('span');
        dynamic.className = ui.rawValue;
        dynamic.textContent = 'JSON';
        td.append(dynamic);
      } else {
        if (value === null || value === undefined) td.className = 'nil';
        else if (typeof value === 'number') td.className = 'num';
        else if (column === 'TimeGenerated') td.className = 'time';
        td.textContent = value === null || value === undefined ? '—' : String(value);
        td.title = value === null || value === undefined ? 'null' : String(value);
        td.addEventListener('dblclick', event => {
          event.preventDefault();
          pivotOnValue(column, value, event.altKey);
        });
      }
      tr.append(td);
    });
    body.append(tr);
    mobileList.append(renderMobileResult(row, rowKey, result.columns));
  });
  const emptyState = $('#empty-results');
  emptyState.classList.toggle('hidden', result.rows.length > 0);
  $('#empty-results-title').textContent = result.rows.length ? '' : 'No events matched';
  $('#table-wrap').classList.toggle('hidden', result.rows.length === 0);
  mobileList.classList.toggle('hidden', result.rows.length === 0);
  $('#mobile-result-count').textContent = result.rowCount.toLocaleString();
  $<HTMLButtonElement>('#export-results').disabled = result.rows.length === 0;
  timeline.render(result.rows);
}

function exportResults() {
  if (!lastResult?.rows.length) return;
  const result = lastResult;
  const rows = sortedResultRows(result);
  const blob = new Blob([createResultsCsv(result, rows)], { type: 'text/csv;charset=utf-8' });
  const link = document.createElement('a');
  link.href = URL.createObjectURL(blob);
  link.download = `striem-results-${new Date().toISOString().replaceAll(':', '-')}.csv`;
  link.click();
  URL.revokeObjectURL(link.href);
  showToast(`Exported ${rows.length.toLocaleString()} rows`);
}

function addTimelineFilter(start: Date, end: Date): void {
  replaceQuery(addTimeRangeFilter(editor.state.doc.toString(), start, end));
  mobileWorkspace.show('query');
  showToast('Time range added to the query', {
    label: 'Run',
    handler: () => void runQuery(),
  });
}

function pivotOnValue(column: string, value: unknown, negate: boolean): void {
  const previous = editor.state.doc.toString();
  replaceQuery(addValueFilter(previous, column, value, negate));
  showToast(`Filter added: ${column} ${negate ? '!=' : '=='} ${String(value).slice(0, 40)}`, {
    label: 'Undo',
    handler: () => replaceQuery(previous),
  });
}

function renderResultsPanelView() {
  renderTabSelection(resultsPanelView, [
    { name: 'results', tab: $('#results-view'), panel: $('#results-content') },
    { name: 'queries', tab: $('#queries-view'), panel: $('#queries-pane') },
  ]);
}

function queryLabel(query: string): string {
  const line = query.split('\n').map(value => value.trim()).find(Boolean) || 'Query';
  return line.length > 42 ? `${line.slice(0, 39)}...` : line;
}

function syncActiveTable() {
  const source = querySource(editor.state.doc.toString());
  if (!source || source === activeTable || !availableTables.has(source)) return;
  activeTable = source;
  renderDataSources();
  renderFields($<HTMLInputElement>('#field-search').value);
}

function recordQuery(query: string): void {
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
  $<HTMLInputElement>('#save-query-name').value = queryLabel(query);
  $<HTMLDialogElement>('#save-query-dialog').showModal();
  $<HTMLInputElement>('#save-query-name').select();
}

function saveNamedQuery(name: string): void {
  const query = editor.state.doc.toString().trim();
  if (!query || !name.trim()) return;
  savedQueries = [{
    id: crypto.randomUUID?.() || `${Date.now()}-${Math.random()}`,
    name: name.trim(),
    query,
    savedAt: new Date().toISOString(),
  }, ...savedQueries];
  const persisted = writeStored(storageKeys.saved, savedQueries);
  renderQueryLibrary();
  if (persisted) showToast('Query saved to this browser');
}

async function shareCurrentQuery() {
  const url = new URL(window.location.href);
  url.searchParams.set('q', editor.state.doc.toString());
  if (url.toString().length > 7000) {
    showToast('This query is too long to share as a link');
    return;
  }
  window.history.replaceState(null, '', url);
  try {
    await navigator.clipboard.writeText(url.toString());
    showToast('Share link copied');
  } catch {
    showToast('Share link added to the address bar');
  }
}

function createQueryListItem(item: SavedQuery | QueryHistoryItem): HTMLElement {
  const saved = 'id' in item;
  const container = document.createElement('div');
  container.className = ui.compactRow;
  const open = document.createElement('button');
  open.className = ui.compactMain;
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
    remove.className = ui.compactAction;
    remove.textContent = 'Remove';
    remove.addEventListener('click', () => {
      const previous = [...savedQueries];
      savedQueries = savedQueries.filter(query => query.id !== item.id);
      const persisted = writeStored(storageKeys.saved, savedQueries);
      renderQueryLibrary();
      if (persisted) {
        showToast('Saved query removed', { label: 'Undo', handler: () => {
          savedQueries = previous;
          writeStored(storageKeys.saved, savedQueries);
          renderQueryLibrary();
        } });
      }
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
  $('#saved-count').textContent = String(savedQueries.length);
  $('#history-count').textContent = String(queryHistory.length);
  const showingHistory = queryLibraryView === 'history';
  renderTabSelection(queryLibraryView, [
    { name: 'saved', tab: $('#saved-view'), panel: savedList },
    { name: 'history', tab: $('#history-view'), panel: historyList },
  ]);
  $('#clear-history').classList.toggle('hidden', !showingHistory || queryHistory.length === 0);
  if (!savedQueries.length) savedList.innerHTML = '<span class="muted">No saved queries.</span>';
  if (!queryHistory.length) historyList.innerHTML = '<span class="muted">No query history.</span>';
  savedQueries.forEach(item => savedList.append(createQueryListItem(item)));
  queryHistory.forEach(item => historyList.append(createQueryListItem(item)));
}

function toggleBookmark(row: EventRow, rowKey: string, button: HTMLButtonElement): void {
  const existing = bookmarks.find(item => item.rowKey === rowKey);
  if (existing) {
    const previous = [...bookmarks];
    bookmarks = bookmarks.filter(item => item.rowKey !== rowKey);
    button.classList.remove('active');
    button.setAttribute('aria-label', 'Bookmark event');
    button.title = 'Bookmark event';
    const persisted = writeStored(storageKeys.bookmarks, bookmarks);
    if (persisted) {
      showToast('Bookmark removed', { label: 'Undo', handler: () => {
        bookmarks = previous;
        writeStored(storageKeys.bookmarks, bookmarks);
        renderBookmarks();
        refreshBookmarkButtons();
      } });
    }
  } else {
    bookmarks = [{
      id: crypto.randomUUID?.() || `${Date.now()}-${Math.random()}`,
      rowKey,
      row,
      query: editor.state.doc.toString(),
      table: activeTable,
      note: '',
      createdAt: new Date().toISOString(),
    }, ...bookmarks];
    button.classList.add('active');
    button.setAttribute('aria-label', 'Remove bookmark');
    button.title = 'Remove bookmark';
    writeStored(storageKeys.bookmarks, bookmarks);
  }
  renderBookmarks();
  refreshBookmarkButtons();
}

function refreshBookmarkButtons() {
  document.querySelectorAll<HTMLButtonElement & { rowKey: string }>('.bookmark-toggle').forEach(button => {
    const saved = bookmarks.some(item => item.rowKey === button.rowKey);
    button.classList.toggle('active', saved);
    button.setAttribute('aria-label', saved ? 'Remove bookmark' : 'Bookmark event');
    button.title = saved ? 'Remove bookmark' : 'Bookmark event';
  });
}

function bookmarkLabel(bookmark: Bookmark): string {
  const row = bookmark.row;
  return String(row.EventType || row.Message || row.User || row.Host || 'Bookmarked event');
}

function renderBookmarks() {
  const list = $('#bookmark-list');
  list.replaceChildren();
  $('#bookmark-tab-count').textContent = String(bookmarks.length);
  if (!bookmarks.length) {
    list.innerHTML = '<span class="muted">No bookmarked events.</span>';
    return;
  }
  bookmarks.forEach(bookmark => {
    const container = document.createElement('div');
    container.className = `${ui.compactRow} bookmark-row`;
    const open = document.createElement('button');
    open.className = ui.compactMain;
    const title = document.createElement('strong');
    title.textContent = bookmarkLabel(bookmark);
    open.append(title);
    const detailValue = bookmark.note || bookmark.row.TimeGenerated;
    if (detailValue) {
      const detail = document.createElement('small');
      detail.textContent = String(detailValue);
      open.append(detail);
    }
    open.addEventListener('click', () => showRaw(bookmark.row));
    const actions = document.createElement('span');
    actions.className = 'bookmark-actions';
    const note = document.createElement('button');
    note.className = ui.compactAction;
    note.textContent = bookmark.note ? 'Edit note' : 'Add note';
    note.addEventListener('click', () => {
      activeBookmarkId = bookmark.id;
      $<HTMLTextAreaElement>('#bookmark-note').value = bookmark.note || '';
      $<HTMLDialogElement>('#bookmark-note-dialog').showModal();
      $<HTMLTextAreaElement>('#bookmark-note').focus();
    });
    const remove = document.createElement('button');
    remove.className = ui.compactAction;
    remove.textContent = 'Remove';
    remove.addEventListener('click', () => {
      const previous = [...bookmarks];
      bookmarks = bookmarks.filter(item => item.id !== bookmark.id);
      const persisted = writeStored(storageKeys.bookmarks, bookmarks);
      renderBookmarks();
      refreshBookmarkButtons();
      if (persisted) {
        showToast('Bookmark removed', { label: 'Undo', handler: () => {
          bookmarks = previous;
          writeStored(storageKeys.bookmarks, bookmarks);
          renderBookmarks();
          refreshBookmarkButtons();
        } });
      }
    });
    actions.append(note, remove);
    container.append(open, actions);
    list.append(container);
  });
}

function replaceQuery(query: string): void {
  editor.dispatch({
    changes: { from: 0, to: editor.state.doc.length, insert: query },
    selection: { anchor: query.length },
  });
  queryEditor.clearDiagnostics();
  const source = querySource(query);
  if (source && availableTables.has(source)) {
    activeTable = source;
    renderDataSources();
    renderFields($<HTMLInputElement>('#field-search').value);
  }
  editor.focus();
}

function insertField(path: string): void {
  const selection = editor.state.selection.main;
  const previous = selection.from > 0 ? editor.state.doc.sliceString(selection.from - 1, selection.from) : '';
  const prefix = previous && !/\s/.test(previous) ? ' ' : '';
  editor.dispatch({
    changes: { from: selection.from, to: selection.to, insert: prefix + path },
    selection: { anchor: selection.from + prefix.length + path.length },
  });
  editor.focus();
}

function renderSidePanelView() {
  const views = ['fields', 'questions', 'bookmarks'] as const;
  renderTabSelection(sidePanelView, views.map(name => ({
    name,
    tab: $<HTMLButtonElement>(`[data-side-view="${name}"]`),
    panel: $(`#${name}-pane`),
  })));
}

function renderDataSources(): void {
  const list = $('#source-list');
  list.replaceChildren();
  $('#source-count').textContent = String(dataSources.length);
  if (!dataSources.length) {
    const empty = document.createElement('span');
    empty.className = ui.muted;
    empty.textContent = 'No data sources available.';
    list.append(empty);
    return;
  }
  dataSources.forEach(source => {
    const button = document.createElement('button');
    button.className = 'source-row';
    button.type = 'button';
    button.title = `Use ${source.name} in the current query`;
    button.classList.toggle('selected', source.name === activeTable);
    button.setAttribute('aria-current', source.name === activeTable ? 'true' : 'false');
    const details = document.createElement('span');
    details.className = 'source-details';
    const name = document.createElement('strong');
    name.textContent = source.name;
    const description = document.createElement('small');
    description.textContent = source.description || 'Configured data source';
    details.append(name, description);
    const state = document.createElement('span');
    state.className = 'source-state';
    state.textContent = source.name === activeTable ? 'Active' : 'Use';
    button.append(details, state);
    button.addEventListener('click', () => {
      replaceQuery(replaceQuerySource(editor.state.doc.toString(), source.name));
    });
    list.append(button);
  });
}

function renderQuestions() {
  questionCooldownTimers.forEach(timer => window.clearInterval(timer));
  questionCooldownTimers.clear();
  const list = $('#question-list');
  list.replaceChildren();
  const solved = challengeState.solvedQuestions || 0;
  const total = challengeState.totalQuestions || 0;
  $('#question-tab-count').textContent = `${solved}/${total}`;
  if (!total) {
    const empty = document.createElement('span');
    empty.className = ui.muted;
    empty.textContent = 'No investigation tasks.';
    list.append(empty);
    return;
  }

  if (challengeState.completed && challengeState.flag) {
    const flagValue = challengeState.flag;
    const completion = document.createElement('div');
    completion.className = 'completion-card';
    const title = document.createElement('strong');
    title.textContent = 'Case closed';
    const detail = document.createElement('p');
    detail.textContent = 'Every task is solved. Submit this flag to the CTF platform.';
    const flagRow = document.createElement('div');
    flagRow.className = 'flag-value';
    const flag = document.createElement('code');
    flag.textContent = flagValue;
    flag.title = flagValue;
    const copy = document.createElement('button');
    copy.className = 'secondary';
    copy.textContent = 'Copy';
    copy.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(flagValue);
        showToast('Flag copied');
      } catch {
        showToast('Could not copy the flag');
      }
    });
    flagRow.append(flag, copy);
    completion.append(title, detail, flagRow);
    list.append(completion);
  }

  challengeState.questions.forEach(question => {
    const card = document.createElement('article');
    card.className = ui.questionCard;
    card.classList.toggle('solved', question.solved);
    const heading = document.createElement('div');
    heading.className = ui.questionHead;
    const title = document.createElement('strong');
    title.textContent = question.title;
    heading.append(title);
    if (question.solved) {
      const state = document.createElement('span');
      state.className = 'question-state';
      state.textContent = 'Solved';
      heading.append(state);
    }
    const prompt = document.createElement('p');
    prompt.className = ui.questionPrompt;
    prompt.textContent = question.prompt;
    card.append(heading, prompt);

    if (question.solved && question.answer) {
      const answer = document.createElement('output');
      answer.className = 'mt-3 block overflow-x-auto break-all whitespace-pre-wrap rounded-sm border border-[var(--rule)] bg-[var(--ink-000)] px-3 py-2 font-mono text-xs text-[var(--paper)]';
      answer.setAttribute('aria-label', 'Accepted answer');
      answer.dataset.questionAnswer = question.id;
      answer.tabIndex = -1;
      answer.textContent = question.answer;
      card.append(answer);
    }

    let submitButton: HTMLButtonElement | undefined;
    if (!question.solved || !question.answer) {
      const form = document.createElement('form');
      form.className = ui.questionForm;
      const input = document.createElement('input');
      input.type = 'text';
      input.name = 'answer';
      input.maxLength = 512;
      input.autocomplete = 'off';
      input.required = true;
      input.placeholder = 'Answer';
      input.value = questionDrafts.get(question.id) || '';
      input.setAttribute('aria-label', `Answer for ${question.title}`);
      input.dataset.questionId = question.id;
      input.addEventListener('input', () => {
        questionDrafts.set(question.id, input.value);
        questionFeedback.delete(question.id);
        card.querySelector('.question-feedback')?.remove();
      });
      const submit = document.createElement('button');
      submitButton = submit;
      submit.className = 'primary';
      submit.type = 'submit';
      submit.textContent = 'Check answer';
      const cooldown = (questionCooldowns.get(question.id) || 0) - Date.now();
      if (cooldown > 0) {
        submit.disabled = true;
        submit.textContent = `Try again in ${Math.ceil(cooldown / 1000)} s`;
      }
      form.append(input, submit);
      form.addEventListener('submit', async event => {
        event.preventDefault();
        if (questionSubmitting) return;
        const answer = input.value.trim();
        if (!answer) return;
        questionSubmitting = true;
        questionRequestGeneration++;
        questionDrafts.set(question.id, input.value);
        input.disabled = true;
        submit.disabled = true;
        submit.textContent = 'Checking';
        try {
          const result = await request<AnswerResponse>(`/api/questions/${encodeURIComponent(question.id)}/answer`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ answer }),
          });
          questionRequestGeneration++;
          challengeState = result.state;
          if (!result.correct) questionFeedback.set(question.id, 'No match.');
          if (result.correct) {
            questionDrafts.delete(question.id);
            questionCooldowns.delete(question.id);
          }
          renderQuestions();
          if (!result.correct) window.requestAnimationFrame(() => {
            document.querySelector<HTMLInputElement>(`input[data-question-id="${CSS.escape(question.id)}"]`)?.focus();
          });
          if (result.correct) {
            const message = result.alreadySolved
              ? 'Answer saved'
              : challengeState.completed ? 'Case closed — flag unlocked' : 'Task solved';
            showToast(message);
            window.requestAnimationFrame(() => {
              document.querySelector<HTMLOutputElement>(`output[data-question-answer="${CSS.escape(question.id)}"]`)?.focus();
            });
          }
        } catch (error) {
          questionRequestGeneration++;
          const queryError = asQueryError(error);
          const retry = Number(queryError.retryAfterMs);
          if (retry > 0) questionCooldowns.set(question.id, Date.now() + retry);
          const message = retry > 0
            ? `Too many attempts. Try again in ${Math.max(1, Math.ceil(retry / 1000))} seconds; your answer was not submitted.`
            : (queryError.error || queryError.message || 'Could not submit the answer.');
          questionFeedback.set(question.id, message);
          renderQuestions();
          window.requestAnimationFrame(() => {
            document.querySelector<HTMLInputElement>(`input[data-question-id="${CSS.escape(question.id)}"]`)?.focus();
          });
        } finally {
          questionSubmitting = false;
        }
      });
      card.append(form);
    }
    const feedbackMessage = questionFeedback.get(question.id);
    if (feedbackMessage && (!question.solved || !question.answer)) {
      const feedback = document.createElement('div');
      feedback.className = 'question-feedback incorrect';
      feedback.setAttribute('role', 'status');
      feedback.textContent = feedbackMessage;
      card.append(feedback);
    }
    list.append(card);
    const deadline = questionCooldowns.get(question.id) || 0;
    if (submitButton && deadline > Date.now()) {
      const timer = window.setInterval(() => {
        if (!card.isConnected || !submitButton) {
          window.clearInterval(timer);
          questionCooldownTimers.delete(question.id);
          return;
        }
        const remaining = deadline - Date.now();
        if (remaining > 0) {
          submitButton.textContent = `Try again in ${Math.ceil(remaining / 1000)} s`;
          return;
        }
        window.clearInterval(timer);
        questionCooldownTimers.delete(question.id);
        questionCooldowns.delete(question.id);
        questionFeedback.delete(question.id);
        submitButton.disabled = false;
        submitButton.textContent = 'Check answer';
        card.querySelector('.question-feedback')?.remove();
      }, 250);
      questionCooldownTimers.set(question.id, timer);
    }
  });
}

function renderLoadFailure(target: HTMLElement, message: string, retry: () => void): void {
  const container = document.createElement('div');
  container.className = 'load-failure';
  const detail = document.createElement('span');
  detail.textContent = message;
  const button = document.createElement('button');
  button.className = 'secondary';
  button.type = 'button';
  button.textContent = 'Retry';
  button.addEventListener('click', retry);
  container.append(detail, button);
  target.replaceChildren(container);
}

async function loadQuestions(backgroundRefresh = false) {
  const initialLoad = !questionsLoaded;
  const generation = ++questionRequestGeneration;
  try {
    const nextState = await request<ChallengeState>('/api/questions');
    if (generation !== questionRequestGeneration) return;
    if (backgroundRefresh && JSON.stringify(nextState) === JSON.stringify(challengeState)) return;
    challengeState = nextState;
    questionsLoaded = true;
    renderQuestions();
    if (initialLoad && challengeState.totalQuestions > 0 && sidePanelView === 'fields') {
      sidePanelView = 'questions';
      renderSidePanelView();
    }
  } catch {
    if (generation !== questionRequestGeneration) return;
    if (backgroundRefresh && questionsLoaded) return;
    renderLoadFailure($('#question-list'), 'Tasks could not be loaded.', () => void loadQuestions());
  }
}

function renderFields(filter = ''): void {
  const list = $('#field-list');
  const normalized = filter.trim().toLowerCase();
  list.replaceChildren();
  const selectedGroups = activeTable === 'Events'
    ? fieldGroups
    : fieldGroups.filter(group => group.table === activeTable);
  const groups = [{ table: 'Common', fields: commonFields }, ...selectedGroups]
    .map(group => ({
      ...group,
      fields: group.fields.filter(field => field.path.toLowerCase().includes(normalized)
        || group.table.toLowerCase().includes(normalized)),
    }))
    .filter(group => group.fields.length > 0);
  if (!groups.length) {
    const empty = document.createElement('span');
    empty.className = ui.muted;
    empty.textContent = 'No matching fields.';
    list.append(empty);
    return;
  }
  groups.forEach(group => {
    const heading = document.createElement('div');
    heading.className = ui.fieldGroup;
    heading.textContent = group.table;
    list.append(heading);
    group.fields.forEach(field => {
      const button = document.createElement('button');
      button.className = ui.fieldRow;
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
    const [result, schema] = await Promise.all([
      request<FieldsResponse>('/api/fields'),
      request<SchemaResponse>('/api/schema'),
    ]);
    commonFields = result.common;
    fieldGroups = result.tables;
    dataSources = schema.tables ?? [];
    const challengeName = schema.challengeName?.trim() || 'Investigation workspace';
    const challengeLabel = $('#challenge-name');
    challengeLabel.textContent = challengeName;
    challengeLabel.setAttribute('title', challengeName);
    document.title = `${challengeName} | Striem`;
    fieldGroups.forEach(group => availableTables.add(group.table));
    const source = querySource(editor.state.doc.toString());
    if (source && availableTables.has(source)) activeTable = source;
    fieldCompletions = [
      ...commonFields.map(field => ({ ...field, table: 'Common' })),
      ...fieldGroups.flatMap(group => group.fields.map(field => ({ ...field, table: group.table }))),
    ].map(field => ({
      label: field.path,
      type: field.type === 'dynamic' ? 'property' : 'variable',
      detail: `${field.type} · ${field.table}`,
    }));
    tableCompletions = [
      { label: 'Events', type: 'class' },
      ...fieldGroups.map(group => ({ label: group.table, type: 'class' })),
    ];
    renderDataSources();
    renderFields();
  } catch {
    renderLoadFailure($('#source-list'), 'Data sources could not be loaded.', () => void loadFields());
    renderLoadFailure($('#field-list'), 'Fields could not be loaded.', () => void loadFields());
  }
}

document.querySelectorAll<HTMLDialogElement>('.form-dialog').forEach(dialog => {
  dialog.querySelectorAll('.dialog-close, .dialog-cancel').forEach(button => {
    button.addEventListener('click', () => dialog.close());
  });
});

$('#save-query-form').addEventListener('submit', event => {
  event.preventDefault();
  saveNamedQuery($<HTMLInputElement>('#save-query-name').value);
  $<HTMLDialogElement>('#save-query-dialog').close();
});

$('#bookmark-note-form').addEventListener('submit', event => {
  event.preventDefault();
  const bookmark = bookmarks.find(item => item.id === activeBookmarkId);
  if (bookmark) {
    bookmark.note = $<HTMLTextAreaElement>('#bookmark-note').value.trim();
    writeStored(storageKeys.bookmarks, bookmarks);
    renderBookmarks();
  }
  activeBookmarkId = null;
  $<HTMLDialogElement>('#bookmark-note-dialog').close();
});

$('#confirm-form').addEventListener('submit', event => {
  event.preventDefault();
  const previous = [...queryHistory];
  queryHistory = [];
  const persisted = writeStored(storageKeys.history, queryHistory);
  renderQueryLibrary();
  $<HTMLDialogElement>('#confirm-dialog').close();
  if (persisted) {
    showToast('Query history cleared', { label: 'Undo', handler: () => {
      queryHistory = previous;
      writeStored(storageKeys.history, queryHistory);
      renderQueryLibrary();
    } });
  }
});

document.addEventListener('keydown', event => {
  const target = event.target instanceof Element ? event.target : null;
  const inEditor = Boolean(target?.closest('#query'));
  const inTextField = Boolean(target?.closest('input, textarea, select, [contenteditable="true"]'));

  if (event.defaultPrevented) return;

  const isRunShortcut = event.key === 'Enter' && (event.ctrlKey || event.metaKey)
    && !event.altKey && !event.shiftKey;
  if (!isRunShortcut || document.querySelector('dialog[open]')) return;
  if (inTextField && !inEditor) return;
  event.preventDefault();
  runQuery();
});
$('#run-query').addEventListener('click', runQuery);
$('#result-body').addEventListener('keydown', event => {
  if (!(event.target instanceof HTMLButtonElement) || !event.target.classList.contains('result-row-action')) return;
  const row = event.target.closest('tr');
  if (!row) return;
  const actions = Array.from(row.querySelectorAll<HTMLButtonElement>('.result-row-action'));
  const actionIndex = actions.indexOf(event.target);
  let target: HTMLButtonElement | undefined;
  if (event.key === 'ArrowLeft') target = actions[Math.max(0, actionIndex - 1)];
  if (event.key === 'ArrowRight') target = actions[Math.min(actions.length - 1, actionIndex + 1)];
  if (event.key === 'ArrowUp' || event.key === 'ArrowDown' || event.key === 'Home' || event.key === 'End') {
    const rows = Array.from($('#result-body').querySelectorAll('tr'));
    const rowIndex = rows.indexOf(row);
    const targetRow = event.key === 'Home' ? rows[0]
      : event.key === 'End' ? rows.at(-1)
        : rows[rowIndex + (event.key === 'ArrowDown' ? 1 : -1)];
    target = targetRow?.querySelectorAll<HTMLButtonElement>('.result-row-action')[actionIndex];
  }
  if (!target) return;
  event.preventDefault();
  document.querySelectorAll<HTMLButtonElement>('.result-row-action').forEach(button => { button.tabIndex = -1; });
  target.tabIndex = 0;
  target.focus();
});
$('#save-query').addEventListener('click', saveCurrentQuery);
$('#share-query').addEventListener('click', shareCurrentQuery);
const vimToggle = $<HTMLButtonElement>('#vim-toggle');
vimToggle.addEventListener('click', () => queryEditor.toggleVim(vimToggle));
$('#export-results').addEventListener('click', exportResults);
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
  $<HTMLDialogElement>('#confirm-dialog').showModal();
});
const fieldSearch = $<HTMLInputElement>('#field-search');
fieldSearch.addEventListener('input', () => renderFields(fieldSearch.value));
document.querySelectorAll<HTMLButtonElement>('.side-tab').forEach(button => {
  button.addEventListener('click', () => {
    const view = button.dataset.sideView;
    if (view !== 'fields' && view !== 'questions' && view !== 'bookmarks') return;
    sidePanelView = view;
    renderSidePanelView();
  });
});
enableTabKeyboardNavigation($('.results-view-tabs'), '.results-view-tab');
enableTabKeyboardNavigation($('.query-view-tabs'), '.query-view-tab');
enableTabKeyboardNavigation($('.side-tabs'), '.side-tab');

renderQueryLibrary();
renderBookmarks();
renderResultsPanelView();
renderSidePanelView();
loadFields();
loadQuestions();
scheduleQueryValidation(editor.state.doc.toString());
if (!onboardingSeen) onboardingDialog.showModal();
let timelineResizeTimer: number | undefined;
window.addEventListener('resize', () => {
  window.clearTimeout(timelineResizeTimer);
  timelineResizeTimer = window.setTimeout(() => {
    if (lastResult) timeline.render(lastResult.rows);
  }, 100);
});
window.setInterval(() => {
  if (document.hidden || document.activeElement?.matches('.question-form input')) return;
  void loadQuestions(true);
}, 15_000);
