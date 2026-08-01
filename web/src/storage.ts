import type { QueryHistoryItem, SavedQuery } from './types';

export const storageKeys = {
  history: 'striem.queryHistory',
  saved: 'striem.savedQueries',
  questionDrafts: 'striem.questionDrafts',
} as const;

export interface BrowserStorage {
  readArray<T>(key: string): T[];
  write<T>(key: string, value: T): boolean;
}

export function createBrowserStorage(onError: () => void): BrowserStorage {
  return {
    readArray<T>(key: string): T[] {
      try {
        const value: unknown = JSON.parse(window.localStorage.getItem(key) ?? 'null');
        return Array.isArray(value) ? value as T[] : [];
      } catch {
        return [];
      }
    },

    write<T>(key: string, value: T): boolean {
      try {
        window.localStorage.setItem(key, JSON.stringify(value));
        return true;
      } catch {
        onError();
        return false;
      }
    },
  };
}

export function readQueryHistory(storage: BrowserStorage): QueryHistoryItem[] {
  return storage.readArray<unknown>(storageKeys.history).filter(isQueryHistoryItem);
}

export function readSavedQueries(storage: BrowserStorage): SavedQuery[] {
  return storage.readArray<unknown>(storageKeys.saved).filter(isSavedQuery);
}

export function readQuestionDrafts(storage: BrowserStorage): Map<string, string> {
  const entries = storage.readArray<unknown>(storageKeys.questionDrafts)
    .filter((value): value is { id: string; value: string } => isRecord(value) && hasStrings(value, ['id', 'value']));
  return new Map(entries.map(entry => [entry.id, entry.value]));
}

function isQueryHistoryItem(value: unknown): value is QueryHistoryItem {
  return isRecord(value) && hasStrings(value, ['query', 'runAt']);
}

function isSavedQuery(value: unknown): value is SavedQuery {
  return isRecord(value) && hasStrings(value, ['id', 'name', 'query', 'savedAt']);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function hasStrings(record: Record<string, unknown>, keys: string[]): boolean {
  return keys.every(key => typeof record[key] === 'string');
}
