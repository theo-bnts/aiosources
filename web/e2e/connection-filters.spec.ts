import { test, expect } from './fixtures'
import type { SharePassword } from '../src/lib/catalog-types'

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
  const card = page.getByRole('region', { name: 'Access External application' })
  await expect(card.getByText('Disabled', { exact: true })).toBeVisible()
  await card.getByText('Client setup & filters', { exact: true }).click()
  await card.locator('summary', { hasText: /^Filters$/ }).click()
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

test('saved Stremio access updates the same manifest and can be independently replaced or revoked', async ({
  page,
}, testInfo) => {
  await page.goto('/addon/settings')
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Access name', { exact: true }).fill('Living room')
  await dialog.getByLabel('Source access', { exact: true }).selectOption('all')
  await dialog
    .getByText('I authorize access to all current and future sources.', { exact: true })
    .click()
  await expect(dialog.getByLabel('Media type', { exact: true })).toBeHidden()
  await dialog.locator('summary', { hasText: 'Filters' }).click()
  await dialog.getByLabel('Media type', { exact: true }).selectOption('movie')
  await dialog.getByLabel('Preferred language', { exact: true }).selectOption('fr')
  await dialog.getByLabel('Cached results only', { exact: true }).check()
  await dialog.getByRole('button', { name: 'Create disabled access', exact: true }).click()
  await expect(
    dialog.getByRole('heading', { name: 'Save this manifest URL now', exact: true }),
  ).toBeVisible()
  const manifestField = dialog.getByLabel('Manifest URL', { exact: true })
  await expect(manifestField).toHaveAttribute('type', 'password')
  let manifest = await manifestField.inputValue()
  expect((await page.request.get(manifest)).status()).toBe(401)
  await dialog.getByRole('button', { name: 'Copy manifest URL', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(manifest)
  await dialog.getByRole('button', { name: 'I have saved the manifest URL', exact: true }).click()
  const card = page.getByRole('region', { name: 'Access Living room', exact: true })
  await expect(card.getByText('Disabled', { exact: true })).toBeVisible()
  await card.getByRole('button', { name: 'Enable', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Enable access', exact: true })
    .click()
  await expect(card.getByText('Enabled', { exact: true })).toBeVisible()
  const first = await page.request.get(manifest)
  expect(first.status()).toBe(200)
  const firstManifest = await first.json()
  expect(firstManifest.types).toEqual(['movie', 'series', 'anime'])
  await card.getByRole('button', { name: 'Edit permissions & limits', exact: true }).click()
  await dialog.locator('summary', { hasText: 'Filters' }).click()
  await dialog.getByLabel('Media type', { exact: true }).selectOption('series')
  await dialog.getByRole('button', { name: 'Save permissions', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  const changed = await (await page.request.get(manifest)).json()
  expect(changed.types).toEqual(firstManifest.types)
  expect(changed.id).toBe(firstManifest.id)
  await page.reload()
  const saved = await (await page.request.get('/api/stremio/connections')).json()
  expect(saved.items).toHaveLength(1)
  expect(saved.items[0].stream_preferences).toMatchObject({
    types: ['series'],
    language: 'fr',
    cached_only: true,
  })
  expect(saved.items[0]).not.toHaveProperty('password')
  const session = await (await page.request.get('/api/session')).json()
  const other = await page.request.post('/api/stremio/connections', {
    headers: { 'X-CSRF-Token': session.csrf_token },
    data: {
      name: 'Another player',
      scope: 'all',
      enabled: true,
      stream_preferences: { mode: 'all', language: '' },
    },
  })
  expect(other.status()).toBe(201)
  const additional: SharePassword = await other.json()
  const otherManifest = new URL(
    `${additional.share.url}.${additional.password}/manifest.json`,
    manifest,
  ).toString()
  expect((await (await page.request.get(otherManifest)).json()).id).not.toBe(firstManifest.id)
  await card.getByRole('button', { name: 'Replace installation key', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace installation key', exact: true })
    .click()
  await expect(manifestField).toBeVisible()
  expect((await page.request.get(manifest)).status()).toBe(401)
  manifest = await manifestField.inputValue()
  expect((await page.request.get(manifest)).status()).toBe(200)
  await dialog.getByRole('button', { name: 'I have saved the manifest URL', exact: true }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('stremio-access-mobile.png'), fullPage: true })
  await card.getByRole('button', { name: 'Revoke', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Revoke access', exact: true })
    .click()
  await expect(card).toHaveCount(0)
  expect((await page.request.get(manifest)).status()).toBe(401)
  expect((await page.request.get(otherManifest)).status()).toBe(200)
  const storage = await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))
  expect(storage.includes('s1.')).toBe(false)
})

test('legacy Stremio filters change only the generated link and can be cleared', async ({
  page,
}) => {
  await page.goto('/addon/settings')
  await page.locator('summary', { hasText: 'Legacy installation' }).click()
  const before = (await page.request.get('/api/aiosource')).json()
  await page.getByText('Filters', { exact: true }).click()
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

test('saved service selections remain editable without widening access after a key is removed', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  expect(
    (
      await page.request.put('/api/secrets/example-tb', {
        headers,
        data: { value: 'synthetic-unused-key', revision: 0 },
      })
    ).status(),
  ).toBe(204)
  const { config } = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: { ...config, torbox_secret: 'example-tb' },
      })
    ).status(),
  ).toBe(204)
  expect(
    (
      await page.request.post('/api/stremio/connections', {
        headers,
        data: {
          name: 'Changed service',
          scope: 'all',
          stream_preferences: { language: '', mode: 'all', services: ['alldebrid'] },
        },
      })
    ).status(),
  ).toBe(201)
  await page.goto('/addon/settings')
  await page
    .getByRole('region', { name: 'Access Changed service', exact: true })
    .getByRole('button', { name: 'Edit permissions & limits', exact: true })
    .click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('summary', { hasText: /^Filters$/ }).click()
  await expect(dialog.getByLabel('AllDebrid (not configured)', { exact: true })).toBeChecked()
  await dialog.getByLabel('TorBox', { exact: true }).check()
  await expect(dialog.getByLabel('Direct torrents', { exact: true })).not.toBeChecked()
  await dialog.getByLabel('AllDebrid (not configured)', { exact: true }).uncheck()
  await dialog.getByRole('button', { name: 'Save permissions', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  const data = await (await page.request.get('/api/stremio/connections')).json()
  expect(data.items[0].stream_preferences.services).toEqual(['torbox'])
})
