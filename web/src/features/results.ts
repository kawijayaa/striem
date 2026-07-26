import type { EventRow, QueryResult } from '../types';

export interface ResultSort {
  column: string | null;
  direction: 'asc' | 'desc';
}

export function sortResultRows(result: QueryResult, sort: ResultSort): EventRow[] {
  if (!sort.column) return result.rows;
  const column = sort.column;
  const direction = sort.direction === 'asc' ? 1 : -1;
  return [...result.rows].sort((left, right) => direction * compareValues(left[column], right[column]));
}

export function nextResultSort(current: ResultSort, column: string): ResultSort {
  return current.column === column
    ? { column, direction: current.direction === 'asc' ? 'desc' : 'asc' }
    : { column, direction: 'asc' };
}

export function resultRowKey(result: QueryResult, row: EventRow): string {
  return `v2:${result.rows.indexOf(row)}:${JSON.stringify(row)}`;
}

export function resultIdentity(row: EventRow): string {
  return String(row.EventType || row.Message || row.Source || 'Security event');
}

export function resultContext(row: EventRow): string {
  return [row.Source, row.User, row.Host]
    .filter(value => value !== null && value !== undefined && value !== '')
    .join(' · ');
}

export function resultTime(row: EventRow): string | undefined {
  const value = row.TimeGenerated;
  if (typeof value !== 'string' && typeof value !== 'number') return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}

export function resultValue(value: unknown): string {
  if (typeof value === 'number') return value.toLocaleString();
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  return String(value);
}

export function createResultsCsv(result: QueryResult, rows: EventRow[]): string {
  const lines = [
    result.columns.map(encodeCsvValue).join(','),
    ...rows.map(row => result.columns.map(column => encodeCsvValue(row[column])).join(',')),
  ];
  return `\uFEFF${lines.join('\r\n')}\r\n`;
}

function compareValues(left: unknown, right: unknown): number {
  if (left === right) return 0;
  if (left === null || left === undefined) return 1;
  if (right === null || right === undefined) return -1;
  if (typeof left === 'number' && typeof right === 'number') return left - right;
  return comparableValue(left).localeCompare(comparableValue(right), undefined, {
    numeric: true,
    sensitivity: 'base',
  });
}

function comparableValue(value: unknown): string {
  return value !== null && typeof value === 'object' ? JSON.stringify(value) : String(value);
}

function encodeCsvValue(value: unknown): string {
  let text = value !== null && typeof value === 'object' ? JSON.stringify(value) : String(value ?? '');
  if (/^[=+@\t\r]/.test(text) || /^-\D/.test(text)) text = `'${text}`;
  return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}
