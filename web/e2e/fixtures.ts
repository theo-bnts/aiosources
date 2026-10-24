import { test as base, expect, type Page, type Locator } from '@playwright/test'
import type { SharePassword } from '../src/lib/catalog-types'
import { startServer, type TestServer } from './server'
import { debridProviders } from '../src/lib/debrid'

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
  page: async ({ page, server, collectionSource, browserName }, use) => {
    let externalRequests = 0
    if (browserName === 'chromium')
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

export async function createStremioAccess(page: Page, name = 'Test installation') {
  const session = await (await page.request.get('/api/session')).json()
  const response = await page.request.post('/api/stremio/connections', {
    headers: { 'X-CSRF-Token': session.csrf_token },
    data: { name, scope: 'all', enabled: true, stream_preferences: { mode: 'all', language: '' } },
  })
  if (response.status() !== 201) throw new Error('Could not create an isolated Stremio access.')
  const credential: SharePassword = await response.json()
  const manifestURL = new URL(
    `${credential.share.url}.${credential.password}/manifest.json`,
    page.url(),
  ).toString()
  return { ...credential, manifestURL, manifestPath: new URL(manifestURL).pathname }
}

export async function selectMultiple(scope: Page | Locator, label: string, values: string[]) {
  const page = 'page' in scope ? scope.page() : scope
  const control = scope.getByRole('button', { name: label, exact: true })
  await control.click()
  await page.getByRole('menuitem', { name: /^All / }).click()
  if (!values.length) return
  await control.click()
  const names: Record<string, string> = {
    movie: 'Movies',
    series: 'Series',
    tv: 'Series',
    anime: 'Anime',
    other: 'Other / not identified',
    ...Object.fromEntries(debridProviders.map(({ id, name }) => [id, name])),
    direct: 'Direct torrents',
  }
  for (const value of values)
    await page.getByRole('menuitemcheckbox', { name: names[value] ?? value, exact: true }).check()
  await page.keyboard.press('Escape')
}

export async function addRemotePassword(page: Page, value: string) {
  await page.getByRole('button', { name: 'Add sharing password', exact: true }).click()
  const secret = page.getByRole('dialog', { name: 'Add sharing password', exact: true })
  await secret.getByLabel('Sharing password', { exact: true }).fill(value)
  await secret.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(secret).toBeHidden()
}
