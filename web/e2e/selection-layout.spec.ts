import type { Locator, Page } from '@playwright/test'
import { test, expect } from './fixtures'

const libraries = Array.from({ length: 6 }, (_, index) => ({
  id: `layout-library-${index}`,
  name:
    index === 5
      ? `Unbroken${'LibraryName'.repeat(10)}`
      : `Library ${index} with a descriptive name for several collections and imported records`,
}))

async function selectEveryOption(page: Page, control: Locator) {
  await control.click()
  const options = page.getByRole('menuitemcheckbox')
  await expect(options.first()).toBeVisible()
  for (const option of await options.all()) await option.check()
  await page.keyboard.press('Escape')
}

async function expectBounded(control: Locator, truncated = false) {
  await expect
    .poll(() =>
      control.evaluate((element) => {
        const bounds = element.getBoundingClientRect()
        const field = element.closest('.field')!.getBoundingClientRect()
        const dialog = element.closest('[role="dialog"]')?.getBoundingClientRect()
        return (
          bounds.width > 0 &&
          bounds.left >= Math.max(field.left, dialog?.left ?? 0) &&
          bounds.right <= Math.min(field.right, dialog?.right ?? innerWidth, innerWidth) + 1 &&
          bounds.height <= 40
        )
      }),
    )
    .toBe(true)
  if (truncated) {
    const text = control.locator('span').first()
    await expect(text).toHaveAttribute('title', libraries.map((source) => source.name).join(', '))
    expect(
      await text.evaluate(
        (element) =>
          element.scrollWidth > element.clientWidth &&
          getComputedStyle(element).textOverflow === 'ellipsis',
      ),
    ).toBe(true)
  }
}

test('long selections stay inside Stremio, Torznab, sharing and torrent-search fields without losing source choices', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { Origin: server.origin, 'X-CSRF-Token': session.csrf_token }
  for (const library of libraries)
    expect(
      (
        await page.request.put(`/api/providers/${library.id}`, {
          headers,
          data: {
            revision: '',
            json: JSON.stringify({
              version: 1,
              ...library,
              adapter: 'http_json',
              url: server.sourceURL,
              enabled: false,
            }),
          },
        })
      ).status(),
    ).toBe(200)
  for (const [path, endpoint, connection, credential] of [
    ['/addon/settings', '/api/stremio/connections', true, 'manifest URL'],
    ['/torznab', '/api/torznab/endpoints', true, 'API key'],
    ['/sharing/settings', '/api/shares', false, 'password'],
  ] as const) {
    await page.goto(path)
    await page
      .getByRole('button', { name: connection ? 'Create access' : 'Create share', exact: true })
      .click()
    const editor = page.getByRole('dialog')
    await editor
      .getByLabel(connection ? 'Access name' : 'Recipient / share name', { exact: true })
      .fill('Layout example')
    await editor.locator('summary', { hasText: /^Filters$/ }).click()
    const sources = editor.getByRole('button', { name: 'Sources', exact: true })
    await selectEveryOption(page, sources)
    for (const label of ['Media types', 'Services']) {
      const control = editor.getByRole('button', { name: label, exact: true })
      if (await control.count()) await selectEveryOption(page, control)
    }
    for (const width of [320, 390, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await expectBounded(sources, true)
      for (const control of await editor.locator('button[aria-haspopup="menu"]').all())
        await expectBounded(control)
      await sources.click()
      await expect(page.getByRole('menuitemcheckbox')).toHaveCount(libraries.length)
      await expect(page.getByRole('menuitemcheckbox', { checked: true })).toHaveCount(
        libraries.length,
      )
      expect(
        await page
          .getByRole('menu')
          .evaluate((element) => element.getBoundingClientRect().right <= innerWidth + 1),
      ).toBe(true)
      await page.keyboard.press('Escape')
      if (process.env.QA_DIRECTORY && path === '/addon/settings')
        await page.screenshot({
          path: `${process.env.QA_DIRECTORY}/manifest-selection-${width}.png`,
        })
    }
    await editor
      .getByRole('button', {
        name: connection ? 'Create disabled access' : 'Create disabled share',
        exact: true,
      })
      .click()
    await page.getByRole('button', { name: `I have saved the ${credential}`, exact: true }).click()
    expect((await (await page.request.get(endpoint)).json()).items[0].source_ids).toEqual(
      libraries.map((source) => source.id),
    )
  }
  await page.goto('/torrents')
  const form = page.getByRole('form', { name: 'Torrent search', exact: true })
  const sources = form.getByRole('button', { name: 'Sources', exact: true })
  await selectEveryOption(page, sources)
  for (const width of [320, 390, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await expectBounded(sources, true)
  }
  await form.getByRole('button', { name: 'Search', exact: true }).click()
  await expect
    .poll(() => new URL(page.url()).searchParams.getAll('provider'))
    .toEqual(libraries.map((source) => source.id))
})

test('public Stremio and Torznab keep long source selections bounded on mobile and desktop', async ({
  page,
  server,
  browser,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { Origin: server.origin, 'X-CSRF-Token': session.csrf_token }
  for (const library of libraries)
    expect(
      (
        await page.request.put(`/api/providers/${library.id}`, {
          headers,
          data: {
            revision: '',
            json: JSON.stringify({
              version: 1,
              ...library,
              adapter: 'http_json',
              url: server.sourceURL,
              enabled: false,
            }),
          },
        })
      ).status(),
    ).toBe(200)
  const access = await (await page.request.get('/api/public-access')).json()
  expect(
    (
      await page.request.put('/api/public-access', {
        headers,
        data: { ...access, enabled: true, torznab_enabled: true, expose_sources: true },
      })
    ).status(),
  ).toBe(204)
  const context = await browser.newContext({ baseURL: server.origin })
  await context.route('**/*', (route) =>
    new URL(route.request().url()).origin === server.origin
      ? route.continue()
      : route.abort('blockedbyclient'),
  )
  try {
    const visitor = await context.newPage()
    for (const path of ['/', '/public/torznab']) {
      await visitor.goto(path)
      await visitor.locator('summary', { hasText: /^Filters$/ }).click()
      const sources = visitor.getByRole('button', { name: 'Sources', exact: true })
      await selectEveryOption(visitor, sources)
      for (const label of ['Media types', 'Services'])
        await selectEveryOption(visitor, visitor.getByRole('button', { name: label, exact: true }))
      for (const width of [320, 390, 1440]) {
        await visitor.setViewportSize({ width, height: 900 })
        await expectBounded(sources, true)
        for (const control of await visitor.locator('button[aria-haspopup="menu"]').all())
          await expectBounded(control)
        expect(
          await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        ).toBe(true)
      }
    }
  } finally {
    await context.close()
  }
})
