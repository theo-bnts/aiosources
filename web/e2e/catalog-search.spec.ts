import type { Page } from '@playwright/test'
import pg from 'pg'
import { test, expect } from './fixtures'
import type { TestServer } from './server'

async function seedCatalog(page: Page, server: TestServer, debrid = false) {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  for (const [id, name] of [
    ['alpha', 'Alpha source'],
    ['zulu', 'Zulu source'],
  ]) {
    expect(
      (
        await page.request.put(`/api/providers/${id}`, {
          headers,
          data: {
            revision: '',
            json: JSON.stringify({
              version: 1,
              id,
              name,
              adapter: 'http_json',
              enabled: false,
              url: server.sourceURL,
            }),
          },
        })
      ).status(),
    ).toBe(200)
  }
  const status = await (await page.request.get('/api/aiosource')).json()
  status.config.auto_sync = false
  status.config.auto_match = false
  status.config.trackers = {
    alpha: { private: true, announce_secret: '' },
    zulu: { private: false, announce_secret: '' },
  }
  if (debrid) {
    expect(
      (
        await page.request.put('/api/secrets/catalog_fixture', {
          headers,
          data: { revision: 0, value: 'synthetic-never-sent-catalog-key' },
        })
      ).status(),
    ).toBe(204)
    status.config.torbox_secret = 'catalog_fixture'
  }
  expect(
    (await page.request.put('/api/aiosource/config', { headers, data: status.config })).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years,display_title)
      VALUES('movie','101','tt1234567',ARRAY[2026],'Example Film: New Day'),('tv','202','tt2345678',ARRAY[2026],'Example Series');
      INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,display_title,CASE WHEN media_type='movie' THEN 'example film new day' ELSE 'example series' END FROM ingest.aio_works;
      INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
      ('alpha','film-one','{"title":"Example-Film.New.Day.2026.1080p","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":100,"imdb_id":"tt1234567"}'),
      ('zulu','film-two','{"title":"Example-Film.New.Day.2026.2160p","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":200,"imdb_id":"tt1234567"}'),
      ('alpha','series-one','{"title":"Example.Series.S02E03.1080p","info_hash":"cccccccccccccccccccccccccccccccccccccccc","size":100,"imdb_id":"tt2345678"}');
      INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
      SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','native identifier' FROM ingest.torrents t JOIN ingest.aio_works w ON w.imdb_id=t.fields->>'imdb_id';
      DELETE FROM ingest.aio_match_queue;
      INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) VALUES
      ('torbox','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',true,now(),'[{"path":"Example.Film.New.Day.2026.mkv","size":100}]'),
      ('torbox','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',false,now(),'[]'),
      ('torbox','cccccccccccccccccccccccccccccccccccccccc',true,now(),'[{"path":"Example.Series.S02E03.mkv","size":100}]');`)
    const works = await db.query('SELECT id,media_type FROM ingest.aio_works')
    return Object.fromEntries(works.rows.map((row) => [row.media_type, String(row.id)])) as Record<
      string,
      string
    >
  } finally {
    await db.end()
  }
}

test('all three searches accept spaced and dotted title fragments and show totals', async ({
  page,
  server,
}) => {
  await seedCatalog(page, server)
  for (const q of [
    'Example-Film.New.Day',
    'Example.Film.New.Day',
    'Example Film New Day',
    'Example-Film New Day',
    'xample New',
  ]) {
    await page.goto(`/search?q=${encodeURIComponent(q)}`)
    await expect(page.locator('[data-result-count]')).toHaveText('1 title')
    await page.goto(`/torrents?q=${encodeURIComponent(q)}`)
    await expect(page.locator('[data-result-count]')).toHaveText('2 torrents')
    await expect(page.locator('main tbody tr')).toHaveCount(2)
    await page.goto('/metadata/identification?match_status=all')
    const input = page.getByLabel('Title, source ID or info hash', { exact: true })
    await expect(input).toHaveAttribute(
      'placeholder',
      'Search title fragments, source IDs or info hashes',
    )
    await input.fill(q)
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('[data-result-count]')).toHaveText('2 torrents')
    await expect(page.locator('main tbody tr')).toHaveCount(2)
  }
  await page.getByLabel('Identification result', { exact: true }).selectOption('matched')
  await page
    .getByLabel('Title, source ID or info hash', { exact: true })
    .fill('example film new day')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.locator('[data-result-count]')).toHaveText('2 torrents')
  await expect(page.locator('main tbody tr')).toHaveCount(2)
})

test('catalog search groups clean titles, sorts source versions and retains the list when returning', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  await page.goto('/search')
  await page.getByLabel('Title or IMDb / TMDB ID', { exact: true }).fill('example new day')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.locator('[data-result-count]')).toHaveText('1 title')
  await expect(page.locator('main tbody tr')).toHaveCount(1)
  await page.getByRole('link', { name: 'Example Film: New Day', exact: true }).click()
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === `/search/${ids.movie}` && url.searchParams.get('q') === 'example new day',
  )
  await expect(page.getByRole('switch', { name: 'Cached results only', exact: true })).toBeChecked()
  await expect(page.locator('[data-result-count]')).toHaveText('1 source release')
  await expect(page.getByRole('button', { name: 'Add to debrid', exact: true })).toHaveCount(0)
  await page.getByRole('switch', { name: 'Cached results only', exact: true }).uncheck()
  await expect(page.locator('[data-result-count]')).toHaveText('2 source releases')
  await page.reload()
  await expect(
    page.getByRole('switch', { name: 'Cached results only', exact: true }),
  ).not.toBeChecked()
  await expect(page.locator('[data-result-count]')).toHaveText('2 source releases')
  const filters = page.getByRole('region', { name: 'Source release filters', exact: true })
  await expect(filters.getByLabel('Sort versions', { exact: true })).toHaveValue('size_desc')
  await expect(filters.getByRole('switch', { name: 'Filter languages', exact: true })).toBeChecked()
  await expect(page.getByRole('button', { name: 'Download', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add to debrid', exact: true })).toBeVisible()
  await page.getByLabel('Sort versions', { exact: true }).selectOption('size_desc')
  await expect(page.locator('main tbody tr').first()).toContainText('2160p')
  await page.getByLabel('Source', { exact: true }).selectOption('alpha')
  await expect(page.locator('[data-result-count]')).toHaveText('1 source release')
  await expect(page.getByLabel('Source', { exact: true }).getByRole('option')).toHaveText([
    'All sources',
    'Alpha source',
    'Zulu source',
  ])
  await page.getByRole('main').getByRole('link', { name: 'Search', exact: true }).click()
  await expect(page).toHaveURL(
    (url) => url.pathname === '/search' && url.searchParams.get('q') === 'example new day',
  )
  await expect(page.getByLabel('Title or IMDb / TMDB ID', { exact: true })).toHaveValue(
    'example new day',
  )
  await page.goto(`/search/${ids.tv}?cached_only=false`)
  await expect(page.getByLabel('Season', { exact: true })).toHaveValue('2')
  await expect(page.getByLabel('Episode', { exact: true })).toHaveValue('3')
  await expect(page.locator('[data-result-count]')).toHaveText('1 source release')
  await page.getByLabel('Episode', { exact: true }).fill('4')
  await page.getByRole('button', { name: 'Show episode', exact: true }).click()
  await expect(page.locator('[data-result-count]')).toHaveText('0 source releases')
  await expect(page.getByRole('button', { name: 'Download', exact: true })).toHaveCount(0)
})

test('catalog year is a single optional filter and survives opening a title and returning', async ({
  page,
  server,
}) => {
  await seedCatalog(page, server)
  await page.goto('/search')
  await page.getByLabel('Title or IMDb / TMDB ID', { exact: true }).fill('example')
  await page.getByLabel('Year', { exact: true }).fill('2025')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.locator('[data-result-count]')).toHaveText('0 titles')
  await page.getByLabel('Year', { exact: true }).fill('2026')
  await page.getByLabel('Type', { exact: true }).selectOption('tv')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.locator('[data-result-count]')).toHaveText('1 title')
  await page.getByRole('link', { name: 'Example Series', exact: true }).click()
  await page.getByRole('main').getByRole('link', { name: 'Search', exact: true }).click()
  await expect(page.getByLabel('Year', { exact: true })).toHaveValue('2026')
  await expect(page.getByLabel('Type', { exact: true })).toHaveValue('tv')
  await page.getByLabel('Year', { exact: true }).fill('')
  await page.getByLabel('Type', { exact: true }).selectOption('')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.locator('[data-result-count]')).toHaveText('2 titles')
})

for (const [code, status] of [
  ['not_cached', 409],
  ['not_found', 404],
] as const) {
  test(`readiness stops when the provider returns ${code} without starting an uncached transfer`, async ({
    page,
    server,
  }) => {
    const ids = await seedCatalog(page, server, true)
    await page.clock.install()
    let calls = 0
    await page.route(
      /\/api\/(?:catalog\/[^/]+\/download|operations\/[^/]+\/download)$/,
      async (route) => {
        calls++
        const input = route.request().postDataJSON()
        if (calls === 1) expect(input.allow_uncached).toBe(true)
        else expect(input).toEqual({})
        await route.fulfill({
          status: calls === 1 ? 202 : status,
          contentType: 'application/json',
          body: JSON.stringify(
            calls === 1
              ? { status: 'pending', operation_id: 'synthetic-preparation', preparation_id: '7' }
              : { code, error: 'The file is no longer available at the provider.' },
          ),
        })
      },
    )
    await page.goto(`/search/${ids.movie}?cached_only=false`)
    await page.getByRole('button', { name: 'Add to debrid', exact: true }).click()
    await expect(page.getByText('Preparing', { exact: true })).toBeVisible()
    await page.clock.runFor(15_001)
    await expect(page.getByText('Not ready', { exact: true })).toBeVisible()
    await expect(page.locator('[data-request-notice]')).toContainText(
      'The file is no longer available',
    )
    await expect.poll(() => page.context().pages().length).toBe(1)
    await page.clock.runFor(60_001)
    expect(calls).toBe(2)
    await expect(page.getByRole('button', { name: 'Add to debrid', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Download file', exact: true })).toHaveCount(0)
  })
}

test('mobile download controls handle pending and ready responses without keeping links across episode changes', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.clock.install()
  let calls = 0
  let referrer: string | undefined
  await page.context().route('**/api/catalog-downloads/ephemeral-link', async (route) => {
    referrer = route.request().headers()['referer']
    await route.fulfill({
      contentType: 'application/octet-stream',
      headers: {
        'Content-Disposition': 'attachment; filename="example.mkv"',
        'X-Frame-Options': 'SAMEORIGIN',
        'Content-Security-Policy': "default-src 'none'; frame-ancestors 'self'",
      },
      body: 'Synthetic download contents',
    })
  })
  // Only provider resolution is simulated in this browser scenario. Search,
  // authorization, data, cache evidence and selection use the real backend;
  // HTTP/service tests exercise both real provider adapters against local stubs.
  await page.route(
    /\/api\/(?:catalog\/[^/]+\/download|operations\/[^/]+\/download)$/,
    async (route) => {
      const request = route.request()
      const data = request.postDataJSON()
      expect(request.method()).toBe('POST')
      expect(request.headers()['x-csrf-token']).toBeTruthy()
      if (calls === 0) {
        expect(Object.keys(data).sort()).toEqual([
          'allow_uncached',
          'episode',
          'fingerprint',
          'occurrence_id',
          'season',
          'service',
        ])
        expect(data.service).toBe('torbox')
        expect(data.allow_uncached).toBe(false)
      } else expect(data).toEqual({})
      calls++
      await route.fulfill({
        status: calls === 1 ? 202 : 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: calls === 1 ? 'pending' : 'ready',
          operation_id: 'synthetic-download',
          ...(calls > 1 ? { url: '/api/catalog-downloads/ephemeral-link' } : {}),
        }),
      })
    },
  )
  await page.goto(`/search/${ids.tv}?cached_only=false`)
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  await expect(page.getByText('Not ready', { exact: true })).toBeVisible()
  await expect.poll(() => page.context().pages().length).toBe(1)
  const started = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Check cached file', exact: true }).click()
  const downloaded = await started
  expect(downloaded.suggestedFilename()).toBe('example.mkv')
  expect(await downloaded.failure()).toBeNull()
  expect(referrer).toBeUndefined()
  const link = page.getByRole('button', { name: 'Download file', exact: true })
  await expect(page.locator('iframe[data-catalog-download]')).toHaveAttribute(
    'src',
    server.origin + '/api/catalog-downloads/ephemeral-link',
  )
  expect(page.context().pages()).toHaveLength(1)
  const operation = page.getByRole('link', { name: 'View operation', exact: true })
  await expect(operation).toHaveAttribute('data-slot', 'button')
  const actions = await Promise.all([link.boundingBox(), operation.boundingBox()])
  expect(actions.every(Boolean)).toBe(true)
  expect(
    actions[1]!.x >= actions[0]!.x + actions[0]!.width + 6 ||
      actions[1]!.y >= actions[0]!.y + actions[0]!.height + 6,
  ).toBe(true)
  await expect(page.locator('[data-notification]')).toContainText('The download link is ready.')
  expect(
    await page.evaluate(() => ({
      storage: JSON.stringify(localStorage),
      overflow: document.documentElement.scrollWidth > innerWidth,
    })),
  ).toEqual({ storage: expect.not.stringContaining('ephemeral-link'), overflow: false })
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`UPDATE ingest.torrents SET fields=jsonb_set(fields,'{title}','"Example.Series.S02E03.UPDATED.1080p"') WHERE source_id='series-one';
      UPDATE ingest.aio_matches m SET fingerprint=t.fields_fingerprint FROM ingest.torrents t WHERE m.provider_id=t.provider_id AND m.source_id=t.source_id;
      DELETE FROM ingest.aio_match_queue;`)
  } finally {
    await db.end()
  }
  await page.clock.runFor(30_001)
  await expect(page.getByRole('main')).toContainText('Example.Series.S02E03.UPDATED.1080p')
  await expect(link).toHaveCount(0)
  await page.getByLabel('Episode', { exact: true }).fill('4')
  await page.getByRole('button', { name: 'Show episode', exact: true }).click()
  await expect(link).toHaveCount(0)
})

test('ready download actions expire with the link lifetime supplied by the server', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  await page.clock.install()
  let preparations = 0
  await page.route('**/api/catalog/*/download', async (route) => {
    preparations++
    await route.fulfill({
      json: {
        status: 'ready',
        operation_id: 'synthetic-expiry',
        url: '/api/catalog-downloads/expiring',
        url_valid_seconds: 60,
      },
    })
  })
  await page.route('**/api/catalog-downloads/expiring', (route) =>
    route.fulfill({
      contentType: 'application/octet-stream',
      headers: { 'Content-Disposition': 'attachment; filename="example.mkv"' },
      body: 'Synthetic attachment',
    }),
  )
  await page.goto(`/search/${ids.movie}?cached_only=false`)
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Download file', exact: true })).toBeVisible()
  await page.clock.fastForward(61_000)
  await expect(page.getByRole('button', { name: 'Download file', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Download', exact: true })).toBeVisible()
  expect(preparations).toBe(1)
})

test('an uncached download starts automatically when the provider becomes ready', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  await page.clock.install()
  let calls = 0
  let downloadReferrer: string | undefined
  await page.context().route('**/api/catalog-downloads/prepared-download', async (route) => {
    downloadReferrer = route.request().headers()['referer']
    await route.fulfill({
      contentType: 'application/octet-stream',
      headers: { 'Content-Disposition': 'attachment; filename="prepared.mkv"' },
      body: 'Synthetic prepared download',
    })
  })
  await page.route(
    /\/api\/(?:catalog\/[^/]+\/download|operations\/[^/]+\/download)$/,
    async (route) => {
      calls++
      if (calls === 1) expect(route.request().postDataJSON().allow_uncached).toBe(true)
      else expect(route.request().postDataJSON()).toEqual({})
      await route.fulfill({
        status: calls === 1 ? 202 : 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: calls === 1 ? 'pending' : 'ready',
          operation_id: 'synthetic-preparation',
          ...(calls === 1 ? { preparation_id: '7' } : {}),
          ...(calls > 1 ? { url: '/api/catalog-downloads/prepared-download' } : {}),
        }),
      })
    },
  )
  await page.goto(`/search/${ids.movie}?cached_only=false`)
  const started = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Add to debrid', exact: true }).click()
  await expect(page.getByText('Preparing', { exact: true })).toBeVisible()
  expect(page.context().pages()).toHaveLength(1)
  await page.clock.runFor(15_001)
  const downloaded = await started
  expect(calls).toBe(2)
  expect(downloaded.suggestedFilename()).toBe('prepared.mkv')
  expect(await downloaded.failure()).toBeNull()
  expect(downloadReferrer).toBeUndefined()
  await expect(page.getByRole('button', { name: 'Download file', exact: true })).toBeVisible()
})

test('failed preparations remain in the application and can be retried', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  let fail = true
  await page.route(
    /\/api\/(?:catalog\/[^/]+\/download|operations\/[^/]+\/download)$/,
    async (route) => {
      await route.fulfill({
        status: fail ? 503 : 200,
        contentType: 'application/json',
        body: JSON.stringify(
          fail
            ? { error: 'Temporary provider failure' }
            : {
                status: 'ready',
                operation_id: 'synthetic-ready',
                url: '/api/catalog-downloads/manual-fallback',
              },
        ),
      })
    },
  )
  await page.goto(`/search/${ids.movie}?cached_only=false`)
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  await expect(page.locator('[data-request-notice]')).toContainText('Temporary provider failure')
  await expect.poll(() => page.context().pages().length).toBe(1)
  fail = false
  await page.route('**/api/catalog-downloads/manual-fallback', (route) =>
    route.fulfill({
      contentType: 'application/octet-stream',
      headers: { 'Content-Disposition': 'attachment; filename="retry.mkv"' },
      body: 'Synthetic retry',
    }),
  )
  const started = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Download file', exact: true })).toBeVisible()
  expect((await started).suggestedFilename()).toBe('retry.mkv')
  await expect(page.locator('[data-notification]')).toContainText('The download link is ready.')
  expect(page.context().pages()).toHaveLength(1)
})

test('leaving a title cancels its unfinished request without opening a tab', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  let finish = () => {}
  let markRequested = () => {}
  const requested = new Promise<void>((resolve) => {
    markRequested = resolve
  })
  await page.route(
    /\/api\/(?:catalog\/[^/]+\/download|operations\/[^/]+\/download)$/,
    async (route) => {
      await new Promise<void>((resolve) => {
        finish = () => {
          void route
            .abort()
            .catch(() => {})
            .finally(resolve)
        }
        markRequested()
      })
    },
  )
  await page.goto(`/search/${ids.movie}?cached_only=false`)
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  try {
    await requested
    expect(page.context().pages()).toHaveLength(1)
    await page.getByRole('main').getByRole('link', { name: 'Search', exact: true }).click()
    expect(page.context().pages()).toHaveLength(1)
    await expect(page.getByRole('button', { name: 'Download file', exact: true })).toHaveCount(0)
  } finally {
    finish()
  }
})

test('an added file can be followed and downloaded from its original history operation', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  const operation = '30000000-0000-4000-8000-000000000001'
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(
      `INSERT INTO ingest.operation_runs(id,kind,service,trigger,resource,lock_key,status,phase,processed,finished_at) VALUES($1,'download','torbox','manual','movie:tt1234567',1,'succeeded','waiting',1,now());
    `,
      [operation],
    )
    await db.query(
      `INSERT INTO ingest.operation_downloads(run_id,work_id,input,account)
      SELECT $1,$2,jsonb_build_object('occurrence_id',occurrence_id,'fingerprint',fields_fingerprint,'service','torbox','allow_uncached',false,'preparation_id','7'),'synthetic-account'
      FROM ingest.torrents WHERE source_id='film-two'`,
      [operation, ids.movie],
    )
    await page.clock.install()
    await page.route(/\/api\/(?:catalog\/[^/]+\/download|operations\/[^/]+\/download)$/, (route) =>
      route.fulfill({
        status: 202,
        json: { status: 'pending', operation_id: operation, preparation_id: '7' },
      }),
    )
    let checks = 0
    await page.route(`**/api/operations/${operation}/download`, async (route) => {
      expect(route.request().headers()['x-csrf-token']).toBeTruthy()
      expect(route.request().postDataJSON()).toEqual({})
      checks++
      if (checks > 1)
        await db.query("UPDATE ingest.operation_runs SET phase='ready' WHERE id=$1", [operation])
      await route.fulfill({
        status: checks > 1 ? 200 : 202,
        json: {
          status: checks > 1 ? 'ready' : 'pending',
          operation_id: operation,
          ...(checks > 1 ? { url: '/api/catalog-downloads/followed' } : { preparation_id: '7' }),
        },
      })
    })
    await page.route('**/api/catalog-downloads/followed', (route) =>
      route.fulfill({
        contentType: 'application/octet-stream',
        headers: { 'Content-Disposition': 'attachment; filename="followed.mkv"' },
        body: 'Synthetic followed download',
      }),
    )
    await page.goto(`/search/${ids.movie}?cached_only=false`)
    await page.getByRole('button', { name: 'Add to debrid', exact: true }).click()
    await page.getByRole('link', { name: 'View operation', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/debrid/history/${operation}$`))
    await expect(page.getByRole('region', { name: 'Download', exact: true })).toBeVisible()
    await expect
      .poll(async () => {
        await page.clock.runFor(15_001)
        return checks
      })
      .toBeGreaterThan(0)
    const download = page.getByRole('button', { name: 'Download file', exact: true })
    await expect
      .poll(async () => {
        await page.clock.runFor(15_001)
        return download.isVisible()
      })
      .toBe(true)
    const started = page.waitForEvent('download')
    await download.click()
    expect((await started).suggestedFilename()).toBe('followed.mkv')
    expect(page.context().pages()).toHaveLength(1)
    await expect(page).toHaveURL(new RegExp(`/debrid/history/${operation}$`))
  } finally {
    await db.end()
  }
})

test.describe('browser language preferences', () => {
  test.use({ locale: 'fr-FR' })
  test('identification review pre-fills the year and uses the same localized titles as Search', async ({
    page,
    server,
  }) => {
    await seedCatalog(page, server)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `UPDATE ingest.aio_works SET localized_titles='{"fr":"Long Métrage Exemple","en":"Example Feature"}' WHERE media_type='movie'`,
      )
    } finally {
      await db.end()
    }
    await page.goto('/metadata/identification?match_status=all&match_q=example+film')
    await page
      .getByRole('button', { name: 'Review Example-Film.New.Day.2026.1080p', exact: true })
      .click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByLabel('Year', { exact: true })).toHaveValue('2026')
    await expect(
      dialog.getByRole('radio', { name: 'Choose Long Métrage Exemple', exact: true }),
    ).toBeVisible()
    await dialog.getByLabel('Year', { exact: true }).fill('2020')
    await expect(dialog.getByRole('radio')).toHaveCount(0)
    await dialog.getByLabel('Year', { exact: true }).fill('')
    await expect(
      dialog.getByRole('radio', { name: 'Choose Long Métrage Exemple', exact: true }),
    ).toBeVisible()
  })
  test('localized titles and the optional shared language filter remain offline and largest first', async ({
    page,
    server,
  }) => {
    const ids = await seedCatalog(page, server)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(`UPDATE ingest.aio_works SET localized_titles='{"fr":"Film Exemple","en":"Example Film: New Day"}' WHERE media_type='movie';
    INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
     ('alpha','french','{"title":"Example.Film.New.Day.2026.FRENCH","info_hash":"dddddddddddddddddddddddddddddddddddddddd","size":500,"imdb_id":"tt1234567"}'),
     ('alpha','german','{"title":"Example.Film.New.Day.2026.GERMAN","info_hash":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","size":1000,"imdb_id":"tt1234567"}'),
     ('alpha','multi','{"title":"Example.Film.New.Day.2026.MULTI","info_hash":"ffffffffffffffffffffffffffffffffffffffff","size":300,"imdb_id":"tt1234567"}');
    INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
     SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture' FROM ingest.torrents t JOIN ingest.aio_works w ON w.imdb_id=t.fields->>'imdb_id' WHERE t.source_id IN ('french','german','multi');DELETE FROM ingest.aio_match_queue;`)
    } finally {
      await db.end()
    }
    await page.goto('/search?q=example')
    await expect(page.getByRole('link', { name: 'Film Exemple', exact: true })).toBeVisible()
    await page.goto(`/search/${ids.movie}?cached_only=false`)
    await expect(page.getByRole('switch', { name: 'Filter languages', exact: true })).toBeChecked()
    await expect(page.locator('[data-result-count]')).toHaveText('4 source releases')
    await expect(page.locator('main tbody tr').first()).toContainText('FRENCH')
    await expect(page.locator('main tbody')).not.toContainText('GERMAN')
    await page.getByLabel('Include', { exact: true }).selectOption('preferred')
    await expect(page.locator('[data-result-count]')).toHaveText('1 source release')
    await page.getByRole('switch', { name: 'Filter languages', exact: true }).click()
    await expect(page.locator('[data-result-count]')).toHaveText('5 source releases')
    await expect(page.locator('main tbody tr').first()).toContainText('GERMAN')
    await page.goto('/addon/settings')
    await page.getByRole('button', { name: 'Create access', exact: true }).click()
    const editor = page.getByRole('dialog')
    await editor.locator('summary', { hasText: /^Filters$/ }).click()
    await expect(editor.getByLabel('Preferred language', { exact: true })).toHaveValue('')
    await expect(
      editor.getByRole('switch', { name: 'Filter languages', exact: true }),
    ).toBeChecked()
    await editor.getByRole('switch', { name: 'Filter languages', exact: true }).click()
    await expect(
      editor.getByRole('switch', { name: 'Filter languages', exact: true }),
    ).not.toBeChecked()
  })
})
