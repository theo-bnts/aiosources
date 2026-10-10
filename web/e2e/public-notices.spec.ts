import { test, expect } from './fixtures'

test('credits use bundled assets and licenses without changing the current view or source drafts', async ({
  page,
}) => {
  await page.goto('/providers/new')
  await page.getByRole('button', { name: 'Use this template', exact: true }).first().click()
  const name = page.getByRole('textbox', { name: 'Display name', exact: true })
  await name.fill('Example Library')
  const address = page.url()

  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 950 })
    if (width < 1024) await page.getByRole('button', { name: 'Open navigation' }).click()
    await page.getByRole('button', { name: 'About AIOSources', exact: true }).click()
    const credits = page.getByRole('dialog').last()
    await expect(credits.getByRole('heading', { name: 'AIOSources', exact: true })).toBeVisible()
    await expect(credits).toContainText(
      'This product uses the TMDB API but is not endorsed or certified by TMDB.',
    )
    const logo = credits.getByRole('img', { name: 'TMDB', exact: true })
    await expect(logo).toHaveAttribute('src', '/tmdb.svg')
    await expect(logo).toBeVisible()
    expect(await logo.evaluate((node) => (node as HTMLImageElement).naturalWidth)).toBeGreaterThan(
      0,
    )
    await expect(
      credits.getByRole('link', { name: 'Software licenses and notices' }),
    ).toHaveAttribute('href', '/THIRD-PARTY-NOTICES.txt')
    await credits.getByRole('button', { name: 'Close', exact: true }).click()
    if (width < 1024) await page.getByRole('button', { name: 'Close navigation' }).click()
    await expect(page.getByRole('alertdialog')).toHaveCount(0)
    await expect(name).toHaveValue('Example Library')
    expect(page.url()).toBe(address)
  }

  const notices = await page.request.get('/THIRD-PARTY-NOTICES.txt')
  expect(notices.status()).toBe(200)
  expect(notices.headers()['content-type']).toContain('text/plain')
  const text = await notices.text()
  for (const required of [
    'Copyright (c) 2026 moodiness and contributors',
    'Copyright (c) 2026 Théo Bontemps',
    'Go standard library',
    '@fontsource-variable/geist',
    'SIL OPEN FONT LICENSE',
    '@phosphor-icons/react',
    'react 19.',
    'Apache License',
    'CA certificate bundle',
    'Mozilla Public License',
  ]) {
    expect(text).toContain(required)
  }
})
