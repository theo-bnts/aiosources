import pg from 'pg'
import { test, expect } from './fixtures'

test('native fields keep consistent mobile text and multiline inputs stay inside their form', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 })
  await page.goto('/network/incoming')
  const header = page.getByLabel('IP address header', { exact: true })
  const format = page.getByLabel('Header format', { exact: true })
  await expect(header).toBeVisible()
  const textSize = await header.evaluate((element) => getComputedStyle(element).fontSize)
  expect(await format.evaluate((element) => getComputedStyle(element).fontSize)).toBe(textSize)
  const addresses = page.getByLabel('Trusted proxy addresses', { exact: true })
  const initial = await addresses.inputValue()
  await addresses.evaluate((element) => {
    element.style.width = '1200px'
  })
  expect(
    await addresses.evaluate((element) => element.getBoundingClientRect().right <= innerWidth),
  ).toBe(true)
  await expect(addresses).toHaveValue(initial)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('shared dialog close actions remain easy to target on narrow and wide screens', async ({
  page,
}) => {
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    for (const name of ['Interface preferences', 'About AIOSources']) {
      await page.getByRole('button', { name, exact: true }).click()
      const dialog = page.getByRole('dialog')
      const close = dialog
        .getByRole('button', { name: 'Close', exact: true })
        .filter({ has: page.locator('svg') })
      const target = width < 640 ? 40 : 32
      await expect
        .poll(async () => (await close.boundingBox())?.width ?? 0)
        .toBeGreaterThanOrEqual(target)
      await expect
        .poll(async () => (await close.boundingBox())?.height ?? 0)
        .toBeGreaterThanOrEqual(target)
      await close.click()
      await expect(dialog).toBeHidden()
      await expect(page.getByRole('button', { name, exact: true })).toBeFocused()
    }
  }
})

test('destructive confirmations preserve long source names without clipping their actions', async ({
  page,
  server,
}) => {
  const name = 'Example'.repeat(24)
  const session = await (await page.request.get('/api/session')).json()
  expect(
    (
      await page.request.put('/api/providers/example', {
        headers: { Origin: server.origin, 'X-CSRF-Token': session.csrf_token },
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'example',
            name,
            adapter: 'json',
            enabled: false,
            url: server.sourceURL,
          }),
        },
      })
    ).ok(),
  ).toBe(true)
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/providers/settings')
    await page
      .getByRole('region', { name: `Source ${name}`, exact: true })
      .getByRole('button', { name: 'Delete', exact: true })
      .click()
    const dialog = page.getByRole('alertdialog')
    await expect(dialog.getByRole('heading')).toHaveText(`Delete source “${name}”?`)
    expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(
      true,
    )
    await expect(dialog.getByRole('button', { name: 'Delete source', exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Back', exact: true }).click()
    await expect(dialog).toBeHidden()
  }
  expect((await page.request.get('/api/providers/example')).ok()).toBe(true)
})

test('long catalog title headings remain readable with their return navigation on mobile', async ({
  page,
  server,
}) => {
  const title = 'Example'.repeat(32)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  let id = ''
  try {
    const result = await db.query(
      `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years,display_title) VALUES('movie','101','tt0000101',ARRAY[2026],$1) RETURNING id`,
      [title],
    )
    id = String(result.rows[0].id)
    await db.query(`INSERT INTO ingest.aio_titles(work_id,title,normalized) VALUES($1,$2,$3)`, [
      id,
      title,
      title.toLowerCase(),
    ])
    await db.query(
      `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example','one',$1)`,
      [JSON.stringify({ title, imdb_id: 'tt0000101', info_hash: 'a'.repeat(40), size: 100 })],
    )
    await db.query(
      `INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason) SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','synthetic identifier' FROM ingest.torrents t CROSS JOIN ingest.aio_works w; DELETE FROM ingest.aio_match_queue`,
    )
  } finally {
    await db.end()
  }
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`/search/${id}`)
    await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const bounds = await page.getByRole('heading', { name: title, exact: true }).boundingBox()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width)
  }
})
