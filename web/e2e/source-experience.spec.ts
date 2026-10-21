import pg from 'pg'
import { test, expect, createStremioAccess } from './fixtures'

test('manual review selects a local work, protects drafts, publishes it and supports undo', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const status = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: { ...status.config, auto_sync: false, auto_match: false },
      })
    ).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','101','tt1234567',ARRAY[2021]),('movie','102','tt2345678',ARRAY[2024]);
  INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,CASE tmdb_id WHEN '101' THEN 'Example Film' ELSE 'Example Film Returns' END,CASE tmdb_id WHEN '101' THEN 'example film' ELSE 'example film returns' END FROM ingest.aio_works;
  INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example','review-item','{"title":"Example.Film.2021.1080p","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","download_url":"https://source.invalid/synthetic-private-value"}');
  DELETE FROM ingest.aio_match_queue;
  INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,status,reason) SELECT provider_id,source_id,md5(fields::text),'ambiguous','multiple plausible works' FROM ingest.torrents;`)
  } finally {
    await db.end()
  }
  await page.goto('/metadata/identification')
  await page.getByRole('button', { name: 'Review Example.Film.2021.1080p', exact: true }).click()
  let dialog = page.getByRole('dialog')
  await expect(dialog.getByLabel('Year', { exact: true })).toHaveValue('2021')
  await expect(
    dialog.getByRole('radio', { name: 'Choose Example Film', exact: true }),
  ).toBeVisible()
  await dialog.getByRole('radio', { name: 'Choose Example Film', exact: true }).check()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect
    .poll(() =>
      dialog.evaluate((element) => {
        const r = element.getBoundingClientRect()
        return r.x >= 0 && r.y >= 0 && r.right <= innerWidth && r.bottom <= innerHeight
      }),
    )
    .toBe(true)
  await expect
    .poll(() =>
      dialog
        .getByLabel('Title, IMDb, TMDB or TVDB ID', { exact: true })
        .evaluate((element) => element.getBoundingClientRect().width),
    )
    .toBeGreaterThan(300)
  await dialog.getByRole('button', { name: 'Close', exact: true }).click()
  await expect(page.getByRole('alertdialog')).toContainText('identification review')
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click()
  const saving = page.waitForResponse(
    (r) =>
      r.request().method() === 'PUT' &&
      new URL(r.url()).pathname.startsWith('/api/aiosource/matches/'),
  )
  await dialog.getByRole('button', { name: 'Confirm identification', exact: true }).click()
  expect((await saving).status()).toBe(204)
  await expect(dialog).toBeHidden()
  await page.getByLabel('Identification result', { exact: true }).selectOption('matched')
  await page
    .getByRole('form', { name: 'Identification filters', exact: true })
    .getByRole('button', { name: 'Search', exact: true })
    .click()
  await expect(page.locator('#matching-results')).toContainText('tt1234567')
  const { manifestPath } = await createStremioAccess(page)
  const streams = await page.request.get(
    manifestPath.replace('/manifest.json', '/stream/movie/tt1234567.json'),
  )
  expect(streams.status()).toBe(200)
  expect((await streams.json()).streams).toHaveLength(1)
  await page.getByRole('button', { name: 'Review Example.Film.2021.1080p', exact: true }).click()
  dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: 'Use automatic matching', exact: true }).click()
  const undoing = page.waitForResponse(
    (r) =>
      r.request().method() === 'PUT' &&
      new URL(r.url()).pathname.startsWith('/api/aiosource/matches/'),
  )
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Use automatic matching', exact: true })
    .click()
  const undoResponse = await undoing
  expect(undoResponse.status()).toBe(204)
  expect(undoResponse.request().postDataJSON().automatic).toBe(true)
  await expect(dialog).toBeHidden()
  const diagnostics = await (await page.request.get('/api/aiosource/matches')).json()
  expect(diagnostics.items[0].status).toBe('pending')
  expect(JSON.stringify(diagnostics)).not.toContain('synthetic-private-value')
})

test('Stremio statistics and request details show actual results and preserve filtered history', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results)
  VALUES(now(),'movie','imdb:tt1234567',21,'results',3,2,1,0,1,0,1,false,'[{"name":"Example result","provider":"example","source_id":"item","title":"Example Film","service":"alldebrid","cache":"cached","filename":"example.mp4","size":1024}]'),
  (now(),'series','imdb:tt2345678:1:4',18,'empty',0,0,0,0,0,0,0,false,'[]')`)
  } finally {
    await db.end()
  }
  await page.goto('/addon')
  await expect(
    page.getByRole('tablist', { name: 'Stremio views', exact: true }).getByRole('tab'),
  ).toHaveText(['Statistics', 'Settings', 'History'])
  await page.getByRole('link', { name: 'View requests without results', exact: true }).click()
  await expect(page).toHaveURL(/\/addon\/history\?status=empty$/)
  await expect(page.getByRole('link', { name: 'imdb:tt2345678:1:4', exact: true })).toBeVisible()
  await page.goto('/addon/history?status=results&q=1234567')
  const history = page.url()
  await page.getByRole('link', { name: 'imdb:tt1234567', exact: true }).click()
  await expect(page.getByText('Example result', { exact: true })).toBeVisible()
  await expect(page.locator('main')).toContainText('Invalid or missing info hash')
  await expect(page.getByText('Cached', { exact: true })).toHaveAttribute('data-variant', 'success')
  await page.reload()
  await page.getByRole('link', { name: 'Stremio history', exact: true }).click()
  await expect(page).toHaveURL(history)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('link', { name: 'imdb:tt1234567', exact: true }).click()
  await expect(page.getByText('Example result', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('source setup keeps essentials visible and advanced configuration lossless', async ({
  page,
  server,
}) => {
  await page.goto('/providers/new')
  const source = {
    version: 1,
    id: 'example-options',
    name: 'Example options',
    adapter: 'torznab',
    enabled: false,
    url: server.sourceURL,
    page_size: 100,
    auth: { type: 'none' },
    options: {
      search_mode: 'tvsearch',
      category_scope: 'each',
      id_source: 'link',
      id_pattern: '/items/([0-9]+)',
    },
    search: { categories: [5000] },
    output: { fields: ['title', 'info_hash'] },
  }
  await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
  await page.getByLabel('Import a source JSON definition', { exact: true }).setInputFiles({
    name: 'example-source.json',
    mimeType: 'application/json',
    buffer: Buffer.from(JSON.stringify(source)),
  })
  await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await expect(page.getByLabel('Search mode', { exact: true })).toHaveValue('tvsearch')
  await expect(page.getByLabel('Category requests', { exact: true })).toHaveValue('each')
  await expect(page.getByLabel('Normalized catalog', { exact: true })).toBeHidden()
  await page.getByRole('tab', { name: /^Pagination/ }).click()
  await expect(page.getByLabel('Page size', { exact: true })).toHaveValue('100')
  await page.getByRole('tab', { name: /^Source fields/ }).click()
  await expect(page.getByLabel('Native ID pattern', { exact: true })).toBeHidden()
  await page.getByText('Native identity', { exact: true }).click()
  await expect(page.getByLabel('Native ID pattern', { exact: true })).toHaveValue('/items/([0-9]+)')
  await page.getByLabel('Native ID pattern', { exact: true }).fill('/entries/([0-9]+)')
  await page.getByRole('button', { name: 'JSON', exact: true }).click()
  const downloading = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export JSON', exact: true }).click()
  const download = await downloading
  try {
    const stream = await download.createReadStream()
    const chunks: Buffer[] = []
    for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
    const exported = JSON.parse(Buffer.concat(chunks).toString())
    expect(exported.options.id_pattern).toBe('/entries/([0-9]+)')
    expect(exported.options.search_mode).toBe('tvsearch')
    expect(exported.output).toEqual(source.output)
  } finally {
    await download.delete()
  }
  expect(server.sourceRequests()).toBe(0)
})

test('global history details redirect without requesting a global diagnostic scan', async ({
  page,
}) => {
  const rawRequests: string[] = []
  page.on('request', (r) => {
    if (new URL(r.url()).pathname === '/api/raw') rawRequests.push(r.url())
  })
  await page.goto('/raw?status=error')
  await expect(page).toHaveURL(/\/runs$/)
  await expect(page.getByRole('navigation', { name: 'History views', exact: true })).toHaveCount(0)
  expect(rawRequests).toEqual([])
  expect((await page.request.get('/api/raw')).status()).toBe(400)
})
