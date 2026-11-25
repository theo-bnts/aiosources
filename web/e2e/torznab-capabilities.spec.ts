import { createServer as createHTTPServer, type Server } from 'node:http'
import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { closeListener, listen } from './server'

async function startMockTorznabServer(expectedKey: string): Promise<{
  origin: string
  capsRequests: () => number
  close: () => Promise<void>
}> {
  let requests = 0
  const httpServer: Server = createHTTPServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://127.0.0.1')
    if (url.pathname === '/') {
      response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
      response.end('<html><body>Mock indexer landing page</body></html>')
      return
    }
    if (url.pathname !== '/api' || url.searchParams.get('t') !== 'caps') {
      response.writeHead(404)
      response.end()
      return
    }
    requests += 1
    if (expectedKey && url.searchParams.get('apikey') !== expectedKey) {
      response.writeHead(200, { 'Content-Type': 'application/xml; charset=utf-8' })
      response.end(
        `<error code="100" description="Invalid apikey ${url.searchParams.get('apikey') ?? ''}"/>`,
      )
      return
    }
    response.writeHead(200, {
      'Content-Type': 'application/xml; charset=utf-8',
      'X-RateLimit-Limit': '1000',
      'X-RateLimit-Remaining': '995',
    })
    response.end(`<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="Cinema Archive"/>
  <limits default="50" max="200"/>
  <searching>
    <search available="yes" supportedParams="q,cat"/>
    <tv-search available="yes" supportedParams="q,tvdbid,season,ep"/>
    <movie-search available="no" supportedParams="imdbid"/>
    <audio-search available="yes" supportedParams="artist,album"/>
    <book-search available="no" supportedParams=""/>
  </searching>
  <categories>
    <category id="2000" name="Movies">
      <subcat id="2040" name="HD"/>
      <subcat id="2045" name="UHD"/>
    </category>
    <category id="5000" name="TV">
      <subcat id="5040" name="HD"/>
      <subcat id="5070" name="Anime"/>
    </category>
    <category id="3000" name="Audio">
      <subcat id="3040" name="Lossless"/>
    </category>
  </categories>
</caps>`)
  })
  const port = await listen(httpServer)
  return {
    origin: `http://127.0.0.1:${port}`,
    capsRequests: () => requests,
    close: () => closeListener(httpServer),
  }
}

async function addCredentialSecret(page: Page, value: string) {
  await page.getByRole('button', { name: 'Add secret', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Add secret', exact: true })
  await dialog.getByLabel('Credential value', { exact: true }).fill(value)
  await dialog.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(dialog).toBeHidden()
}

test('new Torznab source fills fields from cached capabilities and uses consistent mode tabs and JSON buttons', async ({
  page,
}) => {
  const apiKey = 'torznab-e2e-secret-key'
  const mock = await startMockTorznabServer(apiKey)
  try {
    await page.setViewportSize({ width: 1440, height: 1000 })
    await page.goto('/providers/new')
    const template = page
      .getByRole('row')
      .filter({ has: page.getByText('torznab', { exact: true }) })
    await template.getByRole('button', { name: 'Use this template', exact: true }).click()

    // Editor mode switcher uses the same tab bar style as the rest of the app.
    const modeBar = page.getByRole('group', { name: 'Definition editor mode', exact: true })
    await expect(modeBar).toHaveAttribute('data-slot', 'tabs-list')
    await expect(page.getByRole('button', { name: 'Visual stages', exact: true })).toHaveAttribute(
      'data-slot',
      'tabs-trigger',
    )
    // No global summary menu or t=caps jargon.
    await expect(page.locator('main')).not.toContainText('t=caps')

    // JSON mode buttons match Check configuration (outline variant).
    await page.getByRole('button', { name: 'JSON', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Import JSON', exact: true })).toHaveAttribute(
      'data-variant',
      'outline',
    )
    await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toHaveAttribute(
      'data-variant',
      'outline',
    )
    if (process.env.QA_DIRECTORY) {
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/torznab-json-mode-1440.png`,
        fullPage: true,
        animations: 'disabled',
      })
    }
    await page.getByRole('button', { name: 'Visual stages', exact: true }).click()

    // Enter root URL (without /api) and add secret in Connection.
    await page.getByRole('tab', { name: 'Connection', exact: true }).click()
    await page.getByLabel('Source URL', { exact: true }).fill(mock.origin)
    await addCredentialSecret(page, apiKey)

    // First capability click fetches once and updates Source URL to canonical /api.
    await page
      .getByRole('button', { name: 'Get source url from capabilities', exact: true })
      .click()
    await expect(page.getByLabel('Source URL', { exact: true })).toHaveValue(`${mock.origin}/api`)
    expect(mock.capsRequests()).toBe(1)

    // Categories are already available from cache without a second request.
    const categoriesGroup = page.getByRole('group', {
      name: 'Torznab advertised categories',
      exact: true,
    })
    await expect(categoriesGroup).toBeVisible()
    const categoryFilter = page.getByLabel('Categories', { exact: true })
    await categoryFilter.fill('anime')
    await categoriesGroup.getByRole('checkbox', { name: /Anime/ }).check()
    await categoryFilter.fill('uhd')
    await categoriesGroup.getByRole('checkbox', { name: /UHD/ }).check()
    await categoryFilter.fill('')

    // Search mode dropdown annotates unavailable modes cleanly without extra banners.
    await expect(
      page.getByLabel('Search mode', { exact: true }).locator('option[value="movie"]'),
    ).toHaveText('Films (unavailable)')

    if (process.env.QA_DIRECTORY) {
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/torznab-connection-1440.png`,
        fullPage: true,
        animations: 'disabled',
      })
    }

    // Identity fields reuse the cached capabilities with zero extra server requests.
    await page.getByRole('tab', { name: 'Identity', exact: true }).click()
    await page
      .getByRole('button', { name: 'Get display name from capabilities', exact: true })
      .click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('Cinema Archive')
    await page
      .getByRole('button', { name: 'Get source identifier from capabilities', exact: true })
      .click()
    await expect(page.getByLabel('Source identifier', { exact: true })).toHaveValue(
      'cinema_archive',
    )

    if (process.env.QA_DIRECTORY) {
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/torznab-identity-1440.png`,
        fullPage: true,
        animations: 'disabled',
      })
    }

    // Pagination page size reuses the same cache.
    await page.getByRole('tab', { name: 'Pagination', exact: true }).click()
    await page.getByRole('button', { name: 'Get page size from capabilities', exact: true }).click()
    await expect(page.getByLabel('Page size', { exact: true })).toHaveValue('200')
    expect(mock.capsRequests()).toBe(1)

    // Save and verify persisted JSON.
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page).toHaveURL(/\/providers\/cinema_archive$/)
    const saved = await (await page.request.get('/api/providers/cinema_archive')).json()
    const definition = JSON.parse(saved.json)
    expect(definition.name).toBe('Cinema Archive')
    expect(definition.id).toBe('cinema_archive')
    expect(definition.url).toBe(`${mock.origin}/api`)
    expect(definition.page_size).toBe(200)
    expect(definition.search.categories).toEqual([2045, 5070])
  } finally {
    await mock.close()
  }
})

test('existing Torznab source loads categories cleanly and overrides individual fields on click', async ({
  page,
}) => {
  const apiKey = 'existing-source-key'
  const mock = await startMockTorznabServer(apiKey)
  try {
    const session = await (await page.request.get('/api/session')).json()
    const putSecret = await page.request.put('/api/secrets/existing_torznab_key', {
      headers: { 'X-CSRF-Token': session.csrf_token },
      data: { value: apiKey, revision: 0 },
    })
    expect(putSecret.status()).toBe(204)

    const existingJSON = JSON.stringify(
      {
        version: 1,
        id: 'my_existing_tracker',
        name: 'My Custom Cinema',
        adapter: 'torznab',
        url: `${mock.origin}/api`,
        enabled: false,
        auth: { type: 'query', name: 'apikey', secret_ref: 'existing_torznab_key' },
        request_interval: '1s',
        rate_limit_reset: 'epoch',
        page_size: 50,
        search: { categories: [99999] },
        options: { category_scope: 'combined', search_mode: 'movie' },
      },
      null,
      2,
    )
    const putSource = await page.request.put('/api/providers/my_existing_tracker', {
      headers: { 'X-CSRF-Token': session.csrf_token },
      data: { json: existingJSON, revision: '' },
    })
    expect(putSource.status()).toBe(200)

    await page.setViewportSize({ width: 1440, height: 1000 })
    await page.goto('/providers/my_existing_tracker')
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('My Custom Cinema')
    // Existing source identifier cannot change, so no capability button is shown for it.
    await expect(
      page.getByRole('button', { name: 'Get source identifier from capabilities', exact: true }),
    ).toHaveCount(0)

    // Load categories directly from Connection -> Search scope without touching other fields.
    await page.getByRole('tab', { name: 'Connection', exact: true }).click()
    await page.getByRole('button', { name: 'Load from capabilities', exact: true }).click()

    // Unlisted custom category ID 99999 is preserved.
    await expect(page.getByText('Unlisted category IDs kept in this draft:')).toBeVisible()
    expect(mock.capsRequests()).toBe(1)
    await expect(
      page.getByRole('button', { name: 'Remove category 99999', exact: true }),
    ).toBeVisible()

    const categoriesGroup = page.getByRole('group', {
      name: 'Torznab advertised categories',
      exact: true,
    })
    await categoriesGroup.getByRole('checkbox', { name: /Lossless/ }).check()

    // Clicking the per-field button on Search mode directly overrides unavailable 'movie' with 'search' from cache.
    await page
      .getByRole('button', { name: 'Get search mode from capabilities', exact: true })
      .click()
    await expect(page.getByLabel('Search mode', { exact: true })).toHaveValue('search')

    // Other fields remain untouched until the user clicks their buttons.
    await page.getByRole('tab', { name: 'Pagination', exact: true }).click()
    await expect(page.getByLabel('Page size', { exact: true })).toHaveValue('50')
    await page.getByRole('button', { name: 'Get page size from capabilities', exact: true }).click()
    await expect(page.getByLabel('Page size', { exact: true })).toHaveValue('200')

    await page.getByRole('tab', { name: 'Identity', exact: true }).click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('My Custom Cinema')
    await page
      .getByRole('button', { name: 'Get display name from capabilities', exact: true })
      .click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('Cinema Archive')
    await expect(page.getByLabel('Source identifier', { exact: true })).toHaveValue(
      'my_existing_tracker',
    )
    // Entire flow used only 1 capability request.
    expect(mock.capsRequests()).toBe(1)

    await page.getByRole('tab', { name: 'Connection', exact: true }).click()
    if (process.env.QA_DIRECTORY) {
      await page.setViewportSize({ width: 390, height: 900 })
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/torznab-connection-mobile-390.png`,
        fullPage: true,
        animations: 'disabled',
      })
    }

    // Zero horizontal overflow and consistent control sizing on mobile and desktop.
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 950 })
      const issues = await page.evaluate(() => {
        const expected = innerWidth < 768 ? 44 : 40
        const problems: string[] = []
        const selectors = [
          '[data-slot="input"]',
          '[data-slot="button"]',
          '[data-slot="disclosure-trigger"]',
          '[data-slot="tabs-list"]',
          '.native-select',
        ]
        for (const element of document.querySelectorAll<HTMLElement>(selectors.join(','))) {
          const box = element.getBoundingClientRect()
          if (!box.width || !box.height || getComputedStyle(element).visibility === 'hidden')
            continue
          const slot = element.dataset.slot
          const expectedHeight =
            slot === 'disclosure-trigger' && element.parentElement?.dataset.variant === 'card'
              ? expected - 2
              : expected
          if (Math.abs(box.height - expectedHeight) > 0.5)
            problems.push(`${element.textContent?.trim() || slot}: height ${box.height}`)
        }
        if (document.documentElement.scrollWidth > innerWidth) problems.push('horizontal overflow')
        return problems
      })
      expect(issues).toEqual([])
    }

    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    const updated = await (await page.request.get('/api/providers/my_existing_tracker')).json()
    const updatedDef = JSON.parse(updated.json)
    expect(updatedDef.id).toBe('my_existing_tracker')
    expect(updatedDef.name).toBe('Cinema Archive')
    expect(updatedDef.page_size).toBe(200)
    expect(updatedDef.options.search_mode).toBe('search')
    expect(updatedDef.search.categories).toEqual([3040, 99999])
  } finally {
    await mock.close()
  }
})

test('Torznab capability fetch surfaces safe inline errors without leaking secrets', async ({
  page,
}) => {
  const wrongSecret = 'secret-must-never-leak-into-ui-12345'
  const mock = await startMockTorznabServer('expected-different-key')
  try {
    await page.goto('/providers/new')
    const template = page
      .getByRole('row')
      .filter({ has: page.getByText('torznab', { exact: true }) })
    await template.getByRole('button', { name: 'Use this template', exact: true }).click()
    await page.getByRole('tab', { name: 'Connection', exact: true }).click()
    await page.getByLabel('Source URL', { exact: true }).fill(`${mock.origin}/api`)

    // Template's placeholder secret_ref does not exist -> actionable missing-secret error.
    await page.getByRole('button', { name: 'Load from capabilities', exact: true }).click()
    const alert = page.getByRole('alert')
    await expect(alert).toBeVisible()
    await expect(alert).toContainText('credential secret was not found in Secrets')
    await expect(alert).not.toContainText('t=caps')

    // Store a wrong API key and retry -> auth rejected without echoing secret value.
    await addCredentialSecret(page, wrongSecret)
    await page.getByRole('button', { name: 'Load from capabilities', exact: true }).click()
    await expect(alert).toContainText('rejected the configured credentials')
    await expect(page.locator('body')).not.toContainText(wrongSecret)
  } finally {
    await mock.close()
  }
})
