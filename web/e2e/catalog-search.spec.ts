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
      ('alpha','film-one','{"title":"Example.Film.New.Day.2026.1080p","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":100,"imdb_id":"tt1234567"}'),
      ('zulu','film-two','{"title":"Example.Film.New.Day.2026.2160p","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":200,"imdb_id":"tt1234567"}'),
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

test('identification and torrent search accept the same spaced and dotted title fragments and show totals', async ({
  page,
  server,
}) => {
  await seedCatalog(page, server)
  for (const q of ['Example.Film.New.Day', 'Example Film New Day', 'xample New']) {
    await page.goto(`/torrents?q=${encodeURIComponent(q)}`)
    await expect(page.locator('[data-result-count]')).toHaveText('2 torrents')
    await expect(page.locator('main tbody tr')).toHaveCount(2)
    await page.goto('/metadata/identification?match_status=all')
    await page.getByLabel('Title, source ID or info hash', { exact: true }).fill(q)
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('[data-result-count]')).toHaveText('2 torrents')
    await expect(page.locator('main tbody tr')).toHaveCount(2)
  }
})

test('connection search groups clean titles, sorts source versions and retains the list when returning', async ({
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
  await expect(page).toHaveURL(new RegExp(`/search/${ids.movie}$`))
  await expect(page.locator('[data-result-count]')).toHaveText('2 source releases')
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

test('mobile download controls handle pending and ready responses without keeping links across episode changes', async ({
  page,
  server,
}) => {
  const ids = await seedCatalog(page, server, true)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.clock.install()
  let calls = 0
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
  await page.getByRole('button', { name: 'Check cached file', exact: true }).click()
  const link = page.getByRole('link', { name: 'Download file', exact: true })
  await expect(link).toHaveAttribute('href', 'https://files.example/ephemeral-link')
  await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
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
