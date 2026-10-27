import { test, expect } from './fixtures'

test('personal announces follow encrypted services across public Search, Stremio, Torznab and reloads', async ({
  page,
  server,
  browser,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const access = await (await page.request.get('/api/public-access')).json()
  expect(
    (
      await page.request.put('/api/public-access', {
        headers,
        data: {
          ...access,
          enabled: true,
          stremio_enabled: true,
          search_enabled: true,
          torznab_enabled: true,
          expose_sources: true,
        },
      })
    ).status(),
  ).toBe(204)
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  const key = 'synthetic-personal-service-key'
  const announce = 'https://announce.example.invalid/synthetic-personal-passkey/announce'
  let current = 'synthetic-encrypted-personal-browser-configuration-012345'
  let installs = 0
  await context.route('**/*', async (route) => {
    const request = route.request(),
      url = new URL(request.url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (url.pathname === '/api/public/config')
      return route.fulfill({
        json: {
          enabled: true,
          stremio_enabled: true,
          search_enabled: true,
          torznab_enabled: true,
          providers: [{ id: 'torbox', name: 'TorBox' }],
          sources: [{ id: 'private-example', name: 'Example Library' }],
          private_sources: [{ id: 'private-example', name: 'Example Library' }],
        },
      })
    if (['/api/public/install', '/api/public/credentials'].includes(url.pathname)) {
      const body = request.postDataJSON()
      if (installs++ === 0) {
        expect(body.services).toEqual({ torbox: key })
        expect(body.announces).toEqual({ 'private-example': announce })
      } else {
        expect(body.credential).toBe(current)
        expect(body.services).toEqual({})
        expect(body.announces).toBeUndefined()
        current += '1'
      }
      return route.fulfill({
        json: {
          credential: current,
          services: ['torbox'],
          announces: ['private-example'],
          manifest_url: `${server.origin}/public/stremio/synthetic-installation/manifest.json`,
        },
      })
    }
    if (url.pathname === '/api/public/torznab/install') {
      expect(request.postDataJSON()).toEqual({ options: {}, announces: {}, credential: current })
      current += '2'
      return route.fulfill({
        json: {
          credential: current,
          services: ['torbox'],
          announces: ['private-example'],
          url: `${server.origin}/public/torznab/api`,
          api_key: 'synthetic-encrypted-announces-without-debrid-keys',
        },
      })
    }
    if (url.pathname === '/api/public/catalog/search') {
      expect(request.headers().authorization).toBe(`Bearer ${current}`)
      return route.fulfill({ json: { items: [], total: 0, limit: 50, offset: 0 } })
    }
    return route.continue()
  })
  try {
    await visitor.goto('/public/search')
    await expect(
      visitor.getByRole('button', { name: 'Start searching', exact: true }),
    ).toBeVisible()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await visitor.getByLabel('TorBox API key', { exact: true }).fill(key)
    const field = visitor.getByLabel('Example Library announce URL', { exact: true })
    await field.fill(announce)
    await visitor.getByRole('button', { name: 'Start searching', exact: true }).click()
    await expect(visitor.getByLabel('Title, author or identifier', { exact: true })).toBeVisible()
    await visitor.reload()
    await expect(visitor.getByLabel('Title, author or identifier', { exact: true })).toBeVisible()
    await visitor.locator('summary', { hasText: 'Debrid services' }).click()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await expect(field).toHaveValue('')
    await expect(field).toHaveAttribute('placeholder', /Saved on this browser/)
    const nav = visitor.getByRole('navigation', { name: 'Main navigation' })
    await nav.getByRole('link', { name: 'Stremio', exact: true }).click()
    await expect(
      visitor.getByRole('button', { name: 'Create installation link', exact: true }),
    ).toBeVisible()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await expect(field).toHaveAttribute('placeholder', /Saved on this browser/)
    await visitor.getByRole('button', { name: 'Create installation link', exact: true }).click()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).not.toHaveValue('')
    await nav.getByRole('link', { name: 'Torznab', exact: true }).click()
    await expect(
      visitor.getByRole('button', { name: 'Create connection link', exact: true }),
    ).toBeVisible()
    await visitor.locator('summary', { hasText: 'Advanced' }).click()
    await expect(field).toHaveAttribute('placeholder', /Saved on this browser/)
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(visitor.getByLabel('API key', { exact: true })).not.toHaveValue('')
    const stored = await visitor.evaluate(() => JSON.stringify([localStorage, sessionStorage]))
    expect(stored).not.toContain(key)
    expect(stored).not.toContain(announce)
    await visitor.getByRole('button', { name: 'Forget saved credentials', exact: true }).click()
    await expect(field).toHaveAttribute('placeholder', 'Complete announce URL')
    await nav.getByRole('link', { name: 'Search', exact: true }).click()
    await expect(
      visitor.getByRole('button', { name: 'Start searching', exact: true }),
    ).toBeVisible()
    expect(
      await visitor.evaluate(() => localStorage.getItem('aiosources.public.credentials')),
    ).toBeNull()
  } finally {
    await context.close()
  }
})
