import pg from 'pg'
import { test, expect } from './fixtures'
import type { SourceTorrentOverview } from '../src/lib/types'

test.use({ collectionSource: true })

test('source counts separate current decisions and incomplete imports with working filtered links', async ({
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
      await page.request.put('/api/secrets/remote_fixture', {
        headers,
        data: { revision: 0, value: 'synthetic-remote-password' },
      })
    ).status(),
  ).toBe(204)
  for (const [id, name] of [
    ['alpha', 'Alpha source'],
    ['beta', 'Beta source'],
    ['empty', 'Empty source'],
    ['remote', 'Remote copy'],
  ]) {
    const source = {
      version: 1,
      id,
      name,
      adapter: 'http_json',
      enabled: false,
      url: id === 'remote' ? 'https://catalogue.example/api/catalogs/friend' : server.sourceURL,
      ...(id === 'remote'
        ? { http: { catalog: true }, auth: { type: 'bearer', secret_ref: 'remote_fixture' } }
        : {}),
    }
    expect(
      (
        await page.request.put(`/api/providers/${id}`, {
          headers,
          data: { json: JSON.stringify(source), revision: '' },
        })
      ).status(),
    ).toBe(200)
  }
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years)
VALUES('movie','1','tt1234567',ARRAY[2021]);
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
('alpha','matched','{"title":"Alpha identified release","download_url":"https://source.invalid/private-path"}'),
('alpha','unmatched','{"title":"Alpha unmatched release"}'),
('alpha','ambiguous','{"title":"Alpha review release"}'),
('alpha','pending','{"title":"Alpha pending release"}'),
('alpha','queued','{"title":"Alpha queued release"}'),
('beta','matched','{"title":"Beta identified release"}'),
('remote','matched','{"title":"Remote identified release"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,md5(fields::text),
CASE WHEN source_id IN ('matched','queued') THEN (SELECT id FROM ingest.aio_works) ELSE NULL END,1,
CASE WHEN source_id='ambiguous' THEN 'ambiguous' WHEN source_id='unmatched' THEN 'unmatched' ELSE 'matched' END,
CASE WHEN source_id='ambiguous' THEN 'multiple plausible works' WHEN source_id='unmatched' THEN 'year conflict' ELSE 'exact alias with compatible evidence' END
FROM ingest.torrents WHERE source_id<>'pending';
DELETE FROM ingest.aio_match_queue WHERE source_id NOT IN ('pending','queued');
INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,pause_reason)
VALUES('synthetic-full','alpha','Alpha source','full','paused','fixture','{"id":"alpha","adapter":"http_json"}','manual');
INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type,payload_retained)
VALUES('synthetic-full','alpha',1,''::bytea,'application/json',false);
INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,payload_retained)
SELECT 'synthetic-full','alpha',source_id,id,1,''::bytea,'application/json',false
FROM ingest.pages CROSS JOIN (VALUES('matched'),('new')) AS items(source_id) WHERE run_id='synthetic-full';
INSERT INTO ingest.staged_torrents(run_id,provider_id,source_id,fields,raw_id)
SELECT run_id,provider_id,source_id,'{}'::jsonb,id FROM ingest.raw_records WHERE run_id='synthetic-full';`)
  } finally {
    await db.end()
  }
  const response = await page.request.get('/api/source-torrent-stats')
  expect(response.status()).toBe(200)
  const text = await response.text()
  expect(text).not.toContain('private-path')
  expect(text).not.toContain('Alpha identified release')
  const body: SourceTorrentOverview = JSON.parse(text)
  expect(body.totals).toEqual({ imported: 7, awaiting_import: 2 })
  expect(body.items?.find((item) => item.provider_id === 'alpha')).toMatchObject({
    provider_id: 'alpha',
    imported: 5,
    matched: 1,
    unmatched: 1,
    ambiguous: 1,
    pending: 2,
    pending_import: { run_id: 'synthetic-full', torrents: 2, new: 1, updates: 1 },
  })
  await page.goto('/providers')
  const alpha = page.locator('section').filter({ hasText: 'Alpha source' })
  await expect(
    alpha.getByRole('link', { name: 'View 5 imported torrents from Alpha source', exact: true }),
  ).toBeVisible()
  await expect(
    alpha.getByRole('link', { name: 'View 5 imported torrents from Alpha source', exact: true }),
  ).toContainText('71.4%')
  await expect(
    alpha.getByRole('link', { name: 'View 1 identified torrents from Alpha source', exact: true }),
  ).toContainText('20%')
  await expect(alpha.locator('dl > div').filter({ hasText: 'New records:' })).toContainText('50%')
  await expect(alpha.locator('dl > div').filter({ hasText: 'Updates:' })).toContainText('50%')
  for (const [total, result] of [
    [1, 'identified'],
    [1, 'not identified'],
    [1, 'needs review'],
    [2, 'awaiting identification'],
  ])
    await expect(
      alpha.getByRole('link', {
        name: `View ${total} ${result} torrents from Alpha source`,
        exact: true,
      }),
    ).toBeVisible()
  await expect(alpha.getByRole('link', { name: '2 awaiting import completion' })).toHaveAttribute(
    'href',
    '/runs/synthetic-full',
  )
  await expect(
    page.getByRole('link', { name: 'View 0 imported torrents from Empty source', exact: true }),
  ).toBeVisible()
  await page
    .getByRole('link', { name: 'View 1 identified torrents from Beta source', exact: true })
    .click()
  await expect(page).toHaveURL(/match_status=matched&match_provider=beta$/)
  await expect(
    page
      .locator('#matching-results')
      .getByRole('link', { name: 'Beta identified release', exact: true }),
  ).toBeVisible()
  await expect(page.locator('#matching-results')).not.toContainText('Alpha identified release')
  await page.goto('/providers')
  await alpha
    .getByRole('link', { name: 'View 5 imported torrents from Alpha source', exact: true })
    .click()
  await expect(page).toHaveURL(/\/torrents\?provider=alpha$/)
  await expect(
    page.getByRole('link', { name: 'Alpha identified release', exact: true }),
  ).toBeVisible()
  await expect(page.locator('.page')).not.toContainText('Beta identified release')
  await page.goto('/remotes')
  const remote = page.getByRole('region', { name: 'Remote catalog Remote copy', exact: true })
  await expect(
    remote.getByRole('link', { name: 'View 1 imported torrents from Remote copy', exact: true }),
  ).toBeVisible()
  await expect(
    remote.getByRole('link', { name: 'View 1 identified torrents from Remote copy', exact: true }),
  ).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.goto('/providers')
  const imported = alpha.getByRole('link', {
    name: 'View 5 imported torrents from Alpha source',
    exact: true,
  })
  const bounds = await imported.boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.x).toBeGreaterThanOrEqual(0)
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  expect(server.sourceRequests()).toBe(0)
})

test('unavailable source counts preserve source controls and retry without inventing zero totals', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  expect(
    (
      await page.request.put('/api/providers/example', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'example',
            name: 'Example source',
            adapter: 'http_json',
            url: server.sourceURL,
            enabled: false,
          }),
        },
      })
    ).status(),
  ).toBe(200)
  let failed = true
  await page.route('**/api/source-torrent-stats', async (route) => {
    if (failed)
      await route.fulfill({
        status: 503,
        json: { error: 'Source counts are temporarily unavailable.' },
      })
    else await route.continue()
  })
  await page.goto('/providers')
  await expect(page.getByText('Counts unavailable', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Example source', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: /^View \d+ imported torrents/ })).toHaveCount(0)
  failed = false
  await page.getByRole('button', { name: 'Try again', exact: true }).click()
  await expect(
    page.getByRole('link', { name: 'View 0 imported torrents from Example source', exact: true }),
  ).toBeVisible()
  await expect(page.getByText('Counts unavailable', { exact: true })).toHaveCount(0)
  expect(server.sourceRequests()).toBe(0)
})
