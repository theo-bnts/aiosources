import { openDisclosure } from './disclosures'
import { test, expect } from './fixtures'

test('Stremio saves independent sorting priorities, optional limits and a timeout, keeping the disabled limit', async ({
  page,
}) => {
  await page.goto('/addon/settings')
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('Access name', { exact: true }).fill('Sorted playback')
  await editor.locator('summary', { hasText: /^Filters$/ }).click()
  await expect(
    editor.getByRole('switch', { name: 'Filter languages', exact: true }),
  ).not.toBeChecked()
  for (const label of [
    'Maximum results',
    'Maximum cached results',
    'Stream response timeout (seconds)',
  ])
    await expect(editor.getByLabel(label, { exact: true })).toHaveValue('')
  await editor.getByLabel('Maximum results', { exact: true }).fill('80')
  await editor.getByLabel('Maximum cached results', { exact: true }).fill('20')
  await editor.getByLabel('Stream response timeout (seconds)', { exact: true }).fill('12')
  await editor.getByRole('switch', { name: 'Cached results only', exact: true }).check()
  await expect(
    editor.getByRole('switch', { name: 'Use saved cache checks only', exact: true }),
  ).not.toBeChecked()
  await editor.getByRole('switch', { name: 'Use saved cache checks only', exact: true }).check()
  await expect(
    editor.getByLabel('Stream response timeout (seconds)', { exact: true }),
  ).toBeDisabled()
  await expect(editor.getByLabel('Maximum results', { exact: true })).toBeDisabled()
  await expect(editor.getByLabel('Maximum results', { exact: true })).toHaveValue('80')
  await editor.locator('summary', { hasText: /^Sorting$/ }).click()
  await editor.getByLabel('Sort by', { exact: true }).selectOption('cached')
  await editor.getByLabel('Then by (2)', { exact: true }).selectOption('language')
  await editor.getByLabel('Then by (3)', { exact: true }).selectOption('size')
  await editor.getByRole('button', { name: 'Sort 3 direction: descending', exact: true }).click()
  await expect(editor.getByLabel('Preferred language', { exact: true })).toBeEnabled()
  await expect(
    editor.locator('summary').filter({ hasText: /^(Request limits|Expiration)$/ }),
  ).toHaveText(['Request limits', 'Expiration'])
  await editor.locator('summary', { hasText: /^Request limits$/ }).click()
  await expect(editor.getByLabel('Requests per minute', { exact: true })).toHaveValue('')
  await editor.locator('summary', { hasText: /^Expiration$/ }).click()
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1100 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const expiry = editor.locator('#share-expires')
    expect(
      await expiry.evaluate((element) => element.getBoundingClientRect().right <= innerWidth),
    ).toBe(true)
    if (process.env.QA_DIRECTORY && width !== 320)
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/result-controls-${width}.png`,
        fullPage: true,
      })
  }
  await editor.getByRole('button', { name: 'Create access', exact: true }).click()
  await editor.getByRole('button', { name: 'I have saved the manifest URL', exact: true }).click()
  const saved = (await (await page.request.get('/api/stremio/connections')).json()).items[0]
    .stream_preferences
  expect(saved).toMatchObject({
    mode: 'all',
    language: 'en',
    cached_only: true,
    skip_cache_check: true,
    max_results: 80,
    max_cached: 20,
    response_seconds: 12,
    sorting: [
      { field: 'cached', direction: 'desc' },
      { field: 'language', direction: 'desc' },
      { field: 'size', direction: 'asc' },
    ],
  })
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await editor.locator('summary', { hasText: /^Filters$/ }).click()
  await editor.getByRole('switch', { name: 'Cached results only', exact: true }).uncheck()
  await expect(editor.getByLabel('Maximum cached results', { exact: true })).toBeDisabled()
  await editor.getByRole('switch', { name: 'Use saved cache checks only', exact: true }).uncheck()
  await expect(
    editor.getByLabel('Stream response timeout (seconds)', { exact: true }),
  ).toBeEnabled()
  await expect(editor.getByLabel('Maximum cached results', { exact: true })).toBeEnabled()
  await expect(editor.getByLabel('Maximum results', { exact: true })).toBeEnabled()
  await expect(editor.getByLabel('Maximum results', { exact: true })).toHaveValue('80')
})

test('public Stremio and Torznab use the same optional controls without creating saved access', async ({
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
        data: { ...access, enabled: true, stremio_enabled: true, torznab_enabled: true },
      })
    ).status(),
  ).toBe(204)
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    for (const path of ['/public', '/public/torznab']) {
      await visitor.goto(path)
      await visitor.locator('summary', { hasText: /^Filters$/ }).click()
      if (path === '/public') {
        await expect(
          visitor.getByRole('switch', { name: 'Use saved cache checks only', exact: true }),
        ).not.toBeChecked()
        await visitor
          .getByRole('switch', { name: 'Use saved cache checks only', exact: true })
          .check()
      } else
        await expect(
          visitor.getByRole('switch', { name: 'Use saved cache checks only', exact: true }),
        ).toHaveCount(0)
      await expect(visitor.getByLabel('Maximum results', { exact: true })).toHaveValue('')
      await visitor.getByRole('switch', { name: 'Cached results only', exact: true }).check()
      await expect(visitor.getByLabel('Maximum results', { exact: true })).toBeDisabled()
      await expect(visitor.getByLabel('Maximum cached results', { exact: true })).toBeEnabled()
      await visitor.locator('summary', { hasText: /^Sorting$/ }).click()
      await visitor.getByLabel('Sort by', { exact: true }).selectOption('size')
      await expect(
        visitor.getByRole('button', { name: 'Sort direction: descending', exact: true }),
      ).toBeVisible()
      await expect(
        visitor.getByLabel('Sort by', { exact: true }).locator('option[value=source]'),
      ).toHaveCount(0)
      await visitor.setViewportSize({ width: 320, height: 1000 })
      expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
    for (const endpoint of ['/api/stremio/connections', '/api/torznab/endpoints'])
      expect((await (await page.request.get(endpoint)).json()).items ?? []).toEqual([])
  } finally {
    await context.close()
  }
})

test('source execution saves each group independently, while torrent sorting keeps a normal query URL', async ({
  page,
}) => {
  await page.goto('/providers/settings')
  await page.locator('summary', { hasText: /^Import execution$/ }).click()
  const form = page.getByRole('form', { name: 'Concurrency', exact: true })
  await expect(page.locator('#import-execution-settings details > summary')).toHaveText([
    'Concurrency',
    'Quota handling',
    'Useful progress',
    'Recovery',
  ])
  await openDisclosure(page, 'Concurrency')
  await form.getByLabel('Simultaneous imports', { exact: true }).fill('7')
  await openDisclosure(page, 'Quota handling')
  await page.getByLabel('Automatic retries after HTTP 429', { exact: true }).fill('4')
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toHaveCount(1)
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(
    (await (await page.request.get('/api/settings/collections')).json()).settings.max_quota_retries,
  ).toBe(3)
  const quota = page.getByRole('form', { name: 'Quota handling', exact: true })
  await quota.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(quota.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.goto('/torrents')
  await openDisclosure(page, 'Sorting')
  await page.getByLabel('Sort by', { exact: true }).selectOption('size_desc')
  await page.getByRole('button', { name: 'Sort direction: descending', exact: true }).click()
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page).toHaveURL((url) => url.searchParams.get('sort') === 'size_asc')
})

test('reverting Stremio result controls restores the saved state without an unnecessary save', async ({
  page,
}) => {
  await page.goto('/addon/settings')
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('Access name', { exact: true }).fill('Unfiltered playback')
  await editor.getByRole('button', { name: 'Create access', exact: true }).click()
  await editor.getByRole('button', { name: 'I have saved the manifest URL', exact: true }).click()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await editor.locator('summary', { hasText: /^Filters$/ }).click()
  await editor.getByRole('switch', { name: 'Cached results only', exact: true }).check()
  await editor.getByRole('switch', { name: 'Cached results only', exact: true }).uncheck()
  for (const label of [
    'Maximum results',
    'Maximum cached results',
    'Stream response timeout (seconds)',
  ]) {
    await editor.getByLabel(label, { exact: true }).fill('20')
    await editor.getByLabel(label, { exact: true }).fill('')
  }
  await editor.locator('summary', { hasText: /^Sorting$/ }).click()
  await editor.getByLabel('Sort by', { exact: true }).selectOption('cached')
  await editor.getByLabel('Sort by', { exact: true }).selectOption('')
  await expect(editor.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(editor.getByText('No unsaved changes', { exact: true })).toBeVisible()
})
