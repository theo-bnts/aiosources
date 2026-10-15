import pg from 'pg'
import { test, expect } from './fixtures'

test('connection errors stay opaque on desktop and mobile and recover automatically', async ({
  page,
}) => {
  await page.clock.install()
  await page.goto('/metadata')
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Metadata', level: 1, exact: true })).toBeVisible()
  let unavailable = true
  await page.route('**/api/session', async (route) => {
    if (unavailable) await route.abort('failed')
    else await route.continue()
  })
  await page.route('**/api/aiosource', async (route) => {
    if (unavailable) await route.abort('failed')
    else await route.continue()
  })
  await page.clock.runFor(30_001)
  const alert = page.locator('.fixed[role="alert"], .fixed [role="alert"]')
  await expect(alert).toContainText('The server is unreachable.')
  await expect(alert).toHaveCount(1)
  await expect(
    page.locator('#main-content').getByText('The server is unreachable.', { exact: false }),
  ).toHaveCount(0)
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 844 })
    for (const dark of [true, false]) {
      await page.evaluate((dark) => document.documentElement.classList.toggle('dark', dark), dark)
      expect(
        await alert.evaluate((element) => {
          const canvas = document.createElement('canvas')
          canvas.width = canvas.height = 1
          const context = canvas.getContext('2d')!
          context.fillStyle = getComputedStyle(element).backgroundColor
          context.fillRect(0, 0, 1, 1)
          return context.getImageData(0, 0, 1, 1).data[3]
        }),
      ).toBe(255)
      const bounds = await alert.boundingBox()
      expect(bounds).not.toBeNull()
      expect(bounds!.x).toBeGreaterThanOrEqual(0)
      expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width)
      expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(844)
    }
  }
  unavailable = false
  await page.clock.runFor(30_001)
  await expect(alert).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Metadata', level: 1, exact: true })).toBeVisible()
})

test('lists update without refresh controls and empty pages keep a single create action', async ({
  page,
  server,
}) => {
  for (const [path, label] of [
    ['/providers/settings', 'Add source'],
    ['/sharing', 'Create share'],
    ['/remotes', 'Add remote catalog'],
    ['/security/secrets', 'Create secret'],
  ]) {
    await page.goto(path)
    await expect(
      page.getByRole(path === '/providers/settings' ? 'link' : 'button', {
        name: label,
        exact: true,
      }),
    ).toHaveCount(1)
    await expect(page.getByRole('button', { name: /refresh/i })).toHaveCount(0)
  }
  await page.route('**/api/events', (route) => route.abort('failed'))
  await page.goto('/security/secrets')
  await page.clock.install()
  await page.reload()
  await expect(page.getByRole('button', { name: 'Create secret', exact: true })).toBeVisible()
  const session = await (await page.request.get('/api/session')).json()
  expect(
    (
      await page.request.put('/api/secrets/automatic-test', {
        headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
        data: { value: 'synthetic-not-used-value', revision: 0 },
      })
    ).status(),
  ).toBe(204)
  await page.clock.runFor(30_001)
  await expect(page.locator('main tbody')).toContainText('automatic-test')
  for (const route of ['/logs', '/health', '/security', '/metadata']) {
    await page.goto(route)
    await expect(page.getByRole('button', { name: /refresh/i })).toHaveCount(0)
  }
})

test.describe('historical response archives', () => {
  test.use({ collectionSource: true })

  test('automatic updates do not repeatedly download immutable response bytes', async ({
    page,
    server,
  }) => {
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    let pageID: string
    try {
      await db.query(
        "INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config) VALUES('archive-fixture','fixture','Synthetic archive','preview','succeeded','','{}')",
      )
      const result = await db.query(
        "INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type) VALUES('archive-fixture','fixture',1,$1,'application/json') RETURNING id",
        [Buffer.from('{"title":"Synthetic immutable response"}')],
      )
      pageID = String(result.rows[0].id)
    } finally {
      await db.end()
    }
    let downloads = 0
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === `/api/pages/${pageID}/download`) downloads++
    })
    await page.clock.install()
    await page.goto(`/pages/${pageID}`)
    await expect(page.locator('.cm-content')).toContainText('Synthetic immutable response')
    expect(downloads).toBe(1)
    await page.clock.runFor(90_001)
    await expect(page.locator('.cm-content')).toContainText('Synthetic immutable response')
    expect(downloads).toBe(1)
  })
})
