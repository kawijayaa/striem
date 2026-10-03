import { expressionOperatorWords } from './expression-operators.ts';

export interface QueryCompletionContext {
  from: number;
  to: number;
  kind: 'source' | 'table' | 'binding' | 'aggregation' | 'operator' | 'expression' | 'column' | 'comparison' | 'sort';
  prefix: string;
}

// Keep offsets while hiding literals and comments from the context matcher.
// This also covers verbatim and multiline strings supported by the compiler.
function queryCodeBeforeCursor(source: string): string | null {
  const code = source.split('');
  let quote = '';
  let verbatim = false;
  let multiline = '';
  let comment = false;
  for (let index = 0; index < source.length; index++) {
    const char = source[index]!;
    if (comment) {
      if (char === '\n') comment = false;
      else code[index] = ' ';
      continue;
    }
    if (multiline) {
      if (source.startsWith(multiline, index)) {
        code.fill(' ', index, index + 3);
        code[index + 2] = '0';
        index += 2;
        multiline = '';
      } else code[index] = ' ';
      continue;
    }
    if (quote) {
      code[index] = ' ';
      if (char === '\n' || char === '\r') {
        quote = '';
      } else if (char === '\\' && !verbatim) {
        if (index + 1 < source.length) code[++index] = ' ';
      } else if (char === quote) {
        if (verbatim && source[index + 1] === quote) code[++index] = ' ';
        else { quote = ''; code[index] = '0'; }
      }
      continue;
    }
    if (source.startsWith('//', index)) {
      comment = true;
      code[index] = ' ';
    } else if (source.startsWith('```', index) || source.startsWith('~~~', index)) {
      multiline = source.slice(index, index + 3);
      code.fill(' ', index, index + 3);
      index += 2;
    } else if (char === '"' || char === "'") {
      quote = char;
      verbatim = source[index - 1] === '@';
      code[index] = ' ';
    }
  }
  return quote || multiline || comment ? null : code.join('');
}

export function queryCompletionContext(query: string, position: number): QueryCompletionContext | null {
  const code = queryCodeBeforeCursor(query.slice(0, position));
  if (code === null) return null;
  // A completed subquery must not keep controlling the outer expression.
  const scopes = [{ pipe: -1, statement: -1 }];
  for (let index = 0; index < code.length; index++) {
    const scope = scopes[scopes.length - 1]!;
    if (code[index] === '(') scopes.push({ ...scope });
    else if (code[index] === ')' && scopes.length > 1) scopes.pop();
    else if (code[index] === '|') scope.pipe = index;
    else if (code[index] === ';') scope.statement = index;
  }
  const { pipe, statement } = scopes[scopes.length - 1]!;
  const boundary = Math.max(pipe, statement);
  const segment = code.slice(boundary + 1);
  const source = pipe <= statement;
  const phrase = !source && segment.match(/^\s*(order|sort)[ \t]+(?:b|by)?$/i);
  if (phrase) {
    const from = boundary + 1 + segment.search(/\S/);
    return { from, to: position, kind: 'operator', prefix: query.slice(from, position) };
  }
  const regexOperator = segment.match(/\bmatches[ \t]+(?:r(?:e(?:g(?:e(?:x)?)?)?)?)?$/i);
  if (regexOperator && /^\s*(?:where|filter|search|extend|project|summarize|print|let)\b/i.test(segment)) {
    const from = boundary + 1 + regexOperator.index!;
    const suffix = query.slice(position).match(/^[A-Za-z_]*/)?.[0] ?? '';
    return { from, to: position + suffix.length, kind: 'comparison', prefix: query.slice(from, position) };
  }
  let kind: QueryCompletionContext['kind'] = /^\s*[A-Za-z_-]*$/.test(segment)
    ? source ? 'source' : 'operator'
    : /^\s*(?:order|sort)\s+by\b/i.test(segment) ? 'sort'
    : /^\s*(?:project-away|project-keep|project-reorder|distinct)\s/i.test(segment)
      || /^\s*summarize\b[\s\S]*\bby\s+[^()]*$/i.test(segment) ? 'column'
    : /\b(?:join|union|lookup)\b\s*(?:kind\s*=\s*[A-Za-z_]+\s+)?\(?\s*[A-Za-z_0-9]*$/i.test(segment)
      || /\bmaterialize\(\s*[A-Za-z_0-9]*$/i.test(segment)
      || /^\s*\(\s*[A-Za-z_0-9]*$/.test(segment) ? 'table'
    : /^\s*let\s+[A-Za-z_][A-Za-z_0-9]*\s*=\s*[A-Za-z_0-9]*$/i.test(segment) ? 'binding' : 'expression';
  const token = kind === 'operator'
    ? code.match(/[A-Za-z_][A-Za-z_0-9-]*$/)
    : code.match(/!?[A-Za-z_][A-Za-z_0-9]*(?:\.[A-Za-z_0-9]*)*~?$|[=!<>~]{1,2}$/);
  const from = token ? position - token[0].length : position;
  if (kind === 'expression') {
    const before = code.slice(boundary + 1, from).trimEnd();
    const lastWord = before.match(/[A-Za-z_][A-Za-z_0-9]*$/)?.[0]?.toLowerCase();
    const needsOperand = /^\s*(?:where|filter|search|project|extend|summarize|distinct|take|limit|top|print|order|sort)\s*$/i.test(before)
      || (lastWord !== undefined && expressionOperatorWords.has(lastWord));
    // Comparisons belong in predicates and calculated expressions, not between
    // the columns in a projection or the numeric argument of take/limit.
    const comparisonsAllowed = /^\s*(?:where|filter|search)\b/i.test(segment)
      || /^\s*(?:extend|project|summarize|print|let)\b[\s\S]*=/i.test(segment);
    if (comparisonsAllowed && !needsOperand && /[A-Za-z_0-9.)\]]$/.test(before)) kind = 'comparison';
  }
  if (kind === 'expression' && /^\s*summarize\s+(?:[\s\S]*,\s*)?(?:[A-Za-z_][A-Za-z_0-9]*\s*=\s*)?[A-Za-z_0-9]*$/i.test(segment)) {
    // Only offer row aggregates between complete aggregate expressions; inside
    // their arguments the available functions must remain scalar.
    let depth = 0;
    for (const char of segment) { if (char === '(') depth++; else if (char === ')') depth--; }
    if (depth === 0) kind = 'aggregation';
  }
  const rest = query.slice(position);
  const suffixPattern = kind === 'operator' ? /^[A-Za-z_0-9-]*/
    : token && /^[=!<>~]+$/.test(token[0]) ? token[0] === '!' && /^[A-Za-z_]/.test(rest)
      ? /^[A-Za-z_0-9]*~?/ : /^[=!<>~]*/
    : kind === 'comparison' ? /^[A-Za-z_0-9]*~?/ : /^[A-Za-z_0-9.]*/;
  const suffix = rest.match(suffixPattern)?.[0] ?? '';
  return { from, to: position + suffix.length, kind, prefix: query.slice(from, position) };
}
