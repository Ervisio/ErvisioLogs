import { defineConfig, devices } from '@playwright/test';

// Runs against a real stack started by scripts/e2e.sh, never against a mocked API.
const baseURL = process.env.E2E_BASE_URL;
if (!baseURL) throw new Error('E2E_BASE_URL is not set: run scripts/e2e.sh');

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  // The suite creates the first administrator, so the tests share one instance and run in order.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  timeout: 60_000,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: { baseURL, trace: 'retain-on-failure', screenshot: 'only-on-failure', locale: 'en-US' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
