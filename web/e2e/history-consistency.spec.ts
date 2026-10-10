import type { Locator, Page } from '@playwright/test'
import pg from 'pg'
import { test, expect } from './fixtures'

async function checkRow(row: Locator, value: string) {
  if (value.startsWith('pagination-'))
    await expect(row.getByRole('link', { name: `Open run ${value}`, exact: true })).toBeVisible()
  else await expect(row).toContainText(value)
}

async function checkPages(page: Page, path: string, first: string, last: string) {
  await page.goto(path)
  const table = page.getByRole('table').first()
  const pager = page.getByRole('navigation', { name: 'Pagination', exact: true }).first()
  await checkRow(table.getByRole('row').nth(1), first)
  await expect(table.getByRole('row')).toHaveCount(51)
  await expect(pager.getByRole('button')).toHaveText(['First', 'Previous', 'Next', 'Last'])
  await expect(pager.getByRole('button', { name: 'First', exact: true })).toBeDisabled()
  await pager.getByRole('button', { name: 'Last', exact: true }).click()
  await expect(table.getByRole('row')).toHaveCount(4)
  await checkRow(table.getByRole('row').last(), last)
  await expect(pager.getByRole('button', { name: 'Next', exact: true })).toBeDisabled()
  await expect(pager.getByRole('button', { name: 'Last', exact: true })).toBeDisabled()
  for (const [name, value] of new URL(path, page.url()).searchParams)
    expect(new URL(page.url()).searchParams.get(name)).toBe(value)
  await pager.getByRole('button', { name: 'Previous', exact: true }).click()
  await expect(table.getByRole('row')).toHaveCount(51)
  await pager.getByRole('button', { name: 'Next', exact: true }).click()
  await expect(table.getByRole('row')).toHaveCount(4)
  await pager.getByRole('button', { name: 'First', exact: true }).click()
  await checkRow(table.getByRole('row').nth(1), first)
  await expect(table.getByRole('row')).toHaveCount(51)
}

test.describe('history order and shared pagination', () => {
  test.use({ collectionSource: true })
  test('histories show recent entries first and every paginated view can reach either end', async ({
    page,
    server,
  }) => {
    test.setTimeout(90_000)
    const session = await (await page.request.get('/api/session')).json()
    const definition = {
      version: 1,
      id: 'pagination',
      name: 'Pagination source',
      enabled: false,
      adapter: 'http_json',
      url: server.sourceURL,
    }
    expect(
      (
        await page.request.put('/api/providers/pagination', {
          headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
          data: { revision: '', json: JSON.stringify(definition) },
        })
      ).status(),
    ).toBe(200)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,created_at,finished_at)
        SELECT 'pagination-run-'||lpad(n::text,4,'0'),'pagination','Pagination source','preview','succeeded','',$1,
          now()-n*interval '1 second',now() FROM generate_series(1,103) n`,
        [JSON.stringify(definition)],
      )
      await db.query(`INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,processed,created_at,finished_at)
        SELECT 'pagination-metadata-'||lpad(n::text,4,'0'),'matching','local','manual',0,'succeeded','identification',n,
          now()-n*interval '1 second',now() FROM generate_series(1,103) n;
        INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,processed,created_at,finished_at)
        SELECT 'pagination-debrid-'||lpad(n::text,4,'0'),'cache','torbox','playback',0,'succeeded','availability',n,
          now()-n*interval '1 second',now() FROM generate_series(1,103) n;
        INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results)
        SELECT now()-n*interval '1 second','movie','imdb:tt'||(1000000+n),1,'empty',0,0,0,0,0,0,0,false,'[]'
        FROM generate_series(1,103) n;
        INSERT INTO ingest.activity_logs(event_id,level,kind,message,created_at)
        SELECT 'pagination-log-'||n,'info','system.fixture','Pagination log '||lpad(n::text,4,'0'),now()-n*interval '1 second'
        FROM generate_series(1,103) n;
        INSERT INTO ingest.torrents(provider_id,source_id,fields,last_seen_at)
        SELECT 'pagination','item-'||lpad(n::text,4,'0'),jsonb_build_object('title','Pagination torrent '||lpad(n::text,4,'0')),
          now()-n*interval '1 second' FROM generate_series(1,103) n`)
    } finally {
      await db.end()
    }
    for (const [path, first, last] of [
      ['/runs?provider=pagination&status=succeeded', 'pagination-run-0001', 'pagination-run-0103'],
      [
        '/metadata/history?kind=matching&status=succeeded',
        'pagination-metadata-0001',
        'pagination-metadata-0103',
      ],
      [
        '/debrid/history?service=torbox&status=succeeded',
        'pagination-debrid-0001',
        'pagination-debrid-0103',
      ],
      ['/addon/history?status=empty', 'imdb:tt1000001', 'imdb:tt1000103'],
      ['/logs?q=Pagination%20log', 'Pagination log 0001', 'Pagination log 0103'],
      ['/torrents?q=Pagination', 'Pagination torrent 0001', 'Pagination torrent 0103'],
      [
        '/metadata/identification?match_status=all',
        'Pagination torrent 0001',
        'Pagination torrent 0103',
      ],
    ])
      await checkPages(page, path, first, last)
    await page.goto('/logs?q=Pagination%20log&offset=10000')
    const pager = page.getByRole('navigation', { name: 'Pagination', exact: true })
    await expect(page.getByText('No results on this page', { exact: true })).toBeVisible()
    await pager.getByRole('button', { name: 'Last', exact: true }).click()
    await expect(page.getByRole('row').last()).toContainText('Pagination log 0103')
    await page.goto('/logs?q=Pagination%20log&offset=10000')
    await pager.getByRole('button', { name: 'Previous', exact: true }).click()
    await expect(page.getByRole('row').last()).toContainText('Pagination log 0103')
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 844 })
      for (const name of ['First', 'Previous', 'Next', 'Last'])
        await expect(pager.getByRole('button', { name, exact: true })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
    expect(server.sourceRequests()).toBe(0)
  })
})

test('operation errors remain in their history and manual controls without appearing in unrelated sections', async ({
  page,
  server,
}) => {
  const message = 'Information update interrupted. Committed progress is preserved.'
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query('UPDATE ingest.aio_state SET sync_error=$1', [message])
    await db.query(
      `INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,error,finished_at)
      VALUES('interrupted-update','metadata','tmdb','automatic',0,'interrupted','details',$1,now())`,
      [message],
    )
  } finally {
    await db.end()
  }
  await page.goto('/metadata')
  await expect(page.getByText(message, { exact: true })).toHaveCount(0)
  const activity = page.getByRole('link', { name: 'View information update history', exact: true })
  await expect(
    activity.getByText('Information update needs attention', { exact: true }),
  ).toHaveAttribute('data-variant', 'warning')
  await activity.click()
  await expect(page).toHaveURL(/\/metadata\/history\?kind=metadata$/)
  await expect(page.getByRole('row').filter({ hasText: 'Interrupted' })).toContainText(message)
  await page.getByRole('link', { name: 'Open run interrupted-update', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: message })).toBeVisible()
  await page.goto('/metadata/settings')
  const manual = page
    .locator('details')
    .filter({ has: page.locator('summary', { hasText: 'Manual actions' }) })
  await expect(manual.getByText(message, { exact: false })).toBeHidden()
  await manual.locator('summary').click()
  await expect(manual.getByRole('status')).toContainText(message)
  await expect(manual.getByRole('link', { name: 'View history', exact: true })).toBeVisible()
  for (const path of ['/debrid', '/debrid/settings', '/addon', '/addon/settings']) {
    await page.goto(path)
    await expect(page.getByRole('tablist').first()).toBeVisible()
    await expect(page.locator('[data-slot="skeleton"]')).toHaveCount(0)
    await expect(page.getByText(message, { exact: false })).toHaveCount(0)
  }
})

test('Security opens its first view through navigation and existing secret bookmarks', async ({
  page,
}) => {
  for (const path of ['/security', '/security/secrets', '/secrets']) {
    await page.goto(path)
    await expect(page).toHaveURL(/\/security$/)
    await expect(page.getByRole('tab', { name: 'Secrets', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(
      page.getByRole('button', { name: 'Create secret', exact: true }).first(),
    ).toBeVisible()
  }
  await page.getByRole('tab', { name: 'Two-step verification', exact: true }).click()
  await expect(page).toHaveURL(/\/security\/verification$/)
  await page.reload()
  await expect(
    page.getByRole('tab', { name: 'Two-step verification', exact: true }),
  ).toHaveAttribute('aria-selected', 'true')
  await page
    .getByRole('navigation', { name: 'Main navigation', exact: true })
    .getByRole('link', { name: 'Security', exact: true })
    .click()
  await expect(page.getByRole('tab', { name: 'Secrets', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})
