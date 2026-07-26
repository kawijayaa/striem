import type { Bookmark, QueryHistoryItem, SavedQuery } from './types';

export const storageKeys = {
  history: 'striem.queryHistory',
  saved: 'striem.savedQueries',
  bookmarks: 'striem.bookmarks',
  onboarding: 'striem.onboarding.v1',
} as const;

export interface BrowserStorage {
  readArray<T>(key: string): T[];
  readBoolean(key: string): boolean;
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

    readBoolean(key: string): boolean {
      try {
        return JSON.parse(window.localStorage.getItem(key) ?? 'false') === true;
      } catch {
        return false;
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

export function readBookmarks(storage: BrowserStorage): Bookmark[] {
  return storage.readArray<unknown>(storageKeys.bookmarks)
    .filter(isBookmark)
    .map(bookmark => ({ ...bookmark, note: typeof bookmark.note === 'string' ? bookmark.note : '' }));
}

function isQueryHistoryItem(value: unknown): value is QueryHistoryItem {
  return isRecord(value) && hasStrings(value, ['query', 'runAt']);
}

function isSavedQuery(value: unknown): value is SavedQuery {
  return isRecord(value) && hasStrings(value, ['id', 'name', 'query', 'savedAt']);
}

function isBookmark(value: unknown): value is Bookmark {
  return isRecord(value)
    && isRecord(value.row)
    && hasStrings(value, ['id', 'rowKey', 'query', 'table', 'createdAt']);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function hasStrings(record: Record<string, unknown>, keys: string[]): boolean {
  return keys.every(key => typeof record[key] === 'string');
}
