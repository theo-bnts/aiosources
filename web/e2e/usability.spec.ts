import type { Page } from '@playwright/test'
import { test, expect, addRemotePassword } from './fixtures'

async function configuration(page: Page) {
  const response = await page.request.get('/api/aiosource')
  expect(response.status()).toBe(200)
  return (await response.json()).config
}

async function source(page: Page, id: string) {
  const response = await page.request.get(`/api/providers/${id}`)
  expect(response.status()).toBe(200)
  return response.json()
}

async function createSource(
  page: Page,
  url: string,
  id: string,
  extra: Record<string, unknown> = {},
  save = true,
) {
  await page.goto('/providers/new')
  await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
  await page.getByLabel('Import a source JSON definition', { exact: true }).setInputFiles({
    name: 'synthetic-usability-source.json',
    mimeType: 'application/json',
    buffer: Buffer.from(
      JSON.stringify({
        version: 1,
        id,
        name: 'Synthetic usability source',
        adapter: 'http_json',
        url,
        enabled: false,
        ...extra,
      }),
    ),
  })
  await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  if (save) {
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/providers/${id}$`))
  }
  return JSON.stringify({
    version: 1,
    id,
    name: 'Synthetic usability source',
    adapter: 'http_json',
    url,
    enabled: false,
    ...extra,
  })
}

async function storeCredential(page: Page, button: string, label: string, value: string) {
  await page.getByRole('button', { name: button, exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel(label, { exact: true }).fill(value)
  await dialog.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(dialog).toBeHidden()
}

test('inline credentials stay encrypted, retry one owned reference and require an explicit configuration save', async ({
  page,
}) => {
  await page.goto('/debrid/settings')
  const original = await configuration(page)
  const form = page.getByRole('form', { name: 'Debrid configuration', exact: true })
  await page.getByLabel('Cache validity (minutes)').fill('1500')
  await storeCredential(page, 'Add AllDebrid key', 'AllDebrid API key', 'synthetic-ad-key')
  const ad = await page.getByLabel('AllDebrid API key', { exact: true }).inputValue()
  expect(ad).toMatch(/^alldebrid_[a-f0-9]{32}$/)
  await expect(page.getByRole('button', { name: 'Add AllDebrid key', exact: true })).toBeDisabled()
  expect(await configuration(page)).toEqual(original)
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('1500')
  // The server commits the secret, but its response is lost. Retrying must use
  // the same fresh reference, without saving the parent form or creating orphans.
  let loseResponse = true
  await page.route('**/api/secrets/torbox_*', async (route) => {
    if (route.request().method() === 'PUT' && loseResponse) {
      loseResponse = false
      expect((await route.fetch()).status()).toBe(204)
      await route.abort('failed')
    } else await route.continue()
  })
  await page.getByRole('button', { name: 'Add TorBox key', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('TorBox API key', { exact: true }).fill('synthetic-tb-key')
  await dialog.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(page.locator('[data-connection-notice]')).toContainText('The server is unreachable.')
  await expect(dialog.getByRole('alert')).toHaveCount(0)
  const entriesBeforeRetry = await (await page.request.get('/api/secrets')).json()
  expect(entriesBeforeRetry.items).toHaveLength(2)
  await dialog.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByRole('button', { name: 'Add TorBox key', exact: true })).toBeDisabled()
  expect((await (await page.request.get('/api/secrets')).json()).items).toHaveLength(2)
  expect(await configuration(page)).toEqual(original)
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const saved = await configuration(page)
  expect(saved.alldebrid_secret).toBe(ad)
  expect(saved.torbox_secret).toMatch(/^torbox_[a-f0-9]{32}$/)
  expect(saved.cache_minutes).toBe(1500)
  expect(JSON.stringify(await (await page.request.get('/api/secrets')).json())).not.toContain(
    'synthetic-',
  )
  await page.goto('/metadata/settings')
  // A synthetic credential must never initiate a real TMDB download.
  await page.getByLabel('Automatically update film & series information').uncheck()
  await page.getByText('Manual actions', { exact: true }).click()
  await storeCredential(page, 'Add TMDB token', 'TMDB read access token', 'synthetic-tmdb-token')
  await expect(
    page.getByRole('button', { name: 'Download information updates', exact: true }),
  ).toBeDisabled()
  expect((await configuration(page)).tmdb_secret).toBe('')
  const metadata = page.getByRole('form', { name: 'Metadata configuration', exact: true })
  await metadata.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(metadata.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await configuration(page)).tmdb_secret).toMatch(/^tmdb_[a-f0-9]{32}$/)
  await expect(
    page.getByRole('button', { name: 'Download information updates', exact: true }),
  ).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Add TMDB token', exact: true })).toBeDisabled()
  await page.getByLabel('TMDB read access token', { exact: true }).selectOption('')
  await expect(page.getByRole('button', { name: 'Add TMDB token', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Add TMDB token', exact: true }).click()
  await page
    .getByRole('dialog')
    .getByLabel('TMDB read access token', { exact: true })
    .fill('synthetic-cancelled-value')
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Discard changes', exact: true })
    .click()
  await page.getByRole('button', { name: 'Add TMDB token', exact: true }).click()
  await expect(
    page.getByRole('dialog').getByLabel('TMDB read access token', { exact: true }),
  ).toHaveValue('')
  await page.keyboard.press('Escape')
  expect((await (await page.request.get('/api/secrets')).json()).items).toHaveLength(3)
  await page
    .getByLabel('TMDB read access token', { exact: true })
    .selectOption((await configuration(page)).tmdb_secret)
  await expect(page.getByRole('button', { name: 'Add TMDB token', exact: true })).toBeDisabled()
})

test('full announce URLs added inline keep playback and debrid drafts independent on mobile', async ({
  page,
  server,
}) => {
  await createSource(page, server.sourceURL, 'inline-private')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/debrid/settings')
  const original = await configuration(page)
  await page.getByLabel('Cache validity (minutes)').fill('1500')
  await storeCredential(
    page,
    'Add announce URL',
    'Complete announce URL',
    'https://tracker.example/synthetic%2Fkey/announce?source=one&passkey=synthetic-middle&format=json',
  )
  const reference = await page.getByLabel('Announce URL secret', { exact: true }).inputValue()
  expect((await configuration(page)).revision).toBe(original.revision)
  const playback = page.getByRole('form', { name: 'Private source playback', exact: true })
  await playback.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(playback.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const saved = await configuration(page)
  expect(saved.trackers['inline-private'].announce_secret).toBe(reference)
  expect(saved.cache_minutes).toBe(original.cache_minutes)
  await expect(page.getByLabel('Cache validity (minutes)')).toHaveValue('1500')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('source credentials are added inline without saving their surrounding editor', async ({
  page,
  server,
}) => {
  await createSource(page, server.sourceURL, 'inline-auth')
  const original = await source(page, 'inline-auth')
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await page.getByLabel('Authentication type', { exact: true }).selectOption('bearer')
  await storeCredential(page, 'Add secret', 'Credential value', 'synthetic-source-token')
  const reference = await page.getByLabel('Secret reference', { exact: true }).inputValue()
  expect((await source(page, 'inline-auth')).revision).toBe(original.revision)
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect
    .poll(async () => (await source(page, 'inline-auth')).provider.auth.secret_ref)
    .toBe(reference)
  expect(JSON.stringify(await (await page.request.get('/api/secrets')).json())).not.toContain(
    'synthetic-',
  )
})

test('adaptive authentication, pagination and adapter controls retain hidden values and exact JSON', async ({
  page,
  server,
}) => {
  const originalJSON = await createSource(
    page,
    server.sourceURL,
    'adaptive-source',
    {
      schedule: { enabled: false, every: '1h30m', timezone: 'Europe/Paris', max_pages: 7 },
      auth: {
        type: 'basic',
        username_ref: 'user_ref',
        password_ref: 'password_ref',
        secret_ref: 'token_ref',
        in: 'header',
        name: 'X-Key',
      },
      http: { method: 'POST', body: { untouched: 'body' }, query: { untouched: 'query' } },
      pagination: {
        type: 'page',
        page_param: 'p',
        offset_param: 'offset',
        cursor_param: 'cursor',
        size_param: 'size',
      },
    },
    false,
  )
  const original = { json: originalJSON }
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await expect(page.getByLabel('Username reference', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Secret reference', { exact: true })).toBeHidden()
  await page.getByLabel('Authentication type', { exact: true }).selectOption('api_key')
  await expect(page.getByLabel('Secret reference', { exact: true })).toHaveValue('token_ref')
  await expect(page.getByLabel('Credential parameter name', { exact: true })).toHaveValue('X-Key')
  await expect(page.getByLabel('Password reference', { exact: true })).toBeHidden()
  await page.getByRole('tab', { name: /^Pagination/ }).click()
  await expect(page.getByLabel('Page parameter', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Cursor parameter', { exact: true })).toBeHidden()
  await page.getByLabel('Pagination type', { exact: true }).selectOption('cursor')
  await expect(page.getByLabel('Cursor parameter', { exact: true })).toHaveValue('cursor')
  await page.getByRole('tab', { name: /^Identity/ }).click()
  await page.getByLabel('Source type', { exact: true }).selectOption('torznab')
  await page.getByRole('tab', { name: /^Source fields/ }).click()
  await expect(page.getByText('Torznab reads torrent fields', { exact: false })).toBeVisible()
  await page.getByRole('tab', { name: /^Identity/ }).click()
  await page.getByLabel('Source type', { exact: true }).selectOption('json')
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await page.getByLabel('Authentication type', { exact: true }).selectOption('basic')
  await page.getByRole('tab', { name: /^Pagination/ }).click()
  await page.getByLabel('Pagination type', { exact: true }).selectOption('page')
  await page.getByRole('button', { name: 'JSON', exact: true }).click()
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export JSON', exact: true }).click()
  const exported = await download
  const stream = await exported.createReadStream()
  expect(stream).not.toBeNull()
  const chunks = []
  for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
  await exported.delete()
  expect(Buffer.concat(chunks).toString('utf8')).toBe(
    original.json.replace(/("adapter"\s*:\s*)"http_json"/, '$1"json"'),
  )
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await page.getByRole('tab', { name: /^Imports/ }).click()
  for (const label of ['Timing', 'Daily time', 'Schedule enabled', 'Scheduled collection mode']) {
    await expect(page.getByLabel(label, { exact: true })).toHaveCount(0)
  }
  await page.getByLabel('Request interval', { exact: true }).selectOption('2s')
  await page.getByRole('button', { name: 'JSON', exact: true }).click()
  const settingsDownload = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export JSON', exact: true }).click()
  const settings = await settingsDownload
  const settingsStream = await settings.createReadStream()
  const settingsChunks = []
  for await (const chunk of settingsStream!) settingsChunks.push(Buffer.from(chunk))
  await settings.delete()
  const edited = JSON.parse(Buffer.concat(settingsChunks).toString('utf8'))
  expect(edited.schedule).toEqual(JSON.parse(original.json).schedule)
  expect(edited.request_interval).toBe('2s')
  expect(edited.http).toEqual(JSON.parse(original.json).http)
})

test('source schedules preserve daily times, exact custom expressions and bounded imports', async ({
  page,
  server,
}) => {
  await createSource(page, server.sourceURL, 'simple-schedule', {
    schedule: {
      enabled: false,
      every: '1h30m',
      full_every: '336h',
      timezone: 'Europe/Paris',
      mode: 'incremental',
      max_pages: 7,
      known_pages: 3,
    },
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/providers/simple-schedule#schedules')
  await expect(page.getByRole('tab', { name: 'Schedules', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByLabel('Interval custom duration', { exact: true })).toHaveValue('1h30m')
  await page.getByLabel('Timing', { exact: true }).selectOption('daily')
  await page.getByLabel('Daily time', { exact: true }).fill('')
  const save = page.getByRole('button', { name: 'Save', exact: true })
  await expect(save).toBeDisabled()
  await page.getByLabel('Daily time', { exact: true }).fill('02:30')
  await expect(page.getByLabel('Timezone', { exact: true })).toHaveValue('Europe/Paris')
  await page.getByText('Advanced schedule settings', { exact: true }).click()
  await page.getByLabel('Page budget per tick').fill('12')
  await save.click()
  await expect(save).toBeDisabled()
  let saved = (await source(page, 'simple-schedule')).provider.schedule
  expect(saved).toMatchObject({
    cron: '30 2 * * *',
    timezone: 'Europe/Paris',
    full_every: '336h',
    known_pages: 3,
    max_pages: 12,
    enabled: false,
  })
  expect(saved.every).toBeFalsy()
  await page.reload()
  await expect(page.getByLabel('Daily time', { exact: true })).toHaveValue('02:30')
  await page.getByLabel('Timing', { exact: true }).selectOption('cron')
  const custom = '  15  5 * * 1-5  '
  await page.getByLabel('Cron expression', { exact: true }).fill(custom)
  await save.click()
  await expect(save).toBeDisabled()
  await page.reload()
  await expect(page.getByLabel('Cron expression', { exact: true })).toHaveValue(custom)
  await page.getByText('Advanced schedule settings', { exact: true }).click()
  await page.getByLabel('Page budget per tick').fill('13')
  await save.click()
  await expect(save).toBeDisabled()
  saved = (await source(page, 'simple-schedule')).provider.schedule
  expect(saved.cron).toBe(custom)
  expect(saved.max_pages).toBe(13)
  expect(saved.full_every).toBe('336h')
  await page.getByLabel('Timing', { exact: true }).selectOption('manual')
  await expect(page.getByRole('alert')).toContainText(
    'A periodic rebuild requires an automatic Update schedule',
  )
  await expect(save).toBeDisabled()
  page.once('dialog', (dialog) => void dialog.accept())
  await page.reload()
  expect((await source(page, 'simple-schedule')).provider.schedule).toEqual(saved)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('source schedule conflicts merge untouched controls while preserving local budget edits', async ({
  page,
  server,
}) => {
  await createSource(page, server.sourceURL, 'schedule-conflict', {
    schedule: {
      enabled: false,
      every: '1h30m',
      timezone: 'Europe/Paris',
      mode: 'incremental',
      max_pages: 7,
      known_pages: 3,
    },
  })
  const original = await source(page, 'schedule-conflict')
  await page.goto('/providers/schedule-conflict#schedules')
  await page.getByText('Advanced schedule settings', { exact: true }).click()
  await page.getByLabel('Page budget per tick').fill('12')
  const session = await (await page.request.get('/api/session')).json()
  expect(
    (
      await page.request.put('/api/providers/schedule-conflict', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
        data: {
          revision: original.revision,
          json: original.json
            .replace('Europe/Paris', 'America/New_York')
            .replace('"known_pages":3', '"known_pages":9'),
        },
      })
    ).status(),
  ).toBe(200)
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Revision conflict', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Compare server version', exact: true }).click()
  await page.getByRole('button', { name: 'Merge my field edits', exact: true }).click()
  await expect(page.getByLabel('Timezone', { exact: true })).toHaveValue('America/New_York')
  await expect(page.getByLabel('Known-page boundary', { exact: true })).toHaveValue('9')
  await expect(page.getByLabel('Page budget per tick')).toHaveValue('12')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await source(page, 'schedule-conflict')).provider.schedule).toMatchObject({
    timezone: 'America/New_York',
    known_pages: 9,
    max_pages: 12,
    every: '1h30m',
  })
})

test('remote catalogs belong to Sources and use simple intervals with safe concurrent edits', async ({
  page,
  server,
}) => {
  await page.goto('/sharing/remotes?q=synthetic&state=disabled')
  await expect(page).toHaveURL(/\/remotes\/settings\?q=synthetic&state=disabled$/)
  await expect(page.getByRole('heading', { name: 'Remote catalogs', level: 1 })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Settings', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.getByRole('button', { name: 'Add remote catalog', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  for (const label of ['Timing', 'Timezone', 'Cron expression', 'Page budget per tick']) {
    await expect(dialog.getByLabel(label, { exact: true })).toHaveCount(0)
  }
  await expect(
    dialog.getByRole('switch', { name: 'Automatic synchronization', exact: true }),
  ).toBeChecked()
  await expect(
    dialog.getByLabel('Synchronization interval (minutes)', { exact: true }),
  ).toHaveValue('60')
  await dialog.getByLabel('Display name', { exact: true }).fill('Unsaved remote')
  await page.keyboard.press('Escape')
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Discard changes', exact: true })
    .click()
  expect((await (await page.request.get('/api/remotes')).json()).items).toEqual([])
  await page.getByRole('button', { name: 'Add remote catalog', exact: true }).first().click()
  await dialog.getByLabel('Local source identifier', { exact: true }).fill('interval-remote')
  await dialog.getByLabel('Display name', { exact: true }).fill('Synthetic interval remote')
  await dialog
    .getByLabel('HTTPS catalog URL', { exact: true })
    .fill('https://catalog.example/api/catalogs/synthetic')
  await addRemotePassword(page, 'synthetic-sharing-password')
  await dialog.getByRole('switch', { name: 'Connection enabled', exact: true }).uncheck()
  await dialog.getByLabel('Synchronization interval (minutes)', { exact: true }).fill('45')
  await dialog.getByRole('button', { name: 'Create remote catalog', exact: true }).click()
  await expect(dialog).toBeHidden()
  const created = await source(page, 'interval-remote')
  expect(created.provider.catalog_sync).toEqual({ enabled: true, interval_minutes: 45 })
  expect(JSON.parse(created.json).schedule).toBeUndefined()
  await expect(page.getByText('Every 45 minutes', { exact: true })).toBeVisible()
  expect((await (await page.request.get('/api/schedules')).json()).items).toEqual([])
  await page.getByRole('link', { name: 'Shared catalogs', exact: true }).click()
  await expect(page).toHaveURL(/\/sharing$/)
  await expect(page.getByRole('tab', { name: 'Statistics', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('heading', { name: 'Shared catalogs', level: 2 })).toHaveCount(0)
  await page.getByRole('link', { name: 'Remote catalogs', exact: true }).click()
  await page.reload()
  await expect(page.getByRole('tab', { name: 'Statistics', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await dialog.getByLabel('Display name', { exact: true }).fill('Synthetic renamed remote')
  const session = await (await page.request.get('/api/session')).json()
  const concurrent = JSON.parse(created.json)
  concurrent.catalog_sync.interval_minutes = 90
  concurrent.page_size = 200
  const updated = await page.request.put('/api/providers/interval-remote', {
    headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
    data: { json: JSON.stringify(concurrent), revision: created.revision },
  })
  expect(updated.status()).toBe(200)
  await dialog.getByRole('button', { name: 'Save remote', exact: true }).click()
  await expect(dialog.getByRole('button', { name: 'Save remote', exact: true })).toBeDisabled()
  await dialog.getByRole('button', { name: 'Reload source, keep edits', exact: true }).click()
  await expect(dialog.getByLabel('Display name', { exact: true })).toHaveValue(
    'Synthetic renamed remote',
  )
  await expect(
    dialog.getByLabel('Synchronization interval (minutes)', { exact: true }),
  ).toHaveValue('90')
  await dialog.getByRole('button', { name: 'Save remote', exact: true }).click()
  await expect(dialog).toBeHidden()
  const renamed = await source(page, 'interval-remote')
  expect(renamed.provider.name).toBe('Synthetic renamed remote')
  expect(renamed.provider.catalog_sync).toEqual({ enabled: true, interval_minutes: 90 })
  expect(renamed.provider.page_size).toBe(200)
  expect(renamed.provider.auth.secret_ref).toBe(created.provider.auth.secret_ref)
  expect(JSON.parse(renamed.json).schedule).toBeUndefined()
  await page.goto('/providers/settings')
  await expect(page.getByText('Synthetic renamed remote', { exact: true })).toHaveCount(0)
  await page.goto('/schedules')
  await expect(page.getByText('Synthetic renamed remote', { exact: true })).toHaveCount(0)
  // Expert bookmarks continue to work and preserve the dedicated interval.
  await page.goto('/providers/interval-remote')
  await expect(page.getByRole('tab', { name: 'Schedules', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: /^Imports/ }).click()
  await expect(page.getByLabel('Timing', { exact: true })).toHaveCount(0)
  await page.getByLabel('Request interval', { exact: true }).selectOption('2s')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect
    .poll(async () => (await source(page, 'interval-remote')).provider.request_interval)
    .toBe('2s')
  expect((await source(page, 'interval-remote')).provider.catalog_sync).toEqual(
    renamed.provider.catalog_sync,
  )
})

test('legacy remote schedules convert to intervals without changing credentials or manual activation', async ({
  page,
  server,
}) => {
  await page.goto('/sharing/remotes')
  await page.getByRole('button', { name: 'Add remote catalog', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Local source identifier', { exact: true }).fill('legacy-remote')
  await dialog.getByLabel('Display name', { exact: true }).fill('Synthetic legacy remote')
  await dialog
    .getByLabel('HTTPS catalog URL', { exact: true })
    .fill('https://catalog.example/api/catalogs/legacy')
  await addRemotePassword(page, 'synthetic-legacy-password')
  await dialog.getByRole('switch', { name: 'Connection enabled', exact: true }).uncheck()
  await dialog.getByRole('button', { name: 'Create remote catalog', exact: true }).click()
  await expect(dialog).toBeHidden()
  const initial = await source(page, 'legacy-remote')
  const legacy = JSON.parse(initial.json)
  delete legacy.catalog_sync
  legacy.schedule = {
    enabled: false,
    cron: '0 6 * * *',
    timezone: 'Europe/Paris',
    mode: 'full',
    max_pages: 4,
  }
  const session = await (await page.request.get('/api/session')).json()
  const update = await page.request.put('/api/providers/legacy-remote', {
    headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
    data: { json: JSON.stringify(legacy), revision: initial.revision },
  })
  expect(update.status()).toBe(200)
  await page.reload()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(
    dialog.getByRole('switch', { name: 'Automatic synchronization', exact: true }),
  ).not.toBeChecked()
  await expect(
    dialog.getByLabel('Synchronization interval (minutes)', { exact: true }),
  ).toHaveValue('1440')
  await dialog.getByLabel('Display name', { exact: true }).fill('Converted remote')
  await dialog.getByRole('button', { name: 'Save remote', exact: true }).click()
  await expect(dialog).toBeHidden()
  const saved = await source(page, 'legacy-remote')
  expect(saved.provider.catalog_sync).toEqual({ enabled: false, interval_minutes: 1440 })
  expect(JSON.parse(saved.json).schedule).toBeUndefined()
  expect(saved.provider.auth.secret_ref).toBe(initial.provider.auth.secret_ref)
  expect(saved.provider.enabled).toBe(false)
  expect((await (await page.request.get('/api/schedules')).json()).items).toEqual([])
})

test.describe('real local imports', () => {
  test.use({ collectionSource: true })
  test('simple import actions preserve Preview, initial Full, Incremental and explicit rebuild behavior', async ({
    page,
    server,
  }) => {
    test.setTimeout(90_000)
    await createSource(page, server.sourceURL, 'action-source', {
      enabled: true,
      pagination: { type: 'none' },
      http: { items_path: '/items' },
      mapping: { id: '/id', fields: { title: '/title', info_hash: '/info_hash', size: '/size' } },
    })
    for (const [index, action] of ['preview', 'incremental', 'incremental', 'full'].entries()) {
      await page.goto('/providers/settings')
      await page.getByText('Manual actions', { exact: true }).click()
      await page
        .locator('details')
        .filter({ has: page.getByText('Manual actions', { exact: true }) })
        .getByRole('button', { name: 'Run manual import', exact: true })
        .click()
      const dialog = page.getByRole('dialog')
      await dialog.getByLabel('Source', { exact: true }).selectOption('action-source')
      await dialog.getByLabel('Action', { exact: true }).selectOption(action)
      await expect(
        dialog.getByRole('option', { name: 'Fetch source details', exact: true }),
      ).toBeDisabled()
      if (index === 1) {
        await expect(
          dialog.getByText('The first update automatically', { exact: false }),
        ).toBeVisible()
        await dialog.getByText('Advanced import settings', { exact: true }).click()
        await dialog.getByLabel('Page budget per attempt', { exact: true }).fill('1')
        await dialog.getByLabel('Duration budget per attempt (seconds)', { exact: true }).fill('30')
        await dialog.getByLabel('Known pages (X)', { exact: true }).fill('1')
      }
      const response = page.waitForResponse(
        (value) =>
          new URL(value.url()).pathname === '/api/runs' && value.request().method() === 'POST',
      )
      await dialog.getByRole('button', { name: 'Run manual import', exact: true }).click()
      const created = await response
      expect(created.status()).toBe(202)
      const request = created.request().postDataJSON()
      expect(request.mode).toBe(action)
      if (index === 1)
        expect(request).toMatchObject({ max_pages: 1, max_duration_seconds: 30, known_pages: 1 })
      else expect(request.max_pages).toBeUndefined()
      const run = await created.json()
      expect(run.mode).toBe(index === 1 ? 'full' : action)
      expect(Boolean(run.initial_full)).toBe(index === 1)
      await expect
        .poll(async () => {
          const state = await (await page.request.get(`/api/runs/${run.id}`)).json()
          if (state.status === 'failed') {
            const events = await (await page.request.get(`/api/runs/${run.id}/events`)).json()
            throw new Error(
              `Local fixture import failed: ${JSON.stringify({ error: state.error, events: events.items?.map((event: { kind: string; message: string }) => ({ kind: event.kind, message: event.message })) })}`,
            )
          }
          return state.status
        })
        .toBe('succeeded')
      expect((await (await page.request.get('/api/torrents')).json()).total).toBe(
        index === 0 ? 0 : 1,
      )
    }
    expect(server.sourceRequests()).toBe(4)
  })
})
