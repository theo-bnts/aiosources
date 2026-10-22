import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: Boolean(process.env.CI),
  timeout: 45_000,
  // Each serial scenario includes its own real database and backend setup.
  globalTimeout: 15 * 60_000,
  expect: { timeout: 10_000 },
  reporter: 'list',
  outputDir: './test-results',
  preserveOutput: 'never',
  use: {
    browserName: 'chromium',
    viewport: { width: 1440, height: 1100 },
    locale: 'en-US',
    timezoneId: 'UTC',
    serviceWorkers: 'block',
    acceptDownloads: true,
    // Login traffic, session cookies and source JSON must not become CI artifacts.
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
})
