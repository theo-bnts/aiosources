import { test, expect, createStremioAccess } from './fixtures'
import pg from 'pg'
import type { Page } from '@playwright/test'

async function expectMaintenanceFooter(page: Page) {
  await expect(page.locator('.page > :last-child > summary')).toHaveText('Maintenance')
  await expect(
    page.locator('.page > details > summary').filter({ hasText: /^Maintenance$/ }),
  ).toHaveCount(1)
  await expect(
    page.locator('.page section details > summary').filter({ hasText: /^Maintenance$/ }),
  ).toHaveCount(0)
}

test('metadata configuration persists and missing information explains disabled actions', async ({
  page,
}) => {
  await page.goto('/metadata/settings')
  await expect(page.getByRole('heading', { name: 'Metadata', exact: true, level: 1 })).toBeVisible()
  await expectMaintenanceFooter(page)
  await expect(page.getByLabel('Automatically update film & series information')).toBeChecked()
  await expect(page.getByLabel('Automatically identify torrents')).toBeChecked()
  await page.getByText('Manual actions', { exact: true }).click()
  await expect(
    page.getByRole('button', { name: 'Download information updates', exact: true }),
  ).toBeDisabled()
  const configuration = page.getByRole('form', { name: 'Metadata configuration', exact: true })
  await expect(page.getByLabel('TMDB requests per second')).toHaveValue('35')
  await page.getByLabel('TMDB requests per second').fill('20')
  await page.getByLabel('Information update interval (minutes)').fill('30')
  await configuration.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(configuration.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.reload()
  await expect(page.getByLabel('Information update interval (minutes)')).toHaveValue('30')
  await expect(page.getByLabel('TMDB requests per second')).toHaveValue('20')
  await page.getByText('Manual actions', { exact: true }).click()
  await expect(page.getByRole('button', { name: 'Identify torrents' })).toBeDisabled()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('metadata automation persists schedules, retry dates and manual choices without duplicate status cards', async ({
  page,
  server,
}) => {
  await page.goto('/metadata/settings')
  await expect(page.getByText('Manual actions', { exact: true })).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Download information updates', exact: true }),
  ).toHaveCount(0)
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  expect(
    (
      await page.request.put('/api/secrets/tmdb_schedule_fixture', {
        headers,
        data: { revision: 0, value: 'synthetic-never-sent-token' },
      })
    ).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    // Publish a recent completion before configuring the synthetic token, so
    // enabling automation cannot make an external metadata request.
    await db.query('UPDATE ingest.aio_state SET last_sync=now(),changes_through=now()')
    const current = await (await page.request.get('/api/aiosource')).json()
    expect(
      (
        await page.request.put('/api/aiosource/config', {
          headers,
          data: { ...current.config, tmdb_secret: 'tmdb_schedule_fixture' },
        })
      ).status(),
    ).toBe(204)
    await page.reload()
    const scheduled = await (await page.request.get('/api/aiosource')).json()
    expect(scheduled.automation.updates).toBe('scheduled')
    expect(new Date(scheduled.automation.next_update).getTime()).toBeGreaterThan(Date.now())
    await expect(
      page.getByRole('heading', { name: 'Film & series information', exact: true }),
    ).toHaveCount(0)
    await expect(
      page.getByRole('heading', { name: 'Torrent identification', exact: true }),
    ).toHaveCount(0)
    const form = page.getByRole('form', { name: 'Metadata configuration', exact: true })
    await page.getByLabel('Automatically update film & series information').uncheck()
    await page.getByLabel('Automatically identify torrents').uncheck()
    await form.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    expect((await (await page.request.get('/api/aiosource')).json()).automation.updates).toBe(
      'disabled',
    )
    await page.reload()
    await expect(
      page.getByLabel('Automatically update film & series information'),
    ).not.toBeChecked()
    await page.getByLabel('Automatically update film & series information').check()
    await page.getByLabel('Automatically identify torrents').check()
    await form.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    await db.query(
      "UPDATE ingest.aio_state SET sync_retry_at=now()+INTERVAL '5 minutes',sync_failures=2,sync_error='Information update failed. Committed progress is preserved.'",
    )
    await page.reload()
    const retry = await (await page.request.get('/api/aiosource')).json()
    expect(retry.automation.updates).toBe('retry')
    await expect(page.getByRole('alert')).toHaveCount(0)
    await page.getByText('Manual actions', { exact: true }).click()
    await expect(
      page.getByRole('status').filter({ hasText: 'Committed progress is preserved.' }),
    ).toBeVisible()
    await expect(page.getByRole('link', { name: 'View history', exact: true })).toBeVisible()
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(
      form.getByRole('heading', { name: 'Automatic updates & identification', exact: true }),
    ).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  } finally {
    await db.end()
  }
})

test('Stremio manifest works without admin cookies and replacement revokes the previous token', async ({
  page,
  server,
}) => {
  const access = await createStremioAccess(page)
  await page.goto('/addon/settings')
  await expect(page.getByRole('heading', { name: 'Stremio', exact: true })).toBeVisible()
  await expect(page.getByText('Legacy installation', { exact: true })).toHaveCount(0)
  await expect(page.locator('summary', { hasText: /^Maintenance$/ })).toHaveCount(0)
  const original = access.manifestURL
  const manifest = await page.request.get(original, { headers: { Cookie: '' } })
  expect(manifest.status()).toBe(200)
  expect((await manifest.json()).name).toBe('AIOSources · Test installation')
  const streamURL = original.replace('manifest.json', 'stream/movie/tt1234567.json')
  expect((await (await page.request.get(streamURL)).json()).streams).toEqual([])
  const card = page.getByRole('region', { name: 'Access Test installation', exact: true })
  await card.getByRole('button', { name: 'Replace installation key', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace installation key', exact: true })
    .click()
  const manifestField = page.getByLabel('Manifest URL', { exact: true })
  await expect(manifestField).toBeVisible()
  const updated = await manifestField.inputValue()
  expect(updated).not.toBe(original)
  expect((await page.request.get(original)).status()).toBe(401)
  expect((await page.request.get(updated, { headers: { Cookie: '' } })).status()).toBe(200)
  expect(new URL(updated).origin).toBe(server.origin)
})

test('debrid settings validate, persist and expose no default trackers', async ({ page }) => {
  await page.goto('/debrid/settings')
  await expect(page.getByRole('heading', { name: 'Debrid', exact: true, level: 1 })).toBeVisible()
  await expectMaintenanceFooter(page)
  await expect(page.getByLabel('AllDebrid API key')).toHaveValue('')
  await expect(page.getByLabel('TorBox API key')).toHaveValue('')
  const configuration = page.getByRole('form', { name: 'Debrid configuration', exact: true })
  await page.getByLabel('Cache validity (minutes)').fill('1440')
  await page.getByLabel('Stream response timeout (seconds)').fill('10')
  await configuration.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(configuration.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.reload()
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('1440')
  await expect(page.getByLabel('Stream response timeout (seconds)')).toHaveValue('10')
  await expect(page.getByRole('alert')).toHaveCount(0)
  const sources = await (await page.request.get('/api/providers')).json()
  expect(sources.items).toEqual([])
})

test('private playback saves independently and fields have space below their labels', async ({
  page,
  server,
}) => {
  await page.goto('/providers/new')
  await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
  await page.getByLabel('Import a source JSON definition', { exact: true }).setInputFiles({
    name: 'synthetic-private-source.json',
    mimeType: 'application/json',
    buffer: Buffer.from(
      JSON.stringify({
        version: 1,
        id: 'private-playback',
        name: 'Synthetic private source',
        adapter: 'http_json',
        url: server.sourceURL,
        enabled: false,
      }),
    ),
  })
  await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page).toHaveURL(/\/providers\/private-playback$/)
  await page.goto('/secrets')
  const announce = 'https://tracker.example/synthetic%2Fkey/announce?source=one'
  for (const [name, value] of [
    ['private_announce_url', announce],
    ['invalid_announce_url', 'synthetic-key'],
  ]) {
    await page
      .locator('.page-heading')
      .getByRole('button', { name: 'Create secret', exact: true })
      .click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Reference name').fill(name)
    await dialog.getByLabel('Secret value', { exact: true }).fill(value)
    await dialog.getByRole('button', { name: 'Create secret', exact: true }).click()
    await expect(dialog).toHaveCount(0)
  }
  await page.goto('/debrid/settings')
  const configuration = page.getByRole('form', { name: 'Debrid configuration', exact: true })
  const playback = page.getByRole('form', { name: 'Private source playback', exact: true })
  const source = page.locator('div.border-t').filter({
    has: page.getByRole('heading', { name: 'Synthetic private source', exact: true }),
  })
  await expect(source.getByRole('switch', { name: 'Private source' })).toBeChecked()
  const selector = source.getByLabel('Announce URL secret', { exact: true })
  const stored = await (await page.request.get('/api/aiosource')).json()
  // Invalid fields in the separate configuration form cannot prevent saving
  // playback settings, and its draft must never be published by that save.
  await page.getByLabel('Cache validity (minutes)').fill('0')
  await selector.selectOption('invalid_announce_url')
  await playback.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(playback.getByRole('alert')).toHaveCount(1)
  await expect(configuration.getByRole('alert')).toHaveCount(0)
  await page.getByLabel('Cache validity (minutes)').fill('1')
  await expect(playback.getByRole('alert')).toHaveCount(1)
  await page.getByLabel('Cache validity (minutes)').fill('0')
  await expect(selector).toHaveValue('invalid_announce_url')
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('0')
  await playback.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(playback.getByRole('alert')).toHaveCount(0)
  await expect(selector).toHaveValue('')
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('0')
  await selector.selectOption('private_announce_url')
  await expect(configuration.getByRole('button', { name: 'Save', exact: true })).toBeEnabled()
  await playback.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(playback.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('0')
  expect((await (await page.request.get('/api/aiosource')).json()).config.cache_minutes).toBe(
    stored.config.cache_minutes,
  )
  // Saving the other form keeps a pending source edit intact and unpublished.
  await page.getByLabel('Cache validity (minutes)').fill('1500')
  await source.getByRole('switch', { name: 'Private source' }).uncheck()
  await configuration.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(configuration.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(source.getByRole('switch', { name: 'Private source' })).not.toBeChecked()
  await expect(playback.getByRole('button', { name: 'Save', exact: true })).toBeEnabled()
  const independent = await (await page.request.get('/api/aiosource')).json()
  expect(independent.config.cache_minutes).toBe(1500)
  expect(independent.config.trackers['private-playback'].private).toBe(true)
  await playback.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(source.getByRole('switch', { name: 'Private source' })).toBeChecked()
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('1500')
  // The playback draft can save again against the refreshed revision.
  await source.getByRole('switch', { name: 'Private source' }).uncheck()
  await playback.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(playback.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(
    (await (await page.request.get('/api/aiosource')).json()).config.trackers['private-playback']
      .private,
  ).toBe(false)
  await source.getByRole('switch', { name: 'Private source' }).check()
  await playback.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(playback.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.reload()
  await expect(selector).toHaveValue('private_announce_url')
  await expect(page.getByLabel('Announce URL (without passkey)')).toHaveCount(0)
  await expect(page.getByLabel('Passkey secret', { exact: true })).toHaveCount(0)
  const response = await page.request.get('/api/aiosource')
  const body = await response.text()
  expect(body).not.toContain(announce)
  expect(body).not.toContain('synthetic%2Fkey')
  expect(JSON.parse(body).config.trackers['private-playback']).toEqual({
    private: true,
    announce_secret: 'private_announce_url',
  })
  const assertSpacing = async (id: string) => {
    const label = await page.locator(`label[for="${id}"]`).boundingBox()
    const field = await page.locator(`[id="${id}"]`).boundingBox()
    expect(label).not.toBeNull()
    expect(field).not.toBeNull()
    expect(field!.y - (label!.y + label!.height)).toBeGreaterThanOrEqual(6)
  }
  await assertSpacing('announce-private-playback')
  await assertSpacing('cache_minutes')
  await assertSpacing('alldebrid_secret')
  const assertPlaybackFooter = async () => {
    const field = await selector.boundingBox()
    const button = await playback.getByRole('button', { name: 'Save', exact: true }).boundingBox()
    expect(field).not.toBeNull()
    expect(button).not.toBeNull()
    expect(button!.y).toBeGreaterThan(field!.y + field!.height)
  }
  await assertPlaybackFooter()
  await page.setViewportSize({ width: 390, height: 844 })
  await assertSpacing('announce-private-playback')
  await assertPlaybackFooter()
  await page.goto('/metadata/settings')
  await assertSpacing('tmdb_secret')
  await assertSpacing('sync_minutes')
  await page.goto('/addon/settings')
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  await assertSpacing('share-name')
  await expect(page.getByRole('alert')).toHaveCount(0)
})
