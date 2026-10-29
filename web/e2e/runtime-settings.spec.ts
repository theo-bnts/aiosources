import { openDisclosure } from './disclosures'
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from './fixtures'

test('advanced settings stay grouped, persist, and reject contradictory values', async ({
  page,
}) => {
  await page.goto('/metadata/identification/settings')
  const advanced = page
    .locator('details')
    .filter({ has: page.locator('summary', { hasText: /^Advanced$/ }) })
  await advanced.locator(':scope > summary').click()
  const processing = advanced
    .locator('details')
    .filter({ has: page.locator('summary', { hasText: /^Identification processing$/ }) })
  await processing.locator('summary').click()
  const form = processing.getByRole('form', { name: 'Identification processing' })
  await form.getByLabel('Queued torrents per pass', { exact: true }).fill('10')
  await expect(form).toContainText('The saved batch cannot exceed the queued batch.')
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await form.getByLabel('Torrents per saved batch', { exact: true }).fill('5')
  await form.getByLabel('Maximum identification workers (0 = automatic)', { exact: true }).fill('2')
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(
    (await (await page.request.get('/api/settings/runtime/identification')).json()).values,
  ).toEqual({ workers: 2, queue_batch: 10, transaction_batch: 5 })
  await page.goto('/security/settings')
  await openDisclosure(page, 'Authentication settings')
  const security = page.getByRole('form', { name: 'Authentication settings' })
  await security
    .getByLabel('New administrator session duration (hours)', { exact: true })
    .fill('24')
  await security.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(security.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.reload()
  await openDisclosure(page, 'Authentication settings')
  await expect(
    security.getByLabel('New administrator session duration (hours)', { exact: true }),
  ).toHaveValue('24')
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 1100 })
    await expect(security).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(
      false,
    )
    if (process.env.QA_DIRECTORY) {
      await mkdir(process.env.QA_DIRECTORY, { recursive: true })
      await page.evaluate(() => window.scrollTo(0, 0))
      await page.screenshot({
        path: join(process.env.QA_DIRECTORY, `advanced-security-${width}.png`),
        fullPage: true,
        animations: 'disabled',
      })
    }
    await page.goto('/network/incoming')
    for (const title of ['Browser downloads', 'Configuration limits']) {
      await openDisclosure(page, title)
      await expect(page.getByRole('form', { name: title }).locator('input').first()).toBeVisible()
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(
      false,
    )
    if (process.env.QA_DIRECTORY) {
      await page.evaluate(() => window.scrollTo(0, 0))
      await page.screenshot({
        path: join(process.env.QA_DIRECTORY, `advanced-network-${width}.png`),
        fullPage: true,
        animations: 'disabled',
      })
    }
    await page.goto('/security/settings')
    await openDisclosure(page, 'Authentication settings')
  }
})

test('operational policies persist independently and reject stale updates', async ({ page }) => {
  await page.goto('/debrid/settings')
  const section = page
    .locator('details')
    .filter({ has: page.locator(':scope > summary', { hasText: /^Operation capacity$/ }) })
  await expect(section).not.toHaveAttribute('open', '')
  await openDisclosure(page, 'Operation capacity')
  const form = section.getByRole('form', { name: 'Operation capacity' })
  const initial = await (await page.request.get('/api/settings/runtime/debrid')).json()
  await form.getByLabel('Simultaneous download preparations', { exact: true }).fill('2')
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.reload()
  await openDisclosure(page, 'Operation capacity')
  await expect(form.getByLabel('Simultaneous download preparations', { exact: true })).toHaveValue(
    '2',
  )
  const session = await (await page.request.get('/api/session')).json()
  const stale = await page.request.put('/api/settings/runtime/debrid', {
    headers: { 'X-CSRF-Token': session.csrf_token },
    data: initial,
  })
  expect(stale.status()).toBe(409)
  await page.goto('/network/outgoing')
  await openDisclosure(page, 'Request timeouts')
  const timeouts = page.getByRole('form', { name: 'Request timeouts' })
  await timeouts.getByLabel('Debrid request timeout (seconds)', { exact: true }).fill('90')
  await timeouts.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(timeouts.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(
    (await (await page.request.get('/api/settings/runtime/debrid')).json()).values.download_jobs,
  ).toBe(2)
  await page.goto('/health/settings')
  await openDisclosure(page, 'History retention')
  const history = page.getByRole('form', { name: 'History retention' })
  await history.getByLabel('Keep history (days)', { exact: true }).fill('14')
  await history.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(history.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await (await page.request.get('/api/settings/runtime/history')).json()).values.days).toBe(
    14,
  )
  await page.goto('/import-settings')
  await openDisclosure(page, 'Import execution')
  await openDisclosure(page, 'Concurrency')
  await openDisclosure(page, 'Recovery')
  const recovery = page.getByRole('switch', { name: 'Automatically resume interrupted imports' })
  await expect(recovery).toBeChecked()
  await recovery.click()
  const imports = page.getByRole('form', { name: 'Import execution', exact: true })
  await imports.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(imports.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(
    (await (await page.request.get('/api/settings/collections')).json()).settings
      .auto_resume_interrupted,
  ).toBe(false)
})

test('interface preferences apply across tables and reloads without a server write', async ({
  page,
}) => {
  const writes: string[] = []
  page.on('request', (request) => {
    if (request.method() === 'PUT') writes.push(new URL(request.url()).pathname)
  })
  await page.getByRole('button', { name: 'Interface preferences', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Interface preferences' })
  await dialog.getByLabel('Rows per page', { exact: true }).selectOption('25')
  await dialog.getByLabel('Automatic refresh', { exact: true }).selectOption('reduced')
  await dialog.getByLabel('Time zone', { exact: true }).selectOption('utc')
  await dialog.getByLabel('Date format', { exact: true }).selectOption('iso')
  await page.keyboard.press('Escape')
  const listing = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === '/api/torrents' &&
      new URL(request.url()).searchParams.get('limit') === '25',
  )
  await page.goto('/torrents')
  await listing
  await page.reload()
  await page.getByRole('button', { name: 'Interface preferences', exact: true }).click()
  await expect(dialog.getByLabel('Rows per page', { exact: true })).toHaveValue('25')
  await expect(dialog.getByLabel('Date format', { exact: true })).toHaveValue('iso')
  await expect(dialog.getByLabel('Automatic refresh', { exact: true })).toHaveValue('reduced')
  expect(writes).toEqual([])
})

test('settings and shared controls fit desktop and mobile', async ({ page }) => {
  test.setTimeout(180_000)
  const routes = [
    '/providers/settings',
    '/remotes/settings',
    '/torrents',
    '/metadata/videos/settings',
    '/metadata/books/settings',
    '/metadata/identification',
    '/metadata/identification/settings',
    '/debrid/settings',
    '/search',
    '/addon/settings',
    '/torznab/settings',
    '/sharing/settings',
    '/security/settings',
    '/network/incoming',
    '/network/outgoing',
    '/network/outgoing/routing',
    '/public-access',
    '/public-access/limits',
    '/health/settings',
    '/logs',
  ]
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1100 })
    for (const route of routes) {
      await page.goto(route)
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toBeVisible()
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1),
        `${route} at ${width}px`,
      ).toBe(false)
      if (
        process.env.QA_DIRECTORY &&
        ['/network/incoming', '/health/settings', '/debrid/settings'].includes(route)
      ) {
        const sectionName =
          route === '/debrid/settings'
            ? 'Operation capacity'
            : route === '/health/settings'
              ? 'History retention'
              : 'Request timeouts'
        await openDisclosure(page, sectionName)
        await expect(
          page.getByRole('form', { name: sectionName }).locator('input').first(),
        ).toBeVisible()
        await mkdir(process.env.QA_DIRECTORY, { recursive: true })
        await page.screenshot({
          path: join(
            process.env.QA_DIRECTORY,
            `runtime-${route.replaceAll('/', '-')}-${width}.png`,
          ),
          fullPage: true,
          animations: 'disabled',
        })
      }
    }
    await page.getByRole('button', { name: 'Interface preferences', exact: true }).click()
    await expect(
      page
        .getByRole('dialog', { name: 'Interface preferences' })
        .getByLabel('Date format', { exact: true }),
    ).toBeVisible()
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1),
    ).toBe(false)
    if (process.env.QA_DIRECTORY)
      await page.screenshot({
        path: join(process.env.QA_DIRECTORY, `interface-preferences-${width}.png`),
        animations: 'disabled',
      })
    await page.keyboard.press('Escape')
  }
})
