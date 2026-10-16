import { test, expect } from './fixtures'

test('Torznab uses the shared access editor, keeps keys transient and generates multiple URL filters', async ({
  page,
}) => {
  await page.goto('/torznab')
  await expect(page.getByRole('heading', { name: 'Torznab', exact: true, level: 1 })).toBeVisible()
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Access name', { exact: true }).fill('External application')
  await dialog.getByLabel('Source access', { exact: true }).selectOption('all')
  await dialog
    .getByText('I authorize access to all current and future sources.', { exact: true })
    .click()
  await dialog.getByRole('button', { name: 'Create disabled access', exact: true }).click()
  await expect(
    dialog.getByRole('heading', { name: 'Save this API key now', exact: true }),
  ).toBeVisible()
  await expect(dialog.getByLabel('Torznab API key', { exact: true })).toHaveAttribute(
    'type',
    'password',
  )
  await expect(dialog.getByLabel('Torznab API key', { exact: true })).not.toHaveValue('')
  await dialog.getByRole('button', { name: 'I have saved the API key', exact: true }).click()
  const card = page.getByRole('region', { name: 'Share External application' })
  await expect(card.getByText('Disabled', { exact: true })).toBeVisible()
  await card.getByText('Client setup & filters', { exact: true }).click()
  await card.getByLabel('Media type', { exact: true }).selectOption('movie')
  await expect(card.getByLabel('Additional parameters (optional)', { exact: true })).toHaveValue(
    '&media=movie',
  )
  await card.getByLabel('Media type', { exact: true }).selectOption('tv')
  await expect(card.getByLabel('Additional parameters (optional)', { exact: true })).toHaveValue(
    '&media=tv',
  )
  const response = await page.request.get('/api/torznab/endpoints')
  const data = await response.json()
  expect(data.items).toHaveLength(1)
  expect(data.items[0].media_types).toEqual([])
  expect(data.items[0]).not.toHaveProperty('password')
  expect((await (await page.request.get('/api/shares')).json()).items).toEqual([])
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('Stremio connection filters change only the generated link and can be cleared', async ({
  page,
}) => {
  await page.goto('/addon/settings')
  const before = (await page.request.get('/api/aiosource')).json()
  await page.getByText('Connection filters', { exact: true }).click()
  const manifest = page.getByLabel('Manifest URL', { exact: true })
  const original = await manifest.inputValue()
  await page.getByLabel('Media type', { exact: true }).selectOption('movie')
  const movies = await manifest.inputValue()
  expect(movies).not.toEqual(original)
  expect(movies).toContain('~v1~')
  await page.getByLabel('Cached results only', { exact: true }).click()
  const cached = await manifest.inputValue()
  expect(cached).not.toEqual(movies)
  const data = JSON.parse(
    Buffer.from(new URL(cached).pathname.split('~')[2].split('/')[0], 'base64url').toString(),
  )
  expect(data.types).toEqual(['movie'])
  expect(data.cached_only).toBe(true)
  await page.getByLabel('Media type', { exact: true }).selectOption('')
  await page.getByLabel('Cached results only', { exact: true }).click()
  await expect(manifest).toHaveValue(original)
  expect((await (await page.request.get('/api/aiosource')).json()).config).toEqual(
    (await before).config,
  )
})
