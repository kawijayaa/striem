import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

test('native console preserves numeric JSON and uses explicit query and management routes', async ({ page }) => {
  await page.route('**/api/kusto/capabilities', route => route.fulfill({ json: { enabled: true, management: true } }));
  const raw = '{"tables":[{"columns":[{"name":"Value","type":"long"}],"rows":[[9223372036854775807]]}]}';
  await page.route('**/api/kusto/query', async route => {
    expect(route.request().postDataJSON()).toEqual({ query: 'print x=limit', parameters: { limit: '9223372036854775807' }, progressive: true });
    await route.fulfill({ contentType: 'application/json', body: raw });
  });
  await page.route('**/api/kusto/management', async route => {
    expect(route.request().postDataJSON()).toEqual({ query: '.show tables', parameters: {} });
    await route.fulfill({ json: { tables: [] } });
  });
  await page.goto('/');
  await expect(page.locator('#kusto-link')).toBeVisible();
  await page.goto('/kusto.html');
  await expect(page.getByRole('button', { name: 'Run query', exact: true })).toBeEnabled();
  await page.getByLabel('KQL', { exact: true }).fill('print x=limit');
  await page.getByText('Query parameters', { exact: true }).click();
  await page.getByLabel('Parameters', { exact: true }).fill('{"limit":"9223372036854775807"}');
  await page.getByLabel('Request progressive query results').check();
  await page.getByRole('button', { name: 'Run query', exact: true }).click();
  await expect(page.getByLabel('Service response')).toHaveText(raw);
  const downloading = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download response JSON' }).click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe('kusto-response.json');
  const path = await download.path();
  expect(path).not.toBeNull();
  expect(await readFile(path!, 'utf8')).toBe(raw);
  await page.getByLabel('Execution').selectOption('management');
  await expect(page.getByLabel('Request progressive query results')).toBeDisabled();
  await page.getByLabel('KQL', { exact: true }).fill('.show tables');
  await page.getByLabel('Parameters', { exact: true }).fill('{}');
  await page.getByRole('button', { name: 'Run management command' }).click();
  await expect(page.getByLabel('Service response')).toHaveText('{"tables":[]}');
});

test('native console keeps execution disabled when no service is configured', async ({ page }) => {
  await page.route('**/api/kusto/capabilities', route => route.fulfill({ json: { enabled: false, management: false } }));
  await page.goto('/');
  await expect(page.locator('#ingestion-screen')).toHaveCount(0);
  await expect(page.locator('#kusto-link')).toBeHidden();
  await page.goto('/kusto.html');
  await expect(page.getByText('A Kusto service connection has not been configured by the administrator.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Run query', exact: true })).toBeDisabled();
});
