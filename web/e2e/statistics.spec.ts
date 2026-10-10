import { test, expect } from './fixtures'
import pg from 'pg'

test('statistics are the first view, settings contain configuration and statuses share semantic colors', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  expect(
    (
      await page.request.put('/api/providers/statistics-fixture', {
        headers,
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'statistics-fixture',
            name: 'Statistics fixture',
            adapter: 'http_json',
            url: server.sourceURL,
            enabled: false,
          }),
        },
      })
    ).status(),
  ).toBe(200)
  const config = (await (await page.request.get('/api/aiosource')).json()).config
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: { ...config, auto_match: false },
      })
    ).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(
      `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES ('statistics-fixture','one','{"title":"Statistics release"}')`,
    )
  } finally {
    await db.end()
  }

  for (const [path, heading, statistic] of [
    ['/providers', 'Sources', 'Imported torrents'],
    ['/metadata', 'Metadata', 'Film & series entries'],
    ['/debrid', 'Debrid', 'Configured services'],
  ]) {
    await page.goto(path)
    const views = page.getByRole('tablist', { name: `${heading} views`, exact: true })
    await expect(views.getByRole('tab')).toHaveText(
      heading === 'Metadata'
        ? ['Statistics', 'Settings', 'Identification', 'History']
        : ['Statistics', 'Settings', 'History'],
    )
    await expect(views.getByRole('tab', { name: 'Statistics', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(views.locator('svg[aria-hidden="true"]')).toHaveCount(
      heading === 'Metadata' ? 4 : 3,
    )
    await expect(page.getByText(statistic, { exact: true })).toBeVisible()
    await views.getByRole('tab', { name: 'Settings', exact: true }).click()
    await expect(page).toHaveURL(server.origin + path + '/settings')
    await expect(page.getByText(statistic, { exact: true })).toHaveCount(0)
    await expect(page.locator('[role="tabpanel"] header .page-description')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toHaveCount(0)
    await views.getByRole('tab', { name: 'History', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'History', exact: true })).toHaveCount(0)
    await expect(page.locator('[role="tabpanel"] header p')).toHaveCount(0)
  }
  await page.goto('/providers/settings')
  const source = page.getByRole('row').filter({ hasText: 'Statistics fixture' })
  await expect(
    source.getByRole('switch', { name: 'Enable Statistics fixture', exact: true }),
  ).toBeVisible()
  await expect(source.getByText('Enabled', { exact: true })).toHaveCount(0)
  await expect(source.getByText('Disabled', { exact: true })).toHaveCount(0)
  await page.goto('/health')
  await expect(page.getByText('Available', { exact: true }).first()).toHaveAttribute(
    'data-variant',
    'success',
  )
  await page.goto('/security/verification')
  await expect(page.getByText('Not enabled', { exact: true })).toHaveAttribute(
    'data-variant',
    'warning',
  )
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/providers')
  await expect(
    page.getByRole('link', {
      name: 'View 1 imported torrents from Statistics fixture',
      exact: true,
    }),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  if (process.env.QA_DIRECTORY)
    await page.screenshot({ path: `${process.env.QA_DIRECTORY}/statistics-mobile.png` })
})

test('the Statistics tab preserves unsaved settings until an explicit discard', async ({
  page,
}) => {
  await page.goto('/metadata/settings')
  const interval = page.getByLabel('Information update interval (minutes)', { exact: true })
  const original = await interval.inputValue()
  const changed = original === '30' ? '45' : '30'
  await interval.fill(changed)
  await page.getByRole('tab', { name: 'Statistics', exact: true }).click()
  const prompt = page.getByRole('alertdialog')
  await prompt.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(interval).toHaveValue(changed)
  await page.getByRole('tab', { name: 'Statistics', exact: true }).click()
  await prompt.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(page).toHaveURL(/\/metadata$/)
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await expect(interval).toHaveValue(original)
})
