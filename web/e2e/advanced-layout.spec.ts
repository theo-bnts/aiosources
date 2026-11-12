import { join } from 'node:path'
import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'
import type { Locator } from '@playwright/test'

async function expectControlsWithinGroups(section: Locator) {
  const escaped = await section
    .locator('input:visible, select:visible, button:visible')
    .evaluateAll((controls) =>
      controls
        .filter((control) => {
          const group = control.closest('[data-slot="disclosure-content"]')
          if (!group) return false
          const field = control.getBoundingClientRect()
          const bounds = group.getBoundingClientRect()
          return field.left < bounds.left - 1 || field.right > bounds.right + 1
        })
        .map(
          (control) =>
            control.getAttribute('aria-label') ?? control.textContent?.trim() ?? control.tagName,
        ),
    )
  expect(escaped).toEqual([])
}

test('source setting sections save independently and folding preserves both drafts', async ({
  page,
}) => {
  const initial = await (await page.request.get('/api/settings/collections')).json()
  await openDisclosure(page, 'Import execution')
  await openDisclosure(page, 'Concurrency')
  const imports = page.getByRole('form', { name: 'Concurrency', exact: true })
  await imports.getByLabel('Simultaneous imports', { exact: true }).fill('7')
  await openDisclosure(page, 'Temporary source errors')
  const retries = page.getByRole('form', { name: 'Temporary source errors', exact: true })
  const delay = retries.getByLabel('Initial source retry delay (seconds)', { exact: true })
  await delay.fill('3')
  await page.locator('summary', { hasText: /^Import execution$/ }).click()
  await expect(imports).toBeHidden()
  await retries.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(retries.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await (await page.request.get('/api/settings/collections')).json()).settings).toEqual(
    initial.settings,
  )
  await openDisclosure(page, 'Import execution')
  await openDisclosure(page, 'Concurrency')
  await expect(imports.getByLabel('Simultaneous imports', { exact: true })).toHaveValue('7')
  await imports.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(imports.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(
    (await (await page.request.get('/api/settings/runtime/source_retries')).json()).values
      .initial_seconds,
  ).toBe(3)
  await delay.fill('4')
  await page.locator('summary', { hasText: /^Temporary source errors$/ }).click()
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  const guard = page.getByRole('alertdialog')
  await expect(guard).toContainText('temporary source errors')
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await openDisclosure(page, 'Temporary source errors')
  await expect(delay).toHaveValue('4')
})

test('optional sections start closed at the same level and maintenance bookmarks work on mobile', async ({
  page,
}) => {
  test.setTimeout(120_000)
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    for (const route of [
      '/metadata/videos/settings',
      '/metadata/books/settings',
      '/metadata/identification/settings',
      '/debrid/settings',
      '/health/settings',
      '/security/settings',
      '/network/incoming',
      '/network/outgoing',
    ]) {
      await page.goto(route)
      await expect(page.locator('main summary', { hasText: /^Advanced$/ })).toHaveCount(0)
      await expect(page.locator('main details').first()).toBeAttached()
      await expect(page.locator('main details[open]')).toHaveCount(0)
      if (route.startsWith('/metadata/') || route === '/debrid/settings') {
        const manual = page.locator('details').filter({
          has: page.locator(':scope > summary', { hasText: /^Manual actions$/ }),
        })
        await expect(manual.locator('xpath=ancestor::details')).toHaveCount(0)
        await openDisclosure(page, 'Manual actions')
        await expect(manual.getByRole('button').first()).toBeVisible()
        await expectControlsWithinGroups(manual)
      }
      if (
        process.env.QA_DIRECTORY &&
        ['/metadata/videos/settings', '/debrid/settings'].includes(route)
      ) {
        await page.evaluate(() => window.scrollTo(0, 0))
        await page.screenshot({
          path: join(process.env.QA_DIRECTORY, `advanced-${route.split('/')[1]}-${width}.png`),
          fullPage: true,
          animations: 'disabled',
        })
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
    for (const route of [
      '/metadata/videos/settings',
      '/metadata/books/settings',
      '/metadata/identification/settings',
      '/debrid/settings',
      '/health/settings',
    ]) {
      await page.goto(`${route}#maintenance`)
      const maintenance = page.locator('details#maintenance')
      await expect(maintenance).toHaveAttribute('open', '')
      await expect(maintenance.getByRole('button').first()).toBeVisible()
      const bounds = await maintenance.locator(':scope > summary').boundingBox()
      const header = await page.locator('[data-app-shell] header').boundingBox()
      if (header && bounds) expect(bounds.y).toBeGreaterThanOrEqual(header.y + header.height)
    }
  }
  expect(writes).toEqual([])
})

test('search pages start with closed optional panels and no implicit result restrictions', async ({
  page,
}) => {
  const statuses: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname === '/api/aiosource/matches')
      statuses.push(url.searchParams.get('status') ?? '')
  })
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    for (const route of ['/search', '/torrents', '/metadata/identification']) {
      await page.goto(route)
      await expect(page.locator('main details[open]')).toHaveCount(0)
      await expect(page.locator('main summary', { hasText: /^Filters$/ })).toBeVisible()
      if (route === '/search') {
        await expect(page.locator('#catalog-type')).toHaveText('All media')
        await expect(page.locator('#catalog-year')).toHaveValue('')
      } else if (route === '/torrents') {
        await expect(
          page
            .getByRole('form', { name: 'Files', exact: true })
            .getByRole('button', { name: 'Sources', exact: true }),
        ).toBeHidden()
        await expect(page.getByLabel('Minimum size (bytes)', { exact: true })).toHaveValue('')
        await expect(page.getByLabel('Maximum size (bytes)', { exact: true })).toHaveValue('')
      } else {
        await expect(page.locator('#matching-status')).toHaveText('All results')
        await expect(page.locator('#matching-source')).toHaveText('All sources')
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
  }
  expect(statuses.length).toBeGreaterThan(0)
  expect(statuses.every((status) => status === '')).toBe(true)
  await page.goto('/metadata/identification?match_status=unmatched&match_provider=example')
  await expect(page.locator('#matching-status')).toHaveText('Not identified')
  await expect(page.locator('#matching-source')).toHaveText('example')
  await expect(page.locator('main details[open]')).toHaveCount(0)
  await openDisclosure(page, 'Filters')
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(page.locator('#matching-status')).toHaveText('All results')
  await expect(page.locator('#matching-source')).toHaveText('All sources')
})

test('nested source options stay organized and retain their exact configuration when folded', async ({
  page,
  server,
}) => {
  const source = {
    version: 1,
    id: 'advanced-example',
    name: 'Example archive',
    adapter: 'json',
    url: server.sourceURL,
    enabled: false,
    http: { items_path: '/items' },
    mapping: { id: '/id', fields: { title: '/title' } },
    pagination: { type: 'none' },
    traversal: {
      window_pages: 10,
      total_paths: ['/meta/total'],
      scopes: [{ id: 'all', match: { category: 1 } }],
    },
  }
  await page.goto('/providers/new')
  await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
  await page.getByLabel('Import a source JSON definition', { exact: true }).setInputFiles({
    name: 'synthetic-advanced.json',
    mimeType: 'application/json',
    buffer: Buffer.from(JSON.stringify(source)),
  })
  await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await page.getByRole('tab', { name: 'Imports', exact: true }).click()
  await expect(page.getByLabel('Request interval', { exact: true })).toBeVisible()
  await openDisclosure(page, 'Bounded traversal')
  await expect(page.getByLabel('Window pages', { exact: true })).toBeHidden()
  await openDisclosure(page, 'Coverage & ordering')
  await page.getByLabel('Window pages', { exact: true }).fill('12')
  await page
    .locator('summary')
    .filter({ hasText: /^Bounded traversal\s*Configured$/ })
    .click()
  await expect(page.getByLabel('Window pages', { exact: true })).toBeHidden()
  await openDisclosure(page, 'Coverage & ordering')
  await expect(page.getByLabel('Window pages', { exact: true })).toHaveValue('12')
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    await expectControlsWithinGroups(page.locator('[data-settings-editor="source"]'))
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    if (process.env.QA_DIRECTORY)
      await page.screenshot({
        path: join(process.env.QA_DIRECTORY, `advanced-traversal-${width}.png`),
        fullPage: true,
        animations: 'disabled',
      })
  }
  await page.getByRole('button', { name: 'JSON', exact: true }).click()
  const downloading = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export JSON', exact: true }).click()
  const download = await downloading
  try {
    const stream = await download.createReadStream()
    const chunks: Buffer[] = []
    for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
    expect(JSON.parse(Buffer.concat(chunks).toString())).toEqual({
      ...source,
      traversal: { ...source.traversal, window_pages: 12 },
    })
  } finally {
    await download.delete()
  }
  expect(server.sourceRequests()).toBe(0)
})

test('a missing advanced HTML field opens its setup group without changing the source', async ({
  page,
  server,
}) => {
  await page.goto('/providers/new')
  const template = page.getByRole('row').filter({ has: page.getByText('HTML', { exact: true }) })
  await template.getByRole('button', { name: 'Use this template', exact: true }).click()
  await page.getByLabel('Display name', { exact: true }).fill('Example HTML archive')
  await page.getByLabel('Source identifier', { exact: true }).fill('advanced-html')
  await page.getByRole('tab', { name: 'Connection', exact: true }).click()
  await page.getByLabel('Source URL', { exact: true }).fill(server.sourceURL)
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page).toHaveURL(/\/providers\/advanced-html$/)
  const original = await (await page.request.get('/api/providers/advanced-html')).json()
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  await page.goto('/providers/advanced-html#setting=source.detail-container')
  const detail = page.locator('[data-settings-section="Detail pages"]')
  await expect(detail).toHaveAttribute('open', '')
  await expect(detail.locator(':scope > summary')).toBeFocused()
  await expect(
    detail.getByRole('button', { name: 'Configure detail pages', exact: true }),
  ).toBeVisible()
  expect((await (await page.request.get('/api/providers/advanced-html')).json()).json).toBe(
    original.json,
  )
  expect(writes).toEqual([])
  expect(server.sourceRequests()).toBe(0)
})
