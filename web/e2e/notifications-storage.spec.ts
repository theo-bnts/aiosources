import pg from 'pg'
import { test, expect } from './fixtures'

test('historical source errors, events and activity logs use sentence case without rewriting data', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,error,finished_at)
      VALUES('historical-error','synthetic','Synthetic source','full','failed','','{}','source response could not be processed',now());
      INSERT INTO ingest.events(run_id,kind,message,data)
      VALUES('historical-error','source_error','source pagination did not advance','{"failure_code":"stalled"}');
      INSERT INTO ingest.activity_logs(event_id,level,kind,message)
      VALUES('historical-error','error','system.fixture','source response could not be processed')`)
    await page.goto('/runs/historical-error')
    await expect(page.getByRole('alert')).toContainText('Source response could not be processed')
    await page.getByRole('tab', { name: 'Events', exact: true }).click()
    await expect(page.getByRole('table')).toContainText('Source pagination did not advance')
    await page.goto('/logs')
    await expect(page.getByRole('table')).toContainText('Source response could not be processed')
    const stored = await db.query("SELECT error FROM ingest.runs WHERE id='historical-error'")
    expect(stored.rows[0].error).toBe('source response could not be processed')
  } finally {
    try {
      await db.query(
        "DELETE FROM ingest.events WHERE run_id='historical-error'; DELETE FROM ingest.runs WHERE id='historical-error'",
      )
    } finally {
      await db.end()
    }
  }
})

test('action confirmations use the shared opaque notification, field errors stay in the dialog, and sign out clears notifications', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/security/settings')
  await page.getByRole('button', { name: 'Create secret', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Reference name', { exact: true }).fill('synthetic-reference')
  await dialog.getByLabel('Secret value', { exact: true }).fill('synthetic-secret-never-notified')
  await dialog.getByRole('button', { name: 'Create secret', exact: true }).click()
  const notification = page.locator('[data-notification]')
  await expect(notification).toContainText('Secret “synthetic-reference” was created.')
  await expect(page.getByRole('main')).not.toContainText('was created.')
  await expect(notification).not.toContainText('synthetic-secret-never-notified')
  const background = await notification.evaluate((node) => getComputedStyle(node).backgroundColor)
  expect(background).not.toMatch(/transparent|rgba\([^)]*,\s*0\)/)
  const box = await notification.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x + box!.width).toBeLessThanOrEqual(390)
  expect(box!.y + box!.height).toBeLessThanOrEqual(844)
  await page.getByRole('button', { name: 'Create secret', exact: true }).click()
  await dialog.getByLabel('Reference name', { exact: true }).fill('synthetic-reference')
  await dialog.getByLabel('Secret value', { exact: true }).fill('different-synthetic-secret')
  await dialog.getByRole('button', { name: 'Create secret', exact: true }).click()
  await expect(dialog.getByRole('alert')).toBeVisible()
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Reference name', { exact: true }).fill('')
  await dialog.getByLabel('Secret value', { exact: true }).fill('')
  await dialog.getByRole('button', { name: 'Back', exact: true }).click()
  await expect(dialog).toBeHidden()
  // A confirmation from the authenticated console must not survive into login.
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  await expect(notification).toBeVisible()
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible()
  await expect(notification).toHaveCount(0)
  // Restore the authenticated fixture for its independent no-collection check.
  // Never include the temporary password in failure output.
  try {
    await page.getByLabel('Administrator password', { exact: true }).fill(server.password)
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Create secret', exact: true })).toBeVisible()
  } catch {
    throw new Error('Could not restore the isolated administrator session.')
  }
  await expect(notification).toHaveCount(0)
})

test('manual action failures use notifications without replaying a rejected operation', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(
      "INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','1001','tt1000001',ARRAY[2021])",
    )
  } finally {
    await db.end()
  }
  let writes = 0
  await page.route('**/api/aiosource/match', (route) => {
    writes++
    return route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'Synthetic task is already running.' }),
    })
  })
  await page.goto('/metadata/settings')
  await page.getByText('Manual actions', { exact: true }).click()
  await page.getByRole('button', { name: 'Identify torrents', exact: true }).click()
  const notice = page.locator('[data-request-notice]')
  await expect(notice).toContainText('Synthetic task is already running.')
  await expect(page.getByRole('main')).not.toContainText('Synthetic task is already running.')
  expect(writes).toBe(1)
  await notice.getByRole('button', { name: 'Dismiss notification', exact: true }).click()
  await expect(notice).toHaveCount(0)
  expect(writes).toBe(1)
})

test('a service failure appears only in the notification area and disappears when its view is left', async ({
  page,
}) => {
  await page.route('**/api/debrid/statistics', (route) =>
    route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'Synthetic service unavailable.' }),
    }),
  )
  await page.goto('/debrid')
  const notice = page.locator('[data-request-notice]')
  await expect(notice).toContainText('Synthetic service unavailable.')
  await expect(page.getByRole('main')).not.toContainText('Synthetic service unavailable.')
  await expect(
    page.getByRole('alert').filter({ hasText: 'Synthetic service unavailable.' }),
  ).toHaveCount(1)
  await page.goto('/logs')
  await expect(notice).toHaveCount(0)
})

for (const width of [1440, 390]) {
  test(`storage keeps a compact summary and reveals allocation and recoverable imports at ${width}px`, async ({
    page,
    server,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    const now = new Date().toISOString()
    const part = (key: string, bytes: number, table_bytes: number, index_bytes: number) => ({
      key,
      bytes,
      table_bytes,
      index_bytes,
    })
    await page.route('**/api/system-health', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          checked_at: now,
          next_check_at: now,
          status: 'healthy',
          database: {
            available: true,
            measured_at: now,
            total_bytes: 2000000000,
            relation_bytes: 1900000000,
            other_database_bytes: 100000000,
            categories: [
              {
                ...part('sources', 1500000000, 1000000000, 500000000),
                details: [
                  part('imports', 900000000, 600000000, 300000000),
                  part('published', 500000000, 350000000, 150000000),
                  part('source-history', 100000000, 50000000, 50000000),
                ],
              },
              {
                ...part('metadata', 400000000, 300000000, 100000000),
                details: [part('information', 400000000, 300000000, 100000000)],
              },
            ],
          },
          disk: {
            available: true,
            scope: 'synthetic',
            measured_at: now,
            total_bytes: 10000000000,
            available_bytes: 8000000000,
            free_bytes: 8000000000,
          },
          source_storage: [{ provider_id: 'local-synthetic', bytes: 1500000000 }],
          import_storage_available: true,
          retained_import_count: 1,
          cancelled_import_count: 1,
          retained_imports: [
            {
              run_id: 'synthetic-cancelled',
              provider_id: 'local-synthetic',
              provider_name: 'Readable source name',
              status: 'cancelled',
              created_at: now,
              bytes: 900000000,
            },
          ],
        }),
      }),
    )
    const session = await (await page.request.get('/api/session')).json()
    const saved = await page.request.put('/api/providers/local-synthetic', {
      headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
      data: {
        revision: '',
        json: JSON.stringify({
          version: 1,
          id: 'local-synthetic',
          name: 'Readable source name',
          enabled: false,
          adapter: 'http_json',
          url: server.sourceURL,
        }),
      },
    })
    expect(saved.ok()).toBe(true)
    await page.goto('/health')
    const storage = page.getByRole('region', { name: 'Where storage is used', exact: true })
    const sources = storage.getByRole('button', { name: 'Sources', exact: true })
    await expect(sources).toHaveAttribute('aria-expanded', 'false')
    await expect(
      storage.getByText('Prepared imports and resume state', { exact: true }),
    ).toBeHidden()
    await sources.click()
    await expect(sources).toHaveAttribute('aria-expanded', 'true')
    await expect(
      storage.getByText('Prepared imports and resume state', { exact: true }),
    ).toBeVisible()
    await storage.getByText('By source (estimated)', { exact: true }).click()
    await expect(
      storage.getByRole('cell', { name: 'Readable source name', exact: true }),
    ).toBeVisible()
    await storage.locator('summary').filter({ hasText: 'Imports with prepared data' }).click()
    await expect(
      storage.getByText('This data is preserved while needed for recovery.', { exact: false }),
    ).toBeVisible()
    await expect(
      storage.getByRole('link', { name: /Open import from Readable source name/ }),
    ).toHaveAttribute('href', '/runs/synthetic-cancelled')
    await expect(storage.getByText('Cancelled', { exact: true })).toBeVisible()
    expect(
      await storage
        .locator('[data-slot="table-container"]')
        .first()
        .evaluate((table) => table.scrollWidth <= table.clientWidth + 1),
    ).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}
