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

for (const format of ['stremio', 'torznab'] as const) {
  test(`${format} defaults to all sources without limits and keeps optional policies in Advanced`, async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const headers = { 'X-CSRF-Token': session.csrf_token }
    for (const [id, name] of [
      ['alpha', 'Alpha library'],
      ['beta', 'Beta library'],
    ]) {
      expect(
        (
          await page.request.put(`/api/providers/${id}`, {
            headers,
            data: {
              revision: '',
              json: JSON.stringify({
                version: 1,
                id,
                name,
                adapter: 'http_json',
                url: server.sourceURL,
                enabled: false,
              }),
            },
          })
        ).status(),
      ).toBe(200)
    }
    const endpoint = format === 'stremio' ? '/api/stremio/connections' : '/api/torznab/endpoints'
    await page.goto(format === 'stremio' ? '/addon/settings' : '/torznab')
    await page.getByRole('button', { name: 'Create access', exact: true }).click()
    const editor = page.getByRole('dialog')
    await editor.getByLabel('Access name', { exact: true }).fill('Default access')
    await expect(editor.getByLabel('Source access', { exact: true })).toHaveCount(0)
    await expect(
      editor.getByText('I authorize access to all current and future sources.', { exact: true }),
    ).toHaveCount(0)
    await expect(editor.getByLabel('Sources', { exact: true })).toBeHidden()
    await expect(editor.getByLabel('Requests per minute', { exact: true })).toBeHidden()
    await expect(editor.getByLabel('Concurrent requests', { exact: true })).toBeHidden()
    expect(await editor.locator('summary').allTextContents()).toEqual(['Filters', 'Advanced'])
    await editor.locator('summary', { hasText: /^Filters$/ }).click()
    await expect(editor.getByRole('button', { name: 'Sources', exact: true })).toHaveText(
      'All sources',
    )
    await editor.locator('summary', { hasText: /^Advanced$/ }).click()
    await expect(editor.getByLabel('Requests per minute', { exact: true })).toHaveValue('')
    await expect(editor.getByLabel('Concurrent requests', { exact: true })).toHaveValue('')
    await expect(editor.getByLabel('Expires at (browser local time)', { exact: true })).toHaveValue(
      '',
    )
    await editor.getByRole('button', { name: 'Create disabled access', exact: true }).click()
    await page
      .getByRole('button', {
        name: format === 'stremio' ? 'I have saved the manifest URL' : 'I have saved the API key',
        exact: true,
      })
      .click()
    let access = (await (await page.request.get(endpoint)).json()).items[0]
    expect(access).toMatchObject({
      scope: 'all',
      source_ids: [],
      requests_per_minute: 0,
      max_concurrent_downloads: 0,
      expires_at: null,
    })
    const card = page.getByRole('region', { name: 'Access Default access', exact: true })
    await expect(card.getByText('Request limits', { exact: true })).toHaveCount(0)
    await card.getByRole('button', { name: 'Edit permissions & limits', exact: true }).click()
    await editor.locator('summary', { hasText: /^Filters$/ }).click()
    await editor.getByRole('button', { name: 'Sources', exact: true }).click()
    await page.getByRole('menuitemcheckbox', { name: 'Alpha library', exact: true }).click()
    await page.keyboard.press('Escape')
    await editor.locator('summary', { hasText: /^Advanced$/ }).click()
    await editor.getByLabel('Requests per minute', { exact: true }).fill('9')
    await editor.getByLabel('Concurrent requests', { exact: true }).fill('1')
    await editor
      .getByLabel('Expires at (browser local time)', { exact: true })
      .fill('2032-01-01T12:00')
    await editor.getByRole('button', { name: 'Save permissions', exact: true }).click()
    await expect(editor).toHaveCount(0)
    access = (await (await page.request.get(endpoint)).json()).items[0]
    expect(access).toMatchObject({
      scope: 'selected',
      source_ids: ['alpha'],
      requests_per_minute: 9,
      max_concurrent_downloads: 1,
    })
    expect(access.expires_at).not.toBeNull()
    await card.getByRole('button', { name: 'Edit permissions & limits', exact: true }).click()
    await editor.locator('summary', { hasText: /^Filters$/ }).click()
    await expect(editor.getByRole('button', { name: 'Sources', exact: true })).toHaveText(
      'Alpha library',
    )
    await editor.getByRole('button', { name: 'Sources', exact: true }).click()
    await page.getByRole('menuitem', { name: 'All sources', exact: true }).click()
    await editor.locator('summary', { hasText: /^Advanced$/ }).click()
    await editor.getByLabel('Requests per minute', { exact: true }).fill('')
    await editor.getByLabel('Concurrent requests', { exact: true }).fill('')
    await editor.getByLabel('Expires at (browser local time)', { exact: true }).fill('')
    await editor.getByRole('button', { name: 'Save permissions', exact: true }).click()
    await expect(editor).toHaveCount(0)
    access = (await (await page.request.get(endpoint)).json()).items[0]
    expect(access).toMatchObject({
      scope: 'all',
      source_ids: [],
      requests_per_minute: 0,
      max_concurrent_downloads: 0,
      expires_at: null,
    })
  })
}
