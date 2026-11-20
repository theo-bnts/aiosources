import { test, expect } from './fixtures'

for (const width of [390, 1440]) {
  test(`page and field explanations stay in accessible help at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    for (const [path, title, explanation] of [
      ['/providers/settings', 'Torrents', 'Torrent source connections and automatic imports.'],
      ['/metadata/books/settings', 'Books', 'Book and audiobook information, updates and history.'],
      ['/network/outgoing', 'Outgoing', 'Manage proxies and choose how services connect.'],
    ]) {
      await page.goto(path)
      await expect(page.locator('#page-description')).toHaveCount(0)
      const help = page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', {
        name: `About ${title}`,
        exact: true,
      })
      await help.click()
      await expect(page.getByRole('dialog', { name: `About ${title}`, exact: true })).toContainText(
        explanation,
      )
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog', { name: `About ${title}`, exact: true })).toBeHidden()
    }
    await page.goto('/health/settings')
    const explanation =
      'Identifiers, search fields, matching evidence and playback information are always retained.'
    await expect(page.getByText(explanation, { exact: true })).toHaveCount(0)
    const help = page.getByRole('button', { name: 'About Torrent data', exact: true })
    await help.focus()
    await page.keyboard.press('Enter')
    const popup = page.getByRole('dialog', { name: 'About Torrent data', exact: true })
    await expect(popup).toContainText(explanation)
    await page.keyboard.press('Escape')
    await expect(help).toBeFocused()
    await page.goto('/torrents')
    await page.getByRole('button', { name: 'About Search text', exact: true }).click()
    await expect(
      page.getByRole('dialog', { name: 'About Search text', exact: true }),
    ).toContainText('Combine fragments')
    await page.keyboard.press('Escape')
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
    ).toBeLessThanOrEqual(0)
  })

  test(`configuration checks notify without replacing actionable field issues at ${width}px`, async ({
    page,
    server,
  }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/providers/new')
    const template = page
      .getByRole('row')
      .filter({ has: page.getByText('torznab', { exact: true }) })
    await template.getByRole('button', { name: 'Use this template', exact: true }).click()
    await page.getByLabel('Source identifier', { exact: true }).fill('synthetic_configuration_help')
    const heading = page.getByRole('heading', { name: 'Configure source', exact: true })
    await expect(heading).toBeVisible()
    expect((await heading.boundingBox())!.height).toBeLessThanOrEqual(28)
    await page.getByRole('tab', { name: 'Connection', exact: true }).click()
    const url = page.getByLabel('Source URL', { exact: true })
    const check = page.getByRole('button', { name: 'Check configuration', exact: true })
    const validToast = page
      .locator('[data-notification]')
      .filter({ hasText: 'Configuration is valid.' })
    for (const [value, valid] of [
      [server.sourceURL, true],
      ['ftp://example.invalid/catalog', false],
      [server.sourceURL, true],
    ] as const) {
      await url.fill(value)
      const response = page.waitForResponse(
        (result) =>
          result.request().method() === 'POST' &&
          new URL(result.url()).pathname === '/api/providers/validate',
      )
      await check.click()
      const result = await (await response).json()
      expect(result.valid).toBe(valid)
      const toast = valid
        ? validToast
        : page
            .locator('[data-notification]')
            .filter({ hasText: 'The definition contains errors. Check the highlighted issues.' })
      await expect(toast).toBeVisible()
      if (valid) {
        await expect(page.getByRole('list', { name: 'Source validation issues' })).toHaveCount(0)
        if (process.env.QA_DIRECTORY)
          await page.screenshot({
            path: `${process.env.QA_DIRECTORY}/configuration-check-${width}.png`,
            fullPage: true,
          })
      } else {
        await expect(page.getByRole('list', { name: 'Source validation issues' })).toBeVisible()
      }
      await toast.getByRole('button', { name: 'Dismiss notification', exact: true }).click()
    }
    await expect(
      page
        .locator('main .notice')
        .filter({ hasText: /Valid definition|The definition contains errors/ }),
    ).toHaveCount(0)
    expect(server.sourceRequests()).toBe(0)
  })
}
