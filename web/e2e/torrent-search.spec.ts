import pg from 'pg'
import { test, expect } from './fixtures'

test('torrent search combines title and ID fragments, filters and saved views through the real backend', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  for (const id of ['fixture', 'remote-example']) {
    expect(
      (
        await page.request.put(`/api/providers/${id}`, {
          headers,
          data: {
            revision: '',
            json: JSON.stringify({
              version: 1,
              id,
              name: id,
              adapter: 'http_json',
              enabled: false,
              url: server.sourceURL,
            }),
          },
        })
      ).status(),
    ).toBe(200)
  }
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    for (const [provider, id, title] of [
      ['fixture', 'release-8394612', 'Matrix.Regained.2024.1080p.MULTi.WEB.H265'],
      ['remote-example', 'release-8394613', 'Matrix.Regained.2024.1080p.CAM'],
      ['fixture', 'blue', 'Blue Ocean 2024'],
      ['fixture', 'reverse', 'Ocean Blue 2024'],
      ['fixture', 'literal', String.raw`Release.100%_done\cut`],
      ['fixture', 'decoy', 'Release.100XdoneYcut'],
    ]) {
      await db.query(
        'INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES($1,$2,$3::jsonb)',
        [
          provider,
          id,
          JSON.stringify({
            title,
            size: '9007199254740993',
            info_hash: id === 'release-8394612' ? '00112233445566778899aabbccddeeff00112233' : '',
            seeders: 57,
            attributes: { seeders: ['57'] },
          }),
        ],
      )
    }
  } finally {
    await db.end()
  }
  await page.goto('/torrents')
  expect(await (await page.request.get('/api/torrents')).text()).not.toContain('"seeders"')
  expect((await page.request.get('/api/torrents?min_seeders=1')).status()).toBe(400)
  const search = page.getByLabel('Search text', { exact: true })
  const searchButton = page.getByRole('button', { name: 'Search', exact: true })
  const sourceFilter = page
    .getByRole('form', { name: 'Torrent search', exact: true })
    .getByRole('button', { name: 'Sources', exact: true })
  const results = page.locator('main tbody tr')
  await expect(results).toHaveCount(6)
  await expect(results.locator('td:nth-child(2) a')).toHaveCount(0)
  await expect(searchButton).toHaveCount(1)
  await expect(page.getByText('Combined filters', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('Minimum size (bytes)', { exact: true })).toBeVisible()
  await expect(page.getByLabel(/seeders/i)).toHaveCount(0)
  await expect(page.getByLabel('Info hash', { exact: true })).toHaveCount(0)
  for (const [query, count, included, excluded] of [
    ['ATRIX', 2, 'Matrix.Regained', 'Blue Ocean'],
    ['gain WEB', 1, 'Matrix.Regained.2024.1080p.MULTi.WEB.H265', '1080p.CAM'],
    ['release-839461', 2, 'Matrix.Regained', 'Blue Ocean'],
    ['mote-examp', 1, '1080p.CAM', '1080p.MULTi'],
    ['778899aabb', 1, '1080p.MULTi', '1080p.CAM'],
    ['00112233445566778899aabbccddeeff00112233', 1, '1080p.MULTi', '1080p.CAM'],
    ['AAISEM2EKVTHPCEZVK54ZXPO74ABCIRT', 1, '1080p.MULTi', '1080p.CAM'],
    ['"blue ocean"', 1, 'Blue Ocean', 'Ocean Blue'],
    [String.raw`100%_done\cut`, 1, '100%_done', '100Xdone'],
    ['gain -cam OR blue', 3, '1080p.MULTi', '1080p.CAM'],
  ] as const) {
    await search.fill(query)
    await searchButton.click()
    await expect(page).toHaveURL((url) => url.searchParams.get('q') === query)
    await expect(search).toHaveValue(query)
    await expect(results).toHaveCount(count)
    await expect(page.locator('main table')).toContainText(included)
    await expect(page.locator('main table')).not.toContainText(excluded)
    expect(new URL(page.url()).searchParams.get('q')).toBe(query)
  }
  await search.fill('gain -cam')
  await page.getByLabel('Sort results', { exact: true }).selectOption('relevance')
  await searchButton.click()
  await expect(results).toHaveCount(1)
  await page.getByText('Saved views', { exact: true }).click()
  await page.getByLabel('View name', { exact: true }).fill('Partial title')
  await page.getByRole('button', { name: 'Save as new', exact: true }).click()
  await expect(page.getByText('Saved the currently applied search filters.')).toBeVisible()
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(search).toHaveValue('')
  await expect(results).toHaveCount(6)
  await page.getByRole('button', { name: 'Apply view', exact: true }).click()
  await expect(search).toHaveValue('gain -cam')
  await expect(page.getByLabel('Sort results', { exact: true })).toHaveValue('relevance')
  await expect(results).toHaveCount(1)
  await page.reload()
  await expect(search).toHaveValue('gain -cam')
  await expect(results).toHaveCount(1)
  await sourceFilter.click()
  await page.getByRole('menuitemcheckbox', { name: 'remote-example', exact: true }).click()
  await page.keyboard.press('Escape')
  await search.fill('gain OR blue')
  await page.getByLabel('Minimum size (bytes)', { exact: true }).fill('9007199254740993')
  await page.getByLabel('Maximum size (bytes)', { exact: true }).fill('9007199254740993')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(results).toHaveCount(1)
  await expect(page.locator('main table')).toContainText('1080p.CAM')
  await page.reload()
  await expect(results).toHaveCount(1)
  await expect(page.getByLabel('Minimum size (bytes)', { exact: true })).toHaveValue(
    '9007199254740993',
  )
  expect(new URL(page.url()).searchParams.getAll('provider')).toEqual(['remote-example'])
  await page.setViewportSize({ width: 390, height: 844 })
  await sourceFilter.click()
  await expect(
    page.getByRole('menuitemcheckbox', { name: 'remote-example', exact: true }),
  ).toBeChecked()
  await page.getByRole('menuitemcheckbox', { name: 'fixture', exact: true }).click()
  await expect(page.getByRole('menuitemcheckbox', { name: 'fixture', exact: true })).toBeChecked()
  await page.keyboard.press('Escape')
  await expect(sourceFilter).toHaveText('2 sources')
  await searchButton.click()
  await expect(results).toHaveCount(4)
  expect(new URL(page.url()).searchParams.getAll('provider')).toEqual(['remote-example', 'fixture'])
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(results).toHaveCount(6)
  await expect(sourceFilter).toHaveText('All sources')
  const legacy = new URLSearchParams({
    q: 'gain OR blue',
    info_hash: '00112233445566778899aabbccddeeff00112233',
  })
  await page.goto(`/torrents?${legacy}`)
  await expect(results).toHaveCount(1)
  await expect(search).toHaveValue('gain OR blue 00112233445566778899aabbccddeeff00112233')
  await page.getByLabel('Minimum size (bytes)', { exact: true }).fill('1')
  await searchButton.click()
  await expect(page).toHaveURL((url) => url.searchParams.get('min_size') === '1')
  await expect(results).toHaveCount(1)
  expect(new URL(page.url()).searchParams.get('info_hash')).toBe(legacy.get('info_hash'))
  await search.fill('blue')
  await searchButton.click()
  await expect(page).toHaveURL((url) => url.searchParams.get('q') === 'blue')
  await expect(results).toHaveCount(2)
  expect(new URL(page.url()).searchParams.has('info_hash')).toBe(false)
})
