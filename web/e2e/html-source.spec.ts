import { join } from 'node:path'
import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'

async function newHTMLSource(page: Page, sourceURL: string, enabled = false) {
  await page.goto('/providers/new')
  const template = page.getByRole('row').filter({ has: page.getByText('HTML', { exact: true }) })
  await template.getByRole('button', { name: 'Use this template', exact: true }).click()
  await expect(page.getByLabel('Source type', { exact: true })).toHaveValue('html')
  await page.getByLabel('Display name', { exact: true }).fill('Example HTML archive')
  await page.getByLabel('Source identifier', { exact: true }).fill('html_example')
  if (enabled) await page.getByRole('switch', { name: 'Source enabled', exact: true }).check()
  else
    await expect(
      page.getByRole('switch', { name: 'Source enabled', exact: true }),
    ).not.toBeChecked()
  await page.getByRole('tab', { name: 'Connection', exact: true }).click()
  await page.getByLabel('Source URL', { exact: true }).fill(sourceURL)
  await expect(page.getByLabel('Authentication type', { exact: true })).toHaveValue('none')
  await page.getByRole('tab', { name: 'Source fields', exact: true }).click()
  await page.getByLabel('Record selector', { exact: true }).fill('article[data-id]')
}

async function checkConfiguration(page: Page) {
  const response = page.waitForResponse(
    (value) =>
      value.request().method() === 'POST' &&
      new URL(value.url()).pathname === '/api/providers/validate',
  )
  await page.getByRole('button', { name: 'Check configuration', exact: true }).click()
  return (await (await response).json()) as { valid: boolean; issues: string[] }
}

async function saveHTMLSource(page: Page) {
  const response = page.waitForResponse(
    (value) =>
      value.request().method() === 'PUT' &&
      new URL(value.url()).pathname === '/api/providers/html_example',
  )
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  expect((await response).status()).toBe(200)
  await expect(page).toHaveURL(/\/providers\/html_example$/)
}

test('HTML uses the source stages, validates selectors without requests and keeps a mobile layout', async ({
  page,
  server,
}) => {
  await newHTMLSource(page, server.sourceURL)
  await page.getByLabel('Title selector', { exact: true }).fill('[')
  const invalid = await checkConfiguration(page)
  expect(invalid.valid).toBe(false)
  expect(invalid.issues.join(' ')).toContain('HTML selector')
  await page.getByLabel('Title selector', { exact: true }).fill('.title')
  await page.locator('summary').filter({ hasText: 'Detail pages' }).click()
  await page.getByRole('button', { name: 'Configure detail pages', exact: true }).click()
  await expect(page.getByLabel('Detail container selector', { exact: true })).toHaveValue(
    '#item-details',
  )
  await expect(page.getByLabel('Detail Info hash format', { exact: true })).toHaveValue('info_hash')
  expect((await checkConfiguration(page)).valid).toBe(true)
  expect(server.sourceRequests()).toBe(0)
  await saveHTMLSource(page)
  const saved = await (await page.request.get('/api/providers/html_example')).json()
  expect(JSON.parse(saved.json).html.details.fields.info_hash.required).toBe(true)
  await page.reload()
  for (const width of [320, 390, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    for (const stage of [
      'Identity',
      'Connection',
      'Pagination',
      'Source fields',
      'Imports',
      'Schedules',
    ]) {
      await page.getByRole('tab', { name: stage, exact: true }).click()
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        `${stage} at ${width}px`,
      ).toBe(true)
    }
  }
  await page.getByRole('tab', { name: 'Source fields', exact: true }).click()
  if (process.env.QA_DIRECTORY) {
    await page.setViewportSize({ width: 390, height: 1000 })
    await page.screenshot({
      path: join(process.env.QA_DIRECTORY, 'html-fields-mobile.png'),
      fullPage: true,
    })
    await page.setViewportSize({ width: 1440, height: 1100 })
    await page.screenshot({
      path: join(process.env.QA_DIRECTORY, 'html-fields-desktop.png'),
      fullPage: true,
    })
  }
  await page.getByRole('tab', { name: 'Identity', exact: true }).click()
  await page.getByLabel('Source type', { exact: true }).selectOption('http_json')
  await page
    .getByRole('button', { name: 'Clear incompatible source settings', exact: true })
    .click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Clear settings', exact: true })
    .click()
  expect((await checkConfiguration(page)).valid).toBe(true)
  const unchanged = await (await page.request.get('/api/providers/html_example')).json()
  expect(JSON.parse(unchanged.json).adapter).toBe('html')
  expect(JSON.parse(unchanged.json).html.details).toBeDefined()
})

test.describe('HTML collection', () => {
  test.use({ collectionSource: true })

  test('Preview and Rebuild import actual website details through the existing run dialog and history', async ({
    page,
    server,
  }) => {
    await newHTMLSource(page, server.sourceURL + '/html', true)
    await page.locator('summary').filter({ hasText: 'Detail pages' }).click()
    await page.getByRole('button', { name: 'Configure detail pages', exact: true }).click()
    await page.getByLabel('Detail Info hash selector', { exact: true }).fill('.hash')
    await expect(page.getByLabel('Detail Info hash attribute', { exact: true })).toHaveValue('href')
    await page.getByLabel('Detail Info hash attribute', { exact: true }).fill('')
    await page.getByRole('tab', { name: 'Imports', exact: true }).click()
    await page.getByLabel('Request interval', { exact: true }).selectOption('500ms')
    expect((await checkConfiguration(page)).valid).toBe(true)
    expect(server.sourceRequests()).toBe(0)
    await saveHTMLSource(page)
    for (const mode of ['preview', 'full']) {
      if (mode === 'full') await page.goto('/providers/html_example')
      await page.getByRole('button', { name: 'Run manual import', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: 'Run manual import', exact: true })
      await dialog.getByLabel('Action', { exact: true }).selectOption(mode)
      const response = page.waitForResponse(
        (value) =>
          value.request().method() === 'POST' && new URL(value.url()).pathname === '/api/runs',
      )
      await dialog.getByRole('button', { name: 'Run manual import', exact: true }).click()
      const run = await (await response).json()
      await expect(page).toHaveURL(new RegExp(`/runs/${run.id}$`))
      await expect
        .poll(async () => {
          const current = await (await page.request.get(`/api/runs/${run.id}`)).json()
          return { status: current.status, error: current.error || '' }
        })
        .toEqual({ status: 'succeeded', error: '' })
      const records = await (await page.request.get('/api/torrents?provider=html_example')).json()
      expect(records.total).toBe(mode === 'preview' ? 0 : 2)
      if (mode === 'full') {
        expect(records.items.map((row: { source_id: string }) => row.source_id).sort()).toEqual([
          'one',
          'two',
        ])
        for (const row of records.items) {
          expect(row.fields.size).toBe(1024)
          expect(row.fields.info_hash).toBe((row.source_id === 'one' ? 'a' : 'b').repeat(40))
        }
      }
      await expect(page.getByRole('tab', { name: 'Processed records', exact: true })).toBeVisible()
    }
    expect(server.sourceRequests()).toBe(8)
  })
})
