export function querySource(query: string): string | undefined {
  return query.match(/^(?:\s*)([A-Za-z_][A-Za-z0-9_]*)(?=\s*(?:\||$))/m)?.[1];
}

export function insertAfterSource(query: string, clause: string): string {
  const sourceLine = /^(\s*[A-Za-z_][A-Za-z0-9_]*\s*)(?=\||$)/m;
  return sourceLine.test(query)
    ? query.replace(sourceLine, match => `${match.trimEnd()}\n${clause}\n`)
    : `${query.trimEnd()}\n${clause}`;
}

export function addTimeRangeFilter(query: string, start: Date, end: Date): string {
  return insertAfterSource(
    query,
    `| where TimeGenerated >= datetime("${start.toISOString()}") and TimeGenerated < datetime("${end.toISOString()}")`,
  );
}

function kqlLiteral(value: unknown): string {
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return `"${String(value).replaceAll('\\', '\\\\').replaceAll('"', '\\"')}"`;
}

export function addValueFilter(query: string, column: string, value: unknown, negate = false): string {
  return insertAfterSource(query, `| where ${column} ${negate ? '!=' : '=='} ${kqlLiteral(value)}`);
}
