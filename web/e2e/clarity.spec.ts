import { openDisclosure } from './disclosures'
import pg from 'pg'
import { test, expect } from './fixtures'

test('navigation waits for an explicit save to finish without offering to discard pending writes', async ({
  page,
}) => {
  await page.goto('/import-settings')
  await openDisclosure(page, 'Import execution')
  await openDisclosure(page, 'Concurrency')
  const workers = page.getByLabel('Simultaneous imports', { exact: true })
  await workers.fill('2')
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/settings/collections', async (route) => {
    if (route.request().method() === 'PUT') await gate
    await route.fallback()
  })
  try {
    await page
      .getByRole('form', { name: 'Import execution', exact: true })
      .getByRole('button', { name: 'Save', exact: true })
      .click()
    await page
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Logs', exact: true })
      .click()
    const prompt = page.getByRole('alertdialog')
    await expect(prompt).toContainText('A save is still in progress')
    await expect(
      prompt.getByRole('button', { name: 'Discard changes', exact: true }),
    ).toBeDisabled()
    release()
    await expect(page.getByRole('heading', { name: 'Logs', exact: true })).toBeVisible()
    await expect(prompt).toHaveCount(0)
    expect(
      (await (await page.request.get('/api/settings/collections')).json()).settings.workers,
    ).toBe(2)
  } finally {
    release()
    // Keep the forwarding handler until context teardown; the gate is released.
  }
})

test('source configuration navigation preserves unsaved edits and saving releases the guard', async ({
  page,
  server,
}) => {
  // A slower render exposes edits landing in the outgoing creation form after
  // the URL changes but before the saved editor mounts. Do not hide that gap
  // by waiting for a different heading before making the next edit.
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 8 })
  await page.goto('/providers/new')
  await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
  await page.getByLabel('Import a source JSON definition', { exact: true }).setInputFiles({
    name: 'synthetic-navigation-source.json',
    mimeType: 'application/json',
    buffer: Buffer.from(
      JSON.stringify({
        version: 1,
        id: 'guard-source',
        name: 'Guard source',
        adapter: 'http_json',
        url: server.sourceURL,
        enabled: false,
      }),
    ),
  })
  await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page).toHaveURL(/\/providers\/guard-source$/)
  await expect(page.getByRole('alertdialog')).toHaveCount(0)
  await page.getByLabel('Display name', { exact: true }).fill('Unsaved source name')
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Sources', exact: true })
    .click()
  const prompt = page.getByRole('alertdialog')
  await expect(prompt).toContainText('source configuration')
  await prompt.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('Unsaved source name')
  const persisted = await (await page.request.get('/api/providers/guard-source')).json()
  expect(persisted.provider.name).toBe('Guard source')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Sources', exact: true })
    .click()
  await expect(page.getByRole('heading', { name: 'Sources', exact: true, level: 1 })).toBeVisible()
  await expect(prompt).toHaveCount(0)
})

test('unsaved forms survive navigation, browser Back and reload until explicitly discarded', async ({
  page,
  server,
}) => {
  await page.goto('/logs')
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Sources', exact: true })
    .click()
  const prompt = page.getByRole('alertdialog')
  const beforeSettings = await page.evaluate(() => history.state.idx)
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await expect.poll(() => page.evaluate(() => history.state.idx)).toBe(beforeSettings + 1)
  await openDisclosure(page, 'Advanced')
  await openDisclosure(page, 'Import execution')
  await openDisclosure(page, 'Concurrency')
  const workers = page.getByLabel('Simultaneous imports', { exact: true })
  await workers.fill('7')
  await page.evaluate(() => history.back())
  await expect(prompt).toContainText('import execution settings')
  await expect(prompt.getByRole('button', { name: 'Keep editing', exact: true })).toBeFocused()
  await prompt.getByRole('button', { name: 'Keep editing' }).click()
  await expect(workers).toHaveValue('7')
  const persisted = await (await page.request.get('/api/settings/collections')).json()
  expect(persisted.settings.workers).not.toBe(7)
  const native = page.waitForEvent('dialog')
  const reloading = page.reload({ timeout: 5000 }).catch(() => {})
  const dialog = await native
  expect(dialog.type()).toBe('beforeunload')
  await dialog.dismiss()
  await reloading
  await expect(workers).toHaveValue('7')
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(prompt).toContainText('import execution settings')
  await prompt.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(prompt).toHaveCount(0)
  await expect(workers).toHaveValue('7')
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Logs', exact: true })
    .click()
  await prompt.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Logs', exact: true })).toBeVisible()
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Sources', exact: true })
    .click()
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await openDisclosure(page, 'Advanced')
  await openDisclosure(page, 'Import execution')
  await openDisclosure(page, 'Concurrency')
  await expect(workers).toHaveValue(String(persisted.settings.workers))
  // All groups share one explicit save and preserve their drafts when folded.
  await expect(
    page.getByRole('form', { name: 'Import execution', exact: true }).locator('details'),
  ).toHaveCount(4)
  await openDisclosure(page, 'Quota handling')
  const retries = page.getByLabel('Automatic retries after HTTP 429', { exact: true })
  await retries.fill('11')
  await expect(retries).toBeVisible()
  await expect(retries).toHaveAttribute('aria-invalid', 'true')
  await expect(
    page
      .getByRole('form', { name: 'Import execution', exact: true })
      .getByRole('button', { name: 'Save', exact: true }),
  ).toBeDisabled()
  await retries.fill(String(persisted.settings.max_quota_retries))
  expect((await (await page.request.get('/api/settings/collections')).json()).settings).toEqual(
    persisted.settings,
  )
  expect(server.sourceRequests()).toBe(0)
})

test('closing an edited secret asks before discarding and never stores plaintext in the browser', async ({
  page,
}) => {
  await page.goto('/debrid/settings')
  await page.getByRole('button', { name: 'Add AllDebrid key', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('AllDebrid API key', { exact: true }).fill('synthetic-unsaved-key')
  await editor.getByRole('button', { name: 'Cancel', exact: true }).click()
  const prompt = page.getByRole('alertdialog')
  await expect(prompt).toContainText('new secret')
  await prompt.getByRole('button', { name: 'Keep editing' }).click()
  await expect(editor.getByLabel('AllDebrid API key', { exact: true })).toHaveValue(
    'synthetic-unsaved-key',
  )
  await editor.getByRole('button', { name: 'Close', exact: true }).click()
  await prompt.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(editor).toHaveCount(0)
  const secrets = await (await page.request.get('/api/secrets')).json()
  expect(secrets.items).toEqual([])
  const storage = await page.evaluate(() =>
    JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }),
  )
  expect(storage).not.toContain('synthetic-unsaved-key')
  await page.getByRole('button', { name: 'Add AllDebrid key', exact: true }).click()
  await expect(editor.getByLabel('AllDebrid API key', { exact: true })).toHaveValue('')
  await editor.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(prompt).toHaveCount(0)
})

test('matching counters open real filtered diagnostics, explain decisions and use the browser timezone', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const current = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
        data: { ...current.config, auto_match: false },
      })
    ).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','1','tt1234567',ARRAY[2021]);
UPDATE ingest.aio_state SET last_sync='2026-01-02T03:04:00Z';
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
('diagnostic-source','matched','{"title":"Identified release","download_url":"https://source.invalid/private-path"}'),
('diagnostic-source','ambiguous','{"title":"Ambiguous release"}'),
('diagnostic-source','unmatched','{"title":"Wrong year release"}'),
('diagnostic-source','pending','{"title":"Pending release"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,md5(fields::text),CASE WHEN source_id='matched' THEN (SELECT id FROM ingest.aio_works) ELSE NULL END,1,
CASE WHEN source_id='matched' THEN 'matched' WHEN source_id='ambiguous' THEN 'ambiguous' ELSE 'unmatched' END,
CASE WHEN source_id='ambiguous' THEN 'multiple plausible works' WHEN source_id='unmatched' THEN 'year conflict' ELSE 'exact alias with compatible evidence' END FROM ingest.torrents WHERE source_id<>'pending';
DELETE FROM ingest.aio_match_queue WHERE source_id<>'pending';`)
    await db.query(
      "UPDATE ingest.aio_matches SET updated_at='2026-01-02T03:04:00Z' WHERE provider_id='diagnostic-source'",
    )
  } finally {
    await db.end()
  }
  await page.goto('/providers/settings')
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Metadata', exact: true })
    .click()
  const formatted = await page.evaluate(() =>
    new Intl.DateTimeFormat(undefined, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date('2026-01-02T03:04:00Z')),
  )
  const results = page.locator('#matching-results')
  await expect(results.locator('table')).toHaveCount(0)
  await page.getByRole('link', { name: 'View needs review', exact: true }).click()
  await expect(results.getByRole('link', { name: 'Ambiguous release', exact: true })).toBeVisible()
  await expect(results).toContainText(formatted)
  await expect(results).toContainText('Several films or series are plausible')
  await results.getByLabel('Identification result', { exact: true }).selectOption('unmatched')
  await results.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(results.getByRole('link', { name: 'Wrong year release', exact: true })).toBeVisible()
  await expect(results).toContainText('The release year disagrees')
  await results.getByLabel('Identification result', { exact: true }).selectOption('pending')
  await results.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(results.getByRole('link', { name: 'Pending release', exact: true })).toBeVisible()
  await expect(results).not.toContainText('tt1234567')
  await results.getByLabel('Identification result', { exact: true }).selectOption('matched')
  await results.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(results).toContainText('tt1234567')
  await results.getByRole('link', { name: 'Identified release', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  const diagnostic = await (await page.request.get('/api/aiosource/matches?status=matched')).text()
  expect(diagnostic).not.toContain('private-path')
  await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).click()
  await page.goto('/metadata?match_status=matched')
  await page.setViewportSize({ width: 390, height: 844 })
  const cards = page.getByRole('list', { name: 'Identification results', exact: true })
  await expect(cards.getByRole('link', { name: 'Identified release', exact: true })).toBeVisible()
  await expect(cards).toContainText('Title and supporting information agree.')
  await expect(cards).toContainText('tt1234567')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test.describe('import processing diagnostics', () => {
  test.use({ collectionSource: true })
  test('real Preview diagnostics filter errors without losing accepted samples or raw-payload semantics', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
    const source = {
      version: 1,
      id: 'diagnostic-import',
      name: 'Diagnostic import',
      adapter: 'http_json',
      url: server.sourceURL,
      enabled: true,
      pagination: { type: 'none' },
      http: { items_path: '/items' },
      mapping: { id: '/id', fields: { title: '/title' } },
      schedule: { enabled: false },
    }
    expect(
      (
        await page.request.put('/api/providers/diagnostic-import', {
          headers,
          data: { json: JSON.stringify(source), revision: '' },
        })
      ).status(),
    ).toBe(200)
    const response = await page.request.post('/api/runs', {
      headers,
      data: { provider_id: 'diagnostic-import', mode: 'preview', max_pages: 1 },
    })
    expect(response.status()).toBe(202)
    const run = await response.json()
    await expect
      .poll(async () => (await (await page.request.get(`/api/runs/${run.id}`)).json()).status)
      .toBe('succeeded')
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields,error,ignored,auxiliary,payload_retained)
SELECT run_id,provider_id,'synthetic-error',id,0,''::bytea,content_type,'{"title":"Rejected sample"}','Missing source identifier',false,false,false FROM ingest.pages WHERE run_id=$1;
`,
        [run.id],
      )
    } finally {
      await db.end()
    }
    await page.goto(`/raw?run=${run.id}`)
    await expect(
      page.getByText('1 error · 0 ignored · 0 enrichment · 1 accepted', { exact: false }),
    ).toBeVisible()
    await page
      .getByRole('form', { name: 'Processed record filters', exact: true })
      .getByLabel('Processing result', { exact: true })
      .selectOption('error')
    await expect(page.getByRole('link', { name: 'Rejected sample', exact: true })).toBeVisible()
    await expect(page.getByText('Missing source identifier', { exact: true })).toBeVisible()
    await page.getByRole('link', { name: 'Rejected sample', exact: true }).click()
    await expect(page.getByText('Raw payload not stored', { exact: false }).first()).toBeVisible()
    await page.goBack()
    await expect(page.getByLabel('Processing result', { exact: true })).toHaveValue('error')
    await page.getByLabel('Processing result', { exact: true }).selectOption('accepted')
    await expect(page.getByRole('link', { name: 'Rejected sample', exact: true })).toHaveCount(0)
    await expect(page.getByRole('table')).toContainText('Accepted')
    expect(server.sourceRequests()).toBe(1)
  })
})
