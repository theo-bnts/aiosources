import { test, expect } from './fixtures'

test('content families and individual types can be combined without changing saved filter values', async ({
  page,
}) => {
  await page.goto('/search')
  await page.getByLabel('Title, author or identifier', { exact: true }).fill('example')
  await page.locator('summary', { hasText: /^Filters$/ }).click()
  const control = page.getByRole('button', { name: 'Type', exact: true })
  await control.click()
  const videos = page.getByRole('menuitemcheckbox', { name: 'Videos', exact: true })
  const books = page.getByRole('menuitemcheckbox', { name: 'Books', exact: true })
  await videos.click()
  await page.getByRole('menuitemcheckbox', { name: 'Ebooks', exact: true }).click()
  await expect(books).toHaveAttribute('aria-checked', 'mixed')
  await page.getByRole('menuitemcheckbox', { name: 'Movies', exact: true }).click()
  await expect(videos).toHaveAttribute('aria-checked', 'mixed')
  await books.click()
  await page.keyboard.press('Escape')
  await expect(control).toHaveText('Series, Books')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect
    .poll(() => new URL(page.url()).searchParams.get('type'))
    .toBe('tv,ebook,audiobook,book')
  await page.reload()
  await page.locator('summary', { hasText: /^Filters$/ }).click()
  await expect(control).toHaveText('Series, Books')
  await page.setViewportSize({ width: 320, height: 844 })
  await control.click()
  await expect(
    page.getByRole('menuitemcheckbox', { name: 'Other books', exact: true }),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('menuitem', { name: 'All media', exact: true }).click()
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('type')).toBeNull()
})

test('public connections use the same selectable content families and show Torznab before Stremio', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public', exact: true })
  await access.locator('summary').getByText('Permissions', { exact: true }).click()
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.getByLabel('Allow Torznab connections', { exact: true }).check()
  await access.getByLabel('Allow Stremio connections', { exact: true }).check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/public/torznab')
    const nav = visitor.getByRole('navigation', { name: 'Main navigation' })
    await expect(nav.getByRole('link', { name: /^(Torznab|Stremio)$/ })).toHaveText([
      'Torznab',
      'Stremio',
    ])
    await visitor.locator('summary', { hasText: /^Filters$/ }).click()
    const control = visitor.getByRole('button', { name: 'Media types', exact: true })
    await control.click()
    await visitor.getByRole('menuitemcheckbox', { name: 'Books', exact: true }).click()
    await visitor.getByRole('menuitemcheckbox', { name: 'Movies', exact: true }).click()
    await expect(
      visitor.getByRole('menuitemcheckbox', { name: 'Books', exact: true }),
    ).toBeChecked()
    await expect(
      visitor.getByRole('menuitemcheckbox', { name: 'Videos', exact: true }),
    ).toHaveAttribute('aria-checked', 'mixed')
    await visitor.keyboard.press('Escape')
    await expect(control).toHaveText('Books, Movies')
    await visitor.setViewportSize({ width: 320, height: 844 })
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
  } finally {
    await context.close()
  }
})
