import type { AnswerResponse, ChallengeState, InvestigationQuestion, QueryResult } from '../types';

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function isCount(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}

export function parseQueryResult(value: unknown): QueryResult {
  if (!isRecord(value) || !Array.isArray(value.columns) || !value.columns.every(column => typeof column === 'string')
    || !Array.isArray(value.rows) || !value.rows.every(isRecord)
    || !isCount(value.rowCount) || value.rowCount !== value.rows.length
    || typeof value.durationMs !== 'number' || !Number.isFinite(value.durationMs) || value.durationMs < 0
    || (value.visualization !== undefined && (!isRecord(value.visualization) || typeof value.visualization.type !== 'string'
      || (value.visualization.properties !== undefined && !isRecord(value.visualization.properties))))) {
    throw new Error('Invalid server response');
  }
  return value as unknown as QueryResult;
}

function isQuestion(value: unknown): value is InvestigationQuestion {
  return isRecord(value) && typeof value.id === 'string' && value.id.length > 0
    && typeof value.title === 'string' && typeof value.prompt === 'string'
    && typeof value.solved === 'boolean' && (value.answer === undefined || typeof value.answer === 'string');
}

export function parseChallengeState(value: unknown): ChallengeState {
  if (!isRecord(value) || !Array.isArray(value.questions) || !value.questions.every(isQuestion)
    || !isCount(value.totalQuestions) || value.totalQuestions !== value.questions.length
    || !isCount(value.solvedQuestions) || value.solvedQuestions !== value.questions.filter(question => question.solved).length
    || typeof value.completed !== 'boolean' || value.completed !== (value.totalQuestions > 0 && value.solvedQuestions === value.totalQuestions)
    || (value.flag !== undefined && typeof value.flag !== 'string')
    || new Set(value.questions.map(question => question.id)).size !== value.questions.length) {
    throw new Error('Invalid server response');
  }
  return value as unknown as ChallengeState;
}

export function parseAnswerResponse(value: unknown): AnswerResponse {
  if (!isRecord(value) || typeof value.correct !== 'boolean'
    || (value.alreadySolved !== undefined && typeof value.alreadySolved !== 'boolean')) {
    throw new Error('Invalid server response');
  }
  parseChallengeState(value.state);
  return value as unknown as AnswerResponse;
}
