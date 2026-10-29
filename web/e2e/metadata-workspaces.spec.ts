import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import pg from 'pg'
import type { Page } from '@playwright/test'
import type { TestServer } from './server'
import { openDisclosure } from './disclosures'
import { test, expect } from './fixtures'

async function seedInformation(page: Page, server: TestServer) {
  const session = await (await page.request.get('/api/session')).json()
  const current = await (await page.request.get('/api/aiosource/controls')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers: { 'X-CSRF-Token': session.csrf_token },
        data: {
          ...current.config,
          auto_sync: false,
          auto_match: false,
          openlibrary_enabled: false,
        },
      })
    ).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,display_title,years) VALUES
      ('movie','101','','Example Film','{2024}'),('tv','202','','Example Series','{2025}'),('book',NULL,'OL100W','Example Book','{2023}');
      INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,display_title,lower(display_title) FROM ingest.aio_works WHERE media_type IN ('movie','tv');
      INSERT INTO ingest.ol_authors(id,name) VALUES('OL200A','Example Author');
      INSERT INTO ingest.ol_editions(id,work_keys,title,normalized) VALUES('OL300M',ARRAY['OL100W'],'Example Book','example book');
      UPDATE ingest.ol_import_state SET complete=TRUE,data_complete=TRUE,position=1234,works=1,editions=1,authors=1,snapshot_at='2026-01-01',last_sync='2026-01-02';
      UPDATE ingest.ol_changes_state SET anchor_id='100',anchor_at='2026-01-02',changes_through='2026-01-02',bootstrap_complete=TRUE;
      UPDATE ingest.aio_state SET last_sync='2026-01-02',changes_through='2026-01-02',applied_through='2026-01-02',inventory_date='2026-01-01';
      INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,processed,finished_at) VALUES
      ('video-update','metadata','tmdb','manual',0,'succeeded',2,now()),
      ('book-update','metadata','openlibrary','manual',0,'succeeded',1,now()),
      ('identification-update','matching','local','manual',0,'succeeded',3,now());`)
  } finally {
    await db.end()
  }
}

test('video and book workspaces keep separate counts and histories under one Metadata category', async ({
  page,
  server,
}) => {
  await seedInformation(page, server)
  const fullStatisticsReads: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/aiosource')
      fullStatisticsReads.push(request.url())
  })
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    for (const [section, label, entries, operation] of [
      ['videos', 'Videos', '2', 'video-update'],
      ['books', 'Books', '1', 'book-update'],
    ]) {
      await page.goto(`/metadata/${section}`)
      await expect(page.getByRole('heading', { name: label, exact: true, level: 1 })).toBeVisible()
      await expect(page.getByRole('tablist').getByRole('tab')).toHaveText([
        'Statistics',
        'Settings',
        'History',
      ])
      await expect(
        page.getByText('Catalog entries', { exact: true }).locator('..').locator('dd'),
      ).toHaveText(entries)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      if (process.env.QA_DIRECTORY) {
        await mkdir(process.env.QA_DIRECTORY, { recursive: true })
        await page.screenshot({
          path: join(process.env.QA_DIRECTORY, `metadata-${section}-${width}.png`),
          fullPage: true,
        })
      }
      // A stale service filter cannot make another workspace's runs appear.
      await page.goto(`/metadata/${section}/history?service=other&kind=matching`)
      await expect(page.locator('main tbody tr')).toHaveCount(1)
      await expect(
        page.getByRole('link', { name: `Open run ${operation}`, exact: true }),
      ).toBeVisible()
      await expect(page.getByLabel('Operation', { exact: true })).toHaveCount(0)
      await page.getByRole('link', { name: `Open run ${operation}`, exact: true }).click()
      await expect(page.getByText(operation, { exact: true })).toBeVisible()
      await page
        .getByRole('link', {
          name: section === 'books' ? 'Book history' : 'Video history',
          exact: true,
        })
        .click()
      await expect(page).toHaveURL(
        new RegExp(`/metadata/${section}/history\\?service=other&kind=matching$`),
      )
    }
  }
  await page.goto('/metadata/identification/history')
  expect(fullStatisticsReads, 'Information and history pages must not scan identification').toEqual(
    [],
  )
  await expect(page.getByRole('tablist').getByRole('tab')).toHaveText([
    'Results',
    'Settings',
    'History',
  ])
  await expect(page.locator('main tbody tr')).toHaveCount(1)
  await expect(
    page.getByRole('link', { name: 'Open run identification-update', exact: true }),
  ).toBeVisible()
  await page.goto('/metadata/settings#setting=metadata.books')
  await expect(page).toHaveURL(/\/metadata\/books\/settings#setting=metadata.books$/)
  await expect(page.getByLabel('Automatically update book information')).toBeFocused()
  await page.goto('/metadata/settings#setting=metadata.match')
  await expect(page).toHaveURL(/\/metadata\/identification\/settings#setting=metadata.match$/)
  await expect(page.getByLabel('Automatically identify torrents')).toBeFocused()
  await page.goto('/metadata/history?service=openlibrary&status=succeeded&limit=25')
  await expect(page).toHaveURL(/\/metadata\/books\/history\?status=succeeded&limit=25$/)
  await page.goto('/metadata/history/book-update')
  await expect(page).toHaveURL(/\/metadata\/books\/history\/book-update$/)
  await expect(page.getByRole('tab', { name: 'History', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})

test('workspace saves preserve other provider settings and protect drafts between workspaces', async ({
  page,
}) => {
  await page.goto('/metadata/videos/settings')
  await page.getByLabel('TMDB requests per second', { exact: true }).fill('20')
  await page.getByLabel('Information update interval (minutes)', { exact: true }).fill('30')
  await page.getByLabel('Automatically update film & series information').uncheck()
  const videos = page.getByRole('form', { name: 'Videos configuration', exact: true })
  await videos.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(videos.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.goto('/metadata/books/settings')
  await expect(page.getByLabel('TMDB requests per second')).toHaveCount(0)
  await expect(page.getByLabel('Automatically identify torrents')).toHaveCount(0)
  await page.getByLabel('Book information update interval (minutes)', { exact: true }).fill('45')
  const books = page.getByRole('form', { name: 'Books configuration', exact: true })
  await books.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(books.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.goto('/metadata/identification/settings')
  await page.getByLabel('Identification interval (seconds)', { exact: true }).fill('120')
  const identification = page.getByRole('form', {
    name: 'Identification configuration',
    exact: true,
  })
  await identification.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(identification.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const saved = (await (await page.request.get('/api/aiosource/controls')).json()).config
  expect(saved).toMatchObject({
    tmdb_requests_per_second: 20,
    sync_minutes: 30,
    auto_sync: false,
    openlibrary_minutes: 45,
    match_seconds: 120,
  })
  await page.getByLabel('Identification interval (seconds)', { exact: true }).fill('180')
  if (page.viewportSize()!.width < 1024)
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  await page
    .getByRole('navigation', { name: 'Main navigation', exact: true })
    .getByRole('link', { name: 'Books', exact: true })
    .click()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByLabel('Identification interval (seconds)')).toHaveValue('180')
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await guard.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(page).toHaveURL(/\/metadata\/identification\/history$/)
  expect((await (await page.request.get('/api/aiosource/controls')).json()).config).toEqual(saved)
})

test('video maintenance resets only the video catalog through the ordinary confirmation', async ({
  page,
  server,
}) => {
  await seedInformation(page, server)
  await page.goto('/metadata/videos/settings#maintenance')
  await expect(
    page.getByRole('button', { name: 'Reset book information', exact: true }),
  ).toHaveCount(0)
  await page.getByRole('button', { name: 'Reset video information', exact: true }).click()
  const confirmation = page.getByRole('alertdialog', {
    name: 'Reset video information?',
    exact: true,
  })
  await expect(confirmation).toContainText('book information are retained')
  await confirmation.getByRole('button', { name: 'Reset video information', exact: true }).click()
  await expect(confirmation).toBeHidden()
  const data = await (await page.request.get('/api/aiosource')).json()
  expect(data.status.video_works).toBe(0)
  expect(data.status.works).toBe(1)
  expect(data.books).toMatchObject({ works: 1, editions: 1, authors: 1, position: 1234 })
  await page.goto('/metadata/books/settings')
  await openDisclosure(page, 'Maintenance')
  await expect(
    page.getByRole('button', { name: 'Reset book information', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Reset video information', exact: true }),
  ).toHaveCount(0)
})
