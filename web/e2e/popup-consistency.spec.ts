import { mkdir } from 'node:fs/promises'
import { test, expect } from './fixtures'

// Existing directory editors are exercised through their ordinary actions;
// no source data or external service account is needed for layout checks.
test('directory editors and shared preferences use consistent, accessible mobile dialogs', async ({
  page,
}) => {
  test.setTimeout(120_000)
  const failures: string[] = []
  page.on('console', (message) => {
    if (
      message.type() === 'error' ||
      /Missing.*Description|requires a.*Title/i.test(message.text())
    )
      failures.push(message.text())
  })
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 844 })
    for (const [route, action, title] of [
      ['/administration/network/outgoing/proxies', 'Add proxy', 'Add proxy'],
      ['/sources/remote-catalogs/settings', 'Add remote catalog', 'Add remote catalog'],
      ['/connections/stremio/settings', 'Create access', 'Create access'],
      ['/connections/torznab', 'Create access', 'Create access'],
      ['/connections/shared-catalogs/settings', 'Create share', 'Create share'],
      ['/administration/users/trusted', 'Add user', 'Add trusted user'],
      ['/administration/security/secrets', 'Create secret', 'Create secret'],
    ]) {
      await page.goto(route)
      const origin = new URL(page.url()).pathname
      await page.getByRole('button', { name: action, exact: true }).first().click()
      const dialog = page.getByRole('dialog', { name: title, exact: true })
      await expect(dialog).toBeVisible()
      expect(new URL(page.url()).pathname).toBe(origin)
      const size = await dialog.boundingBox()
      expect(size!.x).toBeGreaterThanOrEqual(15)
      expect(size!.x + size!.width).toBeLessThanOrEqual(width - 15)
      expect(await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth + 1)).toBe(true)
      for (const button of await dialog.locator('[data-slot="button"]').all()) {
        if (await button.isVisible())
          expect(
            await button.locator('svg').count(),
            `Missing action icon in ${title}: ${await button.textContent()}`,
          ).toBeGreaterThan(0)
      }
      if (process.env.QA_DIRECTORY) {
        await mkdir(process.env.QA_DIRECTORY, { recursive: true })
        await page.screenshot({
          path: `${process.env.QA_DIRECTORY}/${route.replaceAll('/', '-')}-${width}.png`,
          animations: 'disabled',
        })
      }
      await dialog.getByRole('button', { name: 'Close', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      expect(new URL(page.url()).pathname).toBe(origin)
    }
    await page.getByRole('button', { name: 'Interface preferences', exact: true }).click()
    const preferences = page.getByRole('dialog', { name: 'Interface preferences', exact: true })
    await expect(preferences).not.toContainText('Applies throughout the interface')
    await expect(preferences.getByRole('heading', { name: 'Interface preferences' })).toBeFocused()
    await expect(preferences.getByLabel('Rows per page', { exact: true })).not.toBeFocused()
    await page.keyboard.press('Tab')
    await expect(preferences.getByLabel('Rows per page', { exact: true })).toBeFocused()
    await preferences.getByRole('button', { name: 'Close', exact: true }).click()
    await page.goto('/administration/security/active-sessions')
    await expect(page.locator('main')).not.toContainText('Each session keeps the expiry')
  }
  expect(failures).toEqual([])
})
