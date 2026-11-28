import { test, expect } from './fixtures'

test('alerts active view omits unconfigured channel banner and settings groups platforms alphabetically with Generic first', async ({
  page,
}) => {
  await page.goto('/connections/alerts/active')
  await expect(page.getByRole('heading', { name: 'Alerts', exact: true, level: 1 })).toBeVisible()
  await expect(page.getByText('No active release alerts', { exact: true })).toBeVisible()
  await expect(page.getByText('No notification channel configured yet')).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Search titles', exact: true })).toHaveAttribute(
    'href',
    '/browse/search?unavailable=true',
  )

  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await expect(page).toHaveURL(/\/connections\/alerts\/settings$/)
  await page.getByRole('button', { name: 'Add channel', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Add notification channel', exact: true })
  const platform = dialog.getByLabel('Platform', { exact: true })
  await expect(platform).toHaveValue('webhook')
  await expect(platform.locator('optgroup')).toHaveCount(2)
  await expect(platform.locator('optgroup').nth(0)).toHaveAttribute('label', 'Generic')
  await expect(platform.locator('optgroup').nth(0).locator('option')).toHaveText([
    'Generic webhook',
  ])
  await expect(platform.locator('optgroup').nth(1)).toHaveAttribute('label', 'Platforms')
  await expect(platform.locator('optgroup').nth(1).locator('option')).toHaveText([
    'Discord',
    'Gotify',
    'ntfy',
    'Slack / Mattermost',
    'Telegram',
  ])
})
