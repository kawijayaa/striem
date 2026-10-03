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

test('long queries keep results accessible across desktop and constrained mobile layouts', async ({ page }) => {
  const query = `Telemetry\n${Array.from({ length: 60 }, (_, index) => `// Investigation note ${index}`).join('\n')}\n| take 3`;
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const viewport of [
    { width: 1440, height: 900 }, { width: 1280, height: 720 },
    { width: 900, height: 700 }, { width: 390, height: 844 },
    { width: 320, height: 640 }, { width: 844, height: 390 },
  ]) {
    await page.setViewportSize(viewport);
    await open(page, query);
    const scroller = page.locator('.cm-scroller');
    const dimensions = await scroller.evaluate(element => ({ height: element.clientHeight, content: element.scrollHeight }));
    expect(dimensions.height).toBeLessThanOrEqual(360);
    expect(dimensions.content).toBeGreaterThan(dimensions.height);
    await scroller.evaluate(element => { element.scrollTop = element.scrollHeight; });
    await expect(page.locator('.cm-line').last()).toContainText('| take 3');
    const mobile = viewport.width <= 900;
    await run(page, 3, mobile);
    await expect(page.locator('#results-panel')).toBeVisible();
    if (!mobile) {
      const results = (await page.locator('#results-panel').boundingBox())!;
      expect(results.height).toBeGreaterThan(160);
      expect(results.y).toBeLessThan(viewport.height - 100);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(viewport.width);
    await page.locator('.skip-link').focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('.cm-content')).toBeFocused();
    await expect(page.locator('#query-panel')).toBeVisible();
  }
});

test('JSON dialogs keep search, copy, close and scrollable content within short and narrow screens', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const viewport of [{ width: 320, height: 640 }, { width: 844, height: 390 }]) {
    await page.setViewportSize(viewport);
    await open(page, 'Telemetry | project host,details');
    await run(page, 3, true);
    const details = viewport.width <= 600
      ? page.locator('.mobile-result-card-main').first()
      : page.getByRole('button', { name: 'View event details for row 1' });
    await details.click();
    await page.locator('#raw-search').fill('nested');
    await expect(page.locator('#raw-search-status')).toContainText('1 match');
    for (const selector of ['#raw-dialog', '#raw-search', '#copy-raw', '#close-dialog', '#raw-json']) {
      const box = (await page.locator(selector).boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.y).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
      expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);
      expect(box.height).toBeGreaterThan(0);
    }
    await page.locator('#close-dialog').click();
    await expect(page.locator('#raw-dialog')).toBeHidden();
    await expect(details).toBeFocused();
  }
});

test('long task titles and evidence strings remain readable in the task list', async ({ page }) => {
  const title = `Investigate${'LongTitle'.repeat(20)}`;
  const prompt = `Find the event containing ${'abcdef0123456789'.repeat(24)} and identify its host.`;
  await page.route('**/api/questions', route => route.fulfill({ json: {
    questions: [{ id: 'long', title, prompt, solved: false }],
    totalQuestions: 1, solvedQuestions: 0, completed: false,
  } }));
  for (const width of [1440, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await open(page);
    await page.locator('#show-all-tasks').click();
    const task = page.locator('.question-select');
    await expect(task).toContainText(title);
    await expect(task).toContainText(prompt);
    for (const selector of ['.question-card-head', '.question-prompt']) {
      expect(await page.locator(selector).evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
});

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

test('column resizing stops after interrupted drags and supports Escape cancellation', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await open(page, 'Telemetry | project host,score');
  await run(page, 3);
  const resizer = page.getByRole('separator', { name: 'Resize host column' });
  await resizer.evaluate(element => element.addEventListener('pointerdown', event => {
    element.setAttribute('data-test-pointer', String((event as PointerEvent).pointerId));
  }));
  const width = async () => Number(await resizer.getAttribute('aria-valuenow'));
  const startDrag = async () => {
    const box = (await resizer.boundingBox())!;
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    await page.mouse.move(x, y);
    expect(await resizer.evaluate((_element, point) => {
      const hit = document.elementFromPoint(point.x, point.y);
      return { tag: hit?.tagName, className: hit?.className };
    }, { x, y })).toEqual({ tag: 'SPAN', className: 'column-resizer' });
    await page.mouse.down();
    expect(await resizer.getAttribute('data-test-pointer')).not.toBeNull();
    return { x, y, pointerId: Number(await resizer.getAttribute('data-test-pointer')) };
  };
  for (const interruption of ['pointercancel', 'lostpointercapture']) {
    const before = await width();
    const drag = await startDrag();
    await page.mouse.move(drag.x + 40, drag.y);
    expect(await width()).toBeGreaterThan(before);
    const resized = await width();
    await resizer.dispatchEvent('pointermove', { pointerId: 999, clientX: drag.x + 200 });
    expect(await width()).toBe(resized);
    await resizer.dispatchEvent(interruption, { pointerId: drag.pointerId });
    await resizer.dispatchEvent('pointermove', { pointerId: drag.pointerId, clientX: drag.x + 200 });
    expect(await width()).toBe(resized);
    await page.mouse.up();
  }
  const beforeCancel = await width();
  const drag = await startDrag();
  await page.mouse.move(drag.x + 60, drag.y);
  await resizer.focus();
  await page.keyboard.press('Escape');
  expect(Math.abs((await width()) - beforeCancel)).toBeLessThanOrEqual(1);
  await resizer.dispatchEvent('pointermove', { pointerId: drag.pointerId, clientX: drag.x + 200 });
  expect(Math.abs((await width()) - beforeCancel)).toBeLessThanOrEqual(1);
  await page.mouse.up();
  for (const input of [{ isPrimary: false, button: 0 }, { isPrimary: true, button: 2 }]) {
    await resizer.dispatchEvent('pointerdown', { ...input, pointerId: 999, clientX: drag.x });
    await resizer.dispatchEvent('pointermove', { pointerId: 999, clientX: drag.x + 200 });
    expect(Math.abs((await width()) - beforeCancel)).toBeLessThanOrEqual(1);
  }
  await resizer.press('ArrowRight');
  expect(await width()).toBeGreaterThan(beforeCancel);
  expect(errors).toEqual([]);
});

test('result keyboard navigation crosses JSON columns and preserves dialog focus', async ({ page }) => {
  await open(page, 'Telemetry | project host,details=iff(score==2,dynamic(null),details),score | order by score asc');
  await run(page, 3);
  const rows = page.locator('#result-body tr');
  const host = rows.first().locator('[data-result-column-index="0"]');
  const json = rows.first().getByRole('button', { name: 'Open details JSON' });
  await host.focus();
  await host.press('ArrowRight');
  await expect(json).toBeFocused();
  await expect(page.locator('#result-value-actions')).toBeHidden();
  await json.press('Enter');
  await expect(page.locator('#raw-dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(json).toBeFocused();
  await json.press('ArrowRight');
  const score = rows.first().locator('[data-result-column-index="2"]');
  await expect(score).toBeFocused();
  await score.press('ArrowLeft');
  await expect(json).toBeFocused();
  await json.press('ArrowDown');
  const empty = rows.nth(1).locator('[data-result-column-index="1"]');
  await expect(empty).toBeFocused();
  await expect(page.locator('#result-value-actions')).toBeVisible();
  await empty.press('End');
  await expect(rows.last().getByRole('button', { name: 'Open details JSON' })).toBeFocused();
  await page.keyboard.press('Home');
  await expect(json).toBeFocused();
  expect(await page.locator('#result-body [tabindex="0"]').count()).toBe(2); // One grid cell and row-details action.
  await rows.last().locator('[data-result-column-index="2"]').click();
  await expect(rows.last().locator('[data-result-column-index="2"]')).toHaveAttribute('tabindex', '0');
  await expect(json).toHaveAttribute('tabindex', '-1');
  await json.click();
  await expect(page.locator('#raw-dialog')).toBeVisible();
  await expect(page.locator('#raw-json')).toContainText('logout');
});

test('row details are keyboard accessible across all result rows', async ({ page }) => {
  await open(page, 'Telemetry | project host,score | order by score asc');
  await run(page, 3);
  const rows = page.locator('#result-body tr');
  const actions = rows.locator('.result-row-action');
  await actions.first().focus();
  await page.keyboard.press('ArrowDown');
  await expect(actions.nth(1)).toBeFocused();
  await expect(rows.nth(1)).toHaveClass('selected');
  await expect(actions.first()).toHaveAttribute('tabindex', '-1');
  await expect(actions.nth(1)).toHaveAttribute('tabindex', '0');
  await page.keyboard.press('Enter');
  await expect(page.locator('#raw-dialog')).toBeVisible();
  await expect(page.locator('#raw-json')).toContainText('"host": "gamma"');
  await page.keyboard.press('Escape');
  await expect(actions.nth(1)).toBeFocused();
  await page.keyboard.press('ArrowRight');
  await expect(rows.nth(1).locator('[data-result-column-index="0"]')).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await expect(actions.nth(1)).toBeFocused();
  await expect(page.locator('#result-value-actions')).toBeHidden();
  await page.keyboard.press('End');
  await expect(actions.last()).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(actions.last()).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(actions.nth(1)).toBeFocused();
  await page.keyboard.press('Home');
  await expect(actions.first()).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(actions.first()).toBeFocused();
  await rows.first().locator('[data-result-column-index="0"]').click();
  await actions.nth(1).click();
  await expect(page.locator('#raw-dialog')).toBeVisible();
  await expect(page.locator('#raw-json')).toContainText('"host": "gamma"');
});

test('copy result values retains numeric text under a non-English locale', async ({ browser }) => {
  const context = await browser.newContext({ locale: 'de-DE', permissions: ['clipboard-read', 'clipboard-write'] });
  const page = await context.newPage();
  try {
    await open(page, 'print count=12345,amount=1234.5,enabled=false,missing=long(null),text="a,b"', 'http://127.0.0.1:18081');
    await run(page, 1);
    for (const [column, expected] of ['12345', '1234.5', 'false', 'null', 'a,b'].entries()) {
      await page.locator(`#result-body [data-result-column-index="${column}"]`).click();
      await page.locator('#copy-result-value').click();
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(expected);
    }
  } finally {
    await context.close();
  }
});

test('JSON viewer searches full phrases and Unicode without dismissing keyboard actions', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await open(page, 'print details=dynamic({"action":"login","message":"İprefixTarget [a.b]+?"})');
  await run(page, 1);
  await page.getByRole('button', { name: 'Open details JSON', exact: true }).click();
  const json = page.locator('#raw-json');
  const original = await json.textContent();
  await page.locator('#raw-search').fill('"action": "login"');
  await expect(page.locator('#raw-search-status')).toHaveText('1 match');
  expect(await json.locator('mark').allTextContents()).toEqual(['"action":', ' ', '"login"']);
  await page.locator('#raw-search').fill('target');
  await expect(page.locator('#raw-search-status')).toHaveText('1 match');
  await expect(json.locator('mark')).toHaveText('Target');
  await page.locator('#raw-search').fill('[a.b]+?');
  await expect(json.locator('mark')).toHaveText('[a.b]+?');
  await page.locator('#raw-search').fill('not present');
  await expect(page.locator('#raw-search-status')).toHaveText('0 matches');
  await expect(json).toHaveText(original!);
  await page.locator('#copy-raw').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#toast-message')).toHaveText('JSON copied');
  await expect(page.locator('#raw-dialog')).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(original);
  await page.mouse.click(1, 1);
  await expect(page.locator('#raw-dialog')).toBeHidden();
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

test('results distinguish edited queries, failed runs, cancellation and successful refresh', async ({ page }) => {
  const original = 'Telemetry | project host,score | order by score asc';
  const changed = 'Telemetry | where score>1 | project host,score | order by score asc';
  await open(page, original);
  await run(page, 3);
  const notice = page.locator('#results-notice');
  await expect(notice).toBeHidden();
  await edit(page, changed);
  await expect(notice).toHaveText('Query changed. Run it to update these results.');
  await expect(page.locator('#result-body tr')).toHaveCount(3);
  await edit(page, original);
  await expect(notice).toBeHidden();
  await edit(page, changed);
  await page.route('**/api/query', route => route.fulfill({
    status: 400, contentType: 'application/json', body: JSON.stringify({ error: 'Temporary query failure' }),
  }));
  await page.locator('#run-query').click();
  await expect(page.locator('#query-stats')).toHaveText('Query failed');
  await expect(notice).toHaveText('Query failed. Showing results from the last successful run.');
  await expect(page.locator('#result-body tr')).toHaveCount(3);
  await page.unroute('**/api/query');
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/query', async route => {
    await held;
    await route.continue().catch(() => {});
  });
  try {
    await page.locator('#run-query').click();
    await expect(notice).toHaveText('Query running. Showing results from the last successful run.');
    await page.locator('#run-query').click();
    await expect(page.locator('#query-stats')).toHaveText('Query canceled');
    await expect(notice).toHaveText('Query canceled. Showing results from the last successful run.');
    await expect(page.locator('#result-body tr')).toHaveCount(3);
  } finally {
    release();
    await page.unroute('**/api/query');
  }
  await run(page, 2);
  await expect(notice).toBeHidden();
  await expect(page.locator('#result-body tr')).toHaveCount(2);
});

test('malformed query responses preserve previous results and recover on the next run', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await open(page, 'Telemetry | project host,score');
  await run(page, 3);
  await page.route('**/api/query', route => route.fulfill({
    json: { columns: ['host'], rows: null, rowCount: 0, durationMs: 0 },
  }));
  await page.locator('#run-query').click();
  await expect(page.locator('#query-error')).toHaveText('The query service did not respond. Your query is unchanged. Run it again.');
  await expect(page.locator('#results-notice')).toHaveText('Query failed. Showing results from the last successful run.');
  await expect(page.locator('#result-body tr')).toHaveCount(3);
  await expect(page.locator('#result-head')).toContainText('score');
  await page.unroute('**/api/query');
  await run(page, 3);
  await expect(page.locator('#results-notice')).toBeHidden();
  expect(errors).toEqual([]);
});

test('mobile editing cancels a run and labels the retained results', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await open(page, 'Telemetry | project host,score');
  await run(page, 3, true);
  await page.locator('#mobile-query-tab').click();
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/query', async route => {
    await held;
    await route.continue().catch(() => {});
  });
  try {
    await page.locator('#mobile-run-query').click();
    await expect(page.locator('#mobile-run-label')).toHaveText('Cancel');
    await edit(page, 'Telemetry | where score>2 | project host,score');
    await expect(page.locator('#query-stats')).toHaveText('Query canceled');
    await page.locator('#mobile-results-tab').click();
    await expect(page.locator('#results-notice')).toBeVisible();
    await expect(page.locator('#results-notice')).toHaveText('Query canceled. Showing results from the last successful run.');
    await expect(page.locator('.mobile-result-card')).toHaveCount(3);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  } finally {
    release();
    await page.unroute('**/api/query');
  }
  await page.locator('#mobile-query-tab').click();
  await run(page, 1, true);
  await expect(page.locator('#results-notice')).toBeHidden();
  await expect(page.locator('.mobile-result-card')).toHaveCount(1);
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

test('filter Undo preserves newer editor changes', async ({ page }) => {
  const original = 'Telemetry | project host,score | order by score asc';
  await open(page, original);
  await run(page, 3);
  await page.locator('#result-body tr').first().locator('[data-result-column-index="0"]').click();
  await page.locator('#filter-result-value').click();
  const filtered = await page.locator('.cm-content').innerText();
  const newer = `${filtered}\n| take 1`;
  await edit(page, newer);
  await page.locator('#toast-action').click();
  await expect(page.locator('.cm-content')).toHaveText(newer, { useInnerText: true });
  await expect(page.locator('#toast-message')).toHaveText('Your newer edits were kept. Use Undo in the editor to step back.');
  await expect(page.locator('.cm-content')).toBeFocused();
  await run(page, 1);
});

test('Undo notifications remain available while focused or hovered', async ({ page }) => {
  await open(page, 'Telemetry | project host,score | order by score asc');
  await run(page, 3);
  await page.clock.install();
  await page.locator('#result-body tr').first().locator('[data-result-column-index="0"]').click();
  await page.locator('#filter-result-value').click();
  const action = page.locator('#toast-action');
  await action.focus();
  await page.clock.fastForward(10000);
  await expect(action).toBeFocused();
  await expect(action).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(page.locator('.cm-content')).not.toContainText('host == "beta"');
  await page.locator('#filter-result-value').click();
  await page.locator('#toast').hover();
  await page.clock.fastForward(10000);
  await expect(action).toBeVisible();
  await page.locator('.cm-content').hover();
  await page.clock.fastForward(6001);
  await expect(page.locator('#toast')).toBeHidden();
});

test('undo clearing history keeps newer runs and persists both histories', async ({ page }) => {
  const previous = 'Telemetry | count';
  const newer = 'Telemetry | project host';
  await open(page, previous);
  await run(page, 1);
  await page.locator('#hunts-tab').click();
  await page.locator('#history-view').click();
  await page.locator('#clear-history').click();
  await page.locator('#confirm-form button[type="submit"]').click();
  await expect(page.locator('#history-count')).toHaveText('0');
  await run(page, 1);
  const rerun = await page.evaluate(() => JSON.parse(localStorage.getItem('striem.queryHistory')!)[0]);
  await edit(page, newer);
  await run(page, 3);
  await page.locator('#toast-action').click();
  await expect(page.locator('#history-count')).toHaveText('2');
  await expect(page.locator('#query-history .query-preview')).toHaveText([newer, previous]);
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('striem.queryHistory')!)[1])).toEqual(rerun);
  await page.reload();
  await page.locator('#hunts-tab').click();
  await page.locator('#history-view').click();
  await expect(page.locator('#query-history .query-preview')).toHaveText([newer, previous]);
});

test('hunting autocomplete opens after a pipe and supports keyboard selection and dismissal', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  await edit(page, 'Telemetry ');
  await editor.press('End');
  await editor.pressSequentially('|');
  const popup = page.locator('.cm-tooltip-autocomplete');
  await expect(popup).toBeVisible();
  await expect(popup).toContainText('where');
  await editor.press('Escape');
  await expect(popup).toHaveCount(0);
  await editor.press('Control+Space');
  await expect(popup).toBeVisible();
  await editor.pressSequentially('whe', { delay: 80 });
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^where$/ })).toBeVisible();
  await page.waitForTimeout(100);
  await editor.press('Enter');
  await expect(editor).toHaveText('Telemetry |where ');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^host$/ })).toBeVisible();
  await editor.press('Escape');
  await expect(editor).toHaveText('Telemetry |where ');
});

test('hunting autocomplete keeps column lists separate from operators and functions', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  for (const query of ['Telemetry | project-away ', 'Telemetry | distinct ', 'Telemetry | summarize count() by ']) {
    await edit(page, query);
    await editor.press('End');
    await editor.press('Control+Space');
    await expect(page.locator('.cm-completionLabel').filter({ hasText: /^host$/ })).toBeVisible();
    await expect(page.locator('.cm-completionLabel').filter({ hasText: /^(?:has|contains|==|!=|tostring)$/ })).toHaveCount(0);
    await editor.press('Escape');
  }
  await edit(page, 'Telemetry | project host ');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^user$/ })).toBeVisible();
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^(?:has|contains|==|!=)$/ })).toHaveCount(0);
});

test('hunting autocomplete offers string and symbolic comparisons and accepts Tab', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  for (const [prefix, label, value, count] of [
    ['ha', 'has', '"alpha"', 1], ['con', 'contains', '"ph"', 1],
    ['=', '==', '"alpha"', 1], ['!', '!=', '"alpha"', 2],
    ['!ha', '!has', '"alpha"', 2], ['has_', 'has_cs', '"alpha"', 1],
    ['contains_', 'contains_cs', '"ph"', 1], ['in', 'in', '("alpha", "beta")', 2],
  ] as const) {
    await edit(page, `Telemetry | where host ${prefix}`);
    await editor.press('End');
    await editor.press('Control+Space');
    const escaped = label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    await page.locator('.cm-completionLabel').filter({ hasText: new RegExp(`^${escaped}$`) }).click();
    await editor.pressSequentially(value);
    await expect(editor).toHaveText(`Telemetry | where host ${label} ${value}`);
    await run(page, count);
  }
  await edit(page, 'Telemetry | where host ');
  await editor.press('End');
  await editor.press('Control+Space');
  for (const label of ['has', 'contains', '==', '!=']) {
    const escaped = label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    await expect(page.locator('.cm-completionLabel').filter({ hasText: new RegExp(`^${escaped}$`) })).toHaveCount(1);
  }
  await page.keyboard.press('Escape');
  await editor.pressSequentially('cont', { delay: 80 });
  await expect(page.locator('.cm-tooltip-autocomplete')).toBeVisible();
  await page.waitForTimeout(100);
  await editor.press('Tab');
  await editor.pressSequentially('"ph"');
  await expect(editor).toHaveText('Telemetry | where host contains "ph"');
  await run(page, 1);
  await edit(page, 'Telemetry | where host="alpha"');
  await editor.press('End');
  for (let index = 0; index < 7; index++) await editor.press('ArrowLeft');
  await editor.press('Control+Space');
  await page.locator('.cm-completionLabel').filter({ hasText: /^==$/ }).click();
  await expect(editor).toHaveText('Telemetry | where host== "alpha"');
  await run(page, 1);
});

test('autocomplete replaces only its token and inserts functions without duplicate parentheses', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  const complete = async (label: string) => {
    await editor.press('Control+Space');
    await page.locator('.cm-completionLabel').filter({ hasText: new RegExp(`^${label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`) }).click();
  };
  await edit(page, 'Tel');
  await editor.press('End');
  await complete('Telemetry');
  await expect(editor).toHaveText('Telemetry');
  await edit(page, 'Telemetry | extend value=score-ho');
  await editor.press('End');
  await complete('host');
  await expect(editor).toHaveText('Telemetry | extend value=score-host');
  await edit(page, 'Telemetry | project host');
  await editor.press('End');
  await editor.press('ArrowLeft');
  await editor.press('ArrowLeft');
  await complete('host');
  await expect(editor).toHaveText('Telemetry | project host');
  await edit(page, 'Telemetry | extend value=tostr(score)');
  await editor.press('End');
  for (let index = 0; index < 7; index++) await editor.press('ArrowLeft');
  await complete('tostring');
  await expect(editor).toHaveText('Telemetry | extend value=tostring(score)');
  await run(page, 3);
  await edit(page, 'Telemetry | project value=tostr');
  await editor.press('End');
  await complete('tostring');
  await editor.pressSequentially('host');
  await expect(editor).toHaveText('Telemetry | project value=tostring(host)');
  await run(page, 3);
  await edit(page, 'Telemetry | project details.ne');
  await editor.press('End');
  await complete('details.nested.ok');
  await expect(editor).toHaveText('Telemetry | project details.nested.ok');
  await run(page, 3);
  await edit(page, 'Telemetry | project-aw');
  await editor.press('End');
  await complete('project-away');
  await editor.pressSequentially('details');
  await expect(editor).toHaveText('Telemetry | project-away details');
  await run(page, 3);
  await edit(page, 'Telemetry | order b');
  await editor.press('End');
  await complete('order by');
  await editor.pressSequentially('score d');
  await complete('desc');
  await expect(editor).toHaveText('Telemetry | order by score desc');
  await run(page, 3);
});

test('hunting autocomplete completes regex phrases and restores context after nested queries', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  await edit(page, 'Telemetry | where host matches re');
  await editor.press('End');
  await editor.press('Control+Space');
  await page.locator('.cm-completionLabel').filter({ hasText: /^matches regex$/ }).click();
  await editor.pressSequentially('"^alpha$"');
  await expect(editor).toHaveText('Telemetry | where host matches regex "^alpha$"');
  await run(page, 1);
  await edit(page, 'Telemetry | where host in (Telemetry | project host) a');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^and$/ })).toBeVisible();
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^array_length$/ })).toHaveCount(0);
  await page.locator('.cm-completionLabel').filter({ hasText: /^and$/ }).click();
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^host$/ })).toBeVisible();
  await editor.press('Escape');
  await edit(page, 'Telemetry | where host c');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^contains$/ })).toBeVisible();
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^has_cs$/ })).toHaveCount(0);
});

test('autocomplete ignores strings and comments and scopes unique field suggestions to the current table', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  for (const prefix of ['Telemetry // ', 'Telemetry | where host == "', 'Telemetry | where host == @"',
    'Telemetry | where host == ```line\n']) {
    await edit(page, prefix);
    await editor.press('ControlOrMeta+End');
    await editor.pressSequentially('ho', { delay: 80 });
    await editor.press('Control+Space');
    await page.waitForTimeout(200); // Allow both automatic and explicit completion to resolve.
    await expect(page.locator('.cm-tooltip-autocomplete')).toHaveCount(0);
  }
  for (const source of ['Telemetry', 'CSV', 'Events']) {
    await edit(page, `${source} | project ho`);
    await editor.press('End');
    await editor.press('Control+Space');
    await expect(page.locator('.cm-completionLabel').filter({ hasText: /^host$/ })).toHaveCount(1);
    await page.keyboard.press('Escape');
  }
  await edit(page, 'Telemetry | project ident');
  await editor.press('End');
  await editor.press('Control+Space');
  await page.waitForTimeout(200);
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^identifier$/ })).toHaveCount(0);
  await edit(page, 'CSV | project ident');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^identifier$/ })).toHaveCount(1);
  await page.waitForTimeout(100); // CodeMirror guards keyboard acceptance for 75ms after opening.
  await page.keyboard.press('Enter');
  await expect(editor).toHaveText('CSV | project identifier');
  await run(page, 1);
  await edit(page, 'Telemetry | join (CSV | project ident');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^identifier$/ })).toHaveCount(1);
});

test('autocomplete descriptions and selection remain readable inside narrow viewports', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const viewport of [{ width: 1440, height: 900 }, { width: 320, height: 640 }, { width: 844, height: 390 }]) {
    await page.setViewportSize(viewport);
    await open(page, 'Telemetry | extend value=array');
    const editor = page.locator('.cm-content');
    await editor.click();
    await editor.press('End');
    await editor.press('Control+Space');
    const popup = page.locator('.cm-tooltip-autocomplete');
    await expect(popup).toBeVisible();
    const box = (await popup.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);
    const selected = popup.locator('[aria-selected="true"]');
    expect(await selected.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
    const colors = await selected.evaluate(element => ({
      background: getComputedStyle(element).backgroundColor,
      text: getComputedStyle(element).color,
    }));
    expect(colors).toEqual({ background: 'rgb(9, 71, 113)', text: 'rgb(243, 242, 241)' });
    await expect(page.locator('.completion-documentation')).toBeVisible();
    const info = (await page.locator('.cm-completionInfo').boundingBox())!;
    expect(info.x).toBeGreaterThanOrEqual(0);
    expect(info.x + info.width).toBeLessThanOrEqual(viewport.width);
    expect(info.y + info.height).toBeLessThanOrEqual(viewport.height);
    if (viewport.width <= 600) {
      const list = (await popup.locator('ul').boundingBox())!;
      expect(info.y).toBeGreaterThanOrEqual(list.y + list.height - 1);
    }
    await page.screenshot({ path: test.info().outputPath(`autocomplete-${viewport.width}x${viewport.height}.png`) });
    await page.waitForTimeout(100);
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');
    await expect(editor).toContainText('()');
    await page.keyboard.press('Escape');
    await expect(popup).toBeHidden();
  }
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

test('field catalog retries preserve search and malformed responses do not break the editor', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  let healthy = false;
  await page.route('**/api/fields', route => healthy ? route.continue() : route.fulfill({
    status: 200, json: { common: [{ path: 123, type: 'string' }], tables: [] },
  }));
  await page.goto('/?q=Telemetry');
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  const retry = page.locator('#field-list').getByRole('button', { name: 'Try again' });
  await expect(retry).toBeVisible();
  await page.locator('#field-search').fill('host');
  await expect(retry).toBeVisible();
  healthy = true;
  await retry.click();
  await expect(page.locator('#source-list button')).toHaveCount(5);
  await expect(page.locator('#field-list .field-path')).toHaveText(['host']);
  await expect(page.locator('#field-search')).toHaveValue('host');
  await run(page, 3);
  expect(errors).toEqual([]);
});

test('stalled field catalog loading offers a retry and keeps the query editor usable', async ({ page }) => {
  let release: () => void = () => {};
  const stalled = new Promise<void>(resolve => { release = resolve; });
  let first = true;
  await page.route('**/api/fields', async route => {
    if (first) {
      first = false;
      await stalled;
    }
    await route.continue().catch(() => {});
  });
  await page.clock.install();
  try {
    await page.goto('/?q=Telemetry');
    await expect(page.locator('#ingestion-screen')).toHaveCount(0);
    await expect(page.locator('#field-list')).toHaveAttribute('aria-busy', 'true');
    await page.locator('#field-search').fill('host');
    await expect(page.locator('#field-list')).toHaveText('Loading field catalog…');
    await page.clock.fastForward(10001);
    const retry = page.locator('#field-list').getByRole('button', { name: 'Try again' });
    await expect(retry).toBeVisible();
    await expect(page.locator('#field-list')).toHaveAttribute('aria-busy', 'false');
    await run(page, 3);
    release();
    await retry.click();
    await expect(page.locator('#source-list button')).toHaveCount(5);
    await expect(page.locator('#field-list .field-path')).toHaveText(['host']);
  } finally {
    release();
  }
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
  await page.unroute('**/api/ready');
  await page.getByRole('button', { name: 'Reload workspace' }).click();
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  await expect(page.locator('#source-list button')).toHaveCount(5);
});

test('startup rejects malformed readiness responses and reconnects automatically', async ({ page }) => {
  let reconnect = false;
  await page.route('**/api/ready', route => reconnect ? route.continue() : route.fulfill({
    status: 200, json: { challengeName: 123, status: 'unexpected' },
  }));
  await page.goto('/');
  await expect(page.locator('#ingestion-title')).toHaveText('Connecting to workspace');
  await expect(page.locator('#ingestion-status')).toHaveText('Reconnecting…');
  await expect(page.locator('.app-shell')).toHaveAttribute('inert', '');
  await expect(page.locator('#ingestion-challenge-name')).toBeHidden();
  reconnect = true;
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  await expect(page.locator('#source-list button')).toHaveCount(5);
});

test('a stalled readiness request times out and recovers', async ({ page }) => {
  await page.clock.install();
  let release: () => void = () => {};
  const stalled = new Promise<void>(resolve => { release = resolve; });
  let first = true;
  await page.route('**/api/ready', async route => {
    if (first) {
      first = false;
      await stalled;
    }
    await route.continue().catch(() => {});
  });
  try {
    await page.goto('/');
    await expect(page.locator('#ingestion-screen')).toBeVisible();
    await page.clock.fastForward(8001);
    await expect(page.locator('#ingestion-title')).toHaveText('Connecting to workspace');
    await expect(page.locator('.app-shell')).toHaveAttribute('inert', '');
    release();
    await page.clock.fastForward(1001);
    await expect(page.locator('#ingestion-screen')).toHaveCount(0);
    await expect(page.locator('#source-list button')).toHaveCount(5);
  } finally {
    release();
  }
});

test('malformed task responses leave a retry path and preserve drafts on submission', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  let healthy = false;
  await page.route('**/api/questions', route => healthy ? route.continue() : route.fulfill({
    json: { questions: null, totalQuestions: 0, solvedQuestions: 0, completed: false },
  }));
  await open(page);
  await expect(page.locator('#active-task-title')).toHaveText('Could not load investigation tasks');
  healthy = true;
  await page.locator('#active-task-next').click();
  await expect(page.locator('#active-task-title')).toHaveText('Identify the first host');
  await page.route('**/api/questions/host/answer', route => route.fulfill({ json: { correct: true, state: null } }));
  await page.locator('#active-task-answer').fill('kept draft');
  await page.locator('#active-task-submit').click();
  await expect(page.locator('#active-task-feedback')).toHaveText('The answer service did not respond. Your draft is kept; try again.');
  await expect(page.locator('#active-task-title')).toHaveText('Identify the first host');
  await expect(page.locator('#active-task-answer')).toHaveValue('kept draft');
  await expect(page.locator('#active-task-submit')).toBeEnabled();
  await page.unroute('**/api/questions/host/answer');
  await page.locator('#active-task-submit').click();
  await expect(page.locator('#active-task-feedback')).toContainText('does not match');
  await expect(page.locator('#active-task-answer')).toHaveValue('kept draft');
  expect(errors).toEqual([]);
});

test('pending answer responses preserve editor focus and answer drafts', async ({ page }) => {
  await open(page);
  await expect(page.locator('#active-task-title')).toHaveText('Identify the first host');
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/questions/host/answer', async route => {
    await held;
    await route.continue().catch(() => {});
  });
  try {
    await page.locator('#active-task-answer').fill('wrong');
    await page.locator('#active-task-submit').click();
    await expect(page.locator('#active-task-submit')).toHaveText('Checking answer');
    await edit(page, 'Telemetry | count');
    release();
    await expect(page.locator('#active-task-feedback')).toContainText('does not match');
    await expect(page.locator('.cm-content')).toBeFocused();
    await expect(page.locator('#active-task-answer')).toHaveValue('wrong');
  } finally {
    release();
  }
});

test('stalled task loading and submission recover without losing drafts', async ({ page }) => {
  await page.clock.install();
  let releaseLoad: () => void = () => {};
  const heldLoad = new Promise<void>(resolve => { releaseLoad = resolve; });
  let first = true;
  await page.route('**/api/questions', async route => {
    if (first) {
      first = false;
      await heldLoad;
    }
    await route.continue().catch(() => {});
  });
  try {
    await open(page);
    await expect(page.locator('#active-task-progress')).toHaveText('Loading investigation tasks');
    await page.clock.fastForward(10001);
    await expect(page.locator('#active-task-title')).toHaveText('Could not load investigation tasks');
    releaseLoad();
    await page.locator('#active-task-next').click();
    await expect(page.locator('#active-task-title')).toHaveText('Identify the first host');
    let releaseAnswer: () => void = () => {};
    const heldAnswer = new Promise<void>(resolve => { releaseAnswer = resolve; });
    await page.route('**/api/questions/host/answer', async route => {
      await heldAnswer;
      await route.abort().catch(() => {});
    });
    try {
      await page.locator('#active-task-answer').fill('kept draft');
      await page.locator('#active-task-submit').click();
      await expect(page.locator('#active-task-submit')).toHaveText('Checking answer');
      await page.clock.fastForward(10001);
      await expect(page.locator('#active-task-feedback')).toHaveText('The answer service did not respond. Your draft is kept; try again.');
      await expect(page.locator('#active-task-answer')).toHaveValue('kept draft');
      await expect(page.locator('#active-task-submit')).toBeEnabled();
      await expect(page.locator('#active-task-answer')).toBeEnabled();
    } finally {
      releaseAnswer();
    }
  } finally {
    releaseLoad();
  }
});

test('submitting an answer preserves a task selected during the request and pauses background refresh', async ({ page }) => {
  const response = await page.request.get('/api/questions');
  const state = await response.json();
  state.questions.push({ id: 'format', title: 'Check the format', prompt: 'Inspect formatting.', solved: false });
  state.totalQuestions = state.questions.length;
  let reads = 0;
  await page.route('**/api/questions', route => {
    reads++;
    return route.fulfill({ json: state });
  });
  await page.clock.install();
  await open(page);
  await expect(page.locator('#active-task-title')).toHaveText('Identify the first host');
  await page.clock.fastForward(14000);
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/questions/host/answer', async route => {
    await held;
    state.questions[0].solved = true;
    state.questions[0].answer = 'alpha';
    state.solvedQuestions = 1;
    await route.fulfill({ json: { correct: true, state } });
  });
  try {
    await page.locator('#active-task-answer').fill('alpha');
    await page.locator('#active-task-submit').click();
    await expect(page.locator('#active-task-submit')).toHaveText('Checking answer');
    await page.locator('#questions-tab').click();
    await page.locator('.question-select').last().click();
    await expect(page.locator('#active-task-title')).toHaveText('Check the format');
    await edit(page, 'Telemetry | count');
    await page.clock.fastForward(2000);
    expect(reads).toBe(1);
    release();
    await expect(page.locator('#toast-message')).toHaveText('Task solved');
    await expect(page.locator('#active-task-title')).toHaveText('Check the format');
    await expect(page.locator('.cm-content')).toBeFocused();
    await expect(page.locator('.question-select').last()).toHaveAttribute('aria-current', 'true');
  } finally {
    release();
  }
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

test('materialized table autocomplete inserts tabular functions and executes cached branches', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  const complete = async (label: string) => {
    await editor.press('Control+Space');
    await page.locator('.cm-completionLabel').filter({ hasText: new RegExp(`^${label}$`) }).click();
  };
  await edit(page, 'mat');
  await editor.press('End');
  await complete('materialize');
  await expect(editor).toHaveText('materialize()');
  await complete('Telemetry');
  await expect(editor).toHaveText('materialize(Telemetry)');
  await run(page, 3);
  await edit(page, 'let T=mat');
  await editor.press('End');
  await complete('materialize');
  await complete('Telemetry');
  await expect(editor).toHaveText('let T=materialize(Telemetry)');
  await edit(page, 'let T=materialize(Telemetry); union T,T');
  await run(page, 6);
  await edit(page, 'Telemetry | as hint.materialized=true T | union T');
  await run(page, 6);
  await edit(page, 'Telemetry | project mat');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^materialize$/ })).toHaveCount(0);
});

test('row-selection autocomplete inserts aggregates and executes latest events per group', async ({ page }) => {
  await open(page);
  const editor=page.locator('.cm-content');
  await edit(page,'Telemetry | summarize arg');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({hasText:/^arg_min$/})).toBeVisible();
  await page.locator('.cm-completionLabel').filter({hasText:/^arg_max$/}).click();
  await expect(editor).toHaveText('Telemetry | summarize arg_max()');
  await editor.pressSequentially('score, *');
  await editor.press('End');
  await editor.pressSequentially(' by host');
  await expect(editor).toHaveText('Telemetry | summarize arg_max(score, *) by host');
  await run(page,3);
  await edit(page,'Telemetry | summarize (last,lastUser)=arg_max(score,user),(first,firstUser)=arg_min(score,user),total=count()');
  await run(page,1);
  await edit(page,'Telemetry | project arg');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({hasText:/^arg_max$/})).toHaveCount(0);
  await edit(page,'Telemetry | summarize arg_max(score, arg');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({hasText:/^arg_max$/})).toHaveCount(0);
});


test('arbitrary row autocomplete inserts selectors only in aggregation context', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  await edit(page, 'Telemetry | summarize take_any');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^take_anyif$/ })).toBeVisible();
  await page.locator('.cm-completionLabel').filter({ hasText: /^take_any$/ }).click();
  await expect(editor).toHaveText('Telemetry | summarize take_any()');
  await editor.pressSequentially('score, user');
  await run(page, 1);
  await edit(page, 'Telemetry | project take_any');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^take_any(?:if)?$/ })).toHaveCount(0);
  await edit(page, 'Telemetry | summarize take_any(score, take_any');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^take_any(?:if)?$/ })).toHaveCount(0);
});


test('Boolean literal autocomplete executes typed conditional values', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  await edit(page, 'print result=iff(bo');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^boolean$/ })).toBeVisible();
  await page.locator('.cm-completionLabel').filter({ hasText: /^bool$/ }).click();
  await expect(editor).toHaveText('print result=iff(bool()');
  await editor.pressSequentially('null');
  await editor.press('End');
  await editor.pressSequentially(', 1, 2)');
  await expect(editor).toHaveText('print result=iff(bool(null), 1, 2)');
  await run(page, 1);
  await expect(page.locator('#result-body')).toContainText('2');
});

test('conditional aggregate autocomplete executes and excludes row expressions', async ({ page }) => {
  await open(page);
  const editor = page.locator('.cm-content');
  await edit(page, 'Telemetry | summarize a=av');
  await editor.press('End');
  await editor.press('Control+Space');
  await expect(page.locator('.cm-completionLabel').filter({ hasText: /^avgif$/ })).toBeVisible();
  await page.locator('.cm-completionLabel').filter({ hasText: /^avgif$/ }).click();
  await expect(editor).toHaveText('Telemetry | summarize a=avgif()');
  await editor.pressSequentially('score, true');
  await run(page, 1);
  for (const query of ['Telemetry | project av', 'Telemetry | summarize sum(av']) {
    await edit(page, query);
    await editor.press('End');
    await editor.press('Control+Space');
    await expect(page.locator('.cm-completionLabel').filter({ hasText: /^avg(?:if)?$/ })).toHaveCount(0);
  }
  await edit(page, 'Telemetry | summarize v=va');
  await editor.press('End');
  await editor.press('Control+Space');
  await page.locator('.cm-completionLabel').filter({ hasText: /^variance$/ }).click();
  await expect(editor).toHaveText('Telemetry | summarize v=variance()');
  await editor.pressSequentially('score');
  await run(page, 1);
  for (const query of ['Telemetry | project va', 'Telemetry | summarize sum(va']) {
    await edit(page, query);
    await editor.press('End');
    await editor.press('Control+Space');
    await expect(page.locator('.cm-completionLabel').filter({ hasText: /^variance(?:p|if|pif)?$/ })).toHaveCount(0);
  }
});
