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
  readQuestionDrafts,
  readQueryHistory,
  readSavedQueries,
  storageKeys,
} from './storage';
import type {
  AnswerResponse,
  ChallengeState,
  EventRow,
  FieldGroup,
  FieldMetadata,
  FieldsResponse,
  InvestigationQuestion,
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
  muted: 'muted',
} as const;
const toast = createToast($('#toast'), $('#toast-message'), $('#toast-action'));
const showToast = (message: string, action?: ToastAction) => toast.show(message, action);
const browserStorage = createBrowserStorage(() => {
  showToast('Browser storage is unavailable. Changes will not persist.');
});
const writeStored = <T>(key: string, value: T) => browserStorage.write(key, value);
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
let queryLibraryView: 'saved' | 'history' = 'saved';
let sidePanelView: 'fields' | 'questions' | 'hunts' = 'fields';
let lastResult: QueryResult | null = null;
let resultSort: ResultSort = { column: null, direction: 'asc' };
const resultColumnWidths = new Map<string, number>();
let selectedResultKey: string | null = null;
let selectedResultValue: { column: string; value: unknown } | null = null;
let queryRunning = false;
let queryController: AbortController | null = null;
let validationTimer: number | undefined;
let validationController: AbortController | null = null;
let validationGeneration = 0;
let challengeState: ChallengeState = { questions: [], solvedQuestions: 0, totalQuestions: 0, completed: false };
const questionFeedback = new Map<string, string>();
const questionDrafts = readQuestionDrafts(browserStorage);
const questionCooldowns = new Map<string, number>();
const questionCooldownTimers = new Map<string, number>();
let activeQuestionId: string | null = null;
let questionRequestGeneration = 0;
let questionsLoaded = false;
let questionSubmitting = false;
const sharedQuery = new URL(window.location.href).searchParams.get('q');

function persistQuestionDrafts(): void {
  writeStored(storageKeys.questionDrafts, Array.from(questionDrafts, ([id, value]) => ({ id, value })));
}

function activeQuestion(): InvestigationQuestion | undefined {
  return challengeState.questions.find(question => question.id === activeQuestionId);
}

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
  $('#query-stats').textContent = 'Running KQL query';
  $('#results-content').classList.toggle('query-stale', lastResult !== null);
  $('#results-content').setAttribute('aria-busy', 'true');
  recordQuery(query);
  const started = performance.now();
  const elapsedTimer = window.setInterval(() => {
    const elapsed = Math.max(1, Math.floor((performance.now() - started) / 1000));
    $('#query-stats').textContent = `Running KQL query · ${elapsed} s`;
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
    $('#query-stats').textContent = `${formatCount(result.rowCount, 'row')} · ${result.durationMs} ms`;
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      $('#query-stats').textContent = 'Query canceled';
      return;
    }
    const queryError = asQueryError(error);
    const position = queryError.position ? `Line ${queryError.position.line}, column ${queryError.position.column}: ` : '';
    const serverMessage = queryError.error || queryError.message;
    const message = !serverMessage || /failed to fetch|invalid server response/i.test(serverMessage)
      ? 'The query service did not respond. Your query is unchanged. Run it again.'
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

function formatCount(count: number, noun: string): string {
  return `${count.toLocaleString()} ${noun}${count === 1 ? '' : 's'}`;
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

function selectResultValue(element: HTMLElement, column: string, value: unknown): void {
  selectedResultValue = { column, value };
  document.querySelectorAll('.result-value-selected').forEach(candidate => candidate.classList.remove('result-value-selected'));
  element.classList.add('result-value-selected');
  const displayValue = resultValue(value);
  $('#result-value-label').textContent = `${column}: ${displayValue}`;
  $('#result-value-label').setAttribute('title', `${column}: ${displayValue}`);
  $('#result-value-actions').classList.remove('hidden');
}

function clearSelectedResultValue(): void {
  selectedResultValue = null;
  $('#result-value-actions').classList.add('hidden');
  document.querySelectorAll('.result-value-selected').forEach(candidate => candidate.classList.remove('result-value-selected'));
}

function focusResultCell(row: number, column: number): void {
  const cell = document.querySelector<HTMLTableCellElement>(`.result-value-cell[data-result-row="${row}"][data-result-column-index="${column}"]`);
  if (!cell) return;
  document.querySelectorAll<HTMLTableCellElement>('.result-value-cell').forEach(candidate => { candidate.tabIndex = -1; });
  cell.tabIndex = 0;
  cell.focus();
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
  const standardColumns = new Set(['TimeGenerated', 'Source', 'RawData']);
  let detailList: HTMLDListElement | undefined;
  const details = columns.filter(column => !standardColumns.has(column)
    && row[column] !== null && row[column] !== undefined && typeof row[column] !== 'object').slice(0, 3);
  if (details.length) {
    const list = document.createElement('dl');
    list.className = 'mobile-result-details';
    details.forEach(column => {
      const item = document.createElement('div');
      const term = document.createElement('dt');
      term.textContent = column;
      const value = document.createElement('dd');
      const valueButton = document.createElement('button');
      valueButton.className = 'mobile-result-value';
      valueButton.type = 'button';
      valueButton.textContent = resultValue(row[column]);
      valueButton.title = `Select ${column} value`;
      valueButton.addEventListener('click', () => selectResultValue(valueButton, column, row[column]));
      value.append(valueButton);
      item.append(term, value);
      list.append(item);
    });
    detailList = list;
  }
  open.addEventListener('click', () => showRaw(row));
  const actions = document.createElement('div');
  actions.className = 'mobile-result-card-actions';
  const pivotColumn = columns.find(column => !['TimeGenerated', 'RawData'].includes(column)
    && row[column] !== null && row[column] !== undefined && typeof row[column] !== 'object');
  if (pivotColumn) {
    const filter = document.createElement('button');
    filter.className = 'mobile-pivot-action';
    filter.type = 'button';
    filter.textContent = 'Filter';
    filter.setAttribute('aria-label', `Filter to ${pivotColumn} ${resultValue(row[pivotColumn])}`);
    filter.addEventListener('click', () => pivotOnValue(pivotColumn, row[pivotColumn], false));
    const exclude = document.createElement('button');
    exclude.className = 'mobile-pivot-action';
    exclude.type = 'button';
    exclude.textContent = 'Exclude';
    exclude.setAttribute('aria-label', `Exclude ${pivotColumn} ${resultValue(row[pivotColumn])}`);
    exclude.addEventListener('click', () => pivotOnValue(pivotColumn, row[pivotColumn], true));
    actions.append(filter, exclude);
  }
  card.append(open);
  if (detailList) card.append(detailList);
  card.append(actions);
  return card;
}

function renderResults(result: QueryResult, resetSort = true): void {
  lastResult = result;
  clearSelectedResultValue();
  if (resetSort) resultSort = { column: null, direction: 'asc' };
  const head = $('#result-head');
  const body = $('#result-body');
  const mobileList = $('#mobile-result-list');
  let firstFocusableValue = true;
  head.replaceChildren();
  body.replaceChildren();
  mobileList.replaceChildren();
  const headerRow = document.createElement('tr');
  const actionHeader = document.createElement('th');
  actionHeader.className = 'row-action';
  actionHeader.scope = 'col';
  actionHeader.setAttribute('aria-label', 'Row actions');
  headerRow.append(actionHeader);
  result.columns.forEach(column => {
    const th = document.createElement('th');
    th.scope = 'col';
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
    const selectRow = () => {
      selectedResultKey = rowKey;
      body.querySelectorAll('tr').forEach(candidate => {
        const selected = candidate === tr;
        candidate.classList.toggle('selected', selected);
      });
    };
    tr.addEventListener('click', selectRow);
    const action = document.createElement('td');
    action.className = 'row-action';
    const view = createRowAction('View event details', 'M2.5 3.5h11v9h-11zM5 6h6M5 8.5h6M5 11h3', () => showRaw(row));
    view.classList.add('result-row-action');
    view.tabIndex = rowIndex === 0 ? 0 : -1;
    action.append(view);
    tr.append(action);
    result.columns.forEach((column, columnIndex) => {
      const td = document.createElement('td');
      const value = row[column];
      if (value !== null && typeof value === 'object') {
        td.className = 'raw';
        const dynamic = document.createElement('button');
        dynamic.className = ui.rawValue;
        dynamic.type = 'button';
        dynamic.textContent = 'JSON';
        dynamic.title = `Open ${column} JSON`;
        dynamic.setAttribute('aria-label', `Open ${column} JSON`);
        dynamic.addEventListener('click', event => {
          event.stopPropagation();
          selectRow();
          showRaw(value);
        });
        td.append(dynamic);
      } else {
        if (value === null || value === undefined) td.className = 'nil';
        else if (typeof value === 'number') td.className = 'num';
        else if (column === 'TimeGenerated') td.className = 'time';
        td.textContent = value === null || value === undefined ? '—' : String(value);
        td.title = value === null || value === undefined ? 'null' : String(value);
        td.classList.add('result-value-cell');
        td.dataset.resultRow = String(rowIndex);
        td.dataset.resultColumnIndex = String(columnIndex);
        td.tabIndex = firstFocusableValue ? 0 : -1;
        firstFocusableValue = false;
        td.setAttribute('aria-label', `${column}: ${resultValue(value)}. Press Enter to filter or Shift+Enter to exclude.`);
        td.addEventListener('focus', () => selectResultValue(td, column, value));
        td.addEventListener('click', () => selectResultValue(td, column, value));
        td.addEventListener('keydown', event => {
          if (event.key === 'Enter') {
            event.preventDefault();
            pivotOnValue(column, value, event.shiftKey);
            return;
          }
          const rowDirection = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
          const columnDirection = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
          if (!rowDirection && !columnDirection && event.key !== 'Home' && event.key !== 'End') return;
          event.preventDefault();
          const nextRow = event.key === 'Home' ? 0 : event.key === 'End' ? result.rows.length - 1 : rowIndex + rowDirection;
          const nextColumn = columnIndex + columnDirection;
          focusResultCell(nextRow, nextColumn);
        });
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
  $('#empty-results-title').textContent = result.rows.length ? '' : 'No events matched this query';
  $('#empty-results-detail').textContent = result.rows.length
    ? ''
    : 'Remove a filter or widen the time range, then run the query again.';
  const emptyAction = $<HTMLButtonElement>('#empty-results-action');
  emptyAction.textContent = 'Edit query';
  emptyAction.dataset.action = 'edit';
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
  showToast(`Exported ${formatCount(rows.length, 'row')}`);
}

function addTimelineFilter(start: Date, end: Date): void {
  replaceQuery(addTimeRangeFilter(editor.state.doc.toString(), start, end));
  mobileWorkspace.show('query');
  showToast('Time filter added to the query', {
    label: 'Run query',
    handler: () => void runQuery(),
  });
}

function pivotOnValue(column: string, value: unknown, negate: boolean): void {
  const previous = editor.state.doc.toString();
  if (window.matchMedia('(max-width: 900px)').matches) mobileWorkspace.show('query');
  replaceQuery(addValueFilter(previous, column, value, negate));
  showToast(`${negate ? 'Excluded' : 'Included'} ${column}: ${String(value).slice(0, 40)}`, {
    label: 'Undo',
    handler: () => replaceQuery(previous),
  });
}

async function copySelectedResultValue(): Promise<void> {
  if (!selectedResultValue) return;
  try {
    await navigator.clipboard.writeText(resultValue(selectedResultValue.value));
    showToast(`Copied ${selectedResultValue.column} value`);
  } catch {
    showToast('Could not copy the value. Select it and copy it manually.');
  }
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
  if (!query) {
    showToast('Enter a query before saving a hunt.');
    editor.focus();
    return;
  }
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
  if (persisted) showToast('Hunt saved to this browser');
}

async function shareCurrentQuery() {
  const url = new URL(window.location.href);
  url.searchParams.set('q', editor.state.doc.toString());
  if (url.toString().length > 7000) {
    showToast('This query is too long for a share link.');
    return;
  }
  window.history.replaceState(null, '', url);
  try {
    await navigator.clipboard.writeText(url.toString());
    showToast('Share link copied to the clipboard');
  } catch {
    showToast('Share link added to the address bar. Copy it from there.');
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
  open.addEventListener('click', () => {
    replaceQuery(item.query);
    mobileWorkspace.show('query');
  });
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
        showToast('Saved hunt removed', { label: 'Undo', handler: () => {
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
  if (!savedQueries.length) savedList.append(createHuntEmpty(
    'No saved hunts yet',
    'Save a useful query to keep a proven investigation path in this browser.',
    'Save current query',
    saveCurrentQuery,
  ));
  if (!queryHistory.length) historyList.append(createHuntEmpty(
    'No recent hunts yet',
    'Queries appear here after they run, so you can retrace an investigation quickly.',
    'Run current query',
    () => void runQuery(),
  ));
  savedQueries.forEach(item => savedList.append(createQueryListItem(item)));
  queryHistory.forEach(item => historyList.append(createQueryListItem(item)));
}

function createHuntEmpty(title: string, detail: string, action: string, handler: () => void): HTMLElement {
  const empty = document.createElement('div');
  empty.className = 'hunt-empty';
  const heading = document.createElement('strong');
  heading.textContent = title;
  const copy = document.createElement('p');
  copy.textContent = detail;
  const button = document.createElement('button');
  button.className = 'secondary';
  button.type = 'button';
  button.textContent = action;
  button.addEventListener('click', handler);
  empty.append(heading, copy, button);
  return empty;
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
  const views = ['fields', 'questions', 'hunts'] as const;
  renderTabSelection(sidePanelView, views.map(name => ({
    name,
    tab: $<HTMLButtonElement>(`[data-side-view="${name}"]`),
    panel: $(`#${name}-pane`),
  })));
}

function setQuestionUIVisibility(visible: boolean): void {
  $('#active-task-bar').classList.toggle('hidden', !visible);
  $('#questions-tab').classList.toggle('hidden', !visible);
  $('.side-tabs').classList.toggle('without-questions', !visible);
  if (!visible && sidePanelView === 'questions') sidePanelView = 'fields';
  renderSidePanelView();
}

function showInvestigationPanel(view: 'questions' | 'hunts'): void {
  if (view === 'questions' && !challengeState.totalQuestions) return;
  sidePanelView = view;
  renderSidePanelView();
  mobileWorkspace.show('investigate');
  window.requestAnimationFrame(() => {
    $<HTMLButtonElement>(`[data-side-view="${view}"]`).focus();
  });
}

function renderDataSources(): void {
  const list = $('#source-list');
  list.replaceChildren();
  $('#source-count').textContent = String(dataSources.length);
  if (!dataSources.length) {
    const empty = document.createElement('span');
    empty.className = ui.muted;
    empty.textContent = 'No data sources are configured for this workspace.';
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

function setActiveQuestion(questionId: string): void {
  if (!challengeState.questions.some(question => question.id === questionId)) return;
  activeQuestionId = questionId;
  renderActiveTask();
  renderQuestions();
}

function updateActiveTaskCooldown(question: InvestigationQuestion): void {
  const deadline = questionCooldowns.get(question.id) || 0;
  const remaining = deadline - Date.now();
  const input = $<HTMLInputElement>('#active-task-answer');
  const submit = $<HTMLButtonElement>('#active-task-submit');
  const status = $('#active-task-cooldown');
  if (remaining <= 0) {
    questionCooldowns.delete(question.id);
    status.classList.add('hidden');
    status.textContent = '';
    input.disabled = questionSubmitting;
    submit.disabled = questionSubmitting;
    return;
  }
  const seconds = Math.max(1, Math.ceil(remaining / 1000));
  status.textContent = `Wait ${seconds} second${seconds === 1 ? '' : 's'} before trying again.`;
  status.classList.remove('hidden');
  input.disabled = false;
  submit.disabled = true;
}

function renderActiveTask(): void {
  questionCooldownTimers.forEach(timer => window.clearInterval(timer));
  questionCooldownTimers.clear();
  const bar = $('#active-task-bar');
  const progress = $('#active-task-progress');
  const title = $('#active-task-title');
  const prompt = $('#active-task-prompt');
  const form = $('#active-task-form');
  const input = $<HTMLInputElement>('#active-task-answer');
  const submit = $<HTMLButtonElement>('#active-task-submit');
  const resolution = $('#active-task-resolution');
  const answerValue = $<HTMLInputElement>('#active-task-answer-value');
  const next = $<HTMLButtonElement>('#active-task-next');
  const feedback = $('#active-task-feedback');
  bar.setAttribute('aria-busy', String(!questionsLoaded));
  form.classList.add('hidden');
  resolution.classList.add('hidden');
  feedback.classList.add('hidden');
  $('#active-task-cooldown').classList.add('hidden');

  if (!questionsLoaded) return;
  setQuestionUIVisibility(challengeState.totalQuestions > 0);
  if (!challengeState.totalQuestions) {
    return;
  }

  if (!activeQuestionId || !challengeState.questions.some(question => question.id === activeQuestionId)) {
    activeQuestionId = challengeState.questions.find(question => !question.solved)?.id
      || challengeState.questions[0]?.id
      || null;
  }
  const question = activeQuestion();
  if (!question) return;
  const questionIndex = challengeState.questions.findIndex(candidate => candidate.id === question.id);
  progress.textContent = `Task ${questionIndex + 1} of ${challengeState.totalQuestions} · ${challengeState.solvedQuestions} solved`;
  title.textContent = challengeState.completed ? 'Challenge complete' : question.title;
  prompt.textContent = challengeState.completed
    ? 'Every task is solved. Copy the flag and submit it to the CTF platform.'
    : question.prompt;
  if (!lastResult) {
    $('#empty-results-title').textContent = `Run a query for “${question.title}”`;
    $('#empty-results-detail').textContent = 'Start with the prepared query, then use Fields to add the evidence you need. Press Shift+Enter to run.';
    const emptyAction = $<HTMLButtonElement>('#empty-results-action');
    emptyAction.textContent = 'Run current query';
    emptyAction.dataset.action = 'run';
  }

  if (challengeState.completed && challengeState.flag) {
    answerValue.value = challengeState.flag;
    answerValue.setAttribute('aria-label', `Unlocked flag: ${challengeState.flag}`);
    answerValue.setAttribute('title', challengeState.flag);
    next.textContent = 'Copy flag';
    next.dataset.action = 'copy-flag';
    resolution.classList.remove('hidden');
    return;
  }

  if (question.solved && question.answer) {
    answerValue.value = question.answer;
    answerValue.setAttribute('aria-label', `Correct answer: ${question.answer}`);
    answerValue.removeAttribute('title');
    const nextQuestion = challengeState.questions.find(candidate => !candidate.solved);
    next.textContent = nextQuestion ? 'Next task' : 'View tasks';
    next.dataset.action = nextQuestion ? 'next-task' : 'all-tasks';
    next.dataset.questionId = nextQuestion?.id || '';
    resolution.classList.remove('hidden');
    return;
  }

  input.value = questionDrafts.get(question.id) || '';
  input.setAttribute('aria-label', `Answer for ${question.title}`);
  input.dataset.questionId = question.id;
  submit.textContent = questionSubmitting ? 'Checking answer' : 'Check answer';
  form.classList.remove('hidden');
  const feedbackMessage = questionFeedback.get(question.id);
  if (feedbackMessage) {
    feedback.textContent = feedbackMessage;
    feedback.classList.remove('hidden');
  }
  updateActiveTaskCooldown(question);
  const deadline = questionCooldowns.get(question.id) || 0;
  if (deadline > Date.now()) {
    const timer = window.setInterval(() => {
      if (!form.isConnected || activeQuestionId !== question.id) {
        window.clearInterval(timer);
        questionCooldownTimers.delete(question.id);
        return;
      }
      updateActiveTaskCooldown(question);
      if ((questionCooldowns.get(question.id) || 0) <= Date.now()) {
        window.clearInterval(timer);
        questionCooldownTimers.delete(question.id);
      }
    }, 250);
    questionCooldownTimers.set(question.id, timer);
  }
}

async function submitActiveQuestion(): Promise<void> {
  const question = activeQuestion();
  const input = $<HTMLInputElement>('#active-task-answer');
  if (!question || questionSubmitting) return;
  const answer = input.value.trim();
  if (!answer) return;
  let correct = false;
  questionSubmitting = true;
  questionRequestGeneration++;
  questionDrafts.set(question.id, input.value);
  persistQuestionDrafts();
  renderActiveTask();
  try {
    const result = await request<AnswerResponse>(`/api/questions/${encodeURIComponent(question.id)}/answer`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ answer }),
    });
    questionRequestGeneration++;
    challengeState = result.state;
    correct = result.correct;
    if (correct) {
      questionFeedback.delete(question.id);
      questionDrafts.delete(question.id);
      persistQuestionDrafts();
      questionCooldowns.delete(question.id);
      const message = result.alreadySolved
        ? 'Answer saved'
        : challengeState.completed ? 'Challenge complete. Flag unlocked.' : 'Task solved';
      showToast(message);
    } else {
      questionFeedback.set(question.id, 'That answer does not match. Check the requested format and your evidence, then try again.');
    }
  } catch (error) {
    questionRequestGeneration++;
    const queryError = asQueryError(error);
    const retry = Number(queryError.retryAfterMs);
    if (retry > 0) questionCooldowns.set(question.id, Date.now() + retry);
    const message = retry > 0
      ? 'Too many attempts. Your answer was not submitted; wait for the retry timer, then try again.'
      : (queryError.error || queryError.message || 'The answer could not be submitted. Your draft is saved in this browser; try again.');
    questionFeedback.set(question.id, message);
  } finally {
    questionSubmitting = false;
    renderQuestions();
    renderActiveTask();
    window.requestAnimationFrame(() => {
      if (correct) $<HTMLButtonElement>('#active-task-next').focus();
      else $<HTMLInputElement>('#active-task-answer').focus();
    });
  }
}

function renderQuestions() {
  const list = $('#question-list');
  list.replaceChildren();
  const solved = challengeState.solvedQuestions || 0;
  const total = challengeState.totalQuestions || 0;
  $('#question-tab-count').textContent = `${solved}/${total}`;
  if (!total) {
    const empty = document.createElement('span');
    empty.className = ui.muted;
    empty.textContent = 'No tasks are configured for this challenge.';
    list.append(empty);
    return;
  }

  challengeState.questions.forEach(question => {
    const card = document.createElement('article');
    card.className = ui.questionCard;
    card.classList.toggle('solved', question.solved);
    card.classList.toggle('active', question.id === activeQuestionId);
    const select = document.createElement('button');
    select.className = 'question-select';
    select.type = 'button';
    select.setAttribute('aria-current', question.id === activeQuestionId ? 'true' : 'false');
    const heading = document.createElement('span');
    heading.className = ui.questionHead;
    const title = document.createElement('strong');
    title.textContent = question.title;
    heading.append(title);
    const state = document.createElement('span');
    state.className = 'question-state';
    state.textContent = question.solved ? 'Solved' : question.id === activeQuestionId ? 'Active' : 'Open';
    heading.append(state);
    const prompt = document.createElement('span');
    prompt.className = ui.questionPrompt;
    prompt.textContent = question.prompt;
    select.append(heading, prompt);
    select.addEventListener('click', () => setActiveQuestion(question.id));
    card.append(select);

    if (question.solved && question.answer) {
      const answer = document.createElement('output');
      answer.className = 'question-answer';
      answer.setAttribute('aria-label', 'Correct answer');
      answer.textContent = question.answer;
      card.append(answer);
    }
    list.append(card);
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
  button.textContent = 'Try again';
  button.addEventListener('click', retry);
  container.append(detail, button);
  target.replaceChildren(container);
}

function renderActiveTaskFailure(): void {
  setQuestionUIVisibility(true);
  $('#active-task-bar').setAttribute('aria-busy', 'false');
  $('#active-task-progress').textContent = 'Tasks unavailable';
  $('#active-task-title').textContent = 'Could not load investigation tasks';
  $('#active-task-prompt').textContent = 'The query editor is still available. Try loading the tasks again.';
  $('#active-task-form').classList.add('hidden');
  $('#active-task-feedback').classList.add('hidden');
  $<HTMLInputElement>('#active-task-answer-value').value = '';
  const resolution = $('#active-task-resolution');
  const retry = $<HTMLButtonElement>('#active-task-next');
  retry.textContent = 'Retry tasks';
  retry.dataset.action = 'retry-tasks';
  resolution.classList.remove('hidden');
}

async function loadQuestions(backgroundRefresh = false) {
  if (!backgroundRefresh) {
    $('#active-task-bar').setAttribute('aria-busy', 'true');
    $('#active-task-progress').textContent = 'Loading investigation tasks';
    $('#active-task-title').textContent = 'Preparing workspace';
    $('#active-task-prompt').textContent = 'Loading the active task. You can query the prepared telemetry while this completes.';
    $('#active-task-form').classList.add('hidden');
    $('#active-task-resolution').classList.add('hidden');
  }
  const generation = ++questionRequestGeneration;
  try {
    const nextState = await request<ChallengeState>('/api/questions');
    if (generation !== questionRequestGeneration) return;
    if (backgroundRefresh && JSON.stringify(nextState) === JSON.stringify(challengeState)) return;
    challengeState = nextState;
    questionsLoaded = true;
    if (!activeQuestionId || !challengeState.questions.some(question => question.id === activeQuestionId)) {
      activeQuestionId = challengeState.questions.find(question => !question.solved)?.id
        || challengeState.questions[0]?.id
        || null;
    }
    renderActiveTask();
    renderQuestions();
  } catch {
    if (generation !== questionRequestGeneration) return;
    if (backgroundRefresh && questionsLoaded) return;
    renderLoadFailure($('#question-list'), 'Could not load tasks.', () => void loadQuestions());
    renderActiveTaskFailure();
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
    empty.textContent = normalized ? `No fields match “${filter.trim()}”.` : 'No fields are available for this data source.';
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
    document.title = `${challengeName} | striem`;
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
    renderLoadFailure($('#source-list'), 'Could not load data sources.', () => void loadFields());
    renderLoadFailure($('#field-list'), 'Could not load fields.', () => void loadFields());
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

$('#confirm-form').addEventListener('submit', event => {
  event.preventDefault();
  const previous = [...queryHistory];
  queryHistory = [];
  const persisted = writeStored(storageKeys.history, queryHistory);
  renderQueryLibrary();
  $<HTMLDialogElement>('#confirm-dialog').close();
  if (persisted) {
    showToast('Recent hunts cleared', { label: 'Undo', handler: () => {
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

  const isRunShortcut = event.key === 'Enter' && event.shiftKey
    && !event.altKey && !event.ctrlKey && !event.metaKey;
  if (!isRunShortcut || document.querySelector('dialog[open]')) return;
  if (inTextField && !inEditor) return;
  event.preventDefault();
  runQuery();
});
$('#run-query').addEventListener('click', runQuery);
$('#active-task-form').addEventListener('submit', event => {
  event.preventDefault();
  void submitActiveQuestion();
});
$('#active-task-answer').addEventListener('input', event => {
  const question = activeQuestion();
  if (!question || !(event.target instanceof HTMLInputElement)) return;
  questionDrafts.set(question.id, event.target.value);
  persistQuestionDrafts();
  questionFeedback.delete(question.id);
  $('#active-task-feedback').classList.add('hidden');
});
$('#show-all-tasks').addEventListener('click', () => showInvestigationPanel('questions'));
$('#active-task-next').addEventListener('click', async event => {
  if (!(event.currentTarget instanceof HTMLButtonElement)) return;
  const action = event.currentTarget.dataset.action;
  if (action === 'next-task' && event.currentTarget.dataset.questionId) {
    setActiveQuestion(event.currentTarget.dataset.questionId);
    $<HTMLInputElement>('#active-task-answer').focus();
  }
  if (action === 'all-tasks') showInvestigationPanel('questions');
  if (action === 'retry-tasks') void loadQuestions();
  if (action === 'copy-flag' && challengeState.flag) {
    try {
      await navigator.clipboard.writeText(challengeState.flag);
      showToast('Flag copied');
    } catch {
      showToast('Could not copy the flag. Select it and copy manually.');
    }
  }
});
$('#filter-result-value').addEventListener('click', () => {
  if (selectedResultValue) pivotOnValue(selectedResultValue.column, selectedResultValue.value, false);
});
$('#exclude-result-value').addEventListener('click', () => {
  if (selectedResultValue) pivotOnValue(selectedResultValue.column, selectedResultValue.value, true);
});
$('#copy-result-value').addEventListener('click', () => void copySelectedResultValue());
$('#empty-results-action').addEventListener('click', event => {
  const button = event.currentTarget as HTMLButtonElement;
  if (button.dataset.action === 'edit') {
    mobileWorkspace.show('query');
    editor.focus();
    return;
  }
  void runQuery();
});
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
    if (view !== 'fields' && view !== 'questions' && view !== 'hunts') return;
    sidePanelView = view;
    renderSidePanelView();
  });
});
enableTabKeyboardNavigation($('.query-view-tabs'), '.query-view-tab');
enableTabKeyboardNavigation($('.side-tabs'), '.side-tab');

renderQueryLibrary();
renderActiveTask();
renderSidePanelView();
loadFields();
loadQuestions();
scheduleQueryValidation(editor.state.doc.toString());
let timelineResizeTimer: number | undefined;
window.addEventListener('resize', () => {
  window.clearTimeout(timelineResizeTimer);
  timelineResizeTimer = window.setTimeout(() => {
    if (lastResult) timeline.render(lastResult.rows);
  }, 100);
});
window.setInterval(() => {
  if (!challengeState.totalQuestions || document.hidden || document.activeElement?.matches('#active-task-answer')) return;
  void loadQuestions(true);
}, 15_000);
