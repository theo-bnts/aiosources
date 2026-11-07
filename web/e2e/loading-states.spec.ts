import { test, expect } from './fixtures'

test('source and remote statistics distinguish pending counts from unavailable counts', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { Origin: server.origin, 'X-CSRF-Token': session.csrf_token }
  expect(
    (
      await page.request.put('/api/secrets/remote-password', {
        headers,
        data: { value: 'synthetic-sharing-password', revision: 0 },
      })
    ).status(),
  ).toBe(204)
  expect(
    (
      await page.request.put('/api/providers/remote-copy', {
        headers,
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'remote-copy',
            name: 'Remote copy',
            adapter: 'http_json',
            enabled: false,
            url: 'https://catalog.example/api/catalogs/shared',
            auth: { type: 'bearer', secret_ref: 'remote-password' },
            http: { catalog: true, method: 'GET', items_path: '/items' },
            pagination: {
              type: 'cursor',
              in: 'query',
              cursor_param: 'cursor',
              size_param: 'limit',
              next_path: '/next_cursor',
              start: 0,
            },
            mapping: { id: '/id', fields: {} },
            catalog_sync: { enabled: false, interval_minutes: 60 },
          }),
        },
      })
    ).status(),
  ).toBe(200)

  // A remote-only instance must also finish loading native source totals.
  for (const path of ['/providers', '/remotes']) {
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/api/source-torrent-stats', async (route) => {
      await pending
      await route.continue()
    })
    try {
      await page.goto(path)
      const totals = page.locator('main dl').first()
      await expect(totals.getByText('Loading counts…', { exact: true })).toHaveCount(2)
      await expect(page.locator('main').getByText('Not available', { exact: true })).toHaveCount(0)
      await expect(totals.locator('dd[aria-busy="true"]')).toHaveCount(2)
      release()
      await expect(totals.getByText('Loading counts…', { exact: true })).toHaveCount(0)
      await expect(totals.locator('dd').first()).toHaveText('0')
      await expect(totals.locator('dd').nth(1)).toHaveText('0')
    } finally {
      release()
      await page.unroute('**/api/source-torrent-stats')
    }
  }
})

test('Security opens Secrets directly without requesting a removed statistics dashboard', async ({
  page,
}) => {
  const requested: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (['/api/security', '/api/security/sessions'].includes(path)) requested.push(path)
  })
  await page.goto('/security')
  await expect(page).toHaveURL(/\/security\/secrets$/)
  await expect(page.getByRole('heading', { name: 'Secrets', exact: true })).toBeVisible()
  await expect(page.getByText('Stored secrets', { exact: true })).toHaveCount(0)
  expect(requested).toEqual([])
})

test('a failed statistic becomes unavailable instead of loading indefinitely', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/source-torrent-stats', async (route) => {
    await pending
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'Temporary service failure' }),
    })
  })
  try {
    await page.goto('/providers')
    const imported = page.locator('main dl').first().locator('dd').first()
    await expect(imported).toHaveText('Loading counts…')
    release()
    await expect(imported).toHaveText('Not available')
    await expect(imported).toHaveAttribute('aria-busy', 'false')
    await expect(page.locator('main dl').first().locator('dd').nth(2)).toHaveText('0')
  } finally {
    release()
    await page.unroute('**/api/source-torrent-stats')
  }
})
