export type EventRow = Record<string, unknown>;

export interface QueryPosition {
  line: number;
  column: number;
}

export interface QueryError {
  error?: string;
  message?: string;
  position?: QueryPosition;
  retryAfterMs?: number;
}

export interface QueryResult {
  columns: string[];
  rows: EventRow[];
  rowCount: number;
  durationMs: number;
}

export interface FieldMetadata {
  path: string;
  type: string;
}

export interface FieldGroup {
  table: string;
  fields: FieldMetadata[];
}

export interface FieldsResponse {
  common: FieldMetadata[];
  tables: FieldGroup[];
}

export interface SchemaResponse {
  challengeName?: string;
}

export interface SavedQuery {
  id: string;
  name: string;
  query: string;
  savedAt: string;
}

export interface QueryHistoryItem {
  query: string;
  runAt: string;
}

export interface Bookmark {
  id: string;
  rowKey: string;
  row: EventRow;
  query: string;
  table: string;
  note: string;
  createdAt: string;
}

export interface InvestigationQuestion {
  id: string;
  title: string;
  prompt: string;
  solved: boolean;
  answer?: string;
}

export interface ChallengeState {
  questions: InvestigationQuestion[];
  solvedQuestions: number;
  totalQuestions: number;
  completed: boolean;
  flag?: string;
}

export interface AnswerResponse {
  correct: boolean;
  alreadySolved?: boolean;
  state: ChallengeState;
}
