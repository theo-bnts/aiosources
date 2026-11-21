import { test, expect } from './fixtures'

const keyPages = [
  ['AllDebrid', 'https://alldebrid.com/apikeys'],
  ['Debrid-Link', 'https://debrid-link.com/webapp/apikey'],
  ['Deepbrid', 'https://www.deepbrid.com/devices'],
  ['Premiumize', 'https://www.premiumize.me/account'],
  ['Real-Debrid', 'https://real-debrid.com/apitoken'],
  ['TorBox', 'https://torbox.app/settings'],
] as const

test('debrid key pages are available in administrator and personal visitor forms', async ({
  page,
  server,
  browser,
}) => {
  async function checkLinks(scope: typeof page) {
    for (const [name, href] of keyPages) {
      const link = scope.getByRole('link', { name: `Get ${name} API key`, exact: true })
      await expect(link).toHaveAttribute('href', href)
      await expect(link).toHaveAttribute('target', '_blank')
      await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
      await expect(link.locator('svg')).toHaveCount(1)
      const field = scope.getByLabel(`${name} API key`, { exact: true })
      expect((await link.boundingBox())!.height).toBeLessThan((await field.boundingBox())!.height)
    }
  }
  await page.goto('/debrid/settings')
  await checkLinks(page)
  await page.goto('/public-access')
  const form = page.getByRole('form', { name: 'Public', exact: true })
  await form.getByRole('switch', { name: 'Enable public access', exact: true }).check()
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    for (const route of ['/public', '/public/search']) {
      await visitor.setViewportSize({ width: 390, height: 1000 })
      await visitor.goto(route)
      await checkLinks(visitor)
      await expect(visitor.getByLabel('AllDebrid API key', { exact: true })).toHaveValue('')
    }
  } finally {
    await context.close()
  }
})

test('About credits follow the metadata navigation order', async ({ page }) => {
  await page.getByRole('button', { name: 'About AIOSources', exact: true }).click()
  const about = page.getByRole('dialog', { name: /^AIOSources/ })
  const credits = about.getByRole('region', { name: 'Credits', exact: true })
  for (const link of await credits.locator('a').all())
    await expect(link).toHaveAttribute('target', '_blank')
  expect(
    await credits
      .locator('a')
      .evaluateAll((links) => links.map((link) => link.getAttribute('href'))),
  ).toEqual([
    'https://www.themoviedb.org',
    'https://www.igdb.com',
    'https://nvd.nist.gov',
    'https://github.com/microsoft/winget-pkgs',
    'https://brew.sh',
    'https://selfh.st/icons',
    'https://musicbrainz.org',
    'https://coverartarchive.org',
    'https://openlibrary.org',
    '/THIRD-PARTY-NOTICES.txt',
  ])
})
