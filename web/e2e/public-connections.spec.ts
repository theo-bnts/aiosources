import pg from 'pg'
import { test, expect, selectMultiple } from './fixtures'

test('public connections share the shell and encrypted Torznab filters without visitor profiles', async ({
  page,
  server,
  browser,
}, testInfo) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const definition = {
    version: 1,
    id: 'private-example',
    name: 'Example Library',
    adapter: 'http_json',
    url: server.sourceURL,
    enabled: false,
  }
  expect(
    (
      await page.request.put('/api/providers/private-example', {
        headers,
        data: { revision: '', json: JSON.stringify(definition) },
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
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  const context = await browser.newContext({ baseURL: server.origin })
  await context.route('**/*', (route) =>
    new URL(route.request().url()).origin === server.origin
      ? route.continue()
      : route.abort('blockedbyclient'),
  )
  const visitor = await context.newPage()
  try {
    await db.query(
      `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('private-example','one','{"title":"Example Feature 2024","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`,
    )
    await page.goto('/public-access')
    const access = page.getByRole('form', { name: 'Public access', exact: true })
    await access.getByLabel('Enable public access', { exact: true }).check()
    await access.getByLabel('Allow public Torznab connections', { exact: true }).check()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    await visitor.goto('/')
    const navigation = visitor.getByRole('navigation', { name: 'Main navigation' })
    await expect(navigation.getByRole('link')).toHaveText(['Stremio', 'Torznab'])
    await expect(visitor.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(
      'Connections',
    )
    await expect(visitor.getByLabel('TorBox API key', { exact: true })).toBeVisible()
    await navigation.getByRole('link', { name: 'Torznab', exact: true }).click()
    await expect(
      visitor.getByRole('heading', { name: 'Torznab', exact: true, level: 1 }),
    ).toBeVisible()
    await expect(visitor.getByLabel('Sources', { exact: true })).toHaveCount(0)
    await expect(visitor.getByLabel('Media types', { exact: true })).toBeHidden()
    await visitor.locator('summary', { hasText: 'Filters' }).click()
    await selectMultiple(visitor, 'Media types', ['movie', 'tv'])
    await selectMultiple(visitor, 'Services', ['torbox', 'alldebrid'])
    await visitor.getByLabel('Cached results only', { exact: true }).check()
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    const key = visitor.getByLabel('API key', { exact: true })
    await expect(key).toHaveAttribute('type', 'password')
    await expect(key).not.toHaveValue('')
    await expect(visitor.getByLabel('Media types', { exact: true })).toHaveText('Movies, Series')
    await expect(visitor.getByLabel('Services', { exact: true })).toHaveText('TorBox, AllDebrid')
    await expect(visitor.getByLabel('Cached results only', { exact: true })).toBeChecked()
    let credential = await key.inputValue()
    const feed = async (k: string) =>
      context.request.get(`/public/torznab/api?t=search&apikey=${encodeURIComponent(k)}`)
    let result = await feed(credential)
    expect(result.status()).toBe(200)
    expect(await result.text()).toContain('total="0"')
    await access.getByLabel('Expose source names and filters', { exact: true }).check()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(visitor.getByLabel('Sources', { exact: true })).toBeVisible()
    await selectMultiple(visitor, 'Media types', [])
    await visitor.getByLabel('Cached results only', { exact: true }).uncheck()
    expect(await visitor.locator('summary').allTextContents()).toEqual([
      'Filters',
      'Sorting',
      'Advanced',
      'Full endpoint URL',
    ])
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    const own = 'https://announce.example.invalid/visitor-private-passkey/announce'
    const announceField = visitor.getByLabel('Example Library announce URL', { exact: true })
    await announceField.fill(own)
    await announceField
      .locator('..')
      .getByRole('button', { name: 'Reveal Example Library announce URL', exact: true })
      .click()
    await expect(announceField).toHaveAttribute('type', 'text')
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(key).not.toHaveValue('')
    credential = await key.inputValue()
    result = await feed(credential)
    expect(result.status()).toBe(200)
    expect(await result.text()).toContain('tr=' + encodeURIComponent(own))
    await expect(visitor.getByLabel('Example Library announce URL', { exact: true })).toHaveValue(
      '',
    )
    const encryptedBase = credential.split('~')[0]
    await selectMultiple(visitor, 'Services', ['alldebrid'])
    credential = await key.inputValue()
    expect(credential.split('~')[0]).toBe(encryptedBase)
    const changed = await feed(credential)
    expect(changed.status()).toBe(200)
    expect(await changed.text()).toContain('tr=' + encodeURIComponent(own))
    const stored = await visitor.evaluate(() => JSON.stringify([localStorage, sessionStorage]))
    expect(stored.includes('visitor-private-passkey')).toBe(false)
    expect(stored.includes(credential)).toBe(false)
    // Announce-only Torznab setup is remembered too, without inventing a debrid
    // key or requiring the visitor to re-enter the URL on another public page.
    await visitor.reload()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await expect(announceField).toHaveAttribute('placeholder', /Saved on this browser/)
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(key).not.toHaveValue('')
    credential = await key.inputValue()
    expect(await (await feed(credential)).text()).toContain('tr=' + encodeURIComponent(own))
    await navigation.getByRole('link', { name: 'Stremio', exact: true }).click()
    await expect(
      visitor.getByRole('button', { name: 'Create installation link', exact: true }),
    ).toBeVisible()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await expect(announceField).toHaveAttribute('placeholder', /Saved on this browser/)
    await expect(visitor.getByLabel('TorBox API key', { exact: true })).toHaveValue('')
    await navigation.getByRole('link', { name: 'Torznab', exact: true }).click()
    await expect(
      visitor.getByRole('button', { name: 'Create connection link', exact: true }),
    ).toBeVisible()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await visitor.getByRole('button', { name: 'Remove announce', exact: true }).click()
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(key).not.toHaveValue('')
    const withoutAnnounce = await key.inputValue()
    expect(await (await feed(withoutAnnounce)).text()).not.toContain('tr=')
    await expect(announceField).toHaveAttribute('placeholder', 'Complete announce URL')
    // Replacing the saved value keeps the rest of the ordinary connection flow.
    await announceField.fill(own)
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(key).not.toHaveValue('')
    credential = await key.inputValue()
    const counts = await db.query(
      'SELECT (SELECT count(*) FROM ingest.shares)::int AS grants,(SELECT count(*) FROM ingest.share_requests)::int AS requests',
    )
    expect(counts.rows[0]).toEqual({ grants: 0, requests: 0 })
    await visitor.screenshot({
      path: testInfo.outputPath('public-torznab-desktop.png'),
      fullPage: true,
      animations: 'disabled',
    })
    await visitor.setViewportSize({ width: 390, height: 844 })
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
    await visitor.getByRole('button', { name: 'Open navigation', exact: true }).click()
    await expect(visitor.getByRole('dialog').getByRole('link')).toHaveText([
      'Stremio',
      'Torznab',
      'Admin login',
    ])
    await visitor.getByRole('button', { name: 'Close navigation', exact: true }).click()
    await visitor.screenshot({
      path: testInfo.outputPath('public-torznab-mobile.png'),
      fullPage: true,
      animations: 'disabled',
    })
    await access.getByLabel('Expose source names and filters', { exact: true }).uncheck()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    result = await feed(credential)
    expect(result.status()).toBe(200)
    expect(await result.text()).toContain('total="0"')
    await access.getByLabel('Allow public Torznab connections', { exact: true }).uncheck()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    expect((await feed(credential)).status()).toBe(401)
    expect((await context.request.get('/api/public-access')).status()).toBe(401)
  } finally {
    await context.close()
    await db.end()
  }
})
