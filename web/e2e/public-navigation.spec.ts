import { test, expect } from './fixtures'

test('public navigation only offers enabled connections and follows policy changes', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.getByLabel('Allow public Search', { exact: true }).check()
  await access.getByLabel('Allow public Stremio connections', { exact: true }).uncheck()
  const save = async () => {
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  }
  await save()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  const nav = visitor.getByRole('navigation', { name: 'Main navigation' })
  try {
    await visitor.goto('/public/torznab')
    await expect(visitor).toHaveURL(/\/public\/search$/)
    await expect(nav.getByRole('link', { name: 'Search', exact: true })).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Stremio', exact: true })).toHaveCount(0)
    await expect(nav.getByRole('link', { name: 'Torznab', exact: true })).toHaveCount(0)
    await expect(
      visitor.getByText('The administrator has not enabled this connection.'),
    ).toHaveCount(0)
    await expect(
      visitor.getByRole('button', { name: 'Start searching', exact: true }),
    ).toBeVisible()
    await access.getByLabel('Allow public Stremio connections', { exact: true }).check()
    await access.getByLabel('Allow public Torznab connections', { exact: true }).check()
    await save()
    await expect(nav.getByRole('link', { name: 'Torznab', exact: true })).toBeVisible()
    const connections = nav.getByRole('button', { name: 'Connections', exact: true })
    await expect(connections).toHaveAttribute('aria-expanded', 'true')
    await connections.click()
    await expect(connections).toHaveAttribute('aria-expanded', 'false')
    await connections.click()
    await expect(connections).toHaveAttribute('aria-expanded', 'true')
    await nav.getByRole('link', { name: 'Torznab', exact: true }).click()
    await expect(
      visitor.getByRole('button', { name: 'Create connection link', exact: true }),
    ).toBeVisible()
    await access.getByLabel('Allow public Stremio connections', { exact: true }).uncheck()
    await access.getByLabel('Allow public Torznab connections', { exact: true }).uncheck()
    await save()
    await expect(visitor).toHaveURL(/\/public\/search$/)
    await visitor.setViewportSize({ width: 390, height: 844 })
    expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
    await access.getByLabel('Allow public Search', { exact: true }).uncheck()
    await save()
    await expect(
      visitor.getByRole('heading', { name: 'Open a session', exact: true }),
    ).toBeVisible()
    await expect(visitor.getByRole('button', { name: 'Start searching', exact: true })).toHaveCount(
      0,
    )
  } finally {
    await context.close()
  }
})
