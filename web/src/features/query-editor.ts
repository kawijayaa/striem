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
  'let', 'declare', 'query_parameters', 'where', 'filter', 'search', 'project', 'project-away', 'project-keep', 'project-rename', 'project-reorder',
  'extend', 'summarize', 'distinct', 'order', 'sort', 'top', 'take',
  'getschema', 'limit', 'sample', 'sample-distinct', 'count', 'serialize', 'as', 'mv-expand', 'mv-apply', 'union', 'join',
  'lookup', 'invoke', 'parse', 'parse-where', 'parse-kv', 'render', 'evaluate', 'bag_unpack', 'make-series', 'step', 'from', 'to', 'default', 'nonempty', 'columnsConflict', 'ignoredProperties', 'timechart', 'columnchart', 'barchart', 'piechart', 'linechart', 'areachart', 'scatterchart', 'with', 'flags', 'simple', 'regex', 'relaxed', 'pair_delimiter', 'kv_delimiter', 'quote', 'escape', 'greedy', 'kind', 'innerunique', 'rightsemi', 'rightanti', 'inner', 'leftouter', 'rightouter', 'fullouter', 'leftsemi', 'leftanti', 'with_itemindex',
  'on', 'by', 'of', 'asc', 'desc', 'granny-asc', 'granny-desc',
]);
const logicalOperators = new Set([
  'and', 'or', 'not', '=~', '!~', 'in', 'in~', '!in', '!in~', 'between', '!between', 'contains', 'contains_cs',
  'startswith', 'startswith_cs', 'endswith', 'endswith_cs', 'has', 'has_cs', 'hasprefix', 'hasprefix_cs',
  'hassuffix', 'hassuffix_cs', 'has_any', 'has_all',
]);
const functions = new Set([
  'prev', 'next', 'row_number', 'row_cumsum', 'row_rank_dense', 'row_rank_min', 'now', 'ago', 'datetime', 'bool', 'boolean', 'toscalar', 'materialize', 'arg_max', 'arg_min', 'argmax', 'argmin', 'tostring', 'toguid', 'toint', 'tolong', 'toreal', 'tobool', 'toboolean', 'todouble', 'tolower', 'toupper', 'isnull',
  'isnotnull', 'isempty', 'isnotempty', 'parse_json', 'parse_csv', 'parse_xml', 'parsejson', 'todynamic', 'toobject', 'array_length', 'array_concat', 'array_iff', 'array_iif', 'array_index_of', 'array_sum', 'series_sum', 'zip', 'array_reverse', 'array_slice', 'array_split', 'array_rotate_left', 'array_rotate_right', 'array_shift_left', 'array_shift_right', 'bag_keys', 'todatetime',
  'gzip_compress_to_base64_string', 'gzip_decompress_from_base64_string', 'zlib_compress_to_base64_string', 'zlib_decompress_from_base64_string', 'base64_decode_tostring', 'base64_decodestring', 'base64_encode_tostring', 'base64_encodestring', 'base64_decode_toarray', 'base64_encode_fromarray', 'range', 'binary_all_and', 'binary_all_or', 'binary_all_xor', 'tohex', 'make_datetime', 'datetime_add', 'datetime_diff', 'unixtime_seconds_todatetime', 'unixtime_milliseconds_todatetime', 'unixtime_microseconds_todatetime', 'unixtime_nanoseconds_todatetime', 'datetime_part', 'datepart', 'getyear', 'getmonth', 'monthofyear', 'dayofmonth', 'dayofyear', 'hourofday', 'week_of_year', 'totimespan', 'totime', 'rand', 'bin_at', 'array_sort_asc', 'array_sort_desc', 'format_datetime', 'format_timespan', 'make_timespan', 'dayofweek', 'typeof', 'pi', 'degrees', 'radians', 'cot', 'exp2', 'exp10', 'log2', 'gamma', 'loggamma', 'erf', 'erfc', 'isfinite', 'isinf', 'isnan', 'max_of', 'min_of', 'strcmp', 'isascii', 'isutf8', 'notempty', 'notnull', 'weekofyear', 'countof', 'strcat_array', 'strcat_delim', 'strrep', 'translate', 'reverse', 'binary_and', 'binary_or', 'binary_xor', 'binary_not', 'binary_shift_left', 'binary_shift_right', 'bitset_count_ones', 'unicode_codepoints_from_string', 'unicode_codepoints_to_string', 'to_utf8', 'make_string', 'hash_md5', 'hash_sha1', 'hash_sha256', 'url_decode', 'url_encode', 'url_encode_component', 'parse_url', 'parseurl', 'parse_urlquery', 'parseurlquery', 'bag_has_key', 'set_has_element', 'set_union', 'set_intersect', 'set_difference', 'set_equals', 'jaccard_index',
  'ipv4_is_private', 'ipv4_is_in_range', 'parse_ipv4', 'parse_ipv4_mask', 'format_ipv4', 'format_ipv4_mask', 'ipv4_netmask_suffix', 'ipv4_compare', 'ipv4_is_match', 'ipv4_range_to_cidr_list',
  'parse_ipv6', 'parse_ipv6_mask', 'ipv6_compare', 'ipv6_is_match', 'ipv6_is_in_range', 'ipv4_is_in_any_range', 'ipv6_is_in_any_range',
  'count', 'countif', 'sumif', 'iff', 'case', 'coalesce', 'strlen', 'substring', 'indexof', 'replace',
  'strcat', 'sum', 'min', 'max', 'avg', 'split', 'extract', 'extract_all', 'extractall', 'trim', 'trim_start', 'trim_end', 'replace_string', 'replace_strings', 'replace_regex',
  'variance', 'variancep', 'varianceif', 'variancepif', 'stdev', 'stdevp', 'stdevif', 'stdevpif',
  'pack_array', 'repeat', 'bag_pack', 'pack', 'pack_dictionary', 'bag_merge', 'bag_zip', 'bag_remove_keys', 'bag_set_key', 'make_set', 'make_list', 'make_bag', 'make_bag_if', 'make_dictionary', 'take_any', 'any', 'take_anyif', 'anyif',
  'not', 'bin', 'dcount', 'dcountif', 'count_distinct', 'count_distinctif', 'avgif', 'minif', 'maxif',
  'startofday', 'endofday', 'startofweek', 'endofweek', 'startofmonth', 'endofmonth', 'startofyear', 'endofyear',
]);
const literals = new Set(['true', 'false', 'null']);
const types = new Set(['bool', 'int', 'string', 'long', 'real', 'datetime', 'timespan', 'dynamic']);
const comparisonCompletions = createCompletions('operator', expressionOperatorDefinitions, label => `${label} `)
  .map(completion => ({ ...completion, boost: completion.label === '==' ? 30 : completion.label === '!=' ? 25
    : completion.label === 'has' ? 15 : completion.label === 'contains' ? 10 : 0 }));

const operatorCompletions = createCompletions('keyword', [
  ['let', 'Define a value, query, or function'],
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
  ['invoke', 'Apply a query function to this table'],
  ['parse', 'Extract typed columns from a string with a pattern'],
  ['parse-where', 'Extract columns and keep only rows that match'],
  ['parse-kv', 'Extract typed columns from key/value text'],
  ['render', 'Annotate results with a visualization (shown as a table)'],
  ['evaluate bag_unpack', 'Turn each property of a dynamic column into a column'],
  ['make-series', 'Build arrays of aggregates over time bins'],
], label => `${label} `);

const commandCompletions = createCompletions('keyword', [
  ['.show tables', 'List available tables'], ['.show table', 'Show the schema of one table'],
], label => label === '.show table' ? `${label} ` : label);
const commandSchemaCompletions = createCompletions('keyword', [
  ['cslschema', 'Show column names and KQL types'], ['kqlschema', 'Alias for cslschema'],
  ['schema as json', 'Show ordered column metadata as JSON text'],
], label => label);

const tabularFunctionCompletions = createCompletions('function', [
  ['materialize', 'Cache a table once for reuse during this query'],
], label => `${label}()`);

const aggregateCompletions = createCompletions('function', [
  ['binary_all_and', 'Aggregate integer values with bitwise AND'],
  ['binary_all_or', 'Aggregate integer values with bitwise OR'],
  ['binary_all_xor', 'Aggregate integer values with bitwise XOR'],
  ['make_set', 'Collect distinct values'],
  ['make_list', 'Collect values'],
  ['make_bag', 'Merge property bags in a group'],
  ['make_bag_if', 'Merge property bags matching a predicate'],
  ['make_dictionary', 'Legacy property bag aggregate'],
  ['dcount', 'Count distinct non-null values (exact in Striem)'],
  ['dcountif', 'Count distinct values matching a predicate'],
  ['count_distinct', 'Count distinct non-null values'],
  ['count_distinctif', 'Conditional distinct count'],
  ['variance', 'Sample variance'],
  ['variancep', 'Population variance'],
  ['varianceif', 'Conditional sample variance'],
  ['variancepif', 'Conditional population variance'],
  ['stdev', 'Sample standard deviation'],
  ['stdevp', 'Population standard deviation'],
  ['stdevif', 'Conditional sample standard deviation'],
  ['stdevpif', 'Conditional population standard deviation'],
  ['count', 'Count rows'], ['countif', 'Conditional count'], ['sumif', 'Conditional sum'],
  ['sum', 'Sum values'], ['min', 'Minimum value'], ['max', 'Maximum value'], ['avg', 'Average value'],
  ['avgif', 'Conditional average'], ['minif', 'Conditional minimum'], ['maxif', 'Conditional maximum'],
  ['arg_max', 'Return values from the row with the maximum key in each group'],
  ['arg_min', 'Return values from the row with the minimum key in each group'],
  ['argmax', 'Legacy alias for arg_max with prefixed result names'],
  ['argmin', 'Legacy alias for arg_min with prefixed result names'],
  ['take_any', 'Return expressions from one arbitrary record in each group'],
  ['take_anyif', 'Select a nonempty group value where the predicate is true'],
  ['any', 'Legacy alias for take_any with prefixed result names'],
  ['anyif', 'Legacy alias for take_anyif with prefixed result names'],
], label => `${label}()`);

const functionCompletions = createCompletions('function', [
  ['prev', 'Value from a previous row of a sorted or serialized table'],
  ['next', 'Value from a following row of a sorted or serialized table'],
  ['row_number', 'Row index in a sorted or serialized table'],
  ['row_cumsum', 'Running total in a sorted or serialized table'],
  ['row_rank_dense', 'Rank that increases when the term changes'],
  ['row_rank_min', 'Lowest row number at which the term appears'],
  ['totimespan', 'Convert text such as 1.02:03:04 or 90m to a timespan'],
  ['totime', 'Alias for totimespan'],
  ['rand', 'Random real in [0, 1), or long in [0, n)'],
  ['bin_at', 'Round down to a bin aligned to a fixed point'],
  ['array_sort_asc', 'Sort an array ascending; reorder other arrays to match'],
  ['array_sort_desc', 'Sort an array descending; reorder other arrays to match'],
  ['format_datetime', 'Format a datetime with yyyy-MM-dd HH:mm:ss style specifiers'],
  ['format_timespan', 'Format a timespan with d.hh:mm:ss style specifiers'],
  ['make_timespan', 'Build a timespan from hours, minutes and seconds'],
  ['dayofweek', 'Timespan since the preceding Sunday'],
  ['typeof', 'Type literal, for example extract(..., typeof(long))'],
  ['pi', 'The constant pi'],
  ['degrees', 'Convert radians to degrees'],
  ['radians', 'Convert degrees to radians'],
  ['cot', 'Cotangent'],
  ['exp2', 'Two raised to a power'],
  ['exp10', 'Ten raised to a power'],
  ['log2', 'Base-2 logarithm'],
  ['gamma', 'Gamma function'],
  ['loggamma', 'Logarithm of the absolute gamma value'],
  ['erf', 'Error function'],
  ['erfc', 'Complementary error function'],
  ['isfinite', 'Whether a number is finite'],
  ['isinf', 'Whether a number is infinite'],
  ['isnan', 'Whether a number is NaN'],
  ['max_of', 'Largest non-null argument'],
  ['min_of', 'Smallest non-null argument'],
  ['strcmp', 'Compare two strings (-1, 0 or 1)'],
  ['isascii', 'Whether text is ASCII'],
  ['isutf8', 'Whether text is valid UTF-8'],
  ['notempty', 'Alias for isnotempty'],
  ['notnull', 'Alias for isnotnull'],
  ['weekofyear', 'Legacy ISO week number'],
  ['now', 'Current UTC time'], ['ago', 'Relative UTC time'], ['datetime', 'Datetime literal'],
  ['bool', 'Boolean literal: true, false or null'], ['boolean', 'Alias for bool literals'],
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
  ['range', 'Build a dynamic numeric sequence'],
  ['tohex', 'Convert an integer to hexadecimal text'],
  ['make_datetime', 'Construct a UTC datetime from components'],
  ['datetime_add', 'Add calendar periods or fixed intervals to a datetime'],
  ['unixtime_seconds_todatetime', 'Convert Unix seconds to UTC datetime'],
  ['unixtime_milliseconds_todatetime', 'Convert Unix milliseconds to UTC datetime'],
  ['unixtime_microseconds_todatetime', 'Convert Unix microseconds to UTC datetime'],
  ['unixtime_nanoseconds_todatetime', 'Convert Unix nanoseconds to UTC datetime'],
  ['datetime_part', 'Extract a calendar or clock part'],
  ['datepart', 'Legacy alias for datetime_part'],
  ['getyear', 'Extract the UTC year'],
  ['getmonth', 'Extract the UTC month'],
  ['monthofyear', 'Extract the UTC month'],
  ['dayofmonth', 'Extract the day of the month'],
  ['dayofyear', 'Extract the day of the year'],
  ['hourofday', 'Extract the UTC hour'],
  ['week_of_year', 'Extract the ISO week number'],
  ['datetime_diff', 'Count period boundaries between datetimes'],
  ['countof', 'Count overlapping text or nonoverlapping regex matches'],
  ['strcat_array', 'Join dynamic array values with a delimiter'],
  ['strcat_delim', 'Join scalar values with a delimiter'],
  ['strrep', 'Repeat a string with an optional delimiter'],
  ['translate', 'Replace or delete a set of characters'],
  ['reverse', 'Reverse Unicode character order'],
  ['binary_and', 'Bitwise AND of integers'],
  ['binary_or', 'Bitwise OR of integers'],
  ['binary_xor', 'Bitwise XOR of integers'],
  ['binary_not', 'Complement all 64 bits'],
  ['binary_shift_left', 'Shift left modulo 64 bits'],
  ['binary_shift_right', 'Signed shift right modulo 64 bits'],
  ['bitset_count_ones', 'Count set bits in a 64-bit integer'],
  ['unicode_codepoints_from_string', 'Convert text to a Unicode codepoint array'],
  ['unicode_codepoints_to_string', 'Convert integer codepoints and arrays to text'],
  ['to_utf8', 'Legacy alias for Unicode codepoint extraction'],
  ['make_string', 'Legacy alias for Unicode codepoint conversion'],
  ['hash_md5', 'Compute an MD5 hexadecimal digest'],
  ['hash_sha1', 'Compute a SHA-1 hexadecimal digest'],
  ['hash_sha256', 'Compute a SHA-256 hexadecimal digest'],
  ['url_encode', 'Encode URL text with spaces as plus signs'],
  ['url_encode_component', 'Encode URL text with spaces as percent escapes'],
  ['parse_url', 'Parse an absolute URL into its components'], ['parseurl', 'Legacy alias for parse_url'],
  ['parse_urlquery', 'Parse URL query parameters'], ['parseurlquery', 'Legacy alias for parse_urlquery'],
  ['ipv4_is_private', 'Test for an RFC1918 address'],
  ['ipv4_is_in_range', 'Test an address against IPv4 ranges'],
  ['parse_ipv4', 'Convert an IPv4 address to a long'],
  ['parse_ipv4_mask', 'Convert an IPv4 network to a long'],
  ['format_ipv4', 'Format an IPv4 string or long'],
  ['format_ipv4_mask', 'Format an IPv4 network with CIDR suffix'],
  ['ipv4_netmask_suffix', 'Read an IPv4 prefix length'],
  ['ipv4_compare', 'Compare IPv4 networks in numeric order'],
  ['ipv4_is_match', 'Match IPv4 addresses under a combined prefix'],
  ['ipv4_range_to_cidr_list', 'Cover an IPv4 address interval with CIDR networks'],
  ['gzip_compress_to_base64_string', 'Encode text in gzip and base64'],
  ['gzip_decompress_from_base64_string', 'Decode base64 and gzip text'],
  ['zlib_compress_to_base64_string', 'Encode text in zlib and base64'],
  ['zlib_decompress_from_base64_string', 'Decode base64 and zlib text'],
  ['base64_encode_tostring', 'Encode UTF-8 text as base64'],
  ['base64_encodestring', 'Encode UTF-8 text as base64 (legacy alias)'],
  ['base64_decodestring', 'Decode base64 as UTF-8 text (legacy alias)'],
  ['base64_decode_toarray', 'Decode base64 to a dynamic byte array'],
  ['base64_encode_fromarray', 'Encode a dynamic byte array as base64'],
  ['parse_ipv6', 'Normalize IPv6 or IPv4 to expanded IPv6'],
  ['parse_ipv6_mask', 'Normalize an IPv6 network with an explicit prefix'],
  ['ipv6_compare', 'Compare IPv6 and IPv4 networks'],
  ['ipv6_is_match', 'Match IPv6 and IPv4 networks'],
  ['ipv6_is_in_range', 'Test membership in an IPv6 network'],
  ['ipv4_is_in_any_range', 'Test membership in IPv4 ranges'],
  ['ipv6_is_in_any_range', 'Test membership in IPv6 ranges'],
  ['todatetime', 'Convert ISO text to UTC datetime'],
  ['toscalar', 'Evaluate a scalar or the first table value once'],
  ['iff', 'Conditional value'], ['case', 'Conditional branches'], ['coalesce', 'First non-null value'], ['strlen', 'String length'],
  ['substring', 'Extract text'], ['indexof', 'Find a literal string occurrence'], ['strcat', 'Concatenate values'],
  ['split', 'Split text into a dynamic array'], ['extract_all', 'Extract all regex matches and selected capture groups'], ['extractall', 'Legacy alias for extract_all'], ['extract', 'Extract a regular expression group'],
  ['trim', 'Trim regex matches at both ends'], ['trim_start', 'Trim a leading regex match'], ['trim_end', 'Trim a trailing regex match'], ['replace_string', 'Replace literal text'], ['replace_strings', 'Replace literal strings in priority order'], ['replace_regex', 'Replace regex matches with capture substitutions'], ['replace', 'Legacy regex replacement (regex, rewrite, source)'],
  ['not', 'Negate a Boolean expression'], ['bin', 'Round down to a numeric or time bucket'],
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
    if (!target || (!context.explicit && !target.prefix && !/[\s(,|)\]"']$/.test(context.state.sliceDoc(0, context.pos)))) return null;
    const completions = target.kind === 'management' ? commandCompletions
      : target.kind === 'management_table' ? options.getTableCompletions()
      : target.kind === 'management_schema' ? commandSchemaCompletions
      : target.kind === 'source' ? [...options.getTableCompletions(), ...tabularFunctionCompletions,
      ...operatorCompletions.filter(completion => completion.label === 'let'),
      { label: 'declare query_parameters', type: 'keyword', detail: 'Declare typed request values', apply: 'declare query_parameters ' },
      { label: 'print', type: 'keyword', apply: 'print ' },
      { label: 'range', type: 'keyword', apply: 'range ' }]
      : target.kind === 'table' ? [...options.getTableCompletions(), ...tabularFunctionCompletions]
      : target.kind === 'operator' ? operatorCompletions
      : target.kind === 'comparison' ? comparisonCompletions.filter(completion =>
        completion.label.startsWith(target.prefix.toLowerCase()))
      : target.kind === 'column' ? options.getFieldCompletions()
      : target.kind === 'sort' ? [...options.getFieldCompletions(),
        { label: 'asc', type: 'keyword' }, { label: 'desc', type: 'keyword' }]
      : [...functionCompletions, ...(target.kind === 'aggregation' ? aggregateCompletions : []), ...(target.kind === 'binding' ? [...options.getTableCompletions(), ...tabularFunctionCompletions] : []), ...options.getFieldCompletions().map(completion => ({ ...completion, boost: 10 })),
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
          activateOnTypingDelay: 100,
          maxRenderedOptions: 60,
          aboveCursor: false,
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
          'aria-keyshortcuts': 'Shift+Enter Control+Enter Control+Space',
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
