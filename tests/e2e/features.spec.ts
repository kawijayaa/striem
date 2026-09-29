import { test, expect, type Page } from '@playwright/test';

async function open(page: Page, query = 'Telemetry | project TimeGenerated, host, score, user, details | order by score desc', base = '') {
  await page.goto(`${base}/?q=${encodeURIComponent(query)}`);
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  await expect(page.locator('#source-list button')).toHaveCount(5);
}
async function edit(page: Page, query: string) {
  const editor = page.locator('.cm-content');
  await editor.click();
  await editor.press('ControlOrMeta+a');
  await editor.fill(query);
}
async function run(page: Page, count: number, mobile = false) {
  await page.locator(mobile ? '#mobile-run-query' : '#run-query').click();
  await expect(page.locator('#query-stats')).toHaveText(new RegExp(`^${count} rows? ·`));
  await expect(page.locator('#query-error')).toBeHidden();
}

test('desktop results: sorting, resizing, JSON search, safe rendering, keyboard navigation and pivots', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await open(page);
  await run(page, 3);
  await expect(page.locator('#result-body tr')).toHaveCount(3);
  await expect(page.locator('#result-timeline')).toBeVisible();
  await page.locator('.column-sort[data-column="score"]').click();
  await expect(page.locator('#result-body tr').first()).toContainText('beta');
  const resize = page.getByRole('separator', { name: 'Resize score column' });
  const before = Number(await resize.getAttribute('aria-valuenow'));
  await resize.press('ArrowRight');
  expect(Number(await resize.getAttribute('aria-valuenow'))).toBeGreaterThan(before);
  await page.getByRole('button', { name: 'Open details JSON', exact: true }).last().click();
  await expect(page.locator('#raw-dialog')).toBeVisible();
  await page.locator('#copy-raw').click();
  expect(JSON.parse(await page.evaluate(() => navigator.clipboard.readText())).action).toBe('login');
  await page.locator('#raw-search').fill('login');
  await expect(page.locator('#raw-search-status')).toContainText('match');
  await page.locator('#close-dialog').click();
  await page.getByRole('button', { name: 'View event details' }).nth(1).click();
  await expect(page.locator('#raw-json')).toContainText('<script>window.injected=true</script>');
  expect(await page.evaluate(() => (window as any).injected)).toBeUndefined();
  await page.keyboard.press('Escape');
  const cell = page.locator('#result-body tr').first().locator('[data-result-column-index="1"]');
  await cell.focus();
  await cell.press('ArrowDown');
  await expect(page.locator('#result-body tr').nth(1).locator('[data-result-column-index="1"]')).toBeFocused();
  await cell.click();
  await page.locator('#copy-result-value').click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('beta');
  await page.locator('#exclude-result-value').click();
  await run(page, 2);
  await page.locator('#toast-action').click();
  await run(page, 3);
  await page.locator('#result-body tr').last().locator('[data-result-column-index="1"]').click();
  await page.locator('#filter-result-value').click();
  await expect(page.locator('.cm-content')).toContainText('host == "beta"');
  await run(page, 1);
  await page.locator('#toast-action').click();
  await expect(page.locator('.cm-content')).not.toContainText('host == "beta"');
  expect(errors).toEqual([]);
});

test('filters on calculated result columns preserve the producing pipeline', async ({ page }) => {
  await open(page, 'Telemetry | summarize Total=count() by user | order by Total desc');
  await run(page, 3);
  await page.locator('#result-body tr').first().locator('[data-result-column-index="1"]').click();
  await page.locator('#filter-result-value').click();
  await run(page, 3);
});

test('query diagnostics, empty results, keyboard shortcuts, Vim toggle and cancellation', async ({ page }) => {
  await open(page);
  await edit(page, 'Telemetry | where unknown_field ==');
  await expect(page.locator('.cm-lintRange-error')).not.toHaveCount(0);
  await page.locator('#run-query').click();
  await expect(page.locator('#query-error')).toBeVisible();
  await edit(page, 'Telemetry | where score > 100');
  await page.locator('.cm-content').press('Shift+Enter');
  await expect(page.locator('#query-stats')).toContainText('0 rows');
  await expect(page.locator('#empty-results-title')).toHaveText('No events matched this query');
  await page.locator('.cm-content').press('Control+Enter');
  await expect(page.locator('.cm-content')).toContainText('|');
  await page.locator('#vim-toggle').click();
  await expect(page.locator('#vim-toggle')).toHaveAttribute('aria-pressed', 'true');
  await page.locator('#vim-toggle').click();
  await page.route('**/api/query', async route => { await new Promise(resolve => setTimeout(resolve, 700)); await route.continue().catch(() => {}); });
  await page.locator('#run-query').click();
  await expect(page.locator('#run-label')).toHaveText('Cancel');
  await page.locator('#run-query').click();
  await expect(page.locator('#query-stats')).toHaveText('Query canceled');
});

test('saved hunts persist, reopen, remove and undo; history clears; query links round-trip', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await open(page, 'Telemetry | count');
  await run(page, 1);
  await page.locator('#save-query').click();
  await page.locator('#save-query-name').fill('Browser hunt');
  await page.locator('#save-query-form button[type="submit"]').click();
  await page.locator('#hunts-tab').click();
  await expect(page.locator('#saved-query-list')).toContainText('Browser hunt');
  await page.reload();
  await page.locator('#hunts-tab').click();
  await page.locator('#saved-query-list .compact-main').click();
  await expect(page.locator('.cm-content')).toHaveText('Telemetry | count');
  await page.locator('#saved-query-list .compact-action').click();
  await expect(page.locator('#saved-count')).toHaveText('0');
  await page.locator('#toast-action').click();
  await expect(page.locator('#saved-count')).toHaveText('1');
  await page.locator('#history-view').click();
  await expect(page.locator('#query-history')).toContainText('Telemetry | count');
  await page.locator('#clear-history').click();
  await page.locator('#confirm-form button[type="submit"]').click();
  await expect(page.locator('#history-count')).toHaveText('0');
  await page.locator('#share-query').click();
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(new URL(copied).searchParams.get('q')).toBe('Telemetry | count');
  await page.goto(copied);
  await expect(page.locator('.cm-content')).toHaveText('Telemetry | count');
});

test('source selection, field filtering/insertion, autocomplete and timeline filtering', async ({ page }) => {
  await open(page);
  await page.locator('#source-list button').filter({ hasText: 'CSV' }).click();
  await expect(page.locator('.cm-content')).toContainText('CSV');
  await page.locator('#field-search').fill('identifier');
  await expect(page.locator('#field-list')).toContainText('identifier');
  await edit(page, 'CSV | project ');
  await page.locator('#field-list button').filter({ hasText: 'identifier' }).first().click();
  await expect(page.locator('.cm-content')).toContainText('identifier');
  await run(page, 1);
  await expect(page.locator('#result-body')).toContainText('00123');
  await edit(page, 'Telemetry | project ho');
  await page.locator('.cm-content').press('Control+Space');
  await expect(page.locator('.cm-tooltip-autocomplete')).toContainText('host');
  await page.keyboard.press('Escape');
  await edit(page, 'Telemetry | project TimeGenerated, host');
  await run(page, 3);
  await page.locator('#timeline-bars button').first().click();
  await expect(page.locator('.cm-content')).toContainText('TimeGenerated >= datetime(');
  await run(page, 2);
});

test('mobile navigation, result cards, JSON viewer and value pivots', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await open(page, 'Telemetry | project host, score, TimeGenerated');
  await expect(page.locator('#query-panel')).toBeVisible();
  await expect(page.locator('#results-panel')).toBeHidden();
  await run(page, 3, true);
  await expect(page.locator('#results-panel')).toBeVisible();
  await expect(page.locator('.mobile-result-card')).toHaveCount(3);
  await page.locator('.mobile-result-card-main').first().click();
  await expect(page.locator('#raw-json')).toContainText('alpha');
  await page.locator('#close-dialog').click();
  await page.locator('.mobile-pivot-action').first().click();
  await expect(page.locator('#query-panel')).toBeVisible();
  await expect(page.locator('.cm-content')).toContainText('host == "alpha"');
  await page.locator('#mobile-investigate-tab').click();
  await expect(page.locator('#investigate-panel')).toBeVisible();
  await page.locator('#mobile-investigate-tab').press('Home');
  await expect(page.locator('#mobile-query-tab')).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('workspace without questions imports all formats and hides challenge controls', async ({ page, request }) => {
  await open(page, 'Events | summarize n=count() by Source', 'http://127.0.0.1:18082');
  await expect(page.locator('#active-task-bar')).toBeHidden();
  await expect(page.locator('#questions-tab')).toBeHidden();
  await run(page, 4);
  for (const [source, count] of [['browser', 3], ['gzip', 3], ['csv', 1], ['windows', 1]]) {
    const result = await request.post('http://127.0.0.1:18082/api/query', { headers: { 'X-Striem-Request': '1' }, data: { query: `Events | where Source == "${source}" | count` } });
    expect(result.ok()).toBe(true);
    expect((await result.json()).rows[0].Count).toBe(count);
  }
});

test('loading and startup failure screens', async ({ page }) => {
  let ready = false;
  await page.route('**/api/ready', route => ready ? route.continue() : route.fulfill({ status: 503, json: { status: 'loading', challengeName: 'Loading fixture' } }));
  await page.goto('/');
  await expect(page.locator('#ingestion-screen')).toBeVisible();
  await expect(page.locator('#ingestion-challenge-name')).toHaveText('Loading fixture');
  ready = true;
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  await page.unroute('**/api/ready');
  await page.route('**/api/ready', route => route.fulfill({ status: 503, json: { status: 'error', error: 'Fixture import failed' } }));
  await page.reload();
  await expect(page.locator('#ingestion-title')).toHaveText('Ingestion could not complete');
  await expect(page.locator('#ingestion-detail')).toHaveText('Fixture import failed');
});

test('answer drafts persist, wrong answers recover, completion unlocks flag and persists', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await open(page);
  await page.locator('#active-task-answer').fill('wrong draft');
  await page.reload();
  await expect(page.locator('#active-task-answer')).toHaveValue('wrong draft');
  await page.locator('#active-task-submit').click();
  await expect(page.locator('#active-task-feedback')).toContainText('does not match');
  await page.locator('#active-task-answer').fill('alpha');
  await page.waitForTimeout(250);
  await page.locator('#active-task-submit').click();
  await expect(page.locator('#active-task-title')).toHaveText('Identify the user');
  await page.locator('#active-task-answer').fill('alice');
  await page.locator('#active-task-submit').click();
  await expect(page.locator('#active-task-title')).toHaveText('Challenge complete');
  await expect(page.locator('#active-task-answer-value')).toHaveValue('flag{browser_features_pass}');
  await page.locator('#active-task-next').click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('flag{browser_features_pass}');
  await page.reload();
  await expect(page.locator('#active-task-title')).toHaveText('Challenge complete');
  await page.locator('#questions-tab').click();
  await expect(page.locator('#question-list')).toContainText('alpha');
  await expect(page.locator('#question-list')).toContainText('alice');
});
