import { createServer as createHTTPServer, type Server } from 'node:http'
import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { closeListener, listen } from './server'

async function startMockTorznabServer(expectedKey: string): Promise<{
  origin: string
  close: () => Promise<void>
}> {
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

test('new Torznab source discovers capabilities, fills defaults and picks categories by name', async ({
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

    await expect(
      page.getByText('Auto-configure from Torznab capabilities (t=caps)', { exact: true }),
    ).toBeVisible()
    await page.getByRole('button', { name: 'Open Connection', exact: true }).click()

    // Supply the root URL (without /api) and store a secret through SecretPicker.
    await page.getByLabel('Source URL', { exact: true }).fill(mock.origin)
    await addCredentialSecret(page, apiKey)

    await page.getByRole('button', { name: 'Fetch capabilities (t=caps)', exact: true }).click()

    const summary = page.getByRole('region', {
      name: 'Discovered Torznab capabilities',
      exact: true,
    })
    await expect(summary).toBeVisible()
    await expect(summary).toContainText('Cinema Archive')
    await expect(summary).toContainText('8 categories')
    await expect(summary).toContainText('Quota 995 / 1,000')

    // All 4 fields (name, id, url, page_size) are template defaults or canonicalized URL -> safe 1-click apply.
    if (process.env.QA_DIRECTORY) {
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/torznab-caps-review-1440.png`,
        fullPage: true,
        animations: 'disabled',
      })
    }
    await summary
      .getByRole('button', { name: 'Apply discovered settings (4)', exact: true })
      .click()
    await expect(summary).toContainText('All settings match')
    await expect(page.getByLabel('Source URL', { exact: true })).toHaveValue(`${mock.origin}/api`)

    // Filter and pick specific categories from the advertised tree.
    const categoryFilter = page.getByLabel('Advertised categories', { exact: true })
    await categoryFilter.fill('anime')
    const categoriesGroup = page.getByRole('group', {
      name: 'Torznab advertised categories',
      exact: true,
    })
    await categoriesGroup.getByRole('checkbox', { name: /Anime/ }).check()
    await categoryFilter.fill('uhd')
    await categoriesGroup.getByRole('checkbox', { name: /UHD/ }).check()
    await categoryFilter.fill('')

    if (process.env.QA_DIRECTORY) {
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/torznab-caps-applied-1440.png`,
        fullPage: true,
        animations: 'disabled',
      })
    }

    // Re-expanding the summary shows all fields matching.
    await summary.getByRole('button', { name: 'Show details', exact: true }).click()
    await expect(summary).toContainText('All discovered settings match this draft.')

    // Verify Identity & Pagination stages received the discovered values.
    await page.getByRole('tab', { name: 'Identity', exact: true }).click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('Cinema Archive')
    await expect(page.getByLabel('Source identifier', { exact: true })).toHaveValue(
      'cinema_archive',
    )

    await page.getByRole('tab', { name: 'Pagination', exact: true }).click()
    await expect(page.getByLabel('Page size', { exact: true })).toHaveValue('200')

    // Save the new source and verify persisted JSON.
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

test('existing Torznab source lets users pick categories safely and confirms before overwriting custom values', async ({
  page,
}) => {
  const apiKey = 'existing-source-key'
  const mock = await startMockTorznabServer(apiKey)
  try {
    // Seed an already-configured Torznab source with custom name, page_size, and an unlisted category ID.
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
        options: { category_scope: 'combined', search_mode: 'search' },
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

    // Go straight to Connection -> Search scope and load categories without modifying existing settings.
    await page.getByRole('tab', { name: 'Connection', exact: true }).click()
    await page.getByRole('button', { name: 'Fetch categories (t=caps)', exact: true }).click()

    const summary = page.getByRole('region', {
      name: 'Discovered Torznab capabilities',
      exact: true,
    })
    await expect(summary).toBeVisible()
    // Existing source ID is never proposed for change.
    await expect(summary).not.toContainText('Source identifier')
    // Existing unlisted category ID 99999 is preserved and displayed.
    await expect(page.getByText('Unlisted category IDs kept in this draft:')).toBeVisible()
    await expect(
      page.getByRole('button', { name: 'Remove category 99999', exact: true }),
    ).toBeVisible()

    // Select a category from the fetched list.
    const categoriesGroup = page.getByRole('group', {
      name: 'Torznab advertised categories',
      exact: true,
    })
    await categoriesGroup.getByRole('checkbox', { name: /Lossless/ }).check()

    // Confirm existing custom Display name and Page size were NOT overwritten.
    await page.getByRole('tab', { name: 'Identity', exact: true }).click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('My Custom Cinema')
    // Per-field inline apply button is available right beneath Display name.
    await expect(page.getByRole('button', { name: 'Apply name', exact: true })).toBeVisible()

    // Test per-field apply on Pagination stage.
    await page.getByRole('tab', { name: 'Pagination', exact: true }).click()
    await expect(page.getByLabel('Page size', { exact: true })).toHaveValue('50')
    await page.getByRole('button', { name: 'Use max (200)', exact: true }).click()
    await expect(page.getByLabel('Page size', { exact: true })).toHaveValue('200')

    // Display name still differs -> bulk overwrite requires explicit confirmation dialog listing what changes.
    await summary.getByRole('button', { name: 'Overwrite all (1)', exact: true }).click()
    const confirmDialog = page.getByRole('alertdialog')
    await expect(confirmDialog).toBeVisible()
    await expect(confirmDialog).toContainText('Overwrite existing source settings?')
    await expect(confirmDialog).toContainText('Display name')
    await expect(confirmDialog).toContainText('My Custom Cinema')
    await expect(confirmDialog).toContainText('Cinema Archive')

    // Cancelling preserves custom value.
    await confirmDialog.getByRole('button', { name: 'Back', exact: true }).click()
    await expect(confirmDialog).toBeHidden()
    await page.getByRole('tab', { name: 'Identity', exact: true }).click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('My Custom Cinema')

    // Confirming overwrites Display name while keeping source ID untouched.
    await summary.getByRole('button', { name: 'Overwrite all (1)', exact: true }).click()
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Overwrite settings', exact: true })
      .click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('Cinema Archive')
    await expect(page.getByLabel('Source identifier', { exact: true })).toHaveValue(
      'my_existing_tracker',
    )

    if (process.env.QA_DIRECTORY) {
      await page.getByRole('tab', { name: 'Connection', exact: true }).click()
      await page.setViewportSize({ width: 390, height: 900 })
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/torznab-caps-mobile-390.png`,
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

    // Save and confirm persisted categories (both newly selected + preserved unlisted ID) & updated settings.
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    const updated = await (await page.request.get('/api/providers/my_existing_tracker')).json()
    const updatedDef = JSON.parse(updated.json)
    expect(updatedDef.id).toBe('my_existing_tracker')
    expect(updatedDef.name).toBe('Cinema Archive')
    expect(updatedDef.page_size).toBe(200)
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
    await page.getByRole('button', { name: 'Fetch capabilities (t=caps)', exact: true }).click()
    const alert = page.getByRole('alert')
    await expect(alert).toBeVisible()
    await expect(alert).toContainText('credential secret was not found in Secrets')

    // Store a wrong API key and retry -> auth rejected without echoing secret value.
    await addCredentialSecret(page, wrongSecret)
    await page.getByRole('button', { name: 'Fetch capabilities (t=caps)', exact: true }).click()
    await expect(alert).toContainText('rejected the configured credentials')
    await expect(page.locator('body')).not.toContainText(wrongSecret)
  } finally {
    await mock.close()
  }
})
