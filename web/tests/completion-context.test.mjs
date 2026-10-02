import assert from 'node:assert/strict';
import { test } from 'node:test';
import { queryCompletionContext } from '../src/features/completion-context.ts';

test('completion stays out of comments, escaped, verbatim and multiline strings', () => {
  for (const query of [
    'Events // ho', 'Events | where host == "ho', "Events | where host == 'ho",
    'Events | where host == "escaped\\" ho', 'Events | where host == @"doubled"" ho',
    'Events | where host == h@"ho', 'Events | where host == ```line\nho',
    'Events | where host == ~~~line\nho',
  ]) assert.equal(queryCompletionContext(query, query.length), null, query);
  for (const query of ['Events // comment\n| project ho', 'Events | where host == "a|b" | project ho',
    'Events | where host == ```a|b``` | project ho', 'Events | where host == @"a""b" | project ho']) {
    const context = queryCompletionContext(query, query.length);
    assert.equal(context.kind, 'expression', query);
    assert.equal(context.prefix, 'ho', query);
  }
});

test('completion replacement preserves arithmetic, pipeline operators and surrounding identifiers', () => {
  for (const query of ['Events | extend value=score-ho', 'Events | project details.ac', 'Events | order by ho']) {
    const context = queryCompletionContext(query, query.length);
    assert.equal(query.slice(context.from, context.to), query.endsWith('details.ac') ? 'details.ac' : 'ho');
  }
  const query = 'Events | extend value=tostr(score)';
  const context = queryCompletionContext(query, query.indexOf('tostr') + 2);
  assert.equal(query.slice(context.from, context.to), 'tostr');
  const phrase = 'Events | order b';
  const order = queryCompletionContext(phrase, phrase.length);
  assert.equal(order.kind, 'operator');
  assert.equal(phrase.slice(order.from, order.to), 'order b');
});

test('completion detects table, operator and expression positions without crossing lines', () => {
  for (const [query, kind] of [
    ['Tel', 'source'], ['Events | pro', 'operator'], ['Events | project-aw', 'operator'],
    ['Events | where ', 'expression'], ['Events | order by ', 'sort'],
    ['Events | join Tel', 'table'], ['let X = Events;\nTel', 'source'],
    ['Events | join kind=inner (Tel', 'table'], ['(Tel', 'table'],
    ['Events | order\nho', 'expression'],
  ]) assert.equal(queryCompletionContext(query, query.length).kind, kind, query);
});

test('comparison operators complete after operands and preserve adjacent values', () => {
  for (const [query, prefix] of [
    ['Events | where host ', ''], ['Events | where host ha', 'ha'],
    ['Events | where host !ha', '!ha'], ['Events | where host con', 'con'],
    ['Events | where host =', '='], ['Events | where host !=', '!='],
    ['Events | where host == "alpha" a', 'a'], ['Events | where score > 1 a', 'a'],
    ['Events | where tostring(host) con', 'con'],
  ]) {
    const context = queryCompletionContext(query, query.length);
    assert.equal(context.kind, 'comparison', query);
    assert.equal(context.prefix, prefix, query);
  }
  for (const query of ['Events | where host contains ', 'Events | where host == ', 'Events | where ']) {
    assert.equal(queryCompletionContext(query, query.length).kind, 'expression', query);
  }
  const query = 'Events | where host=="alpha"';
  const position = query.indexOf('==') + 1;
  const comparison = queryCompletionContext(query, position);
  assert.equal(query.slice(comparison.from, comparison.to), '==');
  const field = queryCompletionContext(query, query.indexOf('host') + 2);
  assert.equal(query.slice(field.from, field.to), 'host');
});

test('column lists and numeric limits do not suggest comparisons', () => {
  for (const [query, kind] of [
    ['Events | project host ', 'expression'],
    ['Events | project host, user ', 'expression'],
    ['Events | project-away host, ', 'column'],
    ['Events | project-keep ho', 'column'],
    ['Events | project-reorder ho', 'column'],
    ['Events | distinct ho', 'column'],
    ['Events | summarize count() by ho', 'column'],
    ['Events | take 10 ', 'expression'],
    ['Events | limit 10 ', 'expression'],
    ['Events | extend matched=host ha', 'comparison'],
    ['Events | PROJECT matched=host ha', 'comparison'],
  ]) assert.equal(queryCompletionContext(query, query.length).kind, kind, query);
});

test('multiword regex completion replaces the complete phrase, including its suffix', () => {
  for (const suffix of ['', 'r', 're', 'reg', 'rege', 'regex']) {
    const query = `Events | where host matches ${suffix}`;
    const context = queryCompletionContext(query, query.length);
    assert.equal(context.kind, 'comparison');
    assert.equal(context.prefix, `matches ${suffix}`);
    assert.equal(query.slice(context.from, context.to), `matches ${suffix}`);
  }
  const query = 'Events | where host matches regex "alpha"';
  const context = queryCompletionContext(query, query.indexOf('regex') + 2);
  assert.equal(query.slice(context.from, context.to), 'matches regex');
});

test('closing a nested query restores the outer completion context', () => {
  for (const [query, kind, prefix] of [
    ['Events | where host in (Events | project host) a', 'comparison', 'a'],
    ['Events | where host in (Events | project host) and ', 'expression', ''],
    ['Events | join (Other | project user) on ho', 'expression', 'ho'],
    ['Events | where host in (Events | project ho', 'expression', 'ho'],
    ['Events | where (host in (Events | project host)) a', 'comparison', 'a'],
  ]) {
    const context = queryCompletionContext(query, query.length);
    assert.equal(context.kind, kind, query);
    assert.equal(context.prefix, prefix, query);
  }
});
