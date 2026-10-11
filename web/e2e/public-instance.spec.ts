import pg from 'pg'
import { test, expect } from './fixtures'

test('public access is opt-in and visitors get a private link without administrator menus or saved keys', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/addon/settings')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  const enabled = access.getByRole('switch')
  await expect(enabled).not.toBeChecked()
  await enabled.check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(access.getByRole('link', { name: 'Open public homepage' })).toBeVisible()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  let submissions = 0
  // Provider validation, encryption and playback are tested against HTTP fixtures
  // in Go. This one browser response tests the form without live provider accounts.
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (url.pathname === '/api/public/install') {
      submissions++
      const body = route.request().postDataJSON()
      expect(Object.keys(body.services)).toEqual(['torbox'])
      expect(body.services.torbox === 'synthetic-visitor-key').toBe(true)
      return route.fulfill({
        json: {
          manifest_url: `${server.origin}/public/stremio/synthetic-encrypted-link/manifest.json`,
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
    await expect(visitor.getByRole('navigation')).toHaveCount(0)
    await expect(visitor.getByRole('link', { name: 'Admin login', exact: true })).toBeVisible()
    await expect(visitor.getByLabel('AllDebrid API key')).toHaveAttribute('type', 'password')
    const key = visitor.getByLabel('TorBox API key')
    await expect(key).toHaveAttribute('type', 'password')
    await visitor.getByRole('button', { name: 'Create installation link' }).click()
    await expect(visitor.getByRole('alert')).toContainText('Enter at least one service key.')
    expect(submissions).toBe(0)
    await key.fill('synthetic-visitor-key')
    await visitor.getByRole('button', { name: 'Create installation link' }).click()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).toHaveValue(
      `${server.origin}/public/stremio/synthetic-encrypted-link/manifest.json`,
    )
    await expect(key).toHaveValue('')
    expect(submissions).toBe(1)
    const stored = await visitor.evaluate(() => JSON.stringify([localStorage, sessionStorage]))
    expect(stored.includes('synthetic-visitor-key')).toBe(false)
    expect(stored.includes('synthetic-encrypted-link')).toBe(false)
    await visitor.setViewportSize({ width: 390, height: 844 })
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
    await visitor.getByRole('link', { name: 'Admin login', exact: true }).click()
    await expect(visitor.getByLabel('Administrator password', { exact: true })).toBeVisible()
    await visitor.goto('/')
    await enabled.uncheck()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(visitor.getByLabel('Administrator password', { exact: true })).toBeVisible()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).toHaveCount(0)
  } finally {
    await context.close()
  }
})

test('Stremio history distinguishes public streams, manifests and playback with safe provider diagnostics', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results,origin,kind,service,calls)
      VALUES(now(),'invalid','Manifest',2,'results',0,0,0,0,0,0,0,false,'[]','public','manifest','','[]'),
      (now(),'movie','imdb:tt1234567',20,'results',0,0,0,0,0,0,0,false,'[]','public','playback','torbox','[{"phase":"provider_playback","duration_ms":10,"http_status":200,"error_code":""}]'),
      (now(),'movie','imdb:tt1234567',20,'empty',0,0,0,0,0,0,0,false,'[]','private','stream','','[]')`)
  } finally {
    await db.end()
  }
  await page.goto('/addon/history')
  await page.getByLabel('Installation', { exact: true }).selectOption('public')
  await page.getByLabel('Request type', { exact: true }).selectOption('playback')
  await expect(page.getByRole('link', { name: 'imdb:tt1234567', exact: true })).toHaveCount(1)
  await expect(page.getByRole('link', { name: 'Manifest', exact: true })).toHaveCount(0)
  const listURL = page.url()
  await page.getByRole('link', { name: 'imdb:tt1234567', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Provider calls' })).toContainText(
    'Resolve playback link',
  )
  await expect(page.getByRole('region', { name: 'Provider calls' })).toContainText('HTTP 200')
  await expect(page.getByRole('heading', { name: 'Returned results', exact: true })).toHaveCount(0)
  await expect(page.getByText('Completed', { exact: true })).toHaveCount(2)
  await page.getByRole('link', { name: 'Stremio history', exact: true }).click()
  await expect(page).toHaveURL(listURL)
  await page.getByLabel('Request type', { exact: true }).selectOption('manifest')
  await page.getByRole('link', { name: 'Manifest', exact: true }).click()
  await expect(page.getByText('Manifest returned.', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Provider calls' })).toHaveCount(0)
})
