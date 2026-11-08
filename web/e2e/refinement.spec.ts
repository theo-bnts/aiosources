import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

for (const width of [390, 1440]) {
  test(`workspace creation actions share the tab row at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1100 })
    for (const [route, label] of [
      ['/providers/settings', 'Add source'],
      ['/remotes/settings', 'Add remote catalog'],
      ['/addon/settings', 'Create access'],
      ['/sharing/settings', 'Create share'],
    ]) {
      await page.goto(route)
      const toolbar = page.locator('[data-workspace-toolbar]')
      const action = toolbar
        .locator('[data-slot="button"]')
        .filter({ hasText: new RegExp(`^${label}$`) })
      await expect(action).toBeVisible()
      const tabs = await toolbar.getByRole('tablist').boundingBox()
      const button = await action.boundingBox()
      expect(Math.abs(tabs!.y - button!.y)).toBeLessThan(5)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      await page.getByRole('tab', { name: 'Statistics', exact: true }).click()
      await expect(toolbar.locator('[data-slot="button"]')).toHaveCount(0)
    }
  })
}

test('menus respect the application header and remain usable inside dialogs', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/search')
  await openDisclosure(page, 'Filters')
  const trigger = page.getByRole('button', { name: 'Type', exact: true })
  await trigger.scrollIntoViewIfNeeded()
  await trigger.click()
  const menu = page.getByRole('menu', { name: 'Type', exact: true })
  await expect(menu).toBeVisible()
  const header = await page.locator('[data-app-shell] > div > header').boundingBox()
  const rect = await menu.boundingBox()
  expect(rect!.y).toBeGreaterThanOrEqual(header!.y + header!.height)
  expect(await menu.evaluate((element) => getComputedStyle(element).fontSize)).toBe('14px')
  await page.keyboard.press('Escape')
  await page.goto('/addon/settings')
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await openDisclosure(dialog, 'Filters')
  await dialog.getByRole('button', { name: 'Services', exact: true }).click()
  const services = page.getByRole('menu', { name: 'Services', exact: true })
  await expect(services).toBeVisible()
  await services.getByRole('menuitem', { name: 'All services', exact: true }).click()
  await expect(dialog).toBeVisible()
})

test('optional information settings stay collapsed outside the essential card', async ({
  page,
}) => {
  for (const [route, section, label] of [
    ['/metadata/videos/settings', 'Automatic updates', 'Information update interval (minutes)'],
    ['/metadata/books/settings', 'Catalog filters', 'Relevant editions only'],
    ['/metadata/identification/settings', 'Automatic updates', 'Identification interval (seconds)'],
    ['/debrid/settings', 'Cache availability', 'Cache validity (minutes)'],
  ]) {
    await page.goto(route)
    const control = page.getByLabel(label, { exact: true })
    await expect(control).toBeHidden()
    const detail = page
      .locator('details')
      .filter({ has: page.locator('summary', { hasText: new RegExp(`^${section}$`) }) })
    await expect(
      detail.locator('xpath=ancestor::section[contains(@class,"section-card")]'),
    ).toHaveCount(0)
    await openDisclosure(page, section)
    await expect(control).toBeVisible()
    const form = control.locator('xpath=ancestor::form')
    await expect(form.getByRole('button', { name: 'Save', exact: true })).toHaveCount(1)
  }
})

test('debrid statistics summarize shared observations without counting them as unique torrents', async ({
  page,
}) => {
  await page.route('**/api/debrid/statistics', (route) =>
    route.fulfill({
      json: {
        items: [
          {
            service: 'alldebrid',
            cached: 30,
            uncached: 10,
            last_checked: '',
            checks: { cached: 3, uncached: 2, last_checked: null },
          },
          {
            service: 'torbox',
            cached: 20,
            uncached: 40,
            last_checked: '',
            checks: { cached: 1, uncached: 4, last_checked: null },
          },
        ],
      },
    }),
  )
  await page.goto('/debrid')
  const stats = page.locator('main dl').first()
  await expect(stats).toContainText('Shared cached observations50')
  await expect(stats).toContainText('Shared not cached observations50')
  await expect(stats).toContainText('Account observations10')
})
