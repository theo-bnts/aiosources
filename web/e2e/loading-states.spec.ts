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

test('security statistics name the information loading before showing saved values', async ({
  page,
}) => {
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  const endpoints = /\/api\/(?:security(?:\/sessions)?|secrets)$/
  await page.route(endpoints, async (route) => {
    await pending
    await route.continue()
  })
  try {
    await page.goto('/security')
    const totals = page.locator('main dl').first()
    await expect(totals.locator('dd')).toHaveText([
      'Loading secrets…',
      'Loading verification…',
      'Loading recovery codes…',
      'Loading sessions…',
    ])
    await expect(page.locator('main').getByText('Not available', { exact: true })).toHaveCount(0)
    release()
    await expect(totals.locator('dd')).toHaveText(['0', 'Disabled', 'Not set up', '1'])
  } finally {
    release()
    await page.unroute(endpoints)
  }
})

test('a failed statistic becomes unavailable instead of loading indefinitely', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/secrets', async (route) => {
    await pending
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'Temporary service failure' }),
    })
  })
  try {
    await page.goto('/security')
    const secrets = page.locator('main dl').first().locator('dd').first()
    await expect(secrets).toHaveText('Loading secrets…')
    release()
    await expect(secrets).toHaveText('Not available')
    await expect(secrets).toHaveAttribute('aria-busy', 'false')
    await expect(page.locator('main dl').first().locator('dd').last()).toHaveText('1')
  } finally {
    release()
    await page.unroute('**/api/secrets')
  }
})
