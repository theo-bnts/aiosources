import { openDisclosure } from './disclosures'
import pg from 'pg'
import { test, expect } from './fixtures'

test('metadata and debrid histories have consistent filters, pagination and retained details', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,processed,errors,results,finished_at,error)
      VALUES('metadata-failed','metadata','tmdb','automatic',0,'failed','details',242000,1,'{}',now(),'Information update failed. Committed progress is preserved.'),
      ('cache-success','cache','torbox','playback',0,'succeeded','availability',2,0,'{"cached":1,"uncached":1}',now(),''),
      ('cache-failed','cache','alldebrid','playback',0,'failed','availability',0,1,'{}',now(),'Cache verification failed. Saved availability is preserved.'),
      ('cleanup-success','cleanup','alldebrid','automatic',0,'succeeded','cleanup',1,0,'{"cleaned":1}',now(),'')`)
    await db.query(`INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,processed,results,finished_at)
      SELECT 'matching-'||n,'matching','local','automatic',0,'succeeded','identification',7,'{"matched":5,"unmatched":2}',now() FROM generate_series(1,55) n`)
    await db.query(
      `INSERT INTO ingest.operation_events(run_id,phase,processed,errors) SELECT id,status,processed,errors FROM ingest.operation_runs`,
    )
  } finally {
    await db.end()
  }
  await page.goto('/metadata')
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(page).toHaveURL(/\/metadata\/history$/)
  const breadcrumb = page.getByRole('navigation', { name: 'Breadcrumb', exact: true })
  await expect(breadcrumb.getByRole('listitem')).toHaveText(['Metadata/', 'Metadata'])
  await expect(page.getByRole('link', { name: 'Cache verification', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Next', exact: true }).click()
  await expect(page).toHaveURL(/offset=50/)
  await page.getByLabel('Filter by operation', { exact: true }).selectOption('metadata')
  await expect(page).not.toHaveURL(/offset=/)
  await expect(page.getByRole('link', { name: 'Information update', exact: true })).toHaveCount(1)
  await page.getByLabel('Filter by status', { exact: true }).selectOption('succeeded')
  await expect(page.getByText('No runs in this view', { exact: true })).toBeVisible()
  await page.getByLabel('Filter by status', { exact: true }).selectOption('failed')
  const metadataView = page.url()
  await page.getByRole('link', { name: 'Information update', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Information update', exact: true })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveText(
    'Information update failed. Committed progress is preserved.',
  )
  await expect(page.getByRole('region', { name: 'Operation events', exact: true })).toContainText(
    'Failed',
  )
  await page.reload()
  await expect(page.getByText('metadata-failed', { exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'Metadata history', exact: true }).click()
  await expect(page).toHaveURL(metadataView)
  await page.goto('/debrid')
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(breadcrumb.getByRole('listitem')).toHaveText(['Debrid'])
  await expect(page.getByRole('link', { name: 'Cache verification', exact: true })).toHaveCount(2)
  await expect(
    page.getByRole('link', { name: 'Temporary magnet cleanup', exact: true }),
  ).toHaveCount(1)
  await expect(page.getByRole('link', { name: 'Information update', exact: true })).toHaveCount(0)
  await page.getByLabel('Filter by service', { exact: true }).selectOption('torbox')
  const debridView = page.url()
  await expect(page.getByRole('link', { name: 'Cache verification', exact: true })).toHaveCount(1)
  await expect(
    page.getByRole('link', { name: 'Temporary magnet cleanup', exact: true }),
  ).toHaveCount(0)
  await page.getByRole('link', { name: 'Cache verification', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Cache verification', exact: true })).toBeVisible()
  await expect(page.locator('dl')).toContainText('Cached')
  await expect(page.locator('dl')).toContainText('Not cached')
  await expect(page.getByRole('alert')).toHaveCount(0)
  await page.getByRole('link', { name: 'Debrid history', exact: true }).click()
  await expect(page).toHaveURL(debridView)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('a real manual identification appears in metadata history without mixing source imports', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const current = await (await page.request.get('/api/aiosource')).json()
  const saved = await page.request.put('/api/aiosource/config', {
    headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
    data: { ...current.config, auto_match: false, auto_sync: false },
  })
  expect(saved.status()).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(
      `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','101','tt1234567','{2021}')`,
    )
    await db.query(
      `INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,'Synthetic Movie','synthetic movie' FROM ingest.aio_works WHERE tmdb_id='101'`,
    )
    await db.query(
      `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','one','{"title":"Synthetic Movie 2021","imdb_id":"tt1234567","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`,
    )
  } finally {
    await db.end()
  }
  await page.goto('/metadata/settings')
  await openDisclosure(page, 'Manual actions')
  await page.getByRole('button', { name: 'Identify torrents', exact: true }).click()
  await expect
    .poll(async () => {
      const response = await page.request.get('/api/operations?kind=matching&status=succeeded')
      expect(response.status()).toBe(200)
      return (await response.json()).total
    })
    .toBe(1)
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await page.getByRole('link', { name: 'Torrent identification', exact: true }).click()
  await expect(page.locator('dl')).toContainText('Identified')
  await expect(page.getByText('Local identification · Manual', { exact: true })).toBeVisible()
  expect((await (await page.request.get('/api/runs')).json()).items).toEqual([])
})
