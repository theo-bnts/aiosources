import { test, expect } from './fixtures'

test('public books use the same title and file controls without administration or hidden source names', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const form = page.getByRole('form', { name: 'Public access', exact: true })
  await form.getByLabel('Enable public access', { exact: true }).check()
  await form.getByLabel('Allow public Search', { exact: true }).check()
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({
    baseURL: server.origin,
    viewport: { width: 390, height: 844 },
    locale: 'fr-FR',
  })
  let fileCalls = 0
  const privateRequests: string[] = []
  await context.route('**/*', async (route) => {
    const request = route.request(),
      url = new URL(request.url())
    if (url.origin !== server.origin) return route.abort()
    if (
      url.pathname.startsWith('/api/') &&
      !url.pathname.startsWith('/api/public/') &&
      !['/api/session', '/api/about'].includes(url.pathname)
    )
      privateRequests.push(url.pathname)
    if (url.pathname === '/api/public/credentials')
      return route.fulfill({
        json: {
          credential: 'synthetic-book-public-credential-012345678901234567890',
          services: ['torbox'],
        },
      })
    const title = {
      id: 101,
      type: 'book',
      openlibrary: 'OL100W',
      title: 'Le Verger Ambré',
      authors: ['Morgan Vale'],
      years: [2024],
      tmdb: '',
      imdb: '',
      releases: 1,
      providers: [],
      seasons: [],
      selection: {},
    }
    if (url.pathname === '/api/public/catalog/search') {
      expect(url.searchParams.get('type')).toBe('ebook')
      expect(url.searchParams.get('language')).toBe('fr')
      return route.fulfill({ json: { items: [title], total: 1, limit: 50, offset: 0 } })
    }
    if (url.pathname === '/api/public/catalog/101') return route.fulfill({ json: title })
    if (url.pathname.endsWith('/catalog/101/releases'))
      return route.fulfill({
        json: {
          items: [
            {
              occurrence_id: 'synthetic-book',
              fingerprint: '',
              download_token: 'synthetic-public-book-token',
              provider: '',
              title: 'Morgan Vale - The Amber Orchard EPUB',
              size: 100,
              languages: [],
              services: [{ service: 'torbox', state: 'cached', can_add: false, file_count: 2 }],
            },
          ],
          total: 1,
          limit: 50,
          offset: 0,
        },
      })
    if (url.pathname.endsWith('/catalog/101/download')) {
      fileCalls++
      const body = request.postDataJSON()
      if (fileCalls === 1) {
        expect(body.download_token).toBe('synthetic-public-book-token')
        return route.fulfill({
          json: {
            status: 'pending',
            preparation_id: 'synthetic-sealed-file-choice',
            operation_id: '',
            files: [
              { path: 'Example.epub', size: 100 },
              { path: 'Example.pdf', size: 200 },
            ],
          },
        })
      }
      expect(body.preparation_id).toBe('synthetic-sealed-file-choice')
      expect(body.file).toBe('Example.epub')
      return route.fulfill({
        json: { status: 'ready', url: '/api/public/catalog-downloads/example', operation_id: '' },
      })
    }
    if (url.pathname === '/api/public/catalog-downloads/example')
      return route.fulfill({
        contentType: 'application/epub+zip',
        headers: { 'Content-Disposition': 'attachment; filename="example.epub"' },
        body: 'synthetic-book',
      })
    return route.continue()
  })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/public/search')
    await visitor.getByLabel('TorBox API key', { exact: true }).fill('synthetic-book-visitor-key')
    await visitor.getByRole('button', { name: 'Start searching', exact: true }).click()
    await visitor.getByLabel('Title, author or identifier', { exact: true }).fill('Morgan Orchard')
    await visitor.getByLabel('Type', { exact: true }).selectOption('ebook')
    await visitor.getByRole('button', { name: 'Search', exact: true }).click()
    await visitor.getByRole('link', { name: 'Le Verger Ambré', exact: true }).click()
    await expect(visitor.getByLabel('Source', { exact: true })).toHaveCount(0)
    await expect(visitor.getByLabel('Season', { exact: true })).toHaveCount(0)
    await visitor.getByRole('button', { name: 'Choose file', exact: true }).click()
    await visitor.getByLabel('File', { exact: true }).selectOption('Example.epub')
    const download = visitor.waitForEvent('download')
    await visitor.getByRole('button', { name: 'Download', exact: true }).click()
    expect((await download).suggestedFilename()).toBe('example.epub')
    await expect(visitor.getByRole('link', { name: 'View operation', exact: true })).toHaveCount(0)
    expect(privateRequests).toEqual([])
    expect(context.pages()).toHaveLength(1)
  } finally {
    await context.close()
  }
})
