import pg from 'pg'
import { test, expect } from './fixtures'

test('remote manual imports share the source action dialog and open remote history', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { Origin: server.origin, 'X-CSRF-Token': session.csrf_token }
  expect(
    (
      await page.request.put('/api/secrets/remote-password', {
        headers,
        data: { value: 'synthetic-sharing-password', revision: 0 },
      })
    ).status(),
  ).toBe(204)
  for (const source of [
    {
      version: 1,
      id: 'remote-copy',
      name: 'Remote copy',
      adapter: 'http_json',
      enabled: true,
      url: 'https://catalog.example/api/catalogs/shared',
      auth: { type: 'bearer', secret_ref: 'remote-password' },
      http: { catalog: true, method: 'GET', items_path: '/items' },
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
    },
    {
      version: 1,
      id: 'native',
      name: 'Native source',
      adapter: 'http_json',
      enabled: true,
      url: server.sourceURL,
    },
  ])
    expect(
      (
        await page.request.put(`/api/providers/${source.id}`, {
          headers,
          data: { json: JSON.stringify(source), revision: '' },
        })
      ).status(),
    ).toBe(200)

  const requests: unknown[] = []
  // Inspect only the submitted action; do not contact a remote catalog or enqueue work.
  await page.route('**/api/runs', async (route) => {
    if (route.request().method() !== 'POST') return route.continue()
    requests.push(route.request().postDataJSON())
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ id: 'remote-action', remote_catalog: true }),
    })
  })
  for (const mode of ['incremental', 'full']) {
    if (mode === 'full') await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/remotes/settings')
    const card = page.getByRole('region', { name: 'Remote catalog Remote copy', exact: true })
    await expect(card.getByRole('button', { name: 'Full resync', exact: true })).toHaveCount(0)
    await card.getByRole('button', { name: 'Run manual import', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Run manual import', exact: true })
    await expect(dialog.locator('#collect-provider')).toHaveText('Remote copy')
    await expect(dialog.getByRole('combobox', { name: 'Remote catalog', exact: true })).toHaveCount(
      0,
    )
    const action = dialog.getByLabel('Action', { exact: true })
    await expect(action).toHaveValue('incremental')
    await expect(action.getByRole('option')).toHaveText(['Preview', 'Sync changes', 'Full resync'])
    await action.selectOption(mode)
    await dialog.getByText('Advanced', { exact: true }).click()
    await expect(dialog.getByLabel('Known pages (X)', { exact: true })).toHaveCount(0)
    await dialog.getByLabel('Page budget per attempt', { exact: true }).fill('0')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await dialog.getByRole('button', { name: 'Run manual import', exact: true }).click()
    await expect(page).toHaveURL(/\/remotes\/history\/remote-action$/)
    expect(requests.at(-1)).toEqual({ provider_id: 'remote-copy', mode, max_pages: 0 })
  }
  await page.goto('/providers/settings')
  await page
    .getByRole('region', { name: 'Source Native source', exact: true })
    .getByRole('button', { name: 'Run manual import', exact: true })
    .click()
  const dialog = page.getByRole('dialog', { name: 'Run manual import', exact: true })
  await expect(dialog.locator('#collect-provider')).toHaveText('Native source')
  await expect(dialog.getByRole('combobox', { name: 'Source', exact: true })).toHaveCount(0)
  await expect(dialog.getByLabel('Action', { exact: true }).getByRole('option')).toHaveText([
    'Preview',
    'Update',
    'Rebuild source catalog',
    'Fetch source details',
  ])
})

// Exercise saved records through the real administration API and isolated database.
test.describe('remote catalog history and ownership', () => {
  test.use({ collectionSource: true })

  test('remote imports have separate history, canonical detail links and complete deletion', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const headers = { Origin: server.origin, 'X-CSRF-Token': session.csrf_token }
    expect(
      (
        await page.request.put('/api/secrets/remote-password', {
          headers,
          data: { value: 'synthetic-sharing-password', revision: 0 },
        })
      ).status(),
    ).toBe(204)
    const remote = {
      version: 1,
      id: 'remote-copy',
      name: 'Remote copy',
      adapter: 'http_json',
      enabled: false,
      url: 'https://catalog.example/api/catalogs/shared',
      auth: { type: 'bearer', secret_ref: 'remote-password' },
      http: { catalog: true, method: 'GET', items_path: '/items' },
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
    const native = {
      version: 1,
      id: 'native',
      name: 'Native source',
      adapter: 'http_json',
      enabled: false,
      url: server.sourceURL,
    }
    for (const source of [remote, native])
      expect(
        (
          await page.request.put(`/api/providers/${source.id}`, {
            headers,
            data: { json: JSON.stringify(source), revision: '' },
          })
        ).status(),
      ).toBe(200)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      for (const source of [remote, native]) {
        await db.query(
          `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at) VALUES($1,$2,$3,'full','paused','',$4,now())`,
          [`${source.id}-run`, source.id, source.name, source],
        )
        await db.query(
          `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES($1,'one','{"title":"Synthetic imported record"}')`,
          [source.id],
        )
      }
      await db.query(
        `INSERT INTO ingest.remote_checkpoints(provider_id,endpoint,checkpoint) VALUES('remote-copy',$1,'synthetic-checkpoint')`,
        [remote.url],
      )
      await page.goto('/runs')
      await expect(page.locator('main tbody')).toContainText('Native source')
      await expect(page.locator('main tbody')).not.toContainText('Remote copy')
      await page.goto('/remotes/history')
      await expect(page.locator('main tbody')).toContainText('Remote copy')
      await expect(page.locator('main tbody')).not.toContainText('Native source')
      await expect(page.locator('main tbody')).toContainText('Full sync')
      await expect(
        page.getByLabel('Remote catalog', { exact: true }).getByRole('option'),
      ).toHaveText(['All remote catalogs', 'Remote copy'])
      await page.getByRole('link', { name: 'Open run remote-copy-run', exact: true }).click()
      await expect(page).toHaveURL(/\/remotes\/history\/remote-copy-run$/)
      await expect(page.getByRole('tab', { name: 'History', exact: true })).toHaveAttribute(
        'aria-selected',
        'true',
      )
      await page.goto('/runs/remote-copy-run')
      await expect(page).toHaveURL(/\/remotes\/history\/remote-copy-run$/)
      await page.getByRole('tab', { name: 'Statistics', exact: true }).click()
      await expect(
        page.getByRole('link', { name: 'View 1 imported torrents from Remote copy', exact: true }),
      ).toBeVisible()
      await page.getByRole('tab', { name: 'Settings', exact: true }).click()
      const card = page.getByRole('region', { name: 'Remote catalog Remote copy', exact: true })
      await expect(
        card.getByRole('switch', { name: 'Enable Remote copy', exact: true }),
      ).not.toBeChecked()
      await card.getByRole('switch', { name: 'Enable Remote copy', exact: true }).click()
      await expect(
        card.getByRole('switch', { name: 'Disable Remote copy', exact: true }),
      ).toBeChecked()
      const enabled = await (await page.request.get('/api/providers/remote-copy')).json()
      expect(enabled.provider.enabled).toBe(true)
      expect(enabled.provider.auth.secret_ref).toBe('remote-password')
      expect(enabled.provider.catalog_sync).toEqual(remote.catalog_sync)
      await card.getByRole('switch', { name: 'Disable Remote copy', exact: true }).click()
      await expect(
        card.getByRole('switch', { name: 'Enable Remote copy', exact: true }),
      ).not.toBeChecked()
      await expect(card.getByRole('link', { name: 'Logs', exact: true })).toHaveCount(0)
      await expect(card.getByRole('link', { name: 'History', exact: true })).toHaveCount(0)
      await expect(
        card.getByRole('link', { name: 'View imported data', exact: true }),
      ).toHaveAttribute('href', '/torrents?provider=remote-copy')
      await db.query(
        `UPDATE ingest.runs SET status='running',finished_at=NULL WHERE id='remote-copy-run'`,
      )
      await page.reload()
      await expect(card.getByRole('button', { name: 'Delete', exact: true })).toBeDisabled()
      await db.query(
        `UPDATE ingest.runs SET status='paused',finished_at=now() WHERE id='remote-copy-run'`,
      )
      await page.reload()
      await card.getByRole('button', { name: 'Delete', exact: true }).click()
      const confirmation = page.getByRole('alertdialog')
      await expect(confirmation).toContainText(
        'Secrets, shared metadata and debrid caches are kept.',
      )
      await confirmation.getByRole('button', { name: 'Delete remote catalog', exact: true }).click()
      await expect(confirmation).toBeHidden()
      await expect(card).toHaveCount(0)
      for (const table of [
        'runs',
        'torrents',
        'remote_checkpoints',
        'aio_matches',
        'aio_match_reviews',
        'staged_torrents',
      ])
        expect(
          (
            await db.query(
              `SELECT count(*)::int AS count FROM ingest.${table} WHERE provider_id='remote-copy'`,
            )
          ).rows,
          `Deleted remote data remained in ${table}`,
        ).toEqual([{ count: 0 }])
      expect(
        (
          await db.query(
            `SELECT count(*)::int AS count FROM ingest.torrents WHERE provider_id='native'`,
          )
        ).rows,
      ).toEqual([{ count: 1 }])
      expect(
        (await (await page.request.get('/api/secrets')).json()).items.some(
          (secret: { name: string }) => secret.name === 'remote-password',
        ),
      ).toBe(true)
      await page.goto('/providers/settings')
      await expect(
        page
          .getByRole('region', { name: 'Source Native source', exact: true })
          .getByRole('link', { name: 'View imported data', exact: true }),
      ).toHaveAttribute('href', '/torrents?provider=native')
    } finally {
      await db.end()
    }
  })
})

test('catalog sharing starts with all sources and keeps optional controls in Filters and Advanced', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/sharing')
  await expect(page.getByRole('tab', { name: 'Statistics', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('button', { name: 'Create share', exact: true })).toHaveCount(0)
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await page.getByRole('button', { name: 'Create share', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByLabel('Expires at (browser local time)', { exact: true })).toBeHidden()
  await dialog.getByText('Filters', { exact: true }).click()
  await expect(dialog.getByRole('button', { name: 'Sources', exact: true })).toContainText(
    'All sources',
  )
  await expect(
    dialog.getByLabel('I authorize access to all current and future sources.'),
  ).toHaveCount(0)
  await expect(dialog.getByText('leechers', { exact: true })).toHaveCount(0)
  await dialog.getByText('Advanced', { exact: true }).click()
  await dialog.getByText('Expiration', { exact: true }).click()
  await expect(dialog.getByLabel('Expires at (browser local time)', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
