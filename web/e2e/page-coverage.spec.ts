import { applicationPages } from './application-pages'
import { test, expect } from './fixtures'

test('other book formats have their own explicit filter', async ({ page }) => {
  await page.goto('/search')
  await page.locator('summary', { hasText: /^Filters$/ }).click()
  await page.getByRole('button', { name: 'Type', exact: true }).click()
  await expect(
    page.getByRole('menuitemcheckbox', { name: 'Other books', exact: true }),
  ).toBeVisible()
  await page.keyboard.press('Escape')
  await page.goto('/torznab')
  await page.getByRole('button', { name: 'Create access', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('summary', { hasText: /^Filters$/ }).click()
  await dialog.getByRole('button', { name: 'Media types', exact: true }).click()
  await expect(
    page.getByRole('menuitemcheckbox', { name: 'Other books', exact: true }),
  ).toBeVisible()
})

test('every administration page keeps its navigation, optional controls and responsive bounds', async ({
  page,
}) => {
  test.setTimeout(120_000)
  const failures: number[] = []
  const covered = new Set<string>(applicationPages.map(([, route]) => route))
  page.on('response', (response) => {
    if (new URL(response.url()).pathname.startsWith('/api/') && response.status() >= 400)
      failures.push(response.status())
  })
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 1050 })
    for (const [, route] of applicationPages) {
      await page.goto(route)
      await expect(page).toHaveURL((url) => url.pathname === route)
      await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
      await expect(
        page.locator('main [data-slot="skeleton"]:visible,main [aria-busy="true"]:visible'),
      ).toHaveCount(0)
      await expect(page.locator('main [role="alert"]')).toHaveCount(0)
      const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
      if (width >= 1024) await expect(navigation.locator('a[aria-current="page"]')).toHaveCount(1)
      for (const href of await navigation
        .locator('a[href]')
        .evaluateAll((links) =>
          links.map((link) => new URL((link as HTMLAnchorElement).href).pathname),
        )) {
        expect(covered.has(href), `Add navigation destination ${href} to applicationPages`).toBe(
          true,
        )
      }
      for (const disclosure of await page.locator('main details').all()) {
        if (await disclosure.isVisible()) {
          const summary = disclosure.locator(':scope > summary')
          const initiallyOpen = (await disclosure.getAttribute('open')) !== null
          await expect(summary).not.toHaveText('')
          await summary.focus()
          await page.keyboard.press('Enter')
          if (initiallyOpen) await expect(disclosure).not.toHaveAttribute('open')
          else await expect(disclosure).toHaveAttribute('open')
          await page.keyboard.press('Enter')
          if (initiallyOpen) await expect(disclosure).toHaveAttribute('open')
          else await expect(disclosure).not.toHaveAttribute('open')
        }
      }
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        route,
      ).toBe(true)
      // New workspace tabs must join the same browser/performance inventory.
      for (const tab of await page.getByRole('tab').all()) {
        await tab.click()
        await expect(page).toHaveURL((url) => covered.has(url.pathname))
        await expect(tab).toHaveAttribute('aria-selected', 'true')
      }
    }
  }
  expect(failures).toEqual([])
})
