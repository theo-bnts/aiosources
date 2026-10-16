import { test, expect } from './fixtures'

test('visitor announce fields are opt-in, use source names and never retain credentials', async ({
  page,
  server,
  browser,
}, testInfo) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  expect(
    (
      await page.request.put('/api/providers/private-example', {
        headers,
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'private-example',
            name: 'Example Library',
            adapter: 'http_json',
            url: server.sourceURL,
            enabled: false,
          }),
        },
      })
    ).status(),
  ).toBe(200)
  const { config } = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: { ...config, trackers: { 'private-example': { private: true } } },
      })
    ).status(),
  ).toBe(204)
  await page.goto('/addon/settings')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  await access.getByLabel('Allow visitors to use their own debrid accounts').check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  const ownAnnounce = 'https://announce.example.invalid/visitor-private-passkey/announce?mode=own'
  let submissions = 0
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (url.pathname === '/api/public/install') {
      submissions++
      expect(route.request().postDataJSON()).toEqual({
        services: { torbox: 'synthetic-visitor-key' },
        preferences: { language: 'en', mode: 'unknown' },
        announces: { 'private-example': ownAnnounce },
      })
      return route.fulfill({
        json: {
          manifest_url: `${server.origin}/public/stremio/encrypted-own-configuration/manifest.json`,
        },
      })
    }
    return route.continue()
  })
  try {
    await visitor.goto('/')
    await expect(
      visitor.getByRole('heading', { name: 'Connect your debrid services' }),
    ).toBeVisible()
    await expect(visitor.locator('summary', { hasText: 'Advanced' })).toHaveCount(0)
    await access.locator('summary', { hasText: 'Advanced' }).click()
    const permission = access.getByLabel('Allow visitor announce URLs', { exact: true })
    await expect(permission).not.toBeChecked()
    await permission.check()
    await expect(access).toContainText('Publishes the names of your private sources')
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    await expect(visitor.locator('summary', { hasText: 'Advanced' })).toBeVisible()
    const announce = visitor.getByLabel('Example Library announce URL', { exact: true })
    await expect(announce).not.toBeVisible()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await expect(announce).toHaveAttribute('type', 'password')
    await expect(announce).toHaveAttribute('autocomplete', 'off')
    await visitor.getByLabel('TorBox API key').fill('synthetic-visitor-key')
    await announce.fill(ownAnnounce)
    await visitor.getByRole('button', { name: 'Create installation link' }).click()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).toHaveValue(
      `${server.origin}/public/stremio/encrypted-own-configuration/manifest.json`,
    )
    await expect(announce).toHaveValue('')
    await expect(visitor.getByLabel('TorBox API key')).toHaveValue('')
    expect(submissions).toBe(1)
    const stored = await visitor.evaluate(() => JSON.stringify([localStorage, sessionStorage]))
    expect(stored.includes('visitor-private-passkey')).toBe(false)
    expect(stored.includes('synthetic-visitor-key')).toBe(false)
    expect(stored.includes('encrypted-own-configuration')).toBe(false)
    await visitor.screenshot({ path: testInfo.outputPath('public-desktop.png'), fullPage: true })
    await visitor.setViewportSize({ width: 390, height: 844 })
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
    await expect(visitor.getByRole('navigation')).toHaveCount(0)
    await visitor.screenshot({ path: testInfo.outputPath('public-mobile.png'), fullPage: true })
    await permission.uncheck()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(announce).toHaveCount(0)
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).toBeVisible()
  } finally {
    await context.close()
  }
})

test('reviewing concurrent public settings preserves only fields edited locally', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  await page.goto('/addon/settings')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  await access.getByLabel('Allow visitors to use their own debrid accounts').check()
  const saved = await (await page.request.get('/api/stremio/public-access')).json()
  expect(
    (
      await page.request.put('/api/stremio/public-access', {
        headers,
        data: { ...saved, allow_private_downloads: true },
      })
    ).status(),
  ).toBe(204)
  // Saving with the earlier revision must fail, then review merges the other
  // administrator's optional field instead of silently reverting it to false.
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await access.getByRole('button', { name: 'Load latest settings', exact: true }).click()
  await access.locator('summary', { hasText: 'Advanced' }).click()
  await expect(access.getByLabel('Allow visitor announce URLs', { exact: true })).toBeChecked()
  await expect(access.getByLabel('Allow visitors to use their own debrid accounts')).toBeChecked()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const after = await (await page.request.get('/api/stremio/public-access')).json()
  expect(after.enabled).toBe(true)
  expect(after.allow_private_downloads).toBe(true)
})
