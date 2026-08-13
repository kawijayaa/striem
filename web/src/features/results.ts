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
  const field = logicalScalarFields(row)[0];
  return String(field?.[1] ?? row.Source ?? 'Security event');
}

export function resultContext(row: EventRow): string {
  return [row.Source, ...logicalScalarFields(row).slice(1, 3).map(([, value]) => value)]
    .filter(value => value !== null && value !== undefined && value !== '')
    .join(' · ');
}

function logicalScalarFields(row: EventRow): [string, unknown][] {
  const system = new Set(['TimeGenerated', 'Source', 'RawData']);
  return Object.entries(row).filter(([name, value]) => !system.has(name)
    && value !== null && value !== undefined && value !== '' && typeof value !== 'object');
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
