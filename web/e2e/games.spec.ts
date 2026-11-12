import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

test('Games uses independent settings blocks and the same navigation on desktop and mobile', async ({
  page,
}) => {
  await page.goto('/metadata/games/settings')
  await expect(page.getByRole('heading', { name: 'Games', exact: true, level: 1 })).toBeVisible()
  await expect(
    page.getByRole('tablist', { name: 'Games views', exact: true }).getByRole('tab'),
  ).toHaveText(['Statistics', 'Settings', 'History'])
  await expect(
    page.getByLabel('Automatically update game information', { exact: true }),
  ).not.toBeChecked()
  const main = page.getByRole('form', { name: 'Games configuration', exact: true })
  await page.getByLabel('Twitch client ID', { exact: true }).fill('synthetic-client')
  await openDisclosure(page, 'Automatic updates')
  const updates = page.getByRole('form', { name: 'Games automatic updates', exact: true })
  await updates.getByLabel('Game information update interval (minutes)', { exact: true }).fill('30')
  await main.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(main.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(
    (await (await page.request.get('/api/aiosource/controls')).json()).config.igdb_minutes,
  ).toBe(15)
  await updates.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(updates.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.reload()
  await expect(page.getByLabel('Twitch client ID', { exact: true })).toHaveValue('synthetic-client')
  await expect(
    page.getByLabel('Game information update interval (minutes)', { exact: true }),
  ).toHaveValue('30')
  await expect(page.locator('details[open]')).toHaveCount(0)
  await openDisclosure(page, 'Manual actions')
  await expect(
    page.getByRole('button', { name: 'Update game information', exact: true }),
  ).toBeDisabled()
  await openDisclosure(page, 'Maintenance')
  await expect(
    page.getByRole('button', { name: 'Reset game information', exact: true }),
  ).toBeEnabled()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  await navigation.getByRole('link', { name: 'Files', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Files', exact: true, level: 1 })).toBeVisible()
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  await navigation.getByRole('link', { name: 'Torrents', exact: true }).click()
  await expect(
    page.getByRole('navigation', { name: 'Breadcrumb', exact: true }).getByRole('listitem'),
  ).toHaveText(['Sources/', 'Indexers/', 'Torrents'])
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
  ).toBeLessThanOrEqual(0)
})

test('IGDB credentials use the shared secret editor and never appear in configuration responses', async ({
  page,
}) => {
  await page.goto('/metadata/games/settings')
  await page.getByRole('button', { name: 'Add Twitch client secret', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('Twitch client secret', { exact: true }).fill('synthetic-game-credential')
  await editor.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(editor).toHaveCount(0)
  await page.getByLabel('Twitch client ID', { exact: true }).fill('synthetic-client')
  await page
    .getByRole('form', { name: 'Games configuration', exact: true })
    .getByRole('button', { name: 'Save', exact: true })
    .click()
  await expect(
    page
      .getByRole('form', { name: 'Games configuration', exact: true })
      .getByRole('button', { name: 'Save', exact: true }),
  ).toBeDisabled()
  await expect(
    page.getByRole('button', { name: 'Replace Twitch client secret', exact: true }),
  ).toBeVisible()
  const response = await page.request.get('/api/aiosource/controls')
  const body = await response.text()
  expect(body).not.toContain('synthetic-game-credential')
  expect(JSON.parse(body).config.igdb_secret).toMatch(/^igdb_client_secret_/)
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Games', exact: true, level: 1 })).toBeVisible()
  expect(
    (await (await page.request.get('/api/operations?kind=metadata&service=igdb')).json()).items,
  ).toEqual([])
})
