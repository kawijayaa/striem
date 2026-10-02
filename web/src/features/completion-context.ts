export interface QueryCompletionContext {
  from: number;
  to: number;
  kind: 'source' | 'table' | 'operator' | 'expression' | 'sort';
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
        else quote = '';
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
  const pipe = code.lastIndexOf('|');
  const statement = code.lastIndexOf(';');
  const boundary = Math.max(pipe, statement);
  const segment = code.slice(boundary + 1);
  const source = pipe <= statement;
  const phrase = !source && segment.match(/^\s*(order|sort)[ \t]+(?:b|by)?$/i);
  if (phrase) {
    const from = boundary + 1 + segment.search(/\S/);
    return { from, to: position, kind: 'operator', prefix: query.slice(from, position) };
  }
  const kind = /^\s*[A-Za-z_-]*$/.test(segment)
    ? source ? 'source' : 'operator'
    : /^\s*(?:order|sort)\s+by\b/i.test(segment) ? 'sort'
    : /\b(?:join|union|lookup)\b\s*(?:kind\s*=\s*[A-Za-z_]+\s+)?\(?\s*[A-Za-z_0-9]*$/i.test(segment)
      || /^\s*\(\s*[A-Za-z_0-9]*$/.test(segment) ? 'table' : 'expression';
  const token = kind === 'operator'
    ? code.match(/[A-Za-z_][A-Za-z_0-9-]*$/)
    : code.match(/[A-Za-z_][A-Za-z_0-9]*(?:\.[A-Za-z_0-9]*)*$/);
  const from = token ? position - token[0].length : position;
  const suffix = query.slice(position).match(kind === 'operator' ? /^[A-Za-z_0-9-]*/ : /^[A-Za-z_0-9.]*/)?.[0] ?? '';
  return { from, to: position + suffix.length, kind, prefix: query.slice(from, position) };
}
