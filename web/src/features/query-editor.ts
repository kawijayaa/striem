import { acceptCompletion, autocompletion, closeBrackets, closeBracketsKeymap, completionStatus, snippet, pickedCompletion, insertCompletionText } from '@codemirror/autocomplete';
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
import { queryCompletionContext } from './completion-context';
import { expressionOperatorDefinitions } from './expression-operators';

const keywords = new Set([
  'let', 'where', 'filter', 'search', 'project', 'project-away', 'project-keep', 'project-rename', 'project-reorder',
  'extend', 'summarize', 'distinct', 'order', 'sort', 'top', 'take',
  'getschema', 'limit', 'sample', 'sample-distinct', 'count', 'serialize', 'as', 'mv-expand', 'mv-apply', 'union', 'join',
  'lookup', 'kind', 'innerunique', 'rightsemi', 'rightanti', 'inner', 'leftouter', 'rightouter', 'fullouter', 'leftsemi', 'leftanti', 'with_itemindex',
  'on', 'by', 'of', 'asc', 'desc', 'granny-asc', 'granny-desc',
]);
const logicalOperators = new Set([
  'and', 'or', 'not', '=~', '!~', 'in', 'in~', '!in', '!in~', 'between', '!between', 'contains', 'contains_cs',
  'startswith', 'startswith_cs', 'endswith', 'endswith_cs', 'has', 'has_cs', 'hasprefix', 'hasprefix_cs',
  'hassuffix', 'hassuffix_cs', 'has_any', 'has_all',
]);
const functions = new Set([
  'now', 'ago', 'datetime', 'tostring', 'toint', 'tolong', 'toreal', 'tobool', 'toboolean', 'todouble', 'tolower', 'toupper', 'isnull',
  'isnotnull', 'isempty', 'isnotempty', 'parse_json', 'parse_csv', 'parse_xml', 'parsejson', 'todynamic', 'toobject', 'array_length', 'array_concat', 'array_iff', 'array_iif', 'array_index_of', 'array_sum', 'series_sum', 'zip', 'array_reverse', 'array_slice', 'array_split', 'array_rotate_left', 'array_rotate_right', 'array_shift_left', 'array_shift_right', 'bag_keys', 'todatetime',
  'base64_decode_tostring', 'url_decode', 'parse_url', 'parseurl', 'parse_urlquery', 'parseurlquery', 'bag_has_key', 'set_has_element', 'set_union', 'set_intersect', 'set_difference', 'set_equals', 'jaccard_index',
  'ipv4_is_private', 'ipv4_is_in_range',
  'count', 'countif', 'sumif', 'iff', 'case', 'coalesce', 'strlen', 'substring', 'indexof', 'replace',
  'strcat', 'sum', 'min', 'max', 'avg', 'split', 'extract', 'trim', 'trim_start', 'trim_end', 'replace_string', 'replace_strings', 'replace_regex',
  'variance', 'variancep', 'varianceif', 'variancepif', 'stdev', 'stdevp', 'stdevif', 'stdevpif',
  'pack_array', 'repeat', 'bag_pack', 'pack', 'pack_dictionary', 'bag_merge', 'bag_zip', 'bag_remove_keys', 'bag_set_key', 'make_set', 'make_list', 'make_bag', 'make_bag_if', 'make_dictionary', 'take_any',
  'not', 'bin', 'dcount', 'dcountif', 'count_distinct', 'count_distinctif', 'avgif', 'minif', 'maxif',
  'startofday', 'endofday', 'startofweek', 'endofweek', 'startofmonth', 'endofmonth', 'startofyear', 'endofyear',
]);
const literals = new Set(['true', 'false', 'null']);
const types = new Set(['string', 'long', 'real', 'dynamic']);
const comparisonCompletions = createCompletions('operator', expressionOperatorDefinitions, label => `${label} `)
  .map(completion => ({ ...completion, boost: completion.label === '==' ? 30 : completion.label === '!=' ? 25
    : completion.label === 'has' ? 15 : completion.label === 'contains' ? 10 : 0 }));

const operatorCompletions = createCompletions('keyword', [
  ['let', 'Declare a scalar or tabular value'],
  ['where', 'Filter rows'], ['filter', 'Filter rows'], ['search', 'Search visible columns'],
  ['getschema', 'Describe output columns and types'], ['project', 'Select columns'], ['project-away', 'Remove columns'], ['project-keep', 'Keep columns'],
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
  ['isnotnull', 'Test for non-null'], ['parse_json', 'Parse JSON text'], ['parse_csv', 'Parse the first CSV record into a string array'], ['parse_xml', 'Parse XML into a dynamic object'], ['parsejson', 'Parse JSON text'], ['todynamic', 'Parse JSON text as dynamic'], ['toobject', 'Parse JSON text as dynamic'], ['dynamic', 'Construct a dynamic literal'],
  ['isempty', 'Test for null or empty text'], ['isnotempty', 'Test for non-empty text'],
  ['array_length', 'Number of array elements'], ['array_concat', 'Concatenate dynamic arrays in order'], ['array_reverse', 'Reverse the elements of a dynamic array'], ['array_slice', 'Select an inclusive range of array elements'], ['bag_keys', 'Keys of a dynamic object'],
  ['array_iff', 'Choose array elements using Boolean or numeric conditions'], ['array_iif', 'Alias for conditional array selection'],
  ['array_index_of', 'Find a typed scalar value in an array'],
  ['array_sum', 'Sum numeric array elements as a real value'], ['series_sum', 'Sum numeric series elements as a real value'],
  ['array_split', 'Split an array at one or more indices'],
  ['zip', 'Combine array elements by index with null padding'],
  ['array_rotate_left', 'Rotate array elements to the left'], ['array_rotate_right', 'Rotate array elements to the right'],
  ['array_shift_left', 'Shift array elements left with optional padding'], ['array_shift_right', 'Shift array elements right with optional padding'],
  ['jaccard_index', 'Compute intersection size divided by union size'],
  ['set_union', 'Combine distinct values from arrays'], ['set_intersect', 'Keep distinct values shared by all arrays'], ['set_difference', 'Remove values found in subsequent arrays'],
  ['set_equals', 'Compare distinct array values regardless of order or duplicates'],
  ['repeat', 'Construct an array by repeating a scalar value'],
  ['pack_array', 'Construct a dynamic array from values or all columns'], ['bag_pack', 'Construct a dynamic bag from key/value pairs'], ['pack', 'Legacy bag constructor'], ['pack_dictionary', 'Legacy bag constructor'],
  ['bag_zip', 'Construct a bag from arrays of keys and values'],
  ['bag_merge', 'Merge bags with leftmost values taking precedence'], ['bag_remove_keys', 'Remove root keys or nested object properties'], ['bag_set_key', 'Set a root key or nested object property'],
  ['bag_has_key', 'Test for a dynamic object key'], ['set_has_element', 'Test dynamic array membership'],
  ['base64_decode_tostring', 'Decode Base64 text'], ['url_decode', 'Decode URL-escaped text'],
  ['parse_url', 'Parse an absolute URL into its components'], ['parseurl', 'Legacy alias for parse_url'],
  ['parse_urlquery', 'Parse URL query parameters'], ['parseurlquery', 'Legacy alias for parse_urlquery'],
  ['ipv4_is_private', 'Test for an RFC1918 address'],
  ['ipv4_is_in_range', 'Test an address against IPv4 ranges'],
  ['todatetime', 'Convert ISO text to UTC datetime'],
  ['iff', 'Conditional value'], ['case', 'Conditional branches'], ['coalesce', 'First non-null value'], ['strlen', 'String length'],
  ['substring', 'Extract text'], ['indexof', 'Find a literal string occurrence'], ['strcat', 'Concatenate values'],
  ['split', 'Split text into a dynamic array'], ['extract', 'Extract a regular expression group'],
  ['trim', 'Trim regex matches at both ends'], ['trim_start', 'Trim a leading regex match'], ['trim_end', 'Trim a trailing regex match'], ['replace_string', 'Replace literal text'], ['replace_strings', 'Replace literal strings in priority order'], ['replace_regex', 'Replace regex matches with capture substitutions'], ['replace', 'Legacy regex replacement (regex, rewrite, source)'],
  ['count', 'Count rows'], ['countif', 'Conditional count'], ['sumif', 'Conditional sum'],
  ['sum', 'Sum values'], ['min', 'Minimum value'], ['max', 'Maximum value'], ['avg', 'Average value'],
  ['make_set', 'Collect distinct values'], ['make_list', 'Collect values'],
  ['take_any', 'Select a group value'],
  ['make_bag', 'Merge property bags in a group'], ['make_bag_if', 'Merge property bags matching a predicate'],
  ['make_dictionary', 'Legacy property bag aggregate'],
  ['not', 'Negate a Boolean expression'], ['bin', 'Round down to a numeric or time bucket'],
  ['dcount', 'Count distinct non-null values (exact in Striem)'],
  ['dcountif', 'Count distinct values matching a predicate'],
  ['count_distinct', 'Count distinct non-null values'], ['count_distinctif', 'Conditional distinct count'],
  ['variance', 'Sample variance'], ['variancep', 'Population variance'],
  ['varianceif', 'Conditional sample variance'], ['variancepif', 'Conditional population variance'],
  ['stdev', 'Sample standard deviation'], ['stdevp', 'Population standard deviation'],
  ['stdevif', 'Conditional sample standard deviation'], ['stdevpif', 'Conditional population standard deviation'],
  ['avgif', 'Conditional average'], ['minif', 'Conditional minimum'], ['maxif', 'Conditional maximum'],
  ['startofday', 'UTC start of day'], ['endofday', 'UTC end of day'],
  ['startofweek', 'UTC start of week (Sunday)'], ['endofweek', 'UTC end of week'],
  ['startofmonth', 'UTC start of month'], ['endofmonth', 'UTC end of month'],
  ['startofyear', 'UTC start of year'], ['endofyear', 'UTC end of year'],
], label => `${label}()`);

function createCompletions(
  type: string,
  definitions: [label: string, detail: string][],
  apply: (label: string) => string,
): Completion[] {
  return definitions.map(([label, detail]) => ({
    label, detail, type,
    apply: type === 'function' ? (view, completion, from, to) => {
      const existing = view.state.sliceDoc(to).match(/^[ \t]*\(/);
      if (existing) {
        view.dispatch({
          ...insertCompletionText(view.state, label, from, to),
          selection: { anchor: from + label.length + existing[0].length },
          annotations: pickedCompletion.of(completion),
        });
      } else snippet(`${label}(\${})`)(view, completion, from, to);
    } : apply(label),
  }));
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
    const target = queryCompletionContext(context.state.doc.toString(), context.pos);
    if (!target || (!context.explicit && !target.prefix && !/[\s(,]$/.test(context.state.sliceDoc(0, context.pos)))) return null;
    const completions = target.kind === 'source' ? [...options.getTableCompletions(),
      ...operatorCompletions.filter(completion => completion.label === 'let'),
      { label: 'print', type: 'keyword', apply: 'print ' },
      { label: 'range', type: 'keyword', apply: 'range ' }]
      : target.kind === 'table' ? options.getTableCompletions()
      : target.kind === 'operator' ? operatorCompletions
      : target.kind === 'comparison' ? comparisonCompletions
      : target.kind === 'sort' ? [...options.getFieldCompletions(),
        { label: 'asc', type: 'keyword' }, { label: 'desc', type: 'keyword' }]
      : [...functionCompletions, ...options.getFieldCompletions(),
        { label: 'true', type: 'constant' }, { label: 'false', type: 'constant' }, { label: 'null', type: 'constant' }];
    return {
      from: target.from,
      to: target.to,
      options: completions.map(completionPresentation),
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
        autocompletion({
          override: [completionSource], activateOnTyping: true,
          activateOnCompletion: completion => ['operator', 'keyword', 'function'].includes(completion.type || ''),
          tooltipClass: () => 'hunting-completions',
          positionInfo: (_view, list, option, info, space) => {
            const right = space.right - list.right;
            const left = list.left - space.left;
            if (window.innerWidth <= 600 || Math.max(right, left) < 240) {
              return { style: 'position: static; width: auto; max-width: 100%;', class: 'completion-info-stacked' };
            }
            const top = Math.max(space.top - list.top,
              Math.min(option.top - list.top, space.bottom - list.top - (info.bottom - info.top)));
            const side = right >= left ? 'left: calc(100% + 4px)' : 'right: calc(100% + 4px)';
            return { style: `${side}; top: ${top}px; width: ${Math.min(300, Math.max(right, left) - 8)}px;` };
          },
          icons: false,
          addToOptions: [{ position: 20, render: completion => {
            const icon = document.createElement('span');
            icon.className = `completion-kind completion-kind-${completion.type}`;
            icon.setAttribute('aria-hidden', 'true');
            icon.textContent = completion.type === 'function' ? 'ƒ'
              : completion.type === 'operator' ? '=' : completion.type === 'keyword' ? '|'
              : completion.type === 'class' ? '▦' : completion.type === 'constant' ? '#'
              : completion.type === 'property' ? '{}' : 'a';
            return icon;
          } }],
        }),
        language,
        syntaxHighlighting(highlightStyle),
        editorTheme,
        EditorView.updateListener.of(update => {
          if (!update.docChanged) return;
          options.onDocumentChange();
          update.view.dispatch(setDiagnostics(update.state, []));
        }),
        EditorView.contentAttributes.of({
          'aria-label': 'KQL query editor',
          'aria-keyshortcuts': 'Shift+Enter Control+Enter',
        }),
        keymap.of([
          { key: 'Shift-Enter', run: () => { options.onRun(); return true; } },
          { key: 'Ctrl-Enter', run: insertPipelineLine },
          { key: 'Tab', run: view => completionStatus(view.state) === 'active' && acceptCompletion(view) },
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

function completionPresentation(completion: Completion): Completion {
  const kind = completion.type === 'function' ? 'Function' : completion.type === 'operator' ? 'Operator'
    : completion.type === 'keyword' ? 'Keyword' : completion.type === 'class' ? 'Table'
    : completion.type === 'constant' ? 'Literal' : completion.detail?.split(' · ')[0] || 'Column';
  return {
    ...completion,
    detail: kind,
    info: () => {
      const panel = document.createElement('div');
      panel.className = 'completion-documentation';
      const title = document.createElement('code');
      title.textContent = completion.label;
      const description = document.createElement('p');
      description.textContent = completion.detail || kind;
      panel.append(title, description);
      return panel;
    },
  };
}

function insertPipelineLine(view: EditorView): boolean {
  view.dispatch(view.state.replaceSelection('\n| '));
  return true;
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
