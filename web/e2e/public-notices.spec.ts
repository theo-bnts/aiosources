import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'

async function openAbout(page: Page) {
  const trigger = page.getByRole('button', { name: 'About AIOSources', exact: true })
  await expect(trigger).toHaveCount(1)
  await trigger.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'AIOSources', exact: true })).toBeVisible()
  for (const [name, href] of [
    ['GitHub Source code and releases', 'https://github.com/theo-bnts/aiosources'],
    ['Discord Help in French only', 'https://discord.gg/h3sja5XN8b'],
    ['Support AIOSources GitHub Sponsors', 'https://github.com/sponsors/theo-bnts'],
  ]) {
    const link = dialog.getByRole('link', { name, exact: true })
    await expect(link).toHaveAttribute('href', href)
    await expect(link).toHaveAttribute('target', '_blank')
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  }
  return dialog
}

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
    const credits = await openAbout(page)
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
    await expect(page.getByRole('button', { name: 'About AIOSources', exact: true })).toBeFocused()
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

test('one About entry stays in the same place on public, sign-in and administrator pages', async ({
  page,
  server,
  browser,
}) => {
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  let externalRequests = 0
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (url.origin === server.origin) return route.continue()
    externalRequests++
    return route.abort('blockedbyclient')
  })
  async function position(target: Page) {
    const trigger = target.getByRole('button', { name: 'About AIOSources', exact: true })
    await expect(trigger).toHaveCount(1)
    await expect(trigger).toBeVisible()
    await target.evaluate(() => document.fonts.ready)
    await trigger.evaluate(async (node) => {
      await Promise.all(node.getAnimations().map((animation) => animation.finished))
    })
    return trigger.boundingBox()
  }
  async function checkAtSamePosition(target: Page, expected: Awaited<ReturnType<typeof position>>) {
    expect(await position(target)).toEqual(expected)
    const address = target.url()
    await openAbout(target)
    await target.keyboard.press('Escape')
    await expect(target.getByRole('dialog')).toHaveCount(0)
    await expect(
      target.getByRole('button', { name: 'About AIOSources', exact: true }),
    ).toBeFocused()
    expect(target.url()).toBe(address)
    expect(await target.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    )
  }
  try {
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 950 })
      await visitor.setViewportSize({ width, height: 950 })
      const anchor = await position(page)
      await visitor.goto('/')
      const password = visitor.getByLabel('Administrator password', { exact: true })
      await expect(password).toBeVisible()
      await password.fill('synthetic-form-draft')
      await checkAtSamePosition(visitor, anchor)
      await expect(password).toHaveValue('synthetic-form-draft')
    }

    await page.goto('/addon/settings')
    const access = page.getByRole('form', { name: 'Public access', exact: true })
    await access.getByRole('switch').check()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 950 })
      await visitor.setViewportSize({ width, height: 950 })
      const anchor = await position(page)
      await checkAtSamePosition(page, anchor)
      await visitor.goto('/')
      await expect(
        visitor.getByRole('heading', { name: 'Connect your debrid services', exact: true }),
      ).toBeVisible()
      const key = visitor.getByLabel('TorBox API key', { exact: true })
      await key.fill('synthetic-form-draft')
      await checkAtSamePosition(visitor, anchor)
      await expect(key).toHaveValue('synthetic-form-draft')
      await expect(visitor.getByRole('navigation')).toHaveCount(0)
      expect((await context.request.get('/api/providers')).status()).toBe(401)

      await visitor.getByRole('link', { name: 'Admin login', exact: true }).click()
      await expect(visitor.getByLabel('Administrator password', { exact: true })).toBeVisible()
      await checkAtSamePosition(visitor, anchor)

      await page.goto('/public')
      await expect(page.getByRole('link', { name: 'Administration', exact: true })).toBeVisible()
      await checkAtSamePosition(page, anchor)
      await page.getByRole('link', { name: 'Administration', exact: true }).click()
      await expect(
        page.getByRole('heading', { name: 'Sources', exact: true, level: 1 }),
      ).toBeVisible()
      await checkAtSamePosition(page, anchor)
    }
    expect(externalRequests, 'About must not load external resources').toBe(0)
  } finally {
    await context.close()
  }
})
