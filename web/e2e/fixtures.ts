import { test as base, expect } from '@playwright/test'
import { startServer, type TestServer } from './server'

export const test = base.extend<{ server: TestServer; collectionSource: boolean }>({
  collectionSource: [false, { option: true }],
  server: [
    async ({ collectionSource }, use) => {
      const server = await startServer({ collectionSource })
      try {
        await use(server)
      } finally {
        await server.close()
      }
    },
    // Keep data, login rate limits and background state independent per test.
    { timeout: 90_000 },
  ],
  baseURL: async ({ server }, use) => {
    await use(server.origin)
  },
  page: async ({ page, server, collectionSource }, use) => {
    let externalRequests = 0
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], {
      origin: server.origin,
    })
    // This is an egress guard, not an API mock. All application requests reach the
    // embedded Go server and its real isolated PostgreSQL database unchanged.
    await page.context().route('**/*', async (route) => {
      const url = new URL(route.request().url())
      if (url.origin === server.origin || ['blob:', 'data:'].includes(url.protocol)) {
        await route.continue()
      } else {
        externalRequests++
        await route.abort('blockedbyclient')
      }
    })
    try {
      await page.goto('/providers/settings')
      await page.getByLabel('Administrator password', { exact: true }).fill(server.password)
      await page.getByRole('button', { name: 'Sign in', exact: true }).click()
      await expect(
        page.getByRole('heading', { name: 'Sources', exact: true, level: 1 }),
      ).toBeVisible()
    } catch {
      // Playwright action errors can include fill arguments. Do not let a login
      // failure publish the temporary administrator password in a reporter.
      throw new Error('Could not sign in to the isolated backend through the ordinary login form.')
    }
    try {
      await use(page)
    } finally {
      expect(externalRequests, 'The editor must not request external resources').toBe(0)
      if (!collectionSource) {
        expect(server.sourceRequests(), 'Save and validation must never contact the source').toBe(0)
        const response = await page.request.get('/api/runs')
        expect(response.status()).toBe(200)
        expect(
          (await response.json()).items,
          'Disabled, unscheduled source edits must not enqueue runs',
        ).toEqual([])
      }
    }
  },
})

export { expect } from '@playwright/test'
