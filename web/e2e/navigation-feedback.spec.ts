import pg from 'pg'
import { test, expect } from './fixtures'

test('confirmation wording and its action stay on the same revision until the dialog is reopened', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const input = {
    name: 'Original recipient',
    enabled: false,
    scope: 'all',
    source_ids: [],
    fields: ['title'],
    requests_per_minute: 120,
    max_concurrent_downloads: 2,
  }
  const created = await page.request.post('/api/shares', { headers, data: input })
  expect(created.status()).toBe(201)
  const { share } = await created.json()
  await page.goto('/sharing/settings')
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  const confirmation = page.getByRole('alertdialog')
  await expect(confirmation.getByRole('heading')).toHaveText(
    'Delete “Original recipient” permanently?',
  )
  const refresh = page.waitForResponse(async (response) => {
    if (new URL(response.url()).pathname !== '/api/shares' || response.request().method() !== 'GET')
      return false
    return (await response.json()).items.some(
      (item: { name: string }) => item.name === 'Renamed recipient',
    )
  })
  const changed = await page.request.put(`/api/shares/${share.id}`, {
    headers,
    data: { ...input, name: 'Renamed recipient', revision: share.revision },
  })
  expect(changed.status()).toBe(200)
  await refresh
  await expect(confirmation.getByRole('heading')).toHaveText(
    'Delete “Original recipient” permanently?',
  )
  const rejected = page.waitForResponse(
    (response) =>
      response.url().endsWith(`/api/shares/${share.id}`) &&
      response.request().method() === 'DELETE',
  )
  await confirmation.getByRole('button', { name: 'Delete share', exact: true }).click()
  expect((await rejected).status()).toBe(409)
  await expect(confirmation.getByRole('alert')).toBeVisible()
  await confirmation.getByRole('button', { name: 'Back', exact: true }).click()
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  await expect(confirmation.getByRole('heading')).toHaveText(
    'Delete “Renamed recipient” permanently?',
  )
  await confirmation.getByRole('button', { name: 'Delete share', exact: true }).click()
  await expect(confirmation).toHaveCount(0)
  expect((await (await page.request.get('/api/shares')).json()).items).toEqual([])
})

test('invalid search ranges keep the applied results and the editable draft without querying the server', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.torrents(provider_id,source_id,fields)
      VALUES('search-example','original','{"title":"Original searchable release"}')`)
  } finally {
    await db.end()
  }
  await page.goto('/torrents?q=original')
  const form = page.getByRole('form', { name: 'Torrents', exact: true })
  await expect(form.getByLabel('Search text', { exact: true })).toHaveValue('original')
  await expect(page.getByRole('navigation', { name: 'Pagination', exact: true })).toBeVisible()
  let requests = 0
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname === '/api/torrents' && url.searchParams.get('q') === 'edited') requests++
  })
  const query = form.getByLabel('Search text', { exact: true })
  const minimum = form.getByLabel('Minimum size (bytes)', { exact: true })
  const maximum = form.getByLabel('Maximum size (bytes)', { exact: true })
  await query.fill('edited')
  await minimum.fill('9007199254740993')
  await maximum.fill('9007199254740992')
  await form.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(form.getByRole('alert')).toContainText('Minimum size must not exceed maximum size.')
  expect(new URL(page.url()).searchParams.get('q')).toBe('original')
  await expect(query).toHaveValue('edited')
  await expect(minimum).toHaveValue('9007199254740993')
  await expect(maximum).toHaveAttribute('aria-invalid', 'true')
  await expect(
    page.getByRole('link', { name: 'Original searchable release', exact: true }),
  ).toBeVisible()
  expect(requests).toBe(0)
  await minimum.fill('')
  await maximum.fill('')
  const after = form.getByLabel('Published after (browser local time)', { exact: true })
  const before = form.getByLabel('Published before (browser local time)', { exact: true })
  await after.fill('2026-10-13T12:00')
  await before.fill('2026-10-12T12:00')
  await form.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(form.getByRole('alert')).toContainText(
    'Published after must not be later than published before.',
  )
  expect(new URL(page.url()).searchParams.get('q')).toBe('original')
  await expect(after).toHaveAttribute('aria-invalid', 'true')
  expect(requests).toBe(0)
  await form.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(form.getByRole('alert')).toHaveCount(0)
  await expect(query).toHaveValue('')
  await minimum.fill('0009007199254740992')
  await maximum.fill('9007199254740993')
  const applied = page.waitForResponse(
    (response) => new URL(response.url()).pathname === '/api/torrents',
  )
  await form.getByRole('button', { name: 'Search', exact: true }).click()
  expect((await applied).status()).toBe(200)
  await expect(form.getByRole('alert')).toHaveCount(0)
})

test('non-metadata histories do not promise information retries for retained errors', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,processed,errors,results,finished_at)
      VALUES('cache-partial','cache','torbox','playback',0,'succeeded','availability',2,1,'{"cached":1}',now()),
      ('metadata-partial','metadata','tmdb','automatic',0,'succeeded','details',2,1,'{}',now())`)
  } finally {
    await db.end()
  }
  await page.goto('/debrid/history/cache-partial')
  await expect(page.getByRole('heading', { name: 'Cache verification', exact: true })).toBeVisible()
  await expect(page.locator('dl')).toContainText('Errors')
  await expect(
    page.getByText('Failed information requests remain available', { exact: false }),
  ).toHaveCount(0)
  await page.goto('/metadata/history/metadata-partial')
  await expect(
    page
      .getByRole('status')
      .filter({ hasText: 'Failed information requests remain available for a later retry.' }),
  ).toBeVisible()
})

test('long source names never stretch filter fields beyond small-screen layouts', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  expect(
    (
      await page.request.put('/api/providers/mobile-example', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'mobile-example',
            enabled: false,
            adapter: 'http_json',
            url: server.sourceURL,
            name: 'Example source with a descriptive display name for imported records',
          }),
        },
      })
    ).status(),
  ).toBe(200)
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 })
    for (const [path, field, value] of [
      ['/metadata/identification', 'Source', 'mobile-example'],
      ['/runs', 'Source', 'mobile-example'],
      ['/logs', 'Source or service', 'source:mobile-example'],
    ]) {
      await page.goto(path)
      const control = page.getByLabel(field, { exact: true })
      await expect(control.locator(`option[value="${value}"]`)).toHaveText(
        'Example source with a descriptive display name for imported records',
      )
      await expect
        .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), {
          message: `${path} at ${width}px`,
        })
        .toBe(true)
      await expect(control).toBeVisible()
      await control.selectOption(value)
      await expect(control).toHaveValue(value)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
  }
})

test.describe('return navigation', () => {
  test.use({ collectionSource: true })

  test('last-run links return to filtered source settings after switching detail tabs and reloading', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const definition = {
      version: 1,
      id: 'return-source',
      name: 'Return source',
      enabled: false,
      adapter: 'http_json',
      url: server.sourceURL,
    }
    expect(
      (
        await page.request.put('/api/providers/return-source', {
          headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
          data: { revision: '', json: JSON.stringify(definition) },
        })
      ).status(),
    ).toBe(200)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
        VALUES('return-run','return-source','Return source','preview','succeeded','',$1::jsonb,now())`,
        [JSON.stringify(definition)],
      )
    } finally {
      await db.end()
    }
    await page.goto('/providers/settings?q=Return&state=disabled')
    const origin = page.url()
    await page
      .getByRole('region', { name: 'Source Return source', exact: true })
      .getByRole('link', { name: 'Completed', exact: true })
      .click()
    await expect(page).toHaveURL(/\/runs\/return-run$/)
    await page.getByRole('tab', { name: 'Processed records', exact: true }).click()
    await page.reload()
    await page.getByRole('link', { name: 'All sources', exact: true }).click()
    await expect(page).toHaveURL(origin)
    await expect(page.getByLabel('Find a source', { exact: true })).toHaveValue('Return')
  })

  test('a manual import returns to the source configuration that started it', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    expect(
      (
        await page.request.put('/api/providers/manual-return', {
          headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
          data: {
            revision: '',
            json: JSON.stringify({
              version: 1,
              id: 'manual-return',
              name: 'Manual return',
              adapter: 'http_json',
              url: server.sourceURL,
              enabled: true,
              http: { items_path: '/items' },
              mapping: { id: '/id', fields: { title: '/title' } },
            }),
          },
        })
      ).status(),
    ).toBe(200)
    await page.goto('/providers/manual-return')
    await page.getByRole('button', { name: 'Run manual import', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.locator('#collect-provider')).toHaveText('Manual return')
    await expect(dialog.getByRole('combobox', { name: 'Source', exact: true })).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Run manual import', exact: true }).click()
    await expect(page).toHaveURL(/\/runs\//)
    await page.reload()
    await page.getByRole('link', { name: 'Source settings', exact: true }).click()
    await expect(page).toHaveURL(/\/providers\/manual-return$/)
    expect(server.sourceRequests()).toBeGreaterThan(0)
  })

  test('processed records opened from Search return to the same filters after reload', async ({
    page,
    server,
  }) => {
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(`INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
        VALUES('search-origin','search-source','Search example','preview','succeeded','','{}',now());
        WITH saved_page AS (
          INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type)
          VALUES('search-origin','search-source',1,''::bytea,'application/json') RETURNING id
        ), saved_record AS (
          INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields)
          SELECT 'search-origin','search-source','record',id,1,''::bytea,'application/json','{"title":"Search example release"}'
          FROM saved_page RETURNING id,fields
        )
        INSERT INTO ingest.torrents(provider_id,source_id,fields,raw_id)
        SELECT 'search-source','record',fields,id FROM saved_record`)
    } finally {
      await db.end()
    }
    await page.goto('/torrents?q=example&provider=search-source&sort=oldest')
    const origin = page.url()
    await page.getByRole('link', { name: 'Open processed record for record', exact: true }).click()
    await expect(page).toHaveURL(/\/raw\/[0-9]+$/)
    await page.reload()
    await page.locator('main').getByRole('link', { name: 'Torrents', exact: true }).click()
    await expect(page).toHaveURL(origin)
    await expect(page.getByLabel('Search text', { exact: true })).toHaveValue('example')
  })

  test('remote run details return to the filtered remote catalogs view', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
    expect(
      (
        await page.request.put('/api/secrets/remote_return_key', {
          headers,
          data: { revision: 0, value: 'synthetic-remote-credential' },
        })
      ).status(),
    ).toBe(204)
    const definition = {
      version: 1,
      id: 'remote-return',
      name: 'Remote return',
      adapter: 'http_json',
      enabled: false,
      url: 'https://catalog.example/api/catalogs/example',
      auth: { type: 'bearer', secret_ref: 'remote_return_key' },
      http: { catalog: true, items_path: '/items' },
      pagination: {
        type: 'cursor',
        in: 'query',
        cursor_param: 'cursor',
        size_param: 'limit',
        next_path: '/next_cursor',
        start: 0,
      },
      mapping: { id: '/id', fields: {} },
      catalog_sync: { enabled: false, interval_minutes: 60 },
    }
    expect(
      (
        await page.request.put('/api/providers/remote-return', {
          headers,
          data: { revision: '', json: JSON.stringify(definition) },
        })
      ).status(),
    ).toBe(200)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
        VALUES('remote-return-run','remote-return','Remote return','full','succeeded','',$1::jsonb,now())`,
        [JSON.stringify(definition)],
      )
    } finally {
      await db.end()
    }
    await page.goto('/sharing/remotes?q=Remote&state=disabled')
    // The suffix also matches the legacy address before its redirect commits.
    await expect(page).toHaveURL(server.origin + '/remotes/settings?q=Remote&state=disabled')
    const origin = page.url()
    await page.getByRole('link', { name: 'Completed', exact: true }).click()
    await page.reload()
    await page.getByRole('main').getByRole('link', { name: 'Remote catalogs', exact: true }).click()
    await expect(page).toHaveURL(origin)
    expect(server.sourceRequests()).toBe(0)
  })
})
