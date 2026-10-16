import pg from 'pg'
import { test, expect } from './fixtures'

test.describe('returning from details', () => {
  test.use({ collectionSource: true })
  test('paused runs explain manual recovery and link directly to their own source settings', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const definition = {
      version: 1,
      id: 'example',
      name: 'Example source',
      enabled: false,
      adapter: 'http_json',
      url: server.sourceURL,
    }
    expect(
      (
        await page.request.put('/api/providers/example', {
          headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
          data: { revision: '', json: JSON.stringify(definition) },
        })
      ).status(),
    ).toBe(200)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,trigger,revision,config,pause_reason,finished_at)
        VALUES('quota-hold','example','Example source','full','paused','scheduled','',$1,'quota',now()),
              ('progress-hold','example','Example source','full','paused','scheduled','',$1,'no_progress',now()),
              ('budget-hold','example','Example source','full','paused','scheduled','',$1,'budget',now()),
              ('interrupted-hold','example','Example source','full','paused','manual','',$1,'interrupted',now())`,
        [JSON.stringify({ ...definition, enabled: true })],
      )
    } finally {
      await db.end()
    }
    for (const run of ['quota-hold', 'progress-hold']) {
      await page.goto(`/runs/${run}`)
      await expect(
        page.getByRole('status').filter({ hasText: 'not continue automatically' }),
      ).toBeVisible()
      await expect(page.getByRole('button', { name: 'Keep paused', exact: true })).toHaveCount(0)
      await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeEnabled()
      await expect(
        page.getByRole('link', { name: 'View source settings', exact: true }),
      ).toHaveCount(0)
    }
    await page.goto('/runs/interrupted-hold')
    await expect(page.getByRole('button', { name: 'Keep paused', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Keep paused', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Manual hold:' })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('status').filter({ hasText: 'Manual hold:' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Keep paused', exact: true })).toHaveCount(0)
    await page.goto('/runs/budget-hold')
    await page.getByRole('link', { name: 'View source settings', exact: true }).click()
    await expect(page).toHaveURL(/\/providers\/example#schedules$/)
    await expect(page.getByRole('tab', { name: 'Schedules', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(server.sourceRequests()).toBe(0)
  })
  test('source edits and run tabs return to the originating filters and page, including after reload', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const definition = {
      version: 1,
      id: 'example',
      name: 'Example source',
      enabled: false,
      adapter: 'http_json',
      url: server.sourceURL,
    }
    const saved = await page.request.put('/api/providers/example', {
      headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
      data: { revision: '', json: JSON.stringify(definition) },
    })
    expect(saved.status()).toBe(200)
    await page.goto('/providers/settings?q=Example&state=disabled')
    const sourcesView = page.url()
    await page.getByRole('link', { name: 'Example source', exact: true }).click()
    await page.reload()
    await page.getByRole('link', { name: 'All sources', exact: true }).click()
    await expect(page).toHaveURL(sourcesView)
    await page.getByRole('link', { name: 'Add source', exact: true }).click()
    await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
    await page.locator('input[type="file"]').setInputFiles({
      name: 'source.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify({ ...definition, id: 'added', name: 'Example added' })),
    })
    await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page).toHaveURL(/\/providers\/added$/)
    await page.getByRole('link', { name: 'All sources', exact: true }).click()
    await expect(page).toHaveURL(sourcesView)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
        SELECT 'saved-run-'||n,'example','Example source','preview','succeeded','',$1::jsonb,now()
        FROM generate_series(1,51) n`,
        [JSON.stringify(definition)],
      )
      await db.query(`INSERT INTO ingest.events(run_id,kind,message)
        SELECT 'saved-run-1','page_saved','Synthetic event '||n FROM generate_series(1,55) n`)
    } finally {
      await db.end()
    }
    await page.goto('/runs?provider=example&status=succeeded&limit=50&offset=50')
    const importsView = page.url()
    await page.getByRole('link', { name: /^Open run saved-run-/ }).click()
    await page.getByRole('tab', { name: 'Published changes', exact: true }).click()
    await page.getByRole('tab', { name: 'Processed records', exact: true }).click()
    await page.reload()
    await page.getByRole('link', { name: 'Sources history', exact: true }).click()
    await expect(page).toHaveURL(importsView)
    await page.goto('/runs')
    await page
      .getByRole('link', { name: /^Open run saved-run-/ })
      .first()
      .click()
    await page.getByRole('link', { name: 'Sources history', exact: true }).last().click()
    await expect(page).toHaveURL(server.origin + '/runs')
    await page.goto('/runs/saved-run-1')
    await expect(page.getByRole('region', { name: 'Run events', exact: true })).toBeVisible()
    for (const width of [1920, 390]) {
      await page.setViewportSize({ width, height: 1100 })
      const workspace = page.locator('#main-content .page').first()
      const expected = await workspace.evaluate((element) => {
        const styles = getComputedStyle(element)
        return {
          left: element.getBoundingClientRect().left + parseFloat(styles.paddingLeft),
          width:
            element.clientWidth - parseFloat(styles.paddingLeft) - parseFloat(styles.paddingRight),
        }
      })
      const body = await page.locator('#main-content .page').last().boundingBox()
      expect(body!.x).toBeCloseTo(expected.left, 0)
      expect(body!.width).toBeCloseTo(expected.width, 0)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
    await page.setViewportSize({ width: 1440, height: 1100 })
    await expect(page.getByRole('tab', { name: 'Resume point', exact: true })).toHaveCount(0)
    const events = page.getByRole('region', { name: 'Run events', exact: true })
    await expect(events.getByRole('button', { name: 'Next', exact: true })).toHaveCount(1)
    await expect(events.getByRole('button', { name: 'Previous', exact: true })).toHaveCount(1)
    await expect(events.getByRole('button', { name: 'First', exact: true })).toBeDisabled()
    await expect(events.getByRole('button', { name: 'Last', exact: true })).toBeEnabled()
    await expect(events.getByRole('row').nth(1)).toContainText('Synthetic event 55')
    const next = events.getByRole('button', { name: 'Next', exact: true })
    expect(
      await next.evaluate((button) =>
        Boolean(
          button.compareDocumentPosition(button.closest('section')!.querySelector('table')!) &
          Node.DOCUMENT_POSITION_PRECEDING,
        ),
      ),
    ).toBe(true)
    await next.click()
    await expect(events.getByRole('row')).toHaveCount(6)
    await expect(events.getByRole('row').nth(1)).toContainText('Synthetic event 5')
    await expect(page).toHaveURL(/events_offset=50/)
    await page.reload()
    await expect(events.getByRole('row')).toHaveCount(6)
    await events.getByRole('button', { name: 'First', exact: true }).click()
    await expect(events.getByRole('row')).toHaveCount(51)
    await expect(events.getByRole('row').nth(1)).toContainText('Synthetic event 55')
    await events.getByRole('button', { name: 'Last', exact: true }).click()
    await expect(events.getByRole('row')).toHaveCount(6)
    await events.getByRole('button', { name: 'Previous', exact: true }).click()
    await expect(events.getByRole('row')).toHaveCount(51)
    expect(server.sourceRequests()).toBe(0)
  })
})

test('empty identification and operation event pages recover without discarding filters or page sizes', async ({
  page,
  server,
}) => {
  await page.goto(
    '/metadata?match_status=unmatched&match_provider=example&match_q=missing&match_limit=25&match_offset=100#matching-results',
  )
  const results = page.locator('#matching-results')
  await results.getByRole('button', { name: 'Return to first page', exact: true }).click()
  await expect.poll(() => new URL(page.url()).searchParams.has('match_offset')).toBe(false)
  expect(new URL(page.url()).searchParams.has('match_limit')).toBe(false)
  await expect(results.getByLabel('Identification result', { exact: true })).toHaveValue(
    'unmatched',
  )
  await expect(results.getByLabel('Title, source ID or info hash', { exact: true })).toHaveValue(
    'missing',
  )
  await results.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(results.getByLabel('Identification result', { exact: true })).toHaveValue('all')
  await expect(results.getByLabel('Source', { exact: true })).toHaveValue('')
  await expect(results.getByLabel('Title, source ID or info hash', { exact: true })).toHaveValue('')
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,finished_at)
      VALUES('retained-update','metadata','tmdb','manual',0,'succeeded','details',now())`)
    await db.query(`INSERT INTO ingest.operation_events(run_id,phase,processed,errors)
      VALUES('retained-update','details',5,0)`)
  } finally {
    await db.end()
  }
  await page.goto('/metadata/history/retained-update?limit=25&offset=100')
  const events = page.getByRole('region', { name: 'Operation events', exact: true })
  await events.getByRole('button', { name: 'Return to first page', exact: true }).click()
  await expect.poll(() => new URL(page.url()).searchParams.has('offset')).toBe(false)
  await expect(events.getByRole('cell', { name: '5', exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'History', exact: true }).last().click()
  await expect(page).toHaveURL(server.origin + '/metadata/history')
})

test('sharing forms use common draft feedback and do not write reverted edits', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const authorization = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const source = await page.request.put('/api/providers/example', {
    headers: authorization,
    data: {
      revision: '',
      json: JSON.stringify({
        version: 1,
        id: 'example',
        name: 'Example source',
        enabled: false,
        adapter: 'http_json',
        url: server.sourceURL,
      }),
    },
  })
  expect(source.status()).toBe(200)
  const shared = await page.request.post('/api/shares', {
    headers: authorization,
    data: {
      name: 'Example share',
      scope: 'selected',
      source_ids: ['example'],
      fields: ['title'],
      enabled: false,
      expires_at: null,
      requests_per_minute: 120,
      max_concurrent_downloads: 2,
    },
  })
  expect(shared.status()).toBe(201)
  let writes = 0
  page.on('request', (request) => {
    if (request.method() === 'PUT' && /^\/api\/shares\//.test(new URL(request.url()).pathname))
      writes++
  })
  const dialog = page.getByRole('dialog')
  await page.goto('/sharing')
  await page.getByRole('button', { name: 'Edit permissions & limits', exact: true }).click()
  const shareSave = dialog.getByRole('button', { name: 'Save permissions', exact: true })
  await expect(shareSave).toBeDisabled()
  await dialog.getByLabel('Recipient / share name', { exact: true }).fill('Changed share')
  await expect(shareSave).toBeEnabled()
  await dialog.getByLabel('Recipient / share name', { exact: true }).fill('Example share')
  await expect(shareSave).toBeDisabled()
  await expect(dialog.getByRole('status')).toHaveText('No unsaved changes')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  expect(writes).toBe(0)
  await page.goto('/secrets')
  const advanced = page.locator('details').filter({ hasText: 'Using references in JSON' })
  expect(await advanced.evaluate((element) => (element as HTMLDetailsElement).open)).toBe(false)
  await page
    .locator('.page-heading')
    .getByRole('button', { name: 'Create secret', exact: true })
    .click()
  await expect(dialog.getByRole('status')).toHaveText('No unsaved changes')
  await dialog.getByLabel('Reference name', { exact: true }).fill('example')
  await expect(dialog.getByRole('status')).toHaveText('Unsaved changes')
  await dialog.getByLabel('Reference name', { exact: true }).fill('')
  await expect(dialog.getByRole('status')).toHaveText('No unsaved changes')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
})
