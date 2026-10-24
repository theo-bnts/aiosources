import { openDisclosure } from './disclosures'
import type { Page } from '@playwright/test'
import type { SecretInfo } from '../src/lib/types'
import { test, expect } from './fixtures'

async function proof(page: Page, origin: string) {
  const session = await (await page.request.get('/api/session')).json()
  return { 'X-CSRF-Token': session.csrf_token, Origin: origin }
}

test('Storage separates measurements from settings and has no backup API or duplicate availability badge', async ({
  page,
}) => {
  await page.goto('/health')
  for (const name of ['PostgreSQL database', 'Local available space', 'Where storage is used'])
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
  const tabs = page.getByRole('tablist', { name: 'Storage views', exact: true })
  await expect(tabs).toHaveAttribute('aria-orientation', 'horizontal')
  await expect(tabs.getByRole('tab', { name: 'Statistics', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('heading', { name: 'Stored data', exact: true })).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toHaveCount(0)
  await expect(page.getByText('Available', { exact: true })).toHaveCount(1)
  const data = await (await page.request.get('/api/system-health')).json()
  expect(data.database.available).toBe(true)
  expect(data.database.total_bytes).toBeGreaterThan(0)
  expect(data).not.toHaveProperty('diagnostics')
  expect(data).not.toHaveProperty('growth')
  expect(data).not.toHaveProperty('settings')
  expect((await page.request.get('/api/system-health/settings')).status()).toBe(404)
  for (const route of ['/api/backups', '/api/backups/settings', '/api/backups/recovery'])
    expect((await page.request.get(route)).status()).toBe(404)
  const labels = {
    sources: 'Sources',
    metadata: 'Metadata',
    debrid: 'Debrid',
    stremio: 'Stremio',
    sharing: 'Shared catalogs',
    access: 'Security',
    logs: 'Logs and operation history',
    other: 'Settings and database state',
    overhead: 'Database overhead',
  } as Record<string, string>
  const expected = [
    ...data.database.categories,
    { key: 'overhead', bytes: data.database.other_database_bytes },
  ]
    .sort((a, b) => b.bytes - a.bytes || a.key.localeCompare(b.key))
    .map((c) => labels[c.key])
  await expect(page.getByRole('table').getByRole('row').locator('td:first-child')).toHaveText(
    expected,
  )
})

test('source creation and import actions appear only in Settings', async ({ page }) => {
  for (const path of ['/providers', '/runs']) {
    await page.goto(path)
    await expect(page.getByRole('link', { name: 'Add source', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Run manual import', exact: true })).toHaveCount(
      0,
    )
  }
  await page.goto('/providers/settings')
  await expect(page.getByRole('link', { name: 'Add source', exact: true })).toHaveCount(1)
  await expect(
    page.locator('.page-heading').getByRole('button', { name: 'Run manual import', exact: true }),
  ).toHaveCount(0)
  await expect(page.getByText('Manual actions', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Run manual import', exact: true })).toHaveCount(0)
})

test('secret forms and open deletion confirmations cannot adopt a competing rotation silently', async ({
  page,
  server,
}) => {
  await page.goto('/secrets')
  await page
    .locator('.page-heading')
    .getByRole('button', { name: 'Create secret', exact: true })
    .click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('Reference name', { exact: true }).fill('concurrent_credential')
  await editor.getByLabel('Secret value', { exact: true }).fill('synthetic-initial-value')
  await editor.getByRole('button', { name: 'Create secret', exact: true }).click()
  await expect(editor).toHaveCount(0)
  const metadata = async () => {
    const body: { items: SecretInfo[] } = await (await page.request.get('/api/secrets')).json()
    return body.items.find((item) => item.name === 'concurrent_credential')!
  }
  const initial = await metadata()
  const row = page.getByRole('row').filter({ hasText: 'concurrent_credential' })
  await row.getByRole('button', { name: 'Replace', exact: true }).click()
  await editor.getByLabel('New value', { exact: true }).fill('synthetic-local-draft')
  const headers = await proof(page, server.origin)
  expect(
    (
      await page.request.put('/api/secrets/concurrent_credential', {
        headers,
        data: { value: 'synthetic-competing-value', revision: initial.revision },
      })
    ).status(),
  ).toBe(204)
  let rotated = await metadata()
  const rejected = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/secrets/concurrent_credential') &&
      response.request().method() === 'PUT',
  )
  await editor.getByRole('button', { name: 'Confirm replacement', exact: true }).click()
  expect((await rejected).status()).toBe(409)
  await expect(editor.getByLabel('New value', { exact: true })).toHaveValue('synthetic-local-draft')
  expect((await metadata()).revision).toBe(rotated.revision)
  await editor.getByRole('button', { name: 'Load latest revision', exact: true }).click()
  await expect(
    editor.getByRole('button', { name: 'Load latest revision', exact: true }),
  ).toHaveCount(0)
  await expect(editor.getByLabel('New value', { exact: true })).toHaveValue('synthetic-local-draft')
  const ownRefresh = page.waitForResponse(async (response) => {
    if (!response.url().endsWith('/api/secrets') || response.request().method() !== 'GET')
      return false
    const body: { items: SecretInfo[] } = await response.json()
    return body.items.some((item) => item.name === initial.name && item.revision > rotated.revision)
  })
  await editor.getByRole('button', { name: 'Confirm replacement', exact: true }).click()
  await expect(editor).toHaveCount(0)
  await ownRefresh
  rotated = await metadata()
  await row
    .getByRole('button', { name: 'Delete secret concurrent_credential', exact: true })
    .click()
  const deletion = page.getByRole('alertdialog')
  const refreshed = page.waitForResponse(async (response) => {
    if (!response.url().endsWith('/api/secrets') || response.request().method() !== 'GET')
      return false
    const body: { items: SecretInfo[] } = await response.json()
    return body.items.some((item) => item.name === initial.name && item.revision > rotated.revision)
  })
  expect(
    (
      await page.request.put('/api/secrets/concurrent_credential', {
        headers,
        data: { value: 'synthetic-latest-value', revision: rotated.revision },
      })
    ).status(),
  ).toBe(204)
  await refreshed
  const failedDelete = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/secrets/concurrent_credential') &&
      response.request().method() === 'DELETE',
  )
  await deletion.getByRole('button', { name: 'Delete secret', exact: true }).click()
  expect((await failedDelete).status()).toBe(409)
  await expect(deletion.getByRole('alert')).toBeVisible()
  await deletion.getByRole('button', { name: 'Back', exact: true }).click()
  await expect(row).toBeVisible()
})

test('retired saved views have no controls or API and search URLs remain usable', async ({
  page,
  server,
}) => {
  await page.goto('/torrents?q=example&sort=size_desc')
  await expect(page.getByLabel('Search text', { exact: true })).toHaveValue('example')
  await expect(page.getByText('Saved views', { exact: true })).toHaveCount(0)
  const headers = await proof(page, server.origin)
  for (const method of ['GET', 'POST', 'PUT', 'DELETE']) {
    const response = await page.request.fetch(
      method === 'GET' || method === 'POST' ? '/api/saved-views' : '/api/saved-views/old',
      { method, headers },
    )
    expect(response.status()).toBe(404)
  }
  await page.reload()
  await expect(page.getByLabel('Sort by', { exact: true })).toHaveValue('size_desc')
})

test('named secret creation recovers a lost acknowledgement without replacing a different value', async ({
  page,
}) => {
  await page.goto('/secrets')
  const create = page
    .locator('.page-heading')
    .getByRole('button', { name: 'Create secret', exact: true })
  await create.click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('Reference name', { exact: true }).fill('owned_creation')
  await editor.getByLabel('Secret value', { exact: true }).fill('synthetic-owned-value')
  let loseResponse = true
  await page.route('**/api/secrets/owned_creation', async (route) => {
    if (route.request().method() === 'PUT' && loseResponse) {
      loseResponse = false
      expect((await route.fetch()).status()).toBe(204)
      await route.abort('failed')
    } else await route.continue()
  })
  await editor.getByRole('button', { name: 'Create secret', exact: true }).click()
  await expect(page.locator('[data-connection-notice]')).toContainText('The server is unreachable.')
  await expect(editor.getByRole('alert')).toHaveCount(0)
  const metadata = await (await page.request.get('/api/secrets')).json()
  await expect(
    page.getByRole('row', { includeHidden: true }).filter({ hasText: 'owned_creation' }),
  ).toBeVisible()
  await editor.getByRole('button', { name: 'Create secret', exact: true }).click()
  await expect(editor).toHaveCount(0)
  expect(await (await page.request.get('/api/secrets')).json()).toEqual(metadata)
  await create.click()
  await editor.getByLabel('Reference name', { exact: true }).fill('owned_creation')
  await editor.getByLabel('Secret value', { exact: true }).fill('synthetic-different-value')
  const rejected = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/secrets/owned_creation') &&
      response.request().method() === 'PUT',
  )
  await editor.getByRole('button', { name: 'Create secret', exact: true }).click()
  expect((await rejected).status()).toBe(409)
  await expect(editor.getByLabel('Secret value', { exact: true })).toHaveValue(
    'synthetic-different-value',
  )
  expect(await (await page.request.get('/api/secrets')).json()).toEqual(metadata)
})

test('debrid drafts require review before adopting another writer after a separate form saves', async ({
  page,
  server,
}) => {
  const headers = await proof(page, server.origin)
  expect(
    (
      await page.request.put('/api/providers/synthetic', {
        headers,
        data: {
          json: JSON.stringify({
            version: 1,
            id: 'synthetic',
            name: 'Synthetic source',
            adapter: 'http_json',
            url: server.sourceURL,
            enabled: false,
          }),
          revision: '',
        },
      })
    ).status(),
  ).toBe(200)
  const initial = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: {
          ...initial.config,
          trackers: { synthetic: { private: true, announce_secret: '' } },
        },
      })
    ).status(),
  ).toBe(204)
  await page.goto('/debrid/settings')
  const configuration = page.getByRole('form', { name: 'Debrid configuration', exact: true })
  await openDisclosure(page, 'Private source playback')
  const playback = page.getByRole('form', { name: 'Private source playback', exact: true })
  // The fixture has no enabled source; a private-playback draft changes only
  // this form's tracker map and never publishes the separate debrid draft.
  const privateSource = playback.getByRole('switch', { name: 'Private source' })
  await expect(privateSource).toHaveCount(1)
  await page.getByLabel('Cache validity (minutes)').fill('1500')
  await privateSource.uncheck()
  let changed = false
  await page.route('**/api/aiosource/controls', async (route) => {
    if (!changed) {
      const current = await (await page.request.get('/api/aiosource')).json()
      const startingRevision = initial.config.revision + 1
      if (current.config.revision === startingRevision + 1) {
        changed = true
        expect(
          (
            await page.request.put('/api/aiosource/config', {
              headers,
              data: { ...current.config, response_seconds: 12 },
            })
          ).status(),
        ).toBe(204)
      }
    }
    await route.continue()
  })
  await playback.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(
    configuration.getByRole('button', { name: 'Load latest settings', exact: true }),
  ).toBeVisible()
  expect(changed).toBe(true)
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('1500')
  await expect(configuration.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await configuration.getByRole('button', { name: 'Load latest settings', exact: true }).click()
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('1500')
  await expect(page.getByLabel('Stream response timeout (seconds)')).toHaveCount(0)
  await configuration.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(configuration.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const saved = await (await page.request.get('/api/aiosource')).json()
  expect(saved.config.cache_minutes).toBe(1500)
  expect(saved.config.response_seconds).toBe(12)
})
