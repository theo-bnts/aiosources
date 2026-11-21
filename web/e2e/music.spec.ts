import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

test('Music uses independent settings blocks, catalog filters and responsive navigation', async ({
  page,
}) => {
  await page.goto('/metadata/music/settings')
  await expect(page.getByRole('heading', { name: 'Music', exact: true, level: 1 })).toBeVisible()
  await expect(
    page.getByRole('tablist', { name: 'Music views', exact: true }).getByRole('tab'),
  ).toHaveText(['Statistics', 'Settings', 'History'])
  await expect(
    page.getByLabel('Automatically update music information', { exact: true }),
  ).not.toBeChecked()

  await openDisclosure(page, 'Catalog filters')
  const catalog = page.getByRole('form', { name: 'Music catalog filters', exact: true })
  await expect(
    catalog.getByRole('switch', { name: 'Official releases only', exact: true }),
  ).toBeChecked()
  await expect(
    catalog.getByRole('switch', { name: 'Exclude audiobooks and spoken word', exact: true }),
  ).toBeChecked()
  const coversSwitch = catalog.getByRole('switch', {
    name: 'Import Cover Art Archive index',
    exact: true,
  })
  await expect(coversSwitch).toBeChecked()
  await coversSwitch.uncheck()
  await catalog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(catalog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await openDisclosure(page, 'Automatic updates')
  const updates = page.getByRole('form', { name: 'Music automatic updates', exact: true })
  await updates
    .getByLabel('Music information update interval (minutes)', { exact: true })
    .fill('120')
  await updates.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(updates.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await page.reload()
  await openDisclosure(page, 'Catalog filters')
  await expect(
    page.getByRole('switch', { name: 'Import Cover Art Archive index', exact: true }),
  ).not.toBeChecked()
  await openDisclosure(page, 'Automatic updates')
  await expect(
    page.getByLabel('Music information update interval (minutes)', { exact: true }),
  ).toHaveValue('120')

  await openDisclosure(page, 'Manual actions')
  await expect(
    page.getByRole('button', { name: 'Update music information', exact: true }),
  ).toBeEnabled()
  await openDisclosure(page, 'Maintenance')
  await expect(
    page.getByRole('button', { name: 'Reset music information', exact: true }),
  ).toBeEnabled()

  await page.setViewportSize({ width: 390, height: 844 })
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
  ).toBeLessThanOrEqual(0)
})

test('Optional MetaBrainz Live Data Feed token uses Secrets and never appears in configuration responses', async ({
  page,
}) => {
  await page.goto('/metadata/music/settings')
  await page.getByRole('button', { name: 'Add MetaBrainz token', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor
    .getByLabel('MetaBrainz Live Data Feed token (optional)', { exact: true })
    .fill('synthetic-metabrainz-token')
  await editor.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(editor).toHaveCount(0)
  const main = page.getByRole('form', { name: 'Music configuration', exact: true })
  await main.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(main.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  const response = await page.request.get('/api/aiosource/controls')
  const body = await response.text()
  expect(body).not.toContain('synthetic-metabrainz-token')
  expect(JSON.parse(body).config.musicbrainz_secret).toMatch(/^musicbrainz_replication_token_/)

  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Music', exact: true, level: 1 })).toBeVisible()
  expect(
    (await (await page.request.get('/api/operations?kind=metadata&service=musicbrainz')).json())
      .items,
  ).toEqual([])
})
