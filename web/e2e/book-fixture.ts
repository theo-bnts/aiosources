import type { Page } from '@playwright/test'
import pg from 'pg'
import { expect } from './fixtures'
import type { TestServer } from './server'

const pixel = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aTioAAAAASUVORK5CYII=',
  'base64',
)
async function coverImages(page: Page) {
  await page.route(/^https:\/\/(image\.tmdb\.org|covers\.openlibrary\.org)\//, (route) =>
    route.fulfill({ contentType: 'image/png', body: pixel }),
  )
}
async function seedBooks(page: Page, server: TestServer) {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const source = {
    version: 1,
    id: 'book-fixture',
    name: 'Example books',
    adapter: 'json',
    url: server.sourceURL,
    enabled: false,
  }
  expect(
    (
      await page.request.put('/api/providers/book-fixture', {
        headers,
        data: { revision: '', json: JSON.stringify(source) },
      })
    ).status(),
  ).toBe(200)
  expect(
    (
      await page.request.put('/api/secrets/book_fixture', {
        headers,
        data: { revision: 0, value: 'synthetic-never-sent-book-key' },
      })
    ).status(),
  ).toBe(204)
  const status = await (await page.request.get('/api/aiosource')).json()
  Object.assign(status.config, {
    auto_sync: false,
    auto_match: false,
    openlibrary_enabled: false,
    torbox_secret: 'book_fixture',
    trackers: { 'book-fixture': { private: true, announce_secret: '' } },
  })
  expect(
    (await page.request.put('/api/aiosource/config', { headers, data: status.config })).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.ol_authors(id,name,normalized) VALUES('OL200A','Morgan Vale','morgan vale');
    INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,display_title,book_normalized,years,book_data) VALUES
    ('book',NULL,'OL100W','The Amber Orchard','the amber orchard',ARRAY[2001],'{"author_keys":["OL200A"],"cover":12345}'),
    ('movie','101','','Example Film','',ARRAY[2026],'{}');
    UPDATE ingest.aio_works SET poster_path='/syntheticPoster.jpg' WHERE media_type='movie';
    INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,display_title,'example film' FROM ingest.aio_works WHERE media_type='movie';
    INSERT INTO ingest.ol_editions(id,work_keys,title,normalized,isbn,author_keys,languages,year) VALUES
    ('OL300M',ARRAY['OL100W'],'Le Verger Ambré','le verger ambre',ARRAY['9780306406157'],ARRAY['OL200A'],ARRAY['fr'],2024);
    INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
    ('book-fixture','ebook','{"title":"Morgan Vale - The Amber Orchard EPUB","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":100,"categories":[7020]}'),
    ('book-fixture','audio','{"title":"Morgan Vale - The Amber Orchard M4B","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":200,"categories":[3030]}'),
    ('book-fixture','film','{"title":"Example.Film.2026.1080p","info_hash":"cccccccccccccccccccccccccccccccccccccccc","size":300}');
    INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason) SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture' FROM ingest.torrents t JOIN ingest.aio_works w ON w.media_type=CASE WHEN t.source_id='film' THEN 'movie' ELSE 'book' END;
    DELETE FROM ingest.aio_match_queue;
    INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,finished_at) VALUES
    ('00000000-0000-4000-8000-000000000101','metadata','openlibrary','automatic',1,'succeeded',now()),
    ('00000000-0000-4000-8000-000000000102','metadata','tmdb','automatic',2,'succeeded',now());
    INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) VALUES
    ('torbox','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',true,now(),'[{"path":"The Amber Orchard.epub","size":100},{"path":"The Amber Orchard.pdf","size":200}]'),
    ('torbox','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',true,now(),'[{"path":"The Amber Orchard.m4b","size":200}]');`)
    const rows = await db.query("SELECT id FROM ingest.aio_works WHERE media_type='book'")
    return String(rows.rows[0].id)
  } finally {
    await db.end()
  }
}

export async function exerciseBookSynchronizationControls(page: Page, server: TestServer) {
  await seedBooks(page, server)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`UPDATE ingest.ol_import_state SET complete=TRUE,position=3,imported=0,works=1,editions=1,authors=1,snapshot_at='2026-01-01T00:00:00Z',last_sync='2026-01-02T00:00:00Z';
      UPDATE ingest.ol_changes_state SET needs_dump_after='2026-01-03T00:00:00Z',changes_through='2026-01-02T00:00:00Z',bootstrap_complete=FALSE;
      INSERT INTO ingest.ol_change_jobs(id,revision,changed_at) VALUES('OL500W',2,'2026-01-03T00:00:00Z');`)
  } finally {
    await db.end()
  }
  await page.goto('/metadata/books')
  await expect(page.getByText('Synchronization', { exact: true }).locator('..')).toContainText(
    'Waiting for a newer export',
  )
  await expect(page.getByText('Information pending', { exact: true }).locator('..')).toContainText(
    '1',
  )
  await expect(page.getByText('Changes applied through', { exact: true })).toBeVisible()
  await expect(page.getByText('Export date', { exact: true }).locator('..')).toContainText(
    '2026-01-01',
  )
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.goto('/metadata/books/settings')
  const interval = page.getByLabel('Book information update interval (minutes)', { exact: true })
  await expect(interval).toHaveValue('15')
  await interval.fill('5')
  const save = page.locator('main form').first().getByRole('button', { name: 'Save', exact: true })
  await save.click()
  await expect(save).toBeDisabled()
  await page.reload()
  await expect(interval).toHaveValue('5')
  await page
    .locator('summary')
    .filter({ hasText: /^Advanced$/ })
    .click()
  await page
    .locator('summary')
    .filter({ hasText: /^Manual actions$/ })
    .click()
  await expect(
    page.getByText(/Enable automatic book updates, or run Update book information/),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
}

export async function exerciseBookSearch(page: Page, server: TestServer) {
  const book = await seedBooks(page, server)
  await coverImages(page)
  await page.goto('/search?q=Morgan+Orchard&type=ebook&year=2024')
  await expect(page.locator('[data-result-count]')).toHaveText('1 title')
  await expect(page.getByRole('link', { name: 'The Amber Orchard', exact: true })).toBeVisible()
  await expect(page.locator('main img')).toHaveAttribute(
    'src',
    /^https:\/\/covers.openlibrary.org\/b\/id\/12345/,
  )
  await page.getByRole('link', { name: 'The Amber Orchard', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/search/${book}`))
  await expect(page.getByLabel('Season', { exact: true })).toHaveCount(0)
  const choiceArea = page.locator('tbody tr').filter({ hasText: 'EPUB' })
  const choose = choiceArea.getByRole('button', { name: 'Choose file', exact: true })
  await choose.click()
  await expect(page.getByLabel('File', { exact: true })).toBeVisible()
  await expect(choose).toBeDisabled()
  await page.getByLabel('File', { exact: true }).selectOption('The Amber Orchard.epub')
  let selected = false
  await page.route('**/api/operations/*/download', async (route) => {
    expect(route.request().postDataJSON().file).toBe('The Amber Orchard.epub')
    selected = true
    return route.fulfill({
      json: {
        status: 'ready',
        url: '/api/catalog-downloads/synthetic-book',
        operation_id: 'synthetic-book-operation',
      },
    })
  })
  await page.context().route('**/api/catalog-downloads/synthetic-book', (route) =>
    route.fulfill({
      contentType: 'application/octet-stream',
      headers: { 'Content-Disposition': 'attachment; filename="example.epub"' },
      body: 'synthetic-book',
    }),
  )

  // The mobile WebKit runner does not report attachment downloads even for a
  // plain link. Verify its received attachment; Chromium checks the download.
  const mobileWebKit = page.context().browser()?.browserType().name() === 'webkit'
  const delivered = mobileWebKit
    ? page.waitForResponse(
        (r) => new URL(r.url()).pathname === '/api/catalog-downloads/synthetic-book',
      )
    : page.waitForEvent('download')
  await choiceArea.getByRole('button', { name: 'Download', exact: true }).click()
  const attachment = await delivered
  if ('suggestedFilename' in attachment) expect(attachment.suggestedFilename()).toBe('example.epub')
  else {
    expect(attachment.status()).toBe(200)
    expect(attachment.headers()['content-disposition']).toBe('attachment; filename="example.epub"')
  }
  expect(selected).toBe(true)
  await expect(page.getByRole('link', { name: 'View operation', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.goto('/search?q=Example+Film&type=movie')
  await expect(page.locator('main img')).toHaveAttribute(
    'src',
    'https://image.tmdb.org/t/p/w342/syntheticPoster.jpg',
  )
  await page.goto('/metadata/history?service=openlibrary')
  await expect(page).toHaveURL(/\/metadata\/books\/history$/)
  await expect(page.locator('main tbody tr')).toHaveCount(1)
  await expect(page.locator('main tbody tr')).toContainText('Open Library')
  await page.goto('/metadata/videos/history')
  await expect(page.locator('main tbody tr')).toHaveCount(1)
  await expect(page.locator('main tbody tr')).toContainText('TMDB')
}
