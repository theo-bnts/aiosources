import pg from 'pg'
import { test, expect, createStremioAccess } from './fixtures'

test('save feedback distinguishes reverted controls from committed changes', async ({ page }) => {
  const writes: string[] = []
  page.on('request', (request) => {
    if (request.method() === 'PUT') writes.push(new URL(request.url()).pathname)
  })
  await page.goto('/metadata/settings')
  const configuration = page.getByRole('form', { name: 'Metadata configuration', exact: true })
  const interval = configuration.getByLabel('Information update interval (minutes)', {
    exact: true,
  })
  const original = await interval.inputValue()
  const changed = original === '30' ? '45' : '30'
  const save = configuration.getByRole('button', { name: 'Save', exact: true })
  await expect(configuration.getByRole('status')).toHaveText('No unsaved changes')
  await interval.fill(changed)
  await expect(configuration.getByRole('status')).toHaveText('Unsaved changes')
  await interval.fill(original)
  await expect(save).toBeDisabled()
  await expect(configuration.getByRole('status')).toHaveText('No unsaved changes')
  expect(writes).toEqual([])
  await interval.fill(changed)
  await save.click()
  await expect(configuration.getByRole('status')).toHaveText('Changes saved.')
  await page.reload()
  await expect(interval).toHaveValue(changed)
  expect(writes).toEqual(['/api/aiosource/config'])

  await page.goto('/import-settings')
  const advanced = page.getByRole('form', { name: 'Advanced settings', exact: true })
  const workers = advanced.getByLabel('Simultaneous imports', { exact: true })
  const originalWorkers = await workers.inputValue()
  await workers.fill(originalWorkers === '7' ? '6' : '7')
  await expect(advanced.getByRole('button', { name: 'Discard changes', exact: true })).toHaveCount(
    0,
  )
  await workers.fill(originalWorkers)
  await expect(workers).toHaveValue(originalWorkers)
  await expect(advanced.getByRole('status')).toHaveText('No unsaved changes')
})

test('clipboard failure is recoverable and replacing the manifest clears old copy confirmation', async ({
  page,
}) => {
  const errors: Error[] = []
  page.on('pageerror', (error) => errors.push(error))
  await page.addInitScript(() => {
    const state = { fail: true, value: '' }
    Object.defineProperty(window, '__clipboardTest', { value: state })
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (value: string) => {
          if (state.fail) throw new Error('Synthetic clipboard refusal')
          state.value = value
        },
      },
    })
  })
  await createStremioAccess(page)
  await page.goto('/addon/settings')
  const rotate = async () => {
    await page
      .getByRole('region', { name: 'Access Test installation', exact: true })
      .getByRole('button', { name: 'Replace installation key', exact: true })
      .click()
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Replace installation key', exact: true })
      .click()
    await expect(page.getByLabel('Manifest URL', { exact: true })).toBeVisible()
  }
  await rotate()
  const copy = page.getByRole('button', { name: 'Copy manifest URL', exact: true })
  const manifestField = page.getByLabel('Manifest URL', { exact: true })
  await copy.click()
  await expect(page.getByRole('alert')).toHaveText(
    'Clipboard access is unavailable. Reveal the manifest URL and copy it manually.',
  )
  await expect(manifestField).toHaveAttribute('readonly', '')
  await page.evaluate(() => {
    ;(window as unknown as { __clipboardTest: { fail: boolean } }).__clipboardTest.fail = false
  })
  await copy.click()
  await expect(
    page.getByText('Copied to your clipboard. Keep it private.', { exact: true }),
  ).toBeVisible()
  expect(
    await page.evaluate(
      () => (window as unknown as { __clipboardTest: { value: string } }).__clipboardTest.value,
    ),
  ).toBe(await manifestField.inputValue())
  await page.getByRole('button', { name: 'I have saved the manifest URL', exact: true }).click()
  await rotate()
  await expect(
    page.getByText('Copied to your clipboard. Keep it private.', { exact: true }),
  ).toHaveCount(0)
  await expect(copy).toHaveText('Copy manifest URL')
  await copy.click()
  expect(
    await page.evaluate(
      () => (window as unknown as { __clipboardTest: { value: string } }).__clipboardTest.value,
    ),
  ).toBe(await manifestField.inputValue())
  expect(errors).toEqual([])
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('empty histories and searches return to the first page without losing filters or view context', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,finished_at)
      VALUES('retained-metadata','metadata','tmdb','manual',0,'succeeded',now())`)
  } finally {
    await db.end()
  }
  const views = [
    ['/runs', 'status=failed'],
    ['/metadata/history', 'status=succeeded'],
    ['/debrid/history', 'service=torbox'],
    ['/logs', 'service=tmdb'],
    ['/addon/history', 'status=empty'],
    ['/torrents', 'q=nonexistent'],
  ]
  for (const [route, filter] of views) {
    await page.goto(`${route}?${filter}&offset=1000000&limit=25`)
    await page.getByRole('button', { name: 'Return to first page', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.has('offset')).toBe(false)
    let params = new URL(page.url()).searchParams
    const [key, value] = filter.split('=')
    expect(params.get(key)).toBe(value)
    expect(params.get('limit')).toBe('25')
    if (route === '/metadata/history')
      await expect(
        page.getByRole('link', { name: 'Information update', exact: true }),
      ).toBeVisible()
    await page
      .locator('main')
      .getByRole('button', { name: 'Clear filters', exact: true })
      .first()
      .click()
    await expect.poll(() => new URL(page.url()).searchParams.has(key)).toBe(false)
    params = new URL(page.url()).searchParams
    expect(params.get('limit')).toBe('25')
  }
  await page.goto('/metadata/history?offset=1000000')
  await expect(page.getByText('No results on this page · 1 total', { exact: true })).toBeVisible()
  await page.goto('/logs')
  const query = page.getByLabel('Search text', { exact: true })
  await query.fill('unapplied search')
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(query).toHaveValue('')
  await page.goto('/torrents')
  const title = page.getByLabel('Search text', { exact: true })
  await title.fill('unapplied title')
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(title).toHaveValue('')
})

test('getting started finishes after identification even when no safe match exists', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const current = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: { ...current.config, auto_sync: false, auto_match: false },
      })
    ).status(),
  ).toBe(204)
  expect(
    (
      await page.request.put('/api/providers/ux-source', {
        headers,
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'ux-source',
            name: 'Example source',
            adapter: 'http_json',
            enabled: true,
            url: server.sourceURL,
          }),
        },
      })
    ).status(),
  ).toBe(200)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years)
      VALUES('movie','101','tt7654321','{2021}');
      INSERT INTO ingest.aio_titles(work_id,title,normalized)
      SELECT id,'Unrelated Film','unrelated film' FROM ingest.aio_works;
      INSERT INTO ingest.torrents(provider_id,source_id,fields)
      VALUES('ux-source','one','{"title":"Unknown release 2020","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`)
  } finally {
    await db.end()
  }
  expect((await page.request.post('/api/aiosource/match', { headers })).status()).toBe(204)
  await expect
    .poll(async () => {
      const data = await (await page.request.get('/api/aiosource')).json()
      return !data.matching && data.status.matching_pending === 0 && data.status.unmatched === 1
    })
    .toBe(true)
  await page.goto('/addon/settings')
  await expect(page.getByRole('heading', { name: 'Stremio', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Getting started', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Create access', exact: true })).toBeVisible()
  expect((await (await page.request.get('/api/aiosource')).json()).config.torbox_secret).toBe('')
})
