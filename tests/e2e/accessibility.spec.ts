import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// WCAG 2.1 AA violations, scanned after transitions settle.
async function violations(page: Page): Promise<string[]> {
  await page.waitForTimeout(800);
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  return results.violations.flatMap(violation => violation.nodes.map(node => `${violation.id}: ${node.target.join(' ')}`));
}

test('workspace states meet WCAG 2.1 AA', async ({ page }) => {
  await page.goto('/?q=' + encodeURIComponent('Telemetry | project TimeGenerated, host, score, user, details'));
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  await expect(page.locator('#source-list button')).toHaveCount(5);
  expect(await violations(page), 'initial').toEqual([]);
  await page.locator('#run-query').click();
  await expect(page.locator('#query-stats')).toHaveText(/^3 rows ·/);
  expect(await violations(page), 'results').toEqual([]);
  await page.getByRole('button', { name: 'View event details for row 1' }).click();
  expect(await violations(page), 'event dialog').toEqual([]);
  await page.locator('#close-dialog').click();
  await page.locator('#questions-tab').click();
  expect(await violations(page), 'tasks').toEqual([]);
  await page.locator('#hunts-tab').click();
  expect(await violations(page), 'hunts').toEqual([]);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await violations(page), 'mobile').toEqual([]);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/kusto.html');
  expect(await violations(page), 'native console').toEqual([]);
});

test('keyboard focus is always visible and never trapped', async ({ page }) => {
  await page.goto('/?q=' + encodeURIComponent('Telemetry | take 3'));
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  await page.locator('#run-query').click();
  await expect(page.locator('#query-stats')).toHaveText(/^3 rows ·/);
  await page.locator('body').click({ position: { x: 1, y: 1 } });
  const seen = new Set<string>();
  const invisible: string[] = [];
  for (let step = 0; step < 80; step++) {
    // The editor keeps Tab for indentation unless Escape hands focus on.
    if (await page.locator('.cm-content').evaluate(node => node === document.activeElement)) await page.keyboard.press('Escape');
    await page.keyboard.press('Tab');
    const focus = await page.evaluate(() => {
      const element = document.activeElement as HTMLElement | null;
      if (!element || element === document.body) return null;
      // A control or its wrapper (through :focus-within) draws the indicator.
      const indicated = (node: Element | null) => {
        if (!node) return false;
        const style = getComputedStyle(node);
        return (style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0) || style.boxShadow !== 'none'
          || style.textDecorationLine.includes('underline');
      };
      const visible = element.matches('.cm-content') || indicated(element) || indicated(element.parentElement) || indicated(element.parentElement?.parentElement ?? null);
      return { key: element.id || `${element.tagName}.${element.className}`.slice(0, 80), visible };
    });
    if (!focus) continue;
    if (!focus.visible) invisible.push(focus.key);
    seen.add(focus.key);
  }
  expect(invisible, 'controls without a visible focus indicator').toEqual([]);
  // Tabbing must move through the workspace rather than stick on a control.
  expect(seen.size).toBeGreaterThan(15);
  for (const id of ['run-query', 'fields-tab', 'active-task-answer']) expect([...seen], id).toContain(id);
});
