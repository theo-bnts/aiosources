import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'

async function settleNavigation(page: Page) {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  )
}

async function retainDocumentHeight(page: Page) {
  // Isolate explicit navigation jumps from natural clamping on shorter panels.
  await page.locator('#main-content').evaluate((main) => {
    main.style.minHeight = '2200px'
  })
  await page.evaluate(() => window.scrollTo(0, 25))
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(25)
}

test('horizontal tab bars have no vertical scroll range on desktop or mobile', async ({ page }) => {
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1100 })
    for (const path of ['/providers', '/metadata', '/debrid', '/addon', '/security']) {
      await page.goto(path)
      const tabs = page.getByRole('tablist').first()
      await expect(tabs).toBeVisible()
      const dimensions = await tabs.evaluate((element) => ({
        height: element.clientHeight,
        contentHeight: element.scrollHeight,
      }))
      expect(dimensions.contentHeight, `${path} at ${width}px`).toBe(dimensions.height)
      await tabs.evaluate((element) => {
        element.scrollTop = 10
      })
      expect(await tabs.evaluate((element) => element.scrollTop)).toBe(0)
      const last = tabs.getByRole('tab').last()
      await last.focus()
      await expect(last).toBeFocused()
      expect(await tabs.evaluate((element) => element.scrollTop)).toBe(0)
      await page.evaluate(() => document.fonts.ready)
      await expect
        .poll(() =>
          tabs.evaluate((element) => {
            const active = element.querySelector('[role="tab"][data-state="active"]')!
            const bar = element.getBoundingClientRect()
            const tab = active.getBoundingClientRect()
            return tab.left >= bar.left - 1 && tab.right <= bar.right + 1
          }),
        )
        .toBe(true)
    }
  }
})

test('opening a secondary view directly reveals its active tab without scrolling the page', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  for (const path of ['/metadata/history', '/security/settings', '/runs/missing']) {
    await page.goto(path)
    const tabs = page.getByRole('tablist').first()
    await expect(tabs).toBeVisible()
    await page.evaluate(() => document.fonts.ready)
    await expect
      .poll(() =>
        tabs.evaluate((element) => {
          const active = element.querySelector('[role="tab"][data-state="active"]')!
          const bar = element.getBoundingClientRect()
          const tab = active.getBoundingClientRect()
          return tab.left >= bar.left - 1 && tab.right <= bar.right + 1
        }),
      )
      .toBe(true)
    expect(await page.evaluate(() => window.scrollY)).toBe(0)
  }
})

test('switching page tabs keeps scroll position and keyboard focus while menu navigation resets', async ({
  page,
}) => {
  await page.goto('/security/settings')
  await retainDocumentHeight(page)
  const statistics = page.getByRole('tab', { name: 'Statistics', exact: true })
  await statistics.click()
  await expect(page).toHaveURL(/\/security$/)
  await settleNavigation(page)
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(25)
  await expect(statistics).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(page).toHaveURL(/\/security\/settings$/)
  await settleNavigation(page)
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(25)
  await expect(page.getByRole('tab', { name: 'Settings', exact: true })).toBeFocused()
  await page
    .getByRole('navigation', { name: 'Main navigation', exact: true })
    .getByRole('link', { name: 'Metadata', exact: true })
    .click()
  await expect(page).toHaveURL(/\/metadata$/)
  await settleNavigation(page)
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0)
  await expect(page.locator('#main-content')).toBeFocused()
})

test('history tabs keep scroll and their selected view', async ({ page }) => {
  for (const [url, tab, target] of [
    ['/metadata', 'History', /\/metadata\/history$/],
    ['/addon', 'History', /\/addon\/history$/],
    ['/debrid', 'History', /\/debrid\/history$/],
    ['/providers', 'History', /\/runs$/],
  ] as const) {
    await page.goto(url)
    await retainDocumentHeight(page)
    const trigger = page.getByRole('tab', { name: tab, exact: true })
    await trigger.click()
    await expect(page).toHaveURL(target)
    await settleNavigation(page)
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(25)
    await expect(trigger).toBeFocused()
    await expect(trigger).toHaveAttribute('aria-selected', 'true')
  }
})

test('source stage keyboard navigation keeps position and page tabs protect unsaved edits once', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1440, height: 600 })
  await page.goto('/providers/new')
  await expect(
    page.getByRole('heading', { name: 'Add source', exact: true, level: 2 }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
  await page.getByLabel('Import a source JSON definition', { exact: true }).setInputFiles({
    name: 'synthetic-tab-navigation.json',
    mimeType: 'application/json',
    buffer: Buffer.from(
      JSON.stringify({
        version: 1,
        id: 'tab-guard-source',
        name: 'Tab guard source',
        adapter: 'http_json',
        url: server.sourceURL,
        enabled: false,
      }),
    ),
  })
  await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await retainDocumentHeight(page)
  const identity = page.getByRole('tab', { name: 'Identity', exact: true })
  await identity.focus()
  await page.keyboard.press('End')
  await settleNavigation(page)
  await expect(page.getByRole('tab', { name: 'Schedules', exact: true })).toBeFocused()
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(25)
  await expect(page.getByRole('heading', { name: 'Schedules', exact: true })).toHaveCount(0)
  await page.keyboard.press('Home')
  await expect(identity).toBeFocused()
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page).toHaveURL(/\/providers\/tab-guard-source$/)
  await page.getByLabel('Display name', { exact: true }).fill('Unsaved tab draft')
  const history = page.getByRole('tab', { name: 'History', exact: true })
  await history.click()
  const prompt = page.getByRole('alertdialog')
  await expect(prompt).toContainText('source configuration')
  await prompt.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('Unsaved tab draft')
  await history.click()
  await prompt.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(page).toHaveURL(/\/runs$/)
  await expect(prompt).toHaveCount(0)
  const persisted = await (await page.request.get('/api/providers/tab-guard-source')).json()
  expect(persisted.provider.name).toBe('Tab guard source')
})

test('shared and remote catalogs are standalone pages without horizontal tabs', async ({
  page,
}) => {
  for (const path of ['/sharing', '/remotes', '/sharing/remotes']) {
    await page.goto(path)
    await expect(
      page.getByRole('heading', {
        name: path === '/sharing' ? 'Shared catalogs' : 'Remote catalogs',
        exact: true,
        level: 1,
      }),
    ).toBeVisible()
    await expect(page.getByRole('tablist')).toHaveCount(0)
  }
})
