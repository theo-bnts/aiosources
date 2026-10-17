import pg from 'pg'
import { test, expect } from './fixtures'

test('connection failures in a secret dialog use one notification without replaying the write or losing its draft', async ({
  page,
  server,
}) => {
  await page.goto('/security')
  await page.getByRole('button', { name: 'Create secret', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Reference name', { exact: true }).fill('offline-example')
  await dialog.getByLabel('Secret value', { exact: true }).fill('synthetic-test-value')
  let attempts = 0,
    offline = true
  await page.route('**/api/secrets/offline-example', async (route) => {
    if (route.request().method() === 'PUT') attempts++
    if (offline) await route.abort('failed')
    else await route.continue()
  })
  await dialog.getByRole('button', { name: 'Create secret', exact: true }).click()
  const notification = page.locator('[data-connection-notice]')
  await expect(notification).toContainText('The server is unreachable.')
  await expect(
    page.getByRole('alert').filter({ hasText: 'The server is unreachable.' }),
  ).toHaveCount(1)
  await expect(dialog.getByRole('alert')).toHaveCount(0)
  await expect(dialog.getByLabel('Reference name', { exact: true })).toHaveValue('offline-example')
  await expect(dialog.getByLabel('Secret value', { exact: true })).toHaveValue(
    'synthetic-test-value',
  )
  expect(attempts).toBe(1)
  await notification
    .getByRole('button', { name: 'Dismiss connection notification', exact: true })
    .click()
  await expect(notification).toHaveCount(0)
  const session = await (await page.request.get('/api/session')).json()
  const created = await page.request.put('/api/secrets/offline-example', {
    headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
    data: { value: 'different-synthetic-value', revision: 0 },
  })
  expect(created.status()).toBe(204)
  offline = false
  const rejected = page.waitForResponse(
    (r) => r.url().endsWith('/api/secrets/offline-example') && r.request().method() === 'PUT',
  )
  await dialog.getByRole('button', { name: 'Create secret', exact: true }).click()
  expect((await rejected).status()).toBe(409)
  await expect(dialog.getByRole('alert')).toBeVisible()
  await expect(notification).toHaveCount(0)
  expect(attempts).toBe(2)
})

test('Identification is a separate sidebar page and shared cache statistics match their own lifetime', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) VALUES
     ('torbox',repeat('a',40),true,now()-interval '1 minute','[]'),
     ('torbox',repeat('b',40),false,now()-interval '2 minutes','[]'),
     ('torbox',repeat('c',40),false,now()-interval '16 minutes','[]'),
     ('torbox',repeat('d',40),true,now()+interval '1 hour','[]');`)
  } finally {
    await db.end()
  }
  await page.goto('/debrid')
  const card = page
    .getByRole('region', { name: 'Availability checks' })
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'TorBox', exact: true }) })
  await expect(card).toContainText('2 shared observations')
  await expect(card.locator('dd')).toHaveText(['1 (50%)', '1 (50%)'])
  const response = await page.request.get('/api/debrid/statistics')
  expect(response.status()).toBe(200)
  const text = await response.text()
  expect(text).not.toContain('hash')
  expect(text).not.toContain('files')
  expect(text).not.toContain('account')
  const nav = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  await nav.getByRole('link', { name: 'Identification', exact: true }).click()
  await expect(page).toHaveURL(/\/metadata\/identification$/)
  await expect(page.getByRole('tab')).toHaveCount(0)
  await expect(
    page.getByRole('heading', { name: 'Identification', exact: true, level: 1 }),
  ).toBeVisible()
  await expect(nav.locator('a[aria-current="page"]')).toHaveText('Identification')
  await expect(
    page.getByRole('navigation', { name: 'Breadcrumb', exact: true }).getByRole('listitem'),
  ).toHaveText(['Metadata/', 'Identification'])
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
