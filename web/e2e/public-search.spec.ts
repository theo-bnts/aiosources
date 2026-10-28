import { test, expect } from './fixtures'

// Provider-backed encryption, authorization, cache privacy, preparation and
// revocation run end-to-end in Go. These bounded responses exercise the public
// browser contract without contacting external provider accounts.
const credential = 'synthetic-sealed-browser-credential-without-secrets-012345'

test('public downloads discard a missing preparation before an explicit retry', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.getByLabel('Allow public Search', { exact: true }).check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  await context.addInitScript((credential) => {
    localStorage.setItem(
      'aiosources.public.credentials',
      JSON.stringify({ credential, services: ['torbox'] }),
    )
  }, credential)
  const requests: Record<string, unknown>[] = []
  const privateRequests: string[] = []
  await context.route('**/*', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (url.pathname === '/api/public/catalog/101')
      return route.fulfill({
        json: {
          id: 101,
          type: 'movie',
          title: 'Example Film',
          years: [2026],
          releases: 1,
          providers: [],
          selection: {},
        },
      })
    if (url.pathname === '/api/public/catalog/101/releases')
      return route.fulfill({
        json: {
          total: 1,
          limit: 50,
          offset: 0,
          items: [
            {
              occurrence_id: 'synthetic-public-release',
              download_token: 'synthetic-public-source-capability',
              title: 'Example.Film.2026',
              size: 100,
              languages: [],
              services: [{ service: 'torbox', state: 'uncached', can_add: true }],
            },
          ],
        },
      })
    if (url.pathname === '/api/public/catalog/101/download') {
      expect(request.headers().authorization).toBe(`Bearer ${credential}`)
      requests.push(request.postDataJSON())
      return route.fulfill({
        status: requests.length === 2 ? 404 : 202,
        json:
          requests.length === 2
            ? { code: 'not_found', error: 'The prepared file no longer exists.' }
            : {
                status: 'pending',
                operation_id: requests.length === 1 ? 'synthetic-old' : 'synthetic-new',
                preparation_id:
                  requests.length === 1 ? 'sealed-old-preparation' : 'sealed-new-preparation',
              },
      })
    }
    if (
      url.pathname.startsWith('/api/') &&
      !url.pathname.startsWith('/api/public/') &&
      !['/api/session', '/api/about'].includes(url.pathname)
    )
      privateRequests.push(url.pathname)
    return route.continue()
  })
  const visitor = await context.newPage()
  try {
    await visitor.clock.install()
    await visitor.goto('/public/search/101')
    await visitor.getByRole('button', { name: 'Add to debrid', exact: true }).click()
    await expect(visitor.getByText('Preparing', { exact: true })).toBeVisible()
    await visitor.clock.runFor(15_001)
    await expect(visitor.getByText('Not ready', { exact: true })).toBeVisible()
    expect(requests).toHaveLength(2)
    expect(requests[1]).toEqual({ preparation_id: 'sealed-old-preparation' })
    await visitor.clock.runFor(60_001)
    expect(requests).toHaveLength(2)
    await visitor.getByRole('button', { name: 'Add to debrid', exact: true }).click()
    await expect(visitor.getByText('Preparing', { exact: true })).toBeVisible()
    expect(requests).toHaveLength(3)
    expect(requests[2]).toMatchObject({
      download_token: 'synthetic-public-source-capability',
      service: 'torbox',
      allow_uncached: true,
    })
    expect(requests[2].preparation_id).toBeUndefined()
    await expect(visitor.getByRole('link', { name: 'View operation', exact: true })).toHaveCount(0)
    expect(privateRequests).toEqual([])
    expect(context.pages()).toHaveLength(1)
  } finally {
    await context.close()
  }
})

test('Search is opt-in and remembers encrypted services across Stremio, reloads and browser tabs', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  let submissions = 0
  const privateRequests: string[] = []
  const releaseRequests: URL[] = []
  await context.route('**/*', async (route) => {
    const request = route.request(),
      url = new URL(request.url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (
      url.pathname.startsWith('/api/') &&
      !url.pathname.startsWith('/api/public/') &&
      url.pathname !== '/api/session' &&
      url.pathname !== '/api/about'
    )
      privateRequests.push(url.pathname)
    if (['/api/public/install', '/api/public/credentials'].includes(url.pathname)) {
      const body = request.postDataJSON()
      if (submissions++ === 0)
        expect(body.services).toEqual({ torbox: 'synthetic-own-browser-key' })
      else {
        expect(body.services).toEqual({})
        expect(body.credential).toBe(credential)
      }
      return route.fulfill({
        json: {
          credential,
          services: ['torbox'],
          manifest_url: `${server.origin}/public/stremio/synthetic-manifest-configuration/manifest.json`,
        },
      })
    }
    if (url.pathname.startsWith('/api/public/catalog/')) {
      expect(request.headers().authorization).toBe(`Bearer ${credential}`)
      if (url.pathname.endsWith('/search'))
        return route.fulfill({
          json: {
            items: [
              {
                id: 101,
                type: 'movie',
                title: 'Example Film',
                years: [2026],
                tmdb: '101',
                imdb: 'tt1234567',
                releases: 1,
                providers: [],
              },
            ],
            total: 1,
            limit: 50,
            offset: 0,
          },
        })
      if (url.pathname.endsWith('/releases')) {
        releaseRequests.push(url)
        return route.fulfill({
          json: {
            items: [
              {
                occurrence_id: 'synthetic-row',
                fingerprint: '',
                download_token: 'synthetic-source-capability',
                provider: '',
                title: 'Example.Film.2026.1080p',
                size: 100,
                languages: ['EN'],
                services: [{ service: 'torbox', state: 'cached', can_add: false }],
              },
            ],
            total: 1,
            limit: 50,
            offset: 0,
          },
        })
      }
      if (url.pathname.endsWith('/download')) {
        expect(request.postDataJSON()).toMatchObject({
          download_token: 'synthetic-source-capability',
          service: 'torbox',
          allow_uncached: false,
        })
        return route.fulfill({
          json: {
            status: 'ready',
            operation_id: 'synthetic-public-operation',
            url: '/api/public/catalog-downloads/synthetic-file-ticket',
          },
        })
      }
      return route.fulfill({
        json: {
          id: 101,
          type: 'movie',
          title: 'Example Film',
          years: [2026],
          tmdb: '101',
          imdb: 'tt1234567',
          releases: 1,
          providers: [],
          seasons: null,
          selection: {},
        },
      })
    }
    if (url.pathname === '/api/public/catalog-downloads/synthetic-file-ticket')
      return route.fulfill({
        headers: {
          'Content-Type': 'application/octet-stream',
          'Content-Disposition': 'attachment; filename="example.mkv"',
        },
        body: 'synthetic-file',
      })
    return route.continue()
  })
  try {
    await visitor.goto('/public')
    await expect(
      visitor
        .getByRole('navigation', { name: 'Main navigation' })
        .getByRole('link', { name: 'Search', exact: true }),
    ).toHaveCount(0)
    await access.getByLabel('Allow public Search', { exact: true }).check()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    const searchLink = visitor
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Search', exact: true })
    await expect(searchLink).toBeVisible()
    await searchLink.click()
    await expect(
      visitor.getByRole('button', { name: 'Start searching', exact: true }),
    ).toBeVisible()
    await visitor.getByLabel('TorBox API key', { exact: true }).fill('synthetic-own-browser-key')
    const connection = visitor.waitForResponse(
      (response) => new URL(response.url()).pathname === '/api/public/credentials',
      { timeout: 8000 },
    )
    await visitor.getByRole('button', { name: 'Start searching', exact: true }).click()
    expect((await connection).status()).toBe(200)
    await expect(visitor.getByLabel('Title, author or identifier', { exact: true })).toBeVisible()
    await visitor.getByLabel('Title, author or identifier', { exact: true }).fill('example film')
    await visitor.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(visitor.locator('[data-result-count]')).toHaveText('1 title')
    await visitor.getByRole('link', { name: 'Example Film', exact: true }).click()
    await expect(visitor.locator('[data-result-count]')).toHaveText('1 source release')
    await expect(
      visitor.locator('main').getByRole('link', { name: 'Search', exact: true }),
    ).toHaveAttribute('href', '/public/search?q=example+film')
    await expect(visitor.getByLabel('Source', { exact: true })).toHaveCount(0)
    await expect(visitor.getByRole('option', { name: 'Source name', exact: true })).toHaveCount(0)
    const download = visitor.waitForEvent('download')
    await visitor.getByRole('button', { name: 'Download', exact: true }).click()
    expect((await download).suggestedFilename()).toBe('example.mkv')
    await expect(visitor.getByRole('link', { name: 'View operation' })).toHaveCount(0)
    expect(context.pages()).toHaveLength(1)
    const savedSorting = [
      { field: 'source', direction: 'asc' },
      { field: 'size', direction: 'desc' },
    ]
    await visitor.goto(
      `/public/search/101?provider=hidden-source&sorting=${encodeURIComponent(JSON.stringify(savedSorting))}`,
    )
    await expect(visitor.getByRole('button', { name: 'Download', exact: true })).toBeVisible()
    await visitor.locator('summary', { hasText: /^Sorting$/ }).click()
    await expect(visitor.getByLabel('Sort by', { exact: true })).toHaveValue('size')
    const applied = releaseRequests.at(-1)!
    expect(applied.searchParams.get('provider')).toBeNull()
    expect(JSON.parse(applied.searchParams.get('sorting')!)).toEqual([
      { field: 'size', direction: 'desc' },
    ])
    await visitor
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Stremio', exact: true })
      .click()
    const key = visitor.getByLabel('TorBox API key', { exact: true })
    await expect(key).toHaveValue('')
    await expect(key).toHaveAttribute('placeholder', /Saved on this browser/)
    await visitor.getByRole('button', { name: 'Create installation link', exact: true }).click()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).toHaveValue(
      /synthetic-manifest-configuration/,
    )
    await visitor.reload()
    await expect(key).toHaveAttribute('placeholder', /Saved on this browser/)
    const storage = await visitor.evaluate(() => JSON.stringify([localStorage, sessionStorage]))
    expect(storage).toContain(credential)
    expect(storage).not.toContain('synthetic-own-browser-key')
    expect(storage).not.toContain('synthetic-manifest-configuration')
    const second = await context.newPage()
    await second.goto('/public/search')
    await expect(second.getByLabel('Title, author or identifier', { exact: true })).toBeVisible()
    await visitor.getByRole('button', { name: 'Forget saved credentials', exact: true }).click()
    await expect(second.getByRole('button', { name: 'Start searching', exact: true })).toBeVisible()
    await second.close()
    await visitor.setViewportSize({ width: 390, height: 844 })
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
    expect(privateRequests).toEqual([])
    expect((await context.request.get('/api/providers')).status()).toBe(401)
  } finally {
    await context.close()
  }
})

test('expired visitor keys clear only public credentials and preserve an administrator preview session', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const access = await (await page.request.get('/api/public-access')).json()
  expect(
    (
      await page.request.put('/api/public-access', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
        data: { ...access, enabled: true, search_enabled: true },
      })
    ).status(),
  ).toBe(204)
  await page.evaluate(
    (credential) =>
      localStorage.setItem(
        'aiosources.public.credentials',
        JSON.stringify({ credential, services: ['torbox'] }),
      ),
    credential,
  )
  await page.route('**/api/public/catalog/search?*', (route) =>
    route.fulfill({
      status: 401,
      json: {
        error: 'Your saved service configuration is no longer available. Enter your keys again.',
      },
    }),
  )
  await page.goto('/public/search?q=example')
  await expect(page.getByRole('button', { name: 'Start searching', exact: true })).toBeVisible()
  expect(
    await page.evaluate(() => localStorage.getItem('aiosources.public.credentials')),
  ).toBeNull()
  await page.getByRole('link', { name: 'Administration', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Sources', level: 1, exact: true })).toBeVisible()
  expect((await page.request.get('/api/providers')).status()).toBe(200)
})

test('Stremio keys are reused by public Search on mobile and failed requests preserve saved keys', async ({
  page,
  server,
  browser,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const access = await (await page.request.get('/api/public-access')).json()
  expect(
    (
      await page.request.put('/api/public-access', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
        data: { ...access, enabled: true, search_enabled: true },
      })
    ).status(),
  ).toBe(204)
  const context = await browser.newContext({
    baseURL: server.origin,
    viewport: { width: 390, height: 844 },
  })
  const visitor = await context.newPage()
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (['/api/public/install', '/api/public/credentials'].includes(url.pathname))
      return route.fulfill({
        json: {
          credential,
          services: ['alldebrid', 'torbox'],
          manifest_url: `${server.origin}/public/stremio/synthetic-mobile-installation/manifest.json`,
        },
      })
    if (url.pathname === '/api/public/catalog/search')
      return route.fulfill({
        status: 503,
        json: { error: 'Public access settings are unavailable. Try again shortly.' },
      })
    return route.continue()
  })
  try {
    await visitor.goto('/public')
    await visitor
      .getByLabel('AllDebrid API key', { exact: true })
      .fill('synthetic-mobile-first-key')
    await visitor.getByLabel('TorBox API key', { exact: true }).fill('synthetic-mobile-second-key')
    await visitor.getByRole('button', { name: 'Create installation link', exact: true }).click()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).toBeVisible()
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
    await visitor.goto('/public/search?q=example')
    await expect(visitor.getByLabel('Title, author or identifier', { exact: true })).toHaveValue(
      'example',
    )
    await expect(
      visitor.getByText('Public access settings are unavailable. Try again shortly.', {
        exact: true,
      }),
    ).toBeVisible()
    await visitor.locator('summary', { hasText: 'Debrid services' }).click()
    await expect(visitor.getByLabel('AllDebrid API key', { exact: true })).toHaveAttribute(
      'placeholder',
      /Saved on this browser/,
    )
    await expect(visitor.getByLabel('TorBox API key', { exact: true })).toHaveAttribute(
      'placeholder',
      /Saved on this browser/,
    )
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
    const stored = await visitor.evaluate(() =>
      localStorage.getItem('aiosources.public.credentials'),
    )
    expect(stored).toContain(credential)
    expect(stored).not.toContain('synthetic-mobile-first-key')
    expect(stored).not.toContain('synthetic-mobile-second-key')
    await visitor.getByRole('button', { name: 'Open navigation', exact: true }).click()
    await expect(
      visitor
        .getByRole('navigation', { name: 'Main navigation' })
        .getByRole('link', { name: 'Search', exact: true }),
    ).toHaveAttribute('aria-current', 'page')
    await expect(visitor.getByRole('link', { name: 'Storage', exact: true })).toHaveCount(0)
  } finally {
    await context.close()
  }
})
