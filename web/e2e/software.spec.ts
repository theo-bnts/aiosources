import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

test('Software uses independent settings blocks, catalog filters and responsive navigation', async ({
  page,
}) => {
  await page.goto('/metadata/software/settings')
  await expect(page.getByRole('heading', { name: 'Software', exact: true, level: 1 })).toBeVisible()
  await expect(
    page.getByRole('tablist', { name: 'Software views', exact: true }).getByRole('tab'),
  ).toHaveText(['Statistics', 'Settings', 'History'])
  await expect(
    page.getByLabel('Automatically update software information', { exact: true }),
  ).not.toBeChecked()

  await openDisclosure(page, 'Catalog filters')
  const catalog = page.getByRole('form', { name: 'Software catalog filters', exact: true })
  await expect(
    catalog.getByRole('switch', {
      name: 'Include commercial & vendor catalog (NVD CPE)',
      exact: true,
    }),
  ).toBeChecked()
  await expect(
    catalog.getByRole('switch', {
      name: 'Exclude web plugins, CMS extensions and developer libraries',
      exact: true,
    }),
  ).toBeChecked()
  await expect(
    catalog.getByRole('switch', { name: 'Include operating systems', exact: true }),
  ).not.toBeChecked()
  const iconsSwitch = catalog.getByRole('switch', {
    name: 'Include software icons index',
    exact: true,
  })
  await expect(iconsSwitch).toBeChecked()
  await iconsSwitch.uncheck()
  await catalog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(catalog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await openDisclosure(page, 'Automatic updates')
  const updates = page.getByRole('form', { name: 'Software automatic updates', exact: true })
  await updates
    .getByLabel('Software information update interval (minutes)', { exact: true })
    .fill('180')
  await updates.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(updates.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await page.reload()
  await openDisclosure(page, 'Catalog filters')
  await expect(
    page.getByRole('switch', { name: 'Include software icons index', exact: true }),
  ).not.toBeChecked()
  await openDisclosure(page, 'Automatic updates')
  await expect(
    page.getByLabel('Software information update interval (minutes)', { exact: true }),
  ).toHaveValue('180')

  await openDisclosure(page, 'Manual actions')
  await expect(
    page.getByRole('button', { name: 'Update software information', exact: true }),
  ).toBeEnabled()
  await openDisclosure(page, 'Maintenance')
  await expect(
    page.getByRole('button', { name: 'Reset software information', exact: true }),
  ).toBeEnabled()

  await page.setViewportSize({ width: 390, height: 844 })
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
  ).toBeLessThanOrEqual(0)
})

test('Optional NVD API key uses Secrets and never appears in configuration responses', async ({
  page,
}) => {
  await page.goto('/metadata/software/settings')
  await page.getByRole('button', { name: 'Add NVD API key', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('NVD API key (optional)', { exact: true }).fill('synthetic-nvd-api-key')
  await editor.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(editor).toHaveCount(0)
  const main = page.getByRole('form', { name: 'Software configuration', exact: true })
  await main.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(main.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  const response = await page.request.get('/api/aiosource/controls')
  const body = await response.text()
  expect(body).not.toContain('synthetic-nvd-api-key')
  expect(JSON.parse(body).config.software_nvd_secret).toMatch(/^software_nvd_api_key_/)

  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Software', exact: true, level: 1 })).toBeVisible()
  expect(
    (await (await page.request.get('/api/operations?kind=metadata&service=software')).json()).items,
  ).toEqual([])
})
