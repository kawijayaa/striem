import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseAnswerResponse, parseChallengeState, parseQueryResult } from '../src/features/responses.ts';

test('query responses validate result shape and counts before rendering', () => {
  const valid = { columns: ['value'], rows: [{ value: { nested: [false, null, 0] } }], rowCount: 1, durationMs: 0.5 };
  assert.deepEqual(parseQueryResult(valid), valid);
  assert.deepEqual(parseQueryResult({ columns: [], rows: [], rowCount: 0, durationMs: 0 }).rows, []);
  for (const value of [null, {}, { ...valid, columns: [1] }, { ...valid, rows: [null] }, { ...valid, rows: [[]] }, { ...valid, rowCount: 2 }, { ...valid, rowCount: -1 }, { ...valid, durationMs: Infinity }, { ...valid, durationMs: '1' }]) {
    assert.throws(() => parseQueryResult(value), /Invalid server response/);
  }
  const rendered = { ...valid, visualization: { type: 'timechart', properties: { title: 'Events', series: ['host'] } } };
  assert.deepEqual(parseQueryResult(rendered), rendered);
  for (const visualization of [null, 'timechart', {}, { type: 1 }, { type: 'table', properties: [] }]) {
    assert.throws(() => parseQueryResult({ ...valid, visualization }), /Invalid server response/);
  }
});

const emptyState = { questions: [], solvedQuestions: 0, totalQuestions: 0, completed: false };
const question = { id: 'host', title: 'Host', prompt: 'Find host', solved: false };
const state = { questions: [question], solvedQuestions: 0, totalQuestions: 1, completed: false };

test('task response validation preserves empty and completed challenges and rejects inconsistent state', () => {
  assert.deepEqual(parseChallengeState(emptyState), emptyState);
  assert.deepEqual(parseChallengeState(state), state);
  const completed = { ...state, questions: [{ ...question, solved: true, answer: 'alpha' }], solvedQuestions: 1, completed: true, flag: 'flag' };
  assert.deepEqual(parseChallengeState(completed), completed);
  for (const value of [null, {}, { ...state, questions: [null] }, { ...state, questions: [{ ...question, title: 1 }] }, { ...state, totalQuestions: 2 }, { ...state, solvedQuestions: 1 }, { ...state, completed: true }, { ...completed, flag: 1 }, { ...state, questions: [question, question], totalQuestions: 2 }]) {
    assert.throws(() => parseChallengeState(value), /Invalid server response/);
  }
});

test('answer validation rejects malformed results and task states before accepting correctness', () => {
  const result = { correct: false, alreadySolved: false, state };
  assert.deepEqual(parseAnswerResponse(result), result);
  for (const value of [null, {}, { ...result, correct: 'true' }, { ...result, alreadySolved: 1 }, { ...result, state: {} }]) {
    assert.throws(() => parseAnswerResponse(value), /Invalid server response/);
  }
});
