import { test, expect, selectMultiple } from './fixtures'
import type { SharePassword } from '../src/lib/catalog-types'

test('Torznab saves cache and media filters with its access key and removes duplicate client setup', async ({
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
  await dialog.getByRole('button', { name: 'Reveal API key', exact: true }).click()
  const keyBox = await dialog.getByLabel('Torznab API key', { exact: true }).boundingBox()
  const eyeBox = await dialog
    .getByRole('button', { name: 'Hide API key', exact: true })
    .boundingBox()
  expect(
    keyBox &&
      eyeBox &&
      eyeBox.x >= keyBox.x + keyBox.width - 44 &&
      eyeBox.x + eyeBox.width <= keyBox.x + keyBox.width + 1,
  ).toBe(true)
  await expect(dialog.getByLabel('Torznab API key', { exact: true })).toHaveAttribute(
    'type',
    'text',
  )
  await dialog.getByRole('button', { name: 'I have saved the API key', exact: true }).click()
  const card = page.getByRole('region', { name: 'Access External application' })
  await expect(card.getByRole('switch')).not.toBeChecked()
  await expect(card.getByText('Client setup & filters', { exact: true })).toHaveCount(0)
  await expect(card.getByLabel('Additional parameters (optional)', { exact: true })).toHaveCount(0)
  await expect(card.getByRole('switch')).toBeVisible()
  expect(
    await card.getByRole('switch').evaluate((element) => getComputedStyle(element).backgroundColor),
  ).not.toBe('rgba(0, 0, 0, 0)')
  await card.getByRole('button', { name: 'Edit', exact: true }).click()
  await dialog.locator('summary', { hasText: /^Filters$/ }).click()
  await selectMultiple(dialog, 'Media types', ['movie', 'tv'])
  await selectMultiple(dialog, 'Services', ['torbox'])
  await dialog.getByRole('switch', { name: 'Cached results only', exact: true }).check()
  await dialog.getByRole('button', { name: 'Save permissions', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  const response = await page.request.get('/api/torznab/endpoints')
  const data = await response.json()
  expect(data.items).toHaveLength(1)
  expect(data.items[0].media_types).toEqual(['movie', 'tv'])
  expect(data.items[0].torznab_preferences).toEqual({ services: ['torbox'], cached_only: true })
  expect(data.items[0]).not.toHaveProperty('password')
  expect((await (await page.request.get('/api/shares')).json()).items).toEqual([])
  await page.reload()
  await card.getByRole('button', { name: 'Edit', exact: true }).click()
  await dialog.locator('summary', { hasText: /^Filters$/ }).click()
  await expect(dialog.getByRole('button', { name: 'Services', exact: true })).toHaveText('TorBox')
  await expect(dialog.getByRole('button', { name: 'Media types', exact: true })).toHaveText(
    'Movies, Series',
  )
  await expect(
    dialog.getByRole('switch', { name: 'Cached results only', exact: true }),
  ).toBeChecked()
  await dialog.getByRole('button', { name: 'Close', exact: true }).click()
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
  await expect(dialog.getByLabel('Media types', { exact: true })).toBeHidden()
  await dialog.locator('summary', { hasText: 'Filters' }).click()
  await selectMultiple(dialog, 'Media types', ['movie'])
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
  await expect(card.getByRole('switch')).not.toBeChecked()
  const switchOffColor = await card
    .getByRole('switch')
    .evaluate((element) => getComputedStyle(element).backgroundColor)
  expect(switchOffColor).not.toBe('rgba(0, 0, 0, 0)')
  await card.getByRole('switch', { name: 'Enable Living room', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Enable access', exact: true })
    .click()
  await expect(card.getByRole('switch')).toBeChecked()
  await expect
    .poll(() =>
      card.getByRole('switch').evaluate((element) => getComputedStyle(element).backgroundColor),
    )
    .not.toBe(switchOffColor)
  expect(
    await card.getByRole('switch').evaluate((element) => getComputedStyle(element).backgroundColor),
  ).not.toBe('rgba(0, 0, 0, 0)')
  const switchBox = await card.getByRole('switch').boundingBox()
  const headingBox = await card
    .getByRole('heading', { name: 'Living room', exact: true })
    .boundingBox()
  expect(switchBox && headingBox && Math.abs(switchBox.y - headingBox.y) < 4).toBe(true)

  const first = await page.request.get(manifest)
  expect(first.status()).toBe(200)
  const firstManifest = await first.json()
  expect(firstManifest.types).toEqual(['movie', 'series', 'anime'])
  await card.getByRole('button', { name: 'Edit', exact: true }).click()
  await dialog.locator('summary', { hasText: 'Filters' }).click()
  await selectMultiple(dialog, 'Media types', ['series'])
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
  await card.getByRole('button', { name: 'Delete', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Delete access', exact: true })
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
    .getByRole('button', { name: 'Edit', exact: true })
    .click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('summary', { hasText: /^Filters$/ }).click()
  await dialog.getByRole('button', { name: 'Services', exact: true }).click()
  await expect(
    page.getByRole('menuitemcheckbox', { name: 'AllDebrid (not configured)', exact: true }),
  ).toBeChecked()
  await page.getByRole('menuitemcheckbox', { name: 'TorBox', exact: true }).check()
  await expect(
    page.getByRole('menuitemcheckbox', { name: 'Direct torrents', exact: true }),
  ).not.toBeChecked()
  await page
    .getByRole('menuitemcheckbox', { name: 'AllDebrid (not configured)', exact: true })
    .uncheck()
  await page.keyboard.press('Escape')
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
    await page.setViewportSize({ width: 375, height: 812 })
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
    const expiry = editor.getByLabel('Expires at (browser local time)', { exact: true })
    const bounds = await expiry.evaluate((element) => {
      const input = element.getBoundingClientRect()
      const field = element.closest('fieldset')!.getBoundingClientRect()
      return {
        left: input.left,
        right: input.right,
        fieldLeft: field.left,
        fieldRight: field.right,
        viewport: innerWidth,
      }
    })
    expect(bounds.left).toBeGreaterThanOrEqual(bounds.fieldLeft)
    expect(bounds.right).toBeLessThanOrEqual(bounds.fieldRight + 1)
    expect(bounds.right).toBeLessThanOrEqual(bounds.viewport)
    if (process.env.QA_DIRECTORY)
      await page.screenshot({ path: `${process.env.QA_DIRECTORY}/${format}-expiry-mobile.png` })
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
    await card.getByRole('button', { name: 'Edit', exact: true }).click()
    await editor.locator('summary', { hasText: /^Filters$/ }).click()
    if (format === 'torznab') {
      await editor.getByRole('switch', { name: 'Cached results only', exact: true }).check()
      await editor.getByRole('switch', { name: 'Cached results only', exact: true }).uncheck()
      await selectMultiple(editor, 'Services', ['torbox'])
      await selectMultiple(editor, 'Services', [])
      await expect(
        editor.getByRole('button', { name: 'Save permissions', exact: true }),
      ).toBeDisabled()
      await expect(editor.getByText('No unsaved changes', { exact: true })).toBeVisible()
    }
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
    await card.getByRole('button', { name: 'Edit', exact: true }).click()
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

test('new Stremio access uses the same browser language defaults and saves a resolved locale', async ({
  page,
}) => {
  await page.goto('/addon/settings')
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Access name', { exact: true }).fill('Browser language access')
  await dialog.locator('summary', { hasText: /^Filters$/ }).click()
  await expect(dialog.getByLabel('Preferred language', { exact: true })).toHaveValue('')
  await expect(dialog.getByRole('switch', { name: 'Filter languages', exact: true })).toBeChecked()
  await expect(dialog.getByLabel('Include', { exact: true })).toHaveValue('unknown')
  await dialog.getByRole('button', { name: 'Create disabled access', exact: true }).click()
  await dialog.getByRole('button', { name: 'I have saved the manifest URL', exact: true }).click()
  const data = await (await page.request.get('/api/stremio/connections')).json()
  expect(data.items[0].stream_preferences).toMatchObject({ language: 'en', mode: 'unknown' })
  await expect(
    page.getByRole('region', { name: 'Access Browser language access', exact: true }),
  ).toContainText('English + MULTI + unknown')
})
