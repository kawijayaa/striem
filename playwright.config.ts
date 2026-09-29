import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 30000,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:18081',
    viewport: { width: 1440, height: 1000 },
    launchOptions: process.env.STRIEM_TEST_CHROMIUM ? { executablePath: process.env.STRIEM_TEST_CHROMIUM } : {},
    screenshot: 'only-on-failure',
  },
  webServer: [
    { command: 'node tests/e2e/server.mjs challenge', url: 'http://127.0.0.1:18081/api/ready', timeout: 120000 },
    { command: 'node tests/e2e/server.mjs workspace', url: 'http://127.0.0.1:18082/api/ready', timeout: 120000 },
  ],
});
