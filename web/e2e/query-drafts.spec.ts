import type { Locator, Page } from '@playwright/test'
import pg from 'pg'
import { test, expect } from './fixtures'

async function submitAndContinueTyping(page: Page, input: Locator, key: string) {
  const applied = 'Applied query'
  const draft = 'Next query draft'
  await input.fill(applied)
  const results = page.waitForResponse(
    (response) => new URL(response.url()).searchParams.get('q') === applied,
  )
  await input.evaluate((element, nextDraft) => {
    const field = element as HTMLInputElement
    field.form!.requestSubmit()
    // Continue on the current field, including if submission replaced its form.
    // A deferred route commit must not erase this subsequent input.
    const current = document.getElementById(field.id) as HTMLInputElement
    current.value = nextDraft
    current.dispatchEvent(new Event('input', { bubbles: true }))
  }, draft)
  expect((await results).ok()).toBe(true)
  await expect(page).toHaveURL((url) => url.searchParams.get(key) === applied)
  await expect(input).toHaveValue(draft)
  await expect(input).toBeFocused()
  const submit = input
    .locator('xpath=ancestor::form')
    .getByRole('button', { name: /^(Search|Apply filters)$/, exact: true })
  const nextResults = page.waitForResponse(
    (response) => new URL(response.url()).searchParams.get('q') === draft,
  )
  await submit.focus()
  await submit.press('Enter')
  expect((await nextResults).ok()).toBe(true)
  await expect(submit).toBeFocused()
  const clear = input
    .locator('xpath=ancestor::form')
    .getByRole('button', { name: 'Clear filters', exact: true })
  await clear.click()
  await expect(input).toHaveValue('')
  await expect(input).toBeFocused()
  await input.fill('Unsubmitted query')
  await clear.click()
  await expect(input).toHaveValue('')
  await expect(input).toBeFocused()
}

for (const view of [
  { name: 'Search', path: '/search?q=Original', input: '#catalog-query', key: 'q' },
  { name: 'Torrents', path: '/torrents?q=Original', input: '#torrent-search', key: 'q' },
  {
    name: 'Identification',
    path: '/metadata/identification?match_status=all&match_q=Original',
    input: '#matching-search',
    key: 'match_q',
  },
  { name: 'Logs', path: '/logs?q=Original', input: '#log-query', key: 'q' },
  {
    name: 'Stremio history',
    path: '/addon/history?q=Original',
    input: '#stremio-resource',
    key: 'q',
  },
]) {
  test(`${view.name} preserves typing that follows a submitted query`, async ({ page }) => {
    await page.goto(view.path)
    await submitAndContinueTyping(page, page.locator(view.input), view.key)
  })
}

test('episode selection retains the next number and keyboard focus after submission', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  let id: string
  try {
    const work =
      await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years,display_title)
      VALUES('tv','321','tt0000321',ARRAY[2026],'Example Series') RETURNING id::text`)
    id = work.rows[0].id
    await db.query(`INSERT INTO ingest.torrents(provider_id,source_id,fields)
      VALUES('episode-source','episode-one','{"title":"Example.Series.S01E01","imdb_id":"tt0000321","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":100}');
      INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
      SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','Synthetic identifier'
      FROM ingest.torrents t JOIN ingest.aio_works w ON w.imdb_id=t.fields->>'imdb_id';
      DELETE FROM ingest.aio_match_queue;`)
  } finally {
    await db.end()
  }
  await page.goto(`/search/${id}`)
  const episode = page.getByLabel('Episode', { exact: true })
  await episode.fill('4')
  const results = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === `/api/catalog/${id}/releases` &&
      new URL(response.url()).searchParams.get('episode') === '4',
  )
  await episode.evaluate((element) => {
    const input = element as HTMLInputElement
    input.form!.requestSubmit()
    const next = document.getElementById(input.id) as HTMLInputElement
    next.value = '5'
    next.dispatchEvent(new Event('input', { bubbles: true }))
  })
  expect((await results).ok()).toBe(true)
  await expect(episode).toHaveValue('5')
  await expect(episode).toBeFocused()
})

test.describe('processed records', () => {
  test.use({ collectionSource: true })

  test('processed record search preserves typing that follows a submitted query', async ({
    page,
    server,
  }) => {
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(`INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
        VALUES('query-run','query-source','Query source','preview','succeeded','','{}',now())`)
    } finally {
      await db.end()
    }
    await page.goto('/runs/query-run?tab=raw&raw_q=Original')
    await submitAndContinueTyping(page, page.locator('#raw-search'), 'raw_q')
    await page.locator('main').getByRole('link', { name: 'Identification', exact: true }).click()
    await expect(page).toHaveURL(/\/metadata\/identification$/)
  })
})
