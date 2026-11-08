import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

async function configuration(page: import('@playwright/test').Page) {
  return (await (await page.request.get('/api/aiosource/controls')).json()).config
}

test('main and optional information settings save independently even while other drafts are folded', async ({
  page,
}) => {
  await page.goto('/metadata/videos/settings')
  const original = await configuration(page)
  const main = page.getByRole('form', { name: 'Videos configuration', exact: true })
  const updates = page.getByRole('form', { name: 'Videos automatic updates', exact: true })
  const requests = page.getByRole('form', { name: 'Videos request limits', exact: true })
  await main.getByRole('switch').uncheck()
  await openDisclosure(page, 'Automatic updates')
  await updates.getByLabel('Information update interval (minutes)', { exact: true }).fill('45')
  await openDisclosure(page, 'Request limits')
  await requests.getByLabel('TMDB requests per second', { exact: true }).fill('20')
  await page.locator('summary', { hasText: /^Request limits$/ }).click()
  await main.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(main.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await configuration(page)).toMatchObject({
    auto_sync: false,
    sync_minutes: original.sync_minutes,
    tmdb_requests_per_second: original.tmdb_requests_per_second,
  })
  await expect(
    updates.getByLabel('Information update interval (minutes)', { exact: true }),
  ).toHaveValue('45')
  await updates.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(updates.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await configuration(page)).toMatchObject({
    auto_sync: false,
    sync_minutes: 45,
    tmdb_requests_per_second: original.tmdb_requests_per_second,
  })
  await openDisclosure(page, 'Request limits')
  await expect(requests.getByLabel('TMDB requests per second', { exact: true })).toHaveValue('20')
  await requests.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(requests.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await configuration(page)).toMatchObject({
    auto_sync: false,
    sync_minutes: 45,
    tmdb_requests_per_second: 20,
  })
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('book catalog filters and the main automation switch never publish one another’s draft', async ({
  page,
}) => {
  await page.goto('/metadata/books/settings')
  const original = await configuration(page)
  const main = page.getByRole('form', { name: 'Books configuration', exact: true })
  const filters = page.getByRole('form', { name: 'Books catalog filters', exact: true })
  await main.getByRole('switch').setChecked(!original.openlibrary_enabled)
  await openDisclosure(page, 'Catalog filters')
  await filters.getByRole('switch', { name: 'Relevant editions only', exact: true }).uncheck()
  await filters.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(filters.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await configuration(page)).toMatchObject({
    openlibrary_enabled: original.openlibrary_enabled,
    openlibrary_relevant_only: false,
  })
  await expect(main.getByRole('switch')).toBeChecked({ checked: !original.openlibrary_enabled })
  // Restore the initial switch value without triggering a real external import.
  await main.getByRole('switch').setChecked(Boolean(original.openlibrary_enabled))
  await expect(main.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
})

test('import settings retain folded drafts and scope validation and saves to each block', async ({
  page,
}) => {
  await openDisclosure(page, 'Import execution')
  await openDisclosure(page, 'Concurrency')
  await openDisclosure(page, 'Quota handling')
  await openDisclosure(page, 'Recovery')
  const concurrency = page.getByRole('form', { name: 'Concurrency', exact: true })
  const quota = page.getByRole('form', { name: 'Quota handling', exact: true })
  const recovery = page.getByRole('form', { name: 'Recovery', exact: true })
  const read = async () =>
    (await (await page.request.get('/api/settings/collections')).json()).settings
  const original = await read()
  await concurrency.getByLabel('Simultaneous imports', { exact: true }).fill('7')
  await quota.getByLabel('Automatic retries after HTTP 429', { exact: true }).fill('11')
  await recovery.getByRole('switch').uncheck()
  await expect(quota.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.locator('summary', { hasText: /^Quota handling$/ }).click()
  await concurrency.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(concurrency.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await read()).toMatchObject({
    workers: 7,
    max_quota_retries: original.max_quota_retries,
    auto_resume_interrupted: true,
  })
  await recovery.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(recovery.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await read()).toMatchObject({
    workers: 7,
    max_quota_retries: original.max_quota_retries,
    auto_resume_interrupted: false,
  })
  await openDisclosure(page, 'Quota handling')
  await expect(quota.getByLabel('Automatic retries after HTTP 429', { exact: true })).toHaveValue(
    '11',
  )
  await quota.getByLabel('Automatic retries after HTTP 429', { exact: true }).fill('4')
  await quota.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(quota.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await read()).toMatchObject({
    workers: 7,
    max_quota_retries: 4,
    auto_resume_interrupted: false,
  })
})

test('contextual help supports hover and keyboard without opening its settings block or submitting forms', async ({
  page,
}) => {
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  await page.goto('/debrid/settings')
  const playback = page
    .locator('details')
    .filter({ has: page.locator(':scope > summary', { hasText: /^Private source playback$/ }) })
  const help = playback.getByRole('button', { name: 'About Private source playback', exact: true })
  await help.hover()
  const popup = page.getByRole('dialog', { name: 'About Private source playback', exact: true })
  await expect(popup).toContainText('complete announce URL')
  await expect(popup).toHaveCSS('pointer-events', 'none')
  await expect(playback).not.toHaveAttribute('open', '')
  await help.click()
  await expect(popup).toBeVisible()
  await expect(popup).toHaveCSS('pointer-events', 'auto')
  await page.keyboard.press('Escape')
  await expect(popup).toHaveCount(0)
  await help.focus()
  await page.keyboard.press('Enter')
  await expect(popup).toBeVisible()
  await expect(popup).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(popup.getByRole('link', { name: 'Secrets', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(help).toBeFocused()
  await expect(playback).not.toHaveAttribute('open', '')
  expect(writes).toEqual([])
})

test('contextual help opens on a real touch screen and its links never toggle the surrounding disclosure', async ({
  page,
  browser,
  server,
}) => {
  const context = await browser.newContext({
    storageState: await page.context().storageState(),
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
  })
  try {
    const mobile = await context.newPage()
    await mobile.goto(server.origin + '/debrid/settings')
    const playback = mobile
      .locator('details')
      .filter({ has: mobile.locator(':scope > summary', { hasText: /^Private source playback$/ }) })
    const help = playback.getByRole('button', {
      name: 'About Private source playback',
      exact: true,
    })
    await help.tap()
    const popup = mobile.getByRole('dialog', { name: 'About Private source playback', exact: true })
    await expect(popup).toBeVisible()
    await expect(playback).not.toHaveAttribute('open', '')
    const bounds = await popup.boundingBox()
    expect(bounds!.x).toBeGreaterThanOrEqual(8)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390)
    await popup.getByRole('link', { name: 'Secrets', exact: true }).tap()
    await expect(mobile).toHaveURL(/\/security\/secrets$/)
    await expect(
      mobile.getByRole('heading', { name: 'Secrets', exact: true, level: 1 }),
    ).toBeVisible()
  } finally {
    await context.close()
  }
})

test('hover help inside a modal never intercepts its save action', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/network/outgoing')
  await page.getByRole('button', { name: 'Add proxy', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
  await editor.getByLabel('Name', { exact: true }).fill('Example gateway')
  await editor.getByLabel('Proxy URL', { exact: true }).fill('socks5://example.invalid:40000')
  await editor.getByRole('button', { name: 'About Authentication', exact: true }).hover()
  const help = page.getByRole('dialog', { name: 'About Authentication', exact: true })
  await expect(help).toBeVisible()
  await expect(help).toHaveCSS('pointer-events', 'none')
  await editor.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(editor).toBeHidden()
  await expect(
    page.getByRole('region', { name: 'Proxy Example gateway', exact: true }),
  ).toBeVisible()
})

test('a hover preview lets controls underneath receive their clicks outside a modal too', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/debrid/settings')
  const playback = page
    .locator('details')
    .filter({ has: page.locator(':scope > summary', { hasText: /^Private source playback$/ }) })
  const capacity = page
    .locator('details')
    .filter({ has: page.locator(':scope > summary', { hasText: /^Operation capacity$/ }) })
  await playback.getByRole('button', { name: 'About Private source playback', exact: true }).hover()
  await expect(
    page.getByRole('dialog', { name: 'About Private source playback', exact: true }),
  ).toBeVisible()
  await capacity.locator(':scope > summary > span > span').click({ position: { x: 20, y: 10 } })
  await expect(capacity).toHaveAttribute('open', '')
})

test('book language menus close when an update starts and stay closed when editing becomes available again', async ({
  page,
}) => {
  let busy = false
  await page.route('**/api/aiosource/controls', async (route) => {
    const response = await route.fetch()
    const data = await response.json()
    await route.fulfill({ response, json: { ...data, books: { ...data.books, running: busy } } })
  })
  await page.goto('/metadata/books/settings')
  await openDisclosure(page, 'Catalog filters')
  await page.getByRole('switch', { name: 'Filter book languages', exact: true }).check()
  const selection = page.getByRole('button', { name: 'Edition languages', exact: true })
  await selection.click()
  const menu = page.getByRole('menu', { name: 'Edition languages', exact: true })
  await expect(menu).toBeVisible()
  busy = true
  await expect(selection).toBeDisabled()
  await expect(menu).toHaveCount(0)
  busy = false
  await expect(selection).toBeEnabled()
  await expect(menu).toHaveCount(0)
  await expect(selection).toContainText('Browser language (English)')
  await selection.click()
  await expect(menu).toBeVisible()
  await page.keyboard.press('Escape')
})
