import { test, expect, selectMultiple } from './fixtures'
import { debridProviders } from '../src/lib/debrid'
import type { Page } from '@playwright/test'

async function enablePublicConnections(page: Page, origin: string) {
  const session = await (await page.request.get('/api/session')).json()
  const current = await (await page.request.get('/api/public-access')).json()
  expect(
    (
      await page.request.put('/api/public-access', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: origin },
        data: {
          ...current,
          enabled: true,
          stremio_enabled: true,
          search_enabled: true,
          torznab_enabled: true,
        },
      })
    ).status(),
  ).toBe(204)
}

test('six configured services stay alphabetic in settings, statistics, Stremio, Torznab and routing', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const current = await (await page.request.get('/api/aiosource/controls')).json()
  const config = { ...current.config }
  for (const provider of debridProviders) {
    const reference = `test_${provider.id}`
    expect(
      (
        await page.request.put(`/api/secrets/${reference}`, {
          headers,
          data: { revision: 0, value: `synthetic-never-sent-${provider.id}` },
        })
      ).status(),
    ).toBe(204)
    config[provider.secret] = reference
  }
  expect(
    (await page.request.put('/api/aiosource/config', { headers, data: config })).status(),
  ).toBe(204)
  await page.goto('/debrid/settings')
  const form = page.getByRole('form', { name: 'Debrid configuration', exact: true })
  await expect(form.locator('label').filter({ hasText: /API key$/ })).toHaveText(
    debridProviders.map((p) => `${p.name} API key`),
  )
  for (const provider of debridProviders)
    await expect(
      form.getByRole('button', { name: `Add ${provider.name} key`, exact: true }),
    ).toBeDisabled()
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    if (process.env.QA_DIRECTORY && width !== 320)
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/six-debrid-settings-${width}.png`,
        fullPage: true,
      })
  }
  await page.setViewportSize({ width: 1440, height: 1100 })
  await page.goto('/debrid')
  const statistics = page.getByRole('region', { name: 'Availability checks', exact: true })
  await expect(statistics.getByRole('heading', { level: 3 })).toHaveText(
    debridProviders.map((p) => p.name),
  )
  await expect(statistics.getByText('Configured', { exact: true })).toHaveCount(6)
  await expect(statistics).toContainText('unknown')

  for (const format of ['stremio', 'torznab']) {
    await page.goto(format === 'stremio' ? '/addon/settings' : '/torznab')
    await page.getByRole('button', { name: 'Create access', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await dialog.locator('summary', { hasText: /^Filters$/ }).click()
    await dialog.getByRole('button', { name: 'Services', exact: true }).click()
    await expect(page.getByRole('menuitemcheckbox')).toHaveText([
      ...debridProviders.map((p) => p.name),
      ...(format === 'stremio' ? ['Direct torrents'] : []),
    ])
    await page.keyboard.press('Escape')
    await selectMultiple(
      dialog,
      'Services',
      debridProviders.map((p) => p.id),
    )
    await dialog.getByLabel('Access name', { exact: true }).fill(`All services ${format}`)
    await dialog.getByRole('button', { name: 'Create disabled access', exact: true }).click()
    await dialog
      .getByRole('button', {
        name: format === 'stremio' ? 'I have saved the manifest URL' : 'I have saved the API key',
        exact: true,
      })
      .click()
    const endpoint = format === 'stremio' ? '/api/stremio/connections' : '/api/torznab/endpoints'
    const saved = (await (await page.request.get(endpoint)).json()).items[0]
    expect((saved.stream_preferences ?? saved.torznab_preferences).services).toEqual(
      debridProviders.map((p) => p.id),
    )
  }
  await page.goto('/network/outgoing/routing')
  const routing = page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'Debrid', exact: true, level: 2 }) })
  await expect(routing.locator('label[for^="network-debrid:"]')).toHaveText(
    debridProviders.map((p) => p.name),
  )
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
  await search.getByRole('combobox').fill('Deepbrid API key')
  await search.getByRole('option').first().click()
  await expect(page.getByLabel('Deepbrid API key', { exact: true })).toBeFocused()
})

test('public setup supports all six keys without overflow and remembers only the encrypted configuration', async ({
  page,
  browser,
  server,
}) => {
  await enablePublicConnections(page, server.origin)
  const info = await (
    await page.request.get('/api/public/config', { headers: { Cookie: '' } })
  ).json()
  expect(info.providers).toEqual(debridProviders.map(({ id, name }) => ({ id, name })))
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  const keys = Object.fromEntries(
    debridProviders.map((p) => [p.id, `synthetic-${p.id}-visitor-key`]),
  )
  const credential = 'synthetic-encrypted-six-service-browser-configuration-0123456789'
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (url.pathname === '/api/public/install') {
      expect(route.request().postDataJSON().services).toEqual(keys)
      return route.fulfill({
        json: {
          credential,
          services: debridProviders.map((p) => p.id),
          announces: [],
          manifest_url: `${server.origin}/public/stremio/${credential}/manifest.json`,
        },
      })
    }
    return route.continue()
  })
  try {
    await visitor.goto('/public')
    for (const provider of debridProviders) {
      const field = visitor.getByLabel(`${provider.name} API key`, { exact: true })
      await expect(field).toHaveAttribute('type', 'password')
      await field.fill(keys[provider.id])
    }
    for (const width of [1440, 390, 320]) {
      await visitor.setViewportSize({ width, height: 1000 })
      expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      if (width === 1440) {
        for (let index = 0; index < debridProviders.length; index += 2) {
          const left = await visitor
            .getByLabel(`${debridProviders[index].name} API key`, { exact: true })
            .boundingBox()
          const right = await visitor
            .getByLabel(`${debridProviders[index + 1].name} API key`, { exact: true })
            .boundingBox()
          expect(left && right && Math.abs(left.y - right.y) <= 1).toBe(true)
        }
      }
      if (process.env.QA_DIRECTORY && width !== 320)
        await visitor.screenshot({
          path: `${process.env.QA_DIRECTORY}/six-public-services-${width}.png`,
          fullPage: true,
        })
    }
    await visitor.getByRole('button', { name: 'Create installation link', exact: true }).click()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).not.toHaveValue('')
    await visitor.reload()
    for (const provider of debridProviders) {
      const field = visitor.getByLabel(`${provider.name} API key`, { exact: true })
      await expect(field).toHaveValue('')
      await expect(field).toHaveAttribute('placeholder', /Saved on this browser/)
    }
    const stored = await visitor.evaluate(() => JSON.stringify([localStorage, sessionStorage]))
    expect(stored).toContain(credential)
    for (const key of Object.values(keys)) expect(stored).not.toContain(key)
  } finally {
    await context.close()
  }
})

test('instance previews have a same-origin logo and description in the initial anonymous HTML', async ({
  page,
  server,
}) => {
  for (const publicEnabled of [false, true]) {
    if (publicEnabled) await enablePublicConnections(page, server.origin)
    const response = await page.request.get('/', { headers: { Cookie: '' } })
    expect(response.status()).toBe(200)
    const html = await response.text()
    expect(html).toContain('<title>AIOSources</title>')
    expect(html).toContain('property="og:title" content="AIOSources')
    expect(html).toContain('name="twitter:card" content="summary_large_image"')
    expect(html).toContain(`${server.origin}/share-preview.png`)
    expect(html).not.toContain('__AIOSOURCES_PUBLIC_ORIGIN__')
    const image = await page.request.get('/share-preview.png', { headers: { Cookie: '' } })
    expect(image.status()).toBe(200)
    expect(image.headers()['content-type']).toBe('image/png')
    const data = await image.body()
    expect(data.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a')
    expect([data.readUInt32BE(16), data.readUInt32BE(20)]).toEqual([1200, 630])
  }
})

test('six download services wrap within each release on desktop and mobile', async ({ page }) => {
  await page.route('**/api/catalog/42**', (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/catalog/42')
      return route.fulfill({
        json: {
          id: 42,
          type: 'movie',
          title: 'Example Film',
          years: [2021],
          tmdb: '42',
          imdb: '',
          releases: 1,
          providers: [],
          seasons: null,
          selection: {},
        },
      })
    if (path === '/api/catalog/42/releases')
      return route.fulfill({
        json: {
          total: 1,
          limit: 50,
          offset: 0,
          items: [
            {
              occurrence_id: 'synthetic-occurrence',
              fingerprint: 'synthetic-fingerprint',
              provider: '',
              title: 'Example.Film.2021.1080p.WEB-DL.mkv',
              size: 1000000000,
              languages: [],
              services: debridProviders.map((p) => ({
                service: p.id,
                state: 'cached',
                can_add: false,
              })),
            },
          ],
        },
      })
    return route.continue()
  })
  await page.goto('/search/42')
  await expect(page.getByRole('button', { name: 'Download', exact: true })).toHaveCount(6)
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const cell = page.locator('tbody td').last()
    expect(await cell.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
    if (process.env.QA_DIRECTORY && width !== 320)
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/six-download-services-${width}.png`,
        fullPage: true,
      })
  }
})
