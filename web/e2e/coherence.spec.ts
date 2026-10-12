import pg from 'pg'
import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import type { TestServer } from './server'

async function reviewFixture(page: Page, server: TestServer) {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const initial = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: { ...initial.config, auto_match: false },
      })
    ).status(),
  ).toBe(204)
  expect(
    (
      await page.request.put('/api/providers/review-fixture', {
        headers,
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'review-fixture',
            name: 'Review fixture',
            adapter: 'http_json',
            url: server.sourceURL,
            enabled: false,
          }),
        },
      })
    ).status(),
  ).toBe(200)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`
      INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years)
        VALUES('movie','9001','tt1000001',ARRAY[2024]);
      INSERT INTO ingest.aio_titles(work_id,title,normalized)
        SELECT id,'Review Example','review example' FROM ingest.aio_works;
      INSERT INTO ingest.torrents(provider_id,source_id,fields)
        VALUES('review-fixture','one','{"title":"Review Example 2024"}');
      INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,revision,status,reason)
        SELECT provider_id,source_id,md5(fields::text),1,'ambiguous','multiple plausible works'
        FROM ingest.torrents;
      DELETE FROM ingest.aio_match_queue;
    `)
  } finally {
    await db.end()
  }
  await page.goto('/metadata/identification')
  await page.getByRole('button', { name: 'Review Review Example 2024', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Review identification', exact: true })
  await editor.getByRole('radio', { name: 'Choose Review Example', exact: true }).check()
  return editor
}

test('reviewing changed metadata clears the old choice and refreshes candidate revisions', async ({
  page,
  server,
}) => {
  const editor = await reviewFixture(page, server)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query('UPDATE ingest.aio_works SET revision=revision+1')
  } finally {
    await db.end()
  }
  const rejection = page.waitForResponse(
    (response) =>
      response.url().includes('/api/aiosource/matches/') && response.request().method() === 'PUT',
  )
  await editor.getByRole('button', { name: 'Confirm identification', exact: true }).click()
  expect((await rejection).status()).toBe(409)
  await editor.getByRole('button', { name: 'Review latest data', exact: true }).click()
  const confirmation = page.getByRole('alertdialog')
  await confirmation.getByRole('button', { name: 'Load current information', exact: true }).click()
  await expect(confirmation).toHaveCount(0)
  await expect(
    editor.getByRole('button', { name: 'Confirm identification', exact: true }),
  ).toBeDisabled()
  const candidate = editor.getByRole('radio', { name: 'Choose Review Example', exact: true })
  await expect(candidate).not.toBeChecked()
  await candidate.check()
  const saved = page.waitForResponse(
    (response) =>
      response.url().includes('/api/aiosource/matches/') && response.request().method() === 'PUT',
  )
  await editor.getByRole('button', { name: 'Confirm identification', exact: true }).click()
  expect((await saved).status()).toBe(204)
  await expect(editor).toHaveCount(0)
  await page.getByLabel('Identification result', { exact: true }).selectOption('matched')
  await expect(page.getByRole('row').filter({ hasText: 'Review Example 2024' })).toContainText(
    'Identified',
  )
})

test('a failed review reload keeps its confirmation and selection until it can be retried', async ({
  page,
  server,
}) => {
  const editor = await reviewFixture(page, server)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query('UPDATE ingest.aio_works SET revision=revision+1')
  } finally {
    await db.end()
  }
  await editor.getByRole('button', { name: 'Confirm identification', exact: true }).click()
  await expect(
    editor.getByRole('button', { name: 'Review latest data', exact: true }),
  ).toBeVisible()
  let fail = true
  await page.route('**/api/aiosource/matches/*', (route) => {
    if (fail && route.request().method() === 'GET')
      return route.fulfill({
        status: 503,
        json: { error: 'The test storage is temporarily unavailable.' },
      })
    return route.continue()
  })
  await editor.getByRole('button', { name: 'Review latest data', exact: true }).click()
  const confirmation = page.getByRole('alertdialog')
  await confirmation.getByRole('button', { name: 'Load current information', exact: true }).click()
  await expect(confirmation.getByRole('alert')).toContainText('temporarily unavailable')
  await expect(
    page.getByRole('radio', { name: 'Choose Review Example', exact: true, includeHidden: true }),
  ).toBeChecked()
  fail = false
  await confirmation.getByRole('button', { name: 'Load current information', exact: true }).click()
  await expect(confirmation).toHaveCount(0)
  await expect(
    editor.getByRole('button', { name: 'Confirm identification', exact: true }),
  ).toBeDisabled()
  // Cancel only after the successful, explicit reload discarded the old selection.
  await editor.getByRole('button', { name: 'Close', exact: true }).click()
})

test('identification search distinguishes unavailable results from an empty or incomplete query', async ({
  page,
  server,
}) => {
  const editor = await reviewFixture(page, server)
  const query = editor.getByLabel('Title, IMDb, TMDB or TVDB ID', { exact: true })
  await query.fill('x'.repeat(201))
  await editor.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(editor.getByRole('alert')).toBeVisible()
  await expect(
    editor.getByRole('heading', { name: 'No candidates in this view', exact: true }),
  ).toHaveCount(0)
  await query.fill('a')
  await editor.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(
    editor.getByRole('status').filter({ hasText: 'Enter at least 3 characters' }),
  ).toBeVisible()
  await expect(
    editor.getByRole('heading', { name: 'No candidates in this view', exact: true }),
  ).toHaveCount(0)
  await query.fill('Nonexistent synthetic title')
  await editor.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(
    editor.getByRole('heading', { name: 'No candidates in this view', exact: true }),
  ).toBeVisible()
  await editor.getByRole('button', { name: 'Close', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Discard changes', exact: true })
    .click()
})

test('failed candidate loading does not discard a review when the source record also changed', async ({
  page,
  server,
}) => {
  const editor = await reviewFixture(page, server)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`UPDATE ingest.torrents SET fields=fields || '{"size":1024}'::jsonb`)
  } finally {
    await db.end()
  }
  await editor.getByRole('button', { name: 'Confirm identification', exact: true }).click()
  await expect(
    editor.getByRole('button', { name: 'Review latest data', exact: true }),
  ).toBeVisible()
  let fail = true
  await page.route(/\/api\/aiosource\/candidates\?/, (route) =>
    fail
      ? route.fulfill({ status: 503, json: { error: 'Candidates are temporarily unavailable.' } })
      : route.continue(),
  )
  await editor.getByRole('button', { name: 'Review latest data', exact: true }).click()
  const confirmation = page.getByRole('alertdialog')
  await confirmation.getByRole('button', { name: 'Load current information', exact: true }).click()
  await expect(confirmation.getByRole('alert')).toContainText('temporarily unavailable')
  await expect(
    page.getByRole('radio', { name: 'Choose Review Example', exact: true, includeHidden: true }),
  ).toBeChecked()
  fail = false
  await confirmation.getByRole('button', { name: 'Load current information', exact: true }).click()
  await expect(confirmation).toHaveCount(0)
  await expect(
    editor.getByRole('button', { name: 'Confirm identification', exact: true }),
  ).toBeDisabled()
})

test('public access reviews a competing revision while preserving the local choice', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  await page.goto('/addon/settings')
  const form = page.getByRole('form', { name: 'Public access', exact: true })
  const choice = form.getByRole('switch')
  await choice.check()
  const initial = await (await page.request.get('/api/stremio/public-access')).json()
  const refresh = page.waitForResponse(async (response) => {
    if (
      !response.url().endsWith('/api/stremio/public-access') ||
      response.request().method() !== 'GET'
    )
      return false
    return (await response.json()).revision > initial.revision
  })
  expect(
    (
      await page.request.put('/api/stremio/public-access', {
        headers,
        data: { revision: initial.revision, enabled: false },
      })
    ).status(),
  ).toBe(204)
  await refresh
  const reload = form.getByRole('button', { name: 'Load latest settings', exact: true })
  await expect(reload).toBeVisible()
  await expect(choice).toBeChecked()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await reload.click()
  await expect(reload).toHaveCount(0)
  await expect(choice).toBeChecked()
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await (await page.request.get('/api/stremio/public-access')).json()).enabled).toBe(true)
})

test.describe('browser local dates', () => {
  test.use({ timezoneId: 'Europe/Paris' })

  test('log filters retain unchanged instants and compare different UTC offsets chronologically', async ({
    page,
  }) => {
    const from = '2026-10-25T02:15:10.123+02:00'
    const to = '2026-10-25T01:30:10.456Z'
    const filter = new URLSearchParams({ from, to })
    await page.goto(`/logs?${filter}`)
    await expect(page.getByLabel('From (browser local time)', { exact: true })).toHaveValue(
      '2026-10-25T02:15',
    )
    await expect(page.getByLabel('To (browser local time)', { exact: true })).toHaveValue(
      '2026-10-25T02:30',
    )
    await page.getByLabel('Source or service', { exact: true }).selectOption('service:tmdb')
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click()
    await expect(page).toHaveURL(
      (url) =>
        url.searchParams.get('from') === from &&
        url.searchParams.get('to') === to &&
        url.searchParams.get('service') === 'tmdb',
    )
    await page.getByLabel('From (browser local time)', { exact: true }).fill('2026-10-25T04:00')
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText(
      'The start time must be before the end time.',
    )
    expect(new URL(page.url()).searchParams.get('from')).toBe(from)
  })

  test('an unrelated share edit retains expiration precision during a repeated daylight-saving hour', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const expires = '2026-10-25T01:30:10.123Z'
    const created = await page.request.post('/api/shares', {
      headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
      data: {
        name: 'Expiration fixture',
        enabled: false,
        scope: 'all',
        source_ids: [],
        fields: ['title'],
        expires_at: expires,
        requests_per_minute: 120,
        max_concurrent_downloads: 2,
      },
    })
    expect(created.status()).toBe(201)
    await page.goto('/sharing')
    await page.getByRole('button', { name: 'Edit permissions & limits', exact: true }).click()
    const editor = page.getByRole('dialog')
    await expect(editor.getByLabel('Expires at (browser local time)', { exact: true })).toHaveValue(
      '2026-10-25T02:30:10',
    )
    await editor.getByLabel('Recipient / share name', { exact: true }).fill('Expiration renamed')
    await editor.getByLabel('I authorize access to all current and future sources.').check()
    await editor.getByRole('button', { name: 'Save permissions', exact: true }).click()
    await expect(editor).toHaveCount(0)
    const shares = await (await page.request.get('/api/shares')).json()
    expect(shares.items).toHaveLength(1)
    expect(Date.parse(shares.items[0].expires_at)).toBe(Date.parse(expires))
  })
})
