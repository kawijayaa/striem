export interface SearchMatch {
  start: number;
  end: number;
}

// Match the original text directly: lowercasing can change its length and move
// highlights away from the text that matched (for example, after Unicode İ).
export function findJSONSearchMatches(text: string, query: string): SearchMatch[] {
  if (!query) return [];
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const pattern = new RegExp(escaped, 'giu');
  return Array.from(text.matchAll(pattern), match => ({
    start: match.index,
    end: match.index + match[0].length,
  }));
}
