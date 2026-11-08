import { openDisclosure } from './disclosures'
import pg from 'pg'
import { test, expect } from './fixtures'

test('Storage saving is the default, edits are explicit and data reset requires confirmation', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/health/settings')
  await openDisclosure(page, 'Torrent data')
  const retention = page.getByRole('form', { name: 'Torrent data', exact: true })
  const mode = retention.getByRole('switch', { name: 'All mapped source fields', exact: true })
  await expect(mode).not.toBeChecked()
  await page.getByRole('button', { name: 'About Torrent data', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'About Torrent data', exact: true })).toContainText(
    'Fields and processed-record choices apply to imports started after saving.',
  )
  await page.keyboard.press('Escape')
  const save = retention.getByRole('button', { name: 'Save', exact: true })
  const maintenance = page.locator('details#maintenance')
  const reset = maintenance.getByRole('button', { name: 'Reset all source data', exact: true })
  const resetStatus = maintenance.locator('#source-data-reset-status')
  await expect(reset).toBeHidden()
  await openDisclosure(page, 'Maintenance')
  await expect(save).toBeDisabled()
  await expect(reset).toBeEnabled()
  await mode.check()
  await expect(retention.getByLabel('Additional source fields', { exact: true })).toHaveText(
    'All mapped fields retained',
  )
  await expect(reset).toBeDisabled()
  await expect(resetStatus).toHaveText(
    'Save the selected storage settings before resetting source data.',
  )
  await expect(reset).toHaveAttribute('aria-describedby', 'source-data-reset-status')
  await page.getByRole('tab', { name: 'Statistics', exact: true }).click()
  const unsaved = page.getByRole('alertdialog')
  await expect(unsaved).toContainText('storage settings')
  await unsaved.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(mode).toBeChecked()
  await expect(reset).toBeDisabled()
  await save.click()
  await expect(retention.getByText('Changes saved.', { exact: true })).toBeVisible()
  await expect(reset).toBeEnabled()
  await expect(resetStatus).toHaveText('')
  await page.reload()
  await openDisclosure(page, 'Torrent data')
  await expect(mode).toBeChecked()
  await expect(reset).toBeHidden()
  await openDisclosure(page, 'Maintenance')
  await reset.click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog).toContainText(
    'Source definitions, secrets, schedules, sharing permissions, film, series and book information, and debrid caches are retained.',
  )
  await expect(dialog).toContainText('This cannot be undone.')
  await dialog.getByRole('button', { name: 'Back', exact: true }).click()
  await expect(dialog).toBeHidden()
  await mode.uncheck()
  await save.click()
  await expect(retention.getByText('Changes saved.', { exact: true })).toBeVisible()
  await reset.click()
  await dialog.getByRole('button', { name: 'Reset source data', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(mode).not.toBeChecked()
  await expect(reset).toBeEnabled()
  await page.getByRole('tab', { name: 'Statistics', exact: true }).click()
  await expect(page).toHaveURL(/\/health$/)
  await expect(
    page.getByRole('heading', { name: 'PostgreSQL database', exact: true }),
  ).toBeVisible()
  await expect(mode).toHaveCount(0)
  await expect(reset).toHaveCount(0)
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await openDisclosure(page, 'Torrent data')
  await expect(mode).not.toBeChecked()
  await expect(reset).toBeHidden()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('Storage settings report stale edits without silently overwriting another administrator', async ({
  page,
  server,
}) => {
  await page.goto('/health/settings#maintenance')
  await openDisclosure(page, 'Torrent data')
  const mode = page.getByRole('switch', { name: 'All mapped source fields', exact: true })
  await expect(mode).not.toBeChecked()
  await mode.check()
  const current = await (await page.request.get('/api/settings/storage')).json()
  const session = await (await page.request.get('/api/session')).json()
  const updated = await page.request.put('/api/settings/storage', {
    headers: { Origin: server.origin, 'X-CSRF-Token': session.csrf_token },
    data: current.settings,
  })
  expect(updated.ok()).toBe(true)
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(
    page.getByText('Storage settings changed elsewhere.', { exact: false }),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(
    page.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toBeDisabled()
  await expect(page.locator('#source-data-reset-status')).toHaveText(
    'Reload the saved storage settings before resetting source data.',
  )
  await mode.uncheck()
  await expect(
    page.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toBeDisabled()
  await expect(page.locator('#source-data-reset-status')).toHaveText(
    'Reload the saved storage settings before resetting source data.',
  )
  await page.getByRole('button', { name: 'Reload saved value', exact: true }).click()
  await expect(mode).not.toBeChecked()
  await expect(page.getByText('Storage settings changed elsewhere.', { exact: false })).toBeHidden()
  await expect(
    page.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toBeEnabled()
})

test.describe('resetting source data with imports in progress', () => {
  test.use({ collectionSource: true, viewport: { width: 390, height: 844 }, hasTouch: true })

  test('saving the mode keeps active imports protected and pausing unlocks reset automatically', async ({
    page,
    server,
  }) => {
    test.setTimeout(75_000)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(`INSERT INTO ingest.runs
        (id,provider_id,provider_name,mode,status,trigger,revision,config,started_at)
        VALUES('storage-active','example','Example source','full','running','manual','',
          '{"version":1,"id":"example","adapter":"http_json","enabled":false}',now())`)
      await page.goto('/health/settings#maintenance')
      await openDisclosure(page, 'Torrent data')
      const reset = page.getByRole('button', { name: 'Reset all source data', exact: true })
      const status = page.locator('#source-data-reset-status')
      await expect(reset).toBeDisabled()
      await expect(status).toHaveText('Pause running imports before resetting source data.')
      await page.getByRole('switch', { name: 'All mapped source fields', exact: true }).check()
      await expect(status).toHaveText(
        'Save the selected storage settings before resetting source data.',
      )
      await page.getByRole('button', { name: 'Save', exact: true }).click()
      await expect(page.getByText('Changes saved.', { exact: true })).toBeVisible()
      await expect(reset).toBeDisabled()
      await expect(status).toHaveText('Pause running imports before resetting source data.')

      const session = await (await page.request.get('/api/session')).json()
      const settings = await (await page.request.get('/api/settings/storage')).json()
      const blocked = await page.request.post('/api/settings/storage/reset-sources', {
        headers: { Origin: server.origin, 'X-CSRF-Token': session.csrf_token },
        data: { revision: settings.settings.revision, confirm: true },
      })
      expect(blocked.status()).toBe(409)
      expect(
        (await db.query("SELECT status FROM ingest.runs WHERE id='storage-active'")).rows,
      ).toEqual([{ status: 'running' }])

      await db.query(
        "UPDATE ingest.runs SET status='paused',pause_reason='manual',finished_at=now() WHERE id='storage-active'",
      )
      await expect(reset).toBeEnabled({ timeout: 40_000 })
      await expect(status).toHaveText('')
      await reset.click()
      const dialog = page.getByRole('alertdialog')
      await expect(dialog).toBeVisible()
      await dialog.getByRole('button', { name: 'Reset source data', exact: true }).click()
      await expect(dialog).toBeHidden()
      expect((await db.query('SELECT count(*)::int AS count FROM ingest.runs')).rows).toEqual([
        { count: 0 },
      ])
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true)
    } finally {
      await db.end()
    }
  })
})

test('granular data, indexes and retention save independently on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 1000 })
  await page.goto('/health/settings')
  await expect(page.locator('main details[open]')).toHaveCount(0)
  await openDisclosure(page, 'Torrent data')
  await openDisclosure(page, 'Search indexes')
  await openDisclosure(page, 'Retention overrides')
  const data = page.getByRole('form', { name: 'Torrent data', exact: true })
  const indexes = page.getByRole('form', { name: 'Search indexes', exact: true })
  const retention = page.getByRole('form', { name: 'Retention overrides', exact: true })
  await data.getByRole('switch', { name: 'Successful processed records', exact: true }).check()
  await retention.getByLabel('Stremio request history (days)', { exact: true }).fill('30')
  await retention.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(retention.getByText('Changes saved.', { exact: true })).toBeVisible()
  await expect(data.getByRole('button', { name: 'Save', exact: true })).toBeEnabled()
  await expect(indexes.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  let stored = await (await page.request.get('/api/settings/storage')).json()
  expect(stored.settings.options.successful_records).toBe(false)
  await data.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(data.getByText('Changes saved.', { exact: true })).toBeVisible()
  stored = await (await page.request.get('/api/settings/storage')).json()
  expect(stored.settings.options.successful_records).toBe(true)
  const saved = await (await page.request.get('/api/settings/storage/retention')).json()
  expect(saved.days.requests).toBe(30)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
