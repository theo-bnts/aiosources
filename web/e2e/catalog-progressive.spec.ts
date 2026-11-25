import pg from 'pg'
import { test, expect } from './fixtures'
import type { TestServer } from './server'
import type { Page } from '@playwright/test'

async function seedTitles(server: TestServer) {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO aiosources.aio_works(media_type,tmdb_id,igdb_id,openlibrary_id,years,display_title,book_normalized,localized_titles)
      VALUES ('movie','101','','',ARRAY[2026],'Example Journey','','{"en":"Example Video"}'),
      ('tv','202','','',ARRAY[2030],'Example Journey Home','','{"en":"Example Series"}'),
      ('game',NULL,'303','',ARRAY[2010],'Example Journey','','{"en":"Example Game"}'),
      ('book',NULL,'','OL404W',ARRAY[2020],'Example Journey','example journey','{"en":"Example Book"}');
      INSERT INTO aiosources.aio_titles(work_id,title,normalized)
      SELECT id,display_title,lower(display_title) FROM aiosources.aio_works;
      INSERT INTO aiosources.torrents(provider_id,source_id,fields)
      SELECT 'fixture',id::text,'{"title":"Example Journey","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'::jsonb FROM aiosources.aio_works;
      INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason,book_kind)
      SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture','ebook'
      FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id::text=t.source_id;
      DELETE FROM aiosources.aio_match_queue;`)
  } finally {
    await db.end()
  }
}

function gates() {
  const releases: Record<string, () => void> = {}
  const waiting = Object.fromEntries(
    ['movie,tv', 'game', 'software', 'ebook,audiobook,book', 'music'].map((kind) => [
      kind,
      new Promise<void>((resolve) => (releases[kind] = resolve)),
    ]),
  )
  return {
    waiting,
    releases,
    releaseAll: () => Object.values(releases).forEach((release) => release()),
  }
}

const cardTitles = (page: Page) => page.locator('[data-slot="catalog-card-title"]')

test('Search displays category rails progressively in Trending order and hides empty categories', async ({
  page,
  server,
}) => {
  await seedTitles(server)
  const gate = gates()
  const dispatched: string[] = []
  await page.route('**/api/catalog/search?**', async (route) => {
    const kind = new URL(route.request().url()).searchParams.get('type') || ''
    dispatched.push(kind)
    const response = await route.fetch()
    await gate.waiting[kind]
    await route.fulfill({ response })
  })
  try {
    await page.setViewportSize({ width: 320, height: 1000 })
    await page.goto('/search?q=example%20journey')
    await expect.poll(() => dispatched.length).toBe(5)
    expect(dispatched.sort()).toEqual([
      'ebook,audiobook,book',
      'game',
      'movie,tv',
      'music',
      'software',
    ])
    await expect(page.getByLabel('Category search progress')).toHaveText(
      '· 0/5 categories complete',
    )
    await expect(page.getByText('No matching titles', { exact: true })).toHaveCount(0)
    gate.releases['movie,tv']()
    await expect(cardTitles(page)).toHaveText(['Example Video', 'Example Series'])
    await expect(page.getByLabel('Category search progress')).toHaveText(
      '· 1/5 categories complete',
    )
    gate.releases.game()
    gate.releases.software()
    gate.releases.music()
    await expect(cardTitles(page)).toHaveText(['Example Video', 'Example Series', 'Example Game'])
    gate.releases['ebook,audiobook,book']()
    await expect(page.getByLabel('Category search progress')).toHaveText(
      '· 5/5 categories complete',
    )
    await expect(page.locator('[data-result-count]')).toHaveText('4 titles')
    await expect(page.locator('main h2')).toHaveText(['Videos', 'Games', 'Books'])
    await expect(cardTitles(page)).toHaveText([
      'Example Video',
      'Example Series',
      'Example Game',
      'Example Book',
    ])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  } finally {
    gate.releaseAll()
  }
})

test('a failed category preserves successful rails and retries only the failed request', async ({
  page,
  server,
}) => {
  await seedTitles(server)
  const requests: Record<string, number> = {}
  let recovered = false
  // Isolate explicit retries from the live catalog's independent refreshes.
  await page.route('**/api/events', (route) => route.fulfill({ status: 204 }))
  await page.route('**/api/catalog/search?**', async (route) => {
    const kind = new URL(route.request().url()).searchParams.get('type') || ''
    requests[kind] = (requests[kind] || 0) + 1
    if (kind === 'ebook,audiobook,book' && !recovered)
      return route.fulfill({
        status: 400,
        json: { error: 'Synthetic category failure.', code: 'invalid' },
      })
    await route.continue()
  })
  await page.goto('/search?q=example%20journey')
  await expect(cardTitles(page)).toHaveText(['Example Video', 'Example Series', 'Example Game'])
  await expect(page.getByLabel('Category search progress')).toHaveText('· 4/5 categories complete')
  await expect(page.getByRole('alert')).toHaveText(/Synthetic category failure/)
  const before = { ...requests }
  recovered = true
  await page.getByRole('button', { name: 'Try again', exact: true }).click()
  await expect(cardTitles(page)).toHaveText([
    'Example Video',
    'Example Series',
    'Example Game',
    'Example Book',
  ])
  await expect(page.getByLabel('Category search progress')).toHaveText('· 5/5 categories complete')
  expect(requests).toEqual({
    ...before,
    'ebook,audiobook,book': before['ebook,audiobook,book'] + 1,
  })
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('an empty category cannot announce no matches before the other categories finish', async ({
  page,
}) => {
  const gate = gates()
  const requests: string[] = []
  await page.route('**/api/catalog/search?**', async (route) => {
    const kind = new URL(route.request().url()).searchParams.get('type')!
    requests.push(kind)
    await gate.waiting[kind]
    await route.fulfill({ json: { items: [], total: 0, limit: 24, offset: 0 } })
  })
  try {
    await page.goto('/search?q=unmatched')
    await expect.poll(() => requests.length).toBe(5)
    gate.releases.game()
    gate.releases.software()
    gate.releases.music()
    gate.releases['movie,tv']()
    await expect(page.getByLabel('Category search progress')).toHaveText(
      '· 4/5 categories complete',
    )
    await expect(page.getByText('No matching titles', { exact: true })).toHaveCount(0)
    await expect(page.getByLabel('Searching titles')).toBeVisible()
    gate.releases['ebook,audiobook,book']()
    await expect(page.getByText('No matching titles', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Category search progress')).toHaveText(
      '· 5/5 categories complete',
    )
  } finally {
    gate.releaseAll()
  }
})

test('Show more expands only the requested category rail without refetching others', async ({
  page,
}) => {
  const requests: { type: string; limit: string }[] = []
  await page.route('**/api/catalog/search?**', async (route) => {
    const url = new URL(route.request().url())
    const type = url.searchParams.get('type') || ''
    const limit = Number(url.searchParams.get('limit') || '24')
    requests.push({ type, limit: String(limit) })
    if (type === 'movie,tv') {
      const count = Math.min(limit, 30)
      return route.fulfill({
        json: {
          items: Array.from({ length: count }, (_, i) => ({
            id: i + 1,
            type: 'movie',
            title: `Video Match ${i + 1}`,
            years: [2026],
            releases: 2,
            providers: [],
            tmdb: '',
            imdb: '',
            seasons: null,
            selection: {},
          })),
          total: 30,
          limit,
          offset: 0,
        },
      })
    }
    return route.fulfill({ json: { items: [], total: 0, limit, offset: 0 } })
  })
  await page.goto('/search?q=video')
  const videosRail = page.locator('[data-slot="catalog-rail"][data-category="videos"]')
  await expect(videosRail.locator('[data-slot="catalog-card"]')).toHaveCount(24)
  await expect(videosRail).toContainText('24 of 30')
  const showMore = videosRail.getByRole('button', { name: 'Show more Videos', exact: true })
  await expect(showMore).toBeVisible()
  requests.length = 0
  await showMore.click()
  await expect(videosRail.locator('[data-slot="catalog-card"]')).toHaveCount(30)
  await expect(showMore).toHaveCount(0)
  expect(requests).toEqual([{ type: 'movie,tv', limit: '48' }])
})

test('changing or clearing a search cannot display delayed results from the previous query', async ({
  page,
  server,
}) => {
  await seedTitles(server)
  const gate = gates()
  let waiting = 0
  await page.route('**/api/catalog/search?**', async (route) => {
    const url = new URL(route.request().url())
    if (url.searchParams.get('q') !== 'example') return route.continue()
    const response = await route.fetch()
    const body = await response.json()
    body.items = body.items.map((item: object) => ({ ...item, title: 'Stale older result' }))
    waiting++
    await gate.waiting[url.searchParams.get('type')!]
    await route.fulfill({ json: body })
  })
  try {
    await page.goto('/search?q=example')
    await expect.poll(() => waiting).toBe(5)
    await page.getByLabel('Title, author or identifier', { exact: true }).fill('journey')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.getByLabel('Category search progress')).toHaveText(
      '· 5/5 categories complete',
    )
    await expect(cardTitles(page)).toHaveCount(4)
    gate.releaseAll()
    await expect(page.getByText('Stale older result', { exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect(page.getByText('Find a title', { exact: true })).toBeVisible()
    await expect(cardTitles(page)).toHaveCount(0)
    await expect(page.getByLabel('Category search progress')).toHaveCount(0)
  } finally {
    gate.releaseAll()
  }
})

test('public Search uses the same progressive category rails and only public authenticated endpoints', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public', exact: true })
  await access.locator('summary').getByText('Permissions', { exact: true }).click()
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.getByLabel('Allow Search', { exact: true }).check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const credential = 'synthetic-sealed-browser-credential-without-secrets-012345'
  const context = await browser.newContext({ baseURL: server.origin })
  await context.addInitScript((credential) => {
    localStorage.setItem(
      'aiosources.public.credentials',
      JSON.stringify({ credential, services: ['torbox'] }),
    )
  }, credential)
  const gate = gates()
  const dispatched: string[] = []
  const privateRequests: string[] = []
  await context.route('**/*', async (route) => {
    const request = route.request(),
      url = new URL(request.url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (url.pathname === '/api/public/catalog/search') {
      expect(request.headers().authorization).toBe(`Bearer ${credential}`)
      const kind = url.searchParams.get('type') || ''
      dispatched.push(kind)
      const index = ['movie,tv', 'game', 'ebook,audiobook,book', 'software', 'music'].indexOf(kind)
      expect(index).toBeGreaterThanOrEqual(0)
      const item =
        index < 3
          ? {
              id: index + 1,
              type: ['movie', 'game', 'book'][index],
              title: ['Example Video', 'Example Game', 'Example Book'][index],
              years: [2026],
              releases: 1,
              providers: [],
              ranking: { precision: 8, relevance: 1, year: [2026, 2010, 2020][index] },
            }
          : null
      await gate.waiting[kind]
      return route.fulfill({
        json: { items: item ? [item] : [], total: item ? 1 : 0, limit: 24, offset: 0 },
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
  try {
    const visitor = await context.newPage()
    await visitor.goto('/public/search?q=example')
    await expect.poll(() => dispatched.length).toBe(5)
    gate.releases['movie,tv']()
    await expect(cardTitles(visitor)).toHaveText(['Example Video'])
    gate.releases.game()
    gate.releases.software()
    gate.releases.music()
    await expect(cardTitles(visitor)).toHaveText(['Example Video', 'Example Game'])
    gate.releases['ebook,audiobook,book']()
    await expect(cardTitles(visitor)).toHaveText(['Example Video', 'Example Game', 'Example Book'])
    await expect(visitor.getByLabel('Category search progress')).toHaveText(
      '· 5/5 categories complete',
    )
    expect(privateRequests).toEqual([])
  } finally {
    gate.releaseAll()
    await context.close()
  }
})
