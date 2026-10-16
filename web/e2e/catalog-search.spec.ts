import type { Download, Page } from '@playwright/test'
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
  await page.goto(`/search/${ids.tv}`)
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
    await page.route('**/api/catalog/*/download', async (route) => {
      calls++
      const input = route.request().postDataJSON()
      expect(input.allow_uncached).toBe(calls === 1)
      expect(input.preparation_id).toBe(calls === 1 ? undefined : '7')
      await route.fulfill({
        status: calls === 1 ? 202 : status,
        contentType: 'application/json',
        body: JSON.stringify(
          calls === 1
            ? { status: 'pending', operation_id: 'synthetic-preparation', preparation_id: '7' }
            : { code, error: 'The file is no longer available at the provider.' },
        ),
      })
    })
    await page.goto(`/search/${ids.movie}`)
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
    await expect(page.getByRole('link', { name: 'Download file', exact: true })).toHaveCount(0)
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
  await page.context().route('https://files.example/ephemeral-link', async (route) => {
    referrer = route.request().headers()['referer']
    await route.fulfill({
      contentType: 'application/octet-stream',
      headers: { 'Content-Disposition': 'attachment; filename="example.mkv"' },
      body: 'Synthetic download contents',
    })
  })
  // Only provider resolution is simulated in this browser scenario. Search,
  // authorization, data, cache evidence and selection use the real backend;
  // HTTP/service tests exercise both real provider adapters against local stubs.
  await page.route('**/api/catalog/*/download', async (route) => {
    const request = route.request()
    const data = request.postDataJSON()
    expect(request.method()).toBe('POST')
    expect(request.headers()['x-csrf-token']).toBeTruthy()
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
    calls++
    await route.fulfill({
      status: calls === 1 ? 202 : 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: calls === 1 ? 'pending' : 'ready',
        operation_id: 'synthetic-download',
        ...(calls > 1 ? { url: 'https://files.example/ephemeral-link' } : {}),
      }),
    })
  })
  await page.goto(`/search/${ids.tv}`)
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  await expect(page.getByText('Not ready', { exact: true })).toBeVisible()
  await expect.poll(() => page.context().pages().length).toBe(1)
  const started = new Promise<Download>((resolve) =>
    page.context().once('page', (popup) => popup.once('download', resolve)),
  )
  await page.getByRole('button', { name: 'Check cached file', exact: true }).click()
  const downloaded = await started
  expect(downloaded.suggestedFilename()).toBe('example.mkv')
  expect(await downloaded.failure()).toBeNull()
  expect(referrer).toBeUndefined()
  const link = page.getByRole('link', { name: 'Download file', exact: true })
  await expect(link).toHaveAttribute('href', 'https://files.example/ephemeral-link')
  await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  await expect(link).toHaveAttribute('download', '')
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

test('an uncached download starts automatically when the provider becomes ready', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  await page.clock.install()
  let calls = 0
  let downloadReferrer: string | undefined
  await page.context().route('https://files.example/prepared-download', async (route) => {
    downloadReferrer = route.request().headers()['referer']
    await route.fulfill({
      contentType: 'application/octet-stream',
      headers: { 'Content-Disposition': 'attachment; filename="prepared.mkv"' },
      body: 'Synthetic prepared download',
    })
  })
  await page.route('**/api/catalog/*/download', async (route) => {
    calls++
    expect(route.request().postDataJSON().allow_uncached).toBe(calls === 1)
    expect(route.request().postDataJSON().preparation_id).toBe(calls === 1 ? undefined : '7')
    await route.fulfill({
      status: calls === 1 ? 202 : 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: calls === 1 ? 'pending' : 'ready',
        operation_id: 'synthetic-preparation',
        ...(calls === 1 ? { preparation_id: '7' } : {}),
        ...(calls > 1 ? { url: 'https://files.example/prepared-download' } : {}),
      }),
    })
  })
  await page.goto(`/search/${ids.movie}`)
  const started = new Promise<Download>((resolve) =>
    page.context().once('page', (popup) => popup.once('download', resolve)),
  )
  await page.getByRole('button', { name: 'Add to debrid', exact: true }).click()
  await expect(page.getByText('Preparing', { exact: true })).toBeVisible()
  const pending = page
    .context()
    .pages()
    .find((tab) => tab !== page)!
  await expect(pending.getByRole('heading', { name: 'Preparing your download…' })).toBeVisible()
  expect(await pending.evaluate(() => window.opener === null)).toBe(true)
  await page.clock.runFor(15_001)
  const downloaded = await started
  expect(calls).toBe(2)
  expect(downloaded.suggestedFilename()).toBe('prepared.mkv')
  expect(await downloaded.failure()).toBeNull()
  expect(downloadReferrer).toBeUndefined()
  await expect(page.getByRole('link', { name: 'Download file', exact: true })).toBeVisible()
})

test('failed preparations close their temporary tab and blocked popups retain a direct download', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  let fail = true
  await page.route('**/api/catalog/*/download', async (route) => {
    await route.fulfill({
      status: fail ? 503 : 200,
      contentType: 'application/json',
      body: JSON.stringify(
        fail
          ? { error: 'Temporary provider failure' }
          : {
              status: 'ready',
              operation_id: 'synthetic-ready',
              url: 'https://files.example/manual-fallback',
            },
      ),
    })
  })
  await page.goto(`/search/${ids.movie}`)
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  await expect(page.locator('[data-request-notice]')).toContainText('Temporary provider failure')
  await expect.poll(() => page.context().pages().length).toBe(1)
  fail = false
  await page.evaluate(() => {
    window.open = () => null
  })
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  await expect(page.getByRole('link', { name: 'Download file', exact: true })).toHaveAttribute(
    'href',
    'https://files.example/manual-fallback',
  )
  await expect(page.locator('[data-notification]')).toContainText('Use Download file')
  expect(page.context().pages()).toHaveLength(1)
})

test('leaving a title cancels its unfinished download and closes only its preparation tab', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  let finish = () => {}
  let markRequested = () => {}
  const requested = new Promise<void>((resolve) => {
    markRequested = resolve
  })
  await page.route('**/api/catalog/*/download', async (route) => {
    await new Promise<void>((resolve) => {
      finish = () => {
        void route
          .abort()
          .catch(() => {})
          .finally(resolve)
      }
      markRequested()
    })
  })
  await page.goto(`/search/${ids.movie}`)
  const opened = page.waitForEvent('popup')
  await page.getByRole('button', { name: 'Download', exact: true }).click()
  const pending = await opened
  try {
    await requested
    await expect(pending.getByRole('heading', { name: 'Preparing your download…' })).toBeVisible()
    await page.getByRole('main').getByRole('link', { name: 'Search', exact: true }).click()
    await expect.poll(() => pending.isClosed()).toBe(true)
    await expect(page.getByRole('link', { name: 'Download file', exact: true })).toHaveCount(0)
  } finally {
    finish()
  }
})

test.describe('browser language preferences', () => {
  test.use({ locale: 'fr-FR' })
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
    await page.goto(`/search/${ids.movie}`)
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
    await expect(page.getByLabel('Manifest URL', { exact: true })).toHaveValue(
      /~fr~all\/manifest\.json$/,
    )
    await page.getByRole('switch', { name: 'Filter languages', exact: true }).click()
    await expect(page.getByLabel('Manifest URL', { exact: true })).toHaveValue(
      /~fr~unknown\/manifest\.json$/,
    )
  })
})
