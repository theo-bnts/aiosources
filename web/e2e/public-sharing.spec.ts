import { join } from 'node:path'
import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

test('public shared catalogs follow administrator permissions without saving visitor access', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  const catalog = access.getByRole('switch', { name: 'Allow public shared catalogs', exact: true })
  await expect(catalog).toBeDisabled()
  await access.getByRole('switch', { name: 'Enable public access', exact: true }).check()
  await expect(catalog).toBeDisabled()
  await access.getByRole('switch', { name: 'Expose source names and filters', exact: true }).check()
  await catalog.check()
  await access
    .getByRole('switch', { name: 'Expose source names and filters', exact: true })
    .uncheck()
  await expect(catalog).not.toBeChecked()
  await expect(catalog).toBeDisabled()
  await access.getByRole('switch', { name: 'Expose source names and filters', exact: true }).check()
  await expect(catalog).not.toBeChecked()
  await catalog.check()
  await access
    .getByRole('switch', { name: 'Allow public Stremio connections', exact: true })
    .uncheck()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await openDisclosure(page, 'Shared catalog data')
  const policy = page.getByRole('form', { name: 'Shared catalog data', exact: true })
  await openDisclosure(page, 'Torrents')
  await openDisclosure(page, 'Metadata')
  await openDisclosure(page, 'Debrid')
  for (const name of [
    'Film and series information',
    'Debrid cache observations',
    'Identifiers and episodes',
    'Audio and video information',
    'File information',
  ])
    await policy.getByRole('switch', { name, exact: true }).uncheck()
  await policy.getByRole('button', { name: 'Published fields', exact: true }).click()
  for (const field of ['Size', 'Info hash', 'Publication date', 'Category', 'Categories'])
    await page.getByRole('menuitemcheckbox', { name: field, exact: true }).uncheck()
  await page.keyboard.press('Escape')
  await policy.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(policy.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const configured = await (await page.request.get('/api/public-catalog')).json()
  expect(configured.policy.fields).toEqual(['title'])
  expect(configured.policy.catalog_entities).toEqual({ languages: true })

  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/public')
    await expect(visitor).toHaveURL(/\/public\/sharing$/)
    const nav = visitor.getByRole('navigation', { name: 'Main navigation' })
    await expect(nav.getByRole('button', { name: 'Connections', exact: true })).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Shared catalogs', exact: true })).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Stremio', exact: true })).toHaveCount(0)
    await expect(nav.getByRole('link', { name: 'Torznab', exact: true })).toHaveCount(0)
    await expect(visitor.getByRole('textbox', { name: /API key|announce/ })).toHaveCount(0)
    for (const title of ['Filters', 'Shared data'])
      await expect(
        visitor
          .locator('summary')
          .filter({ hasText: new RegExp(`^${title}$`) })
          .locator('..'),
      ).not.toHaveAttribute('open')
    await openDisclosure(visitor, 'Torrents')
    await expect(
      visitor.getByRole('switch', { name: 'Audio and subtitle languages', exact: true }),
    ).not.toBeChecked()
    await expect(visitor.locator('summary').filter({ hasText: /^Metadata$/ })).toHaveCount(0)
    await expect(visitor.locator('summary').filter({ hasText: /^Debrid$/ })).toHaveCount(0)
    await visitor.getByRole('switch', { name: 'Audio and subtitle languages', exact: true }).check()
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    const endpoint = await visitor.getByLabel('Catalog URL', { exact: true }).inputValue()
    const password = await visitor.getByLabel('Sharing password', { exact: true }).inputValue()
    expect(new URL(endpoint).pathname).toMatch(/^\/public\/catalogs\//)
    expect(password.length).toBeGreaterThan(32)
    await expect(visitor.getByLabel('Sharing password', { exact: true })).toHaveAttribute(
      'type',
      'password',
    )
    const exported = await context.request.get(endpoint, {
      headers: { Authorization: `Bearer ${password}` },
    })
    expect(exported.status()).toBe(200)
    expect(exported.headers()['cache-control']).toBe('no-store')
    expect((await exported.json()).checkpoint).toBeTruthy()
    expect((await (await page.request.get('/api/shares')).json()).items).toEqual([])
    for (const width of [390, 1280]) {
      await visitor.setViewportSize({ width, height: 844 })
      if (width === 390) {
        await visitor.getByRole('button', { name: 'Open navigation', exact: true }).click()
        await expect(
          visitor.getByRole('link', { name: 'Shared catalogs', exact: true }).last(),
        ).toBeVisible()
        await visitor.keyboard.press('Escape')
        await expect(visitor.getByRole('dialog')).toHaveCount(0)
      }
      expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      if (process.env.QA_DIRECTORY)
        await visitor.screenshot({
          path: join(process.env.QA_DIRECTORY, `public-sharing-${width}.png`),
          fullPage: true,
        })
    }
    await catalog.uncheck()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    await expect(
      visitor.getByRole('heading', { name: 'Administrator sign-in', exact: true }),
    ).toBeVisible()
    expect(
      (
        await context.request.get(endpoint, { headers: { Authorization: `Bearer ${password}` } })
      ).status(),
    ).toBe(401)
  } finally {
    await context.close()
  }
})

test('public catalog permissions preserve drafts while reviewing a concurrent administrator change', async ({
  page,
}) => {
  await page.goto('/public-access')
  await openDisclosure(page, 'Shared catalog data')
  await openDisclosure(page, 'Torrents')
  const policy = page.getByRole('form', { name: 'Shared catalog data', exact: true })
  await policy.getByRole('switch', { name: 'Audio and subtitle languages', exact: true }).uncheck()
  const latest = await (await page.request.get('/api/public-catalog')).json()
  const session = await (await page.request.get('/api/session')).json()
  const response = await page.request.put('/api/public-catalog', {
    headers: { 'X-CSRF-Token': session.csrf_token },
    data: {
      ...latest.policy,
      fields: ['title'],
      catalog_entities: { ...latest.policy.catalog_entities, availability: false, files: false },
    },
  })
  expect(response.status()).toBe(204)
  await expect(
    policy.getByRole('button', { name: 'Load latest settings', exact: true }),
  ).toBeVisible()
  await expect(policy.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await policy.getByRole('button', { name: 'Load latest settings', exact: true }).click()
  await expect(
    policy.getByRole('switch', { name: 'Audio and subtitle languages', exact: true }),
  ).not.toBeChecked()
  await expect(
    policy.getByRole('switch', { name: 'File information', exact: true }),
  ).not.toBeChecked()
  await expect(policy.getByRole('button', { name: 'Published fields', exact: true })).toHaveText(
    'Title',
  )
  await policy.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(policy.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const saved = await (await page.request.get('/api/public-catalog')).json()
  expect(saved.policy.fields).toEqual(['title'])
  expect(saved.policy.catalog_entities.languages ?? false).toBe(false)
  expect(saved.policy.catalog_entities.files ?? false).toBe(false)
  expect(saved.policy.catalog_entities.availability ?? false).toBe(false)
})

test('public sharing data choices clear stale output and follow live ceilings', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.getByLabel('Expose source names and filters', { exact: true }).check()
  await access.getByLabel('Allow public shared catalogs', { exact: true }).check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/public/sharing')
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(visitor.getByLabel('Catalog URL', { exact: true })).toBeVisible()
    await openDisclosure(visitor, 'Torrents')
    await visitor.getByRole('switch', { name: 'Audio and subtitle languages', exact: true }).check()
    await expect(visitor.getByLabel('Catalog URL', { exact: true })).toHaveCount(0)
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(visitor.getByLabel('Catalog URL', { exact: true })).toBeVisible()
    await openDisclosure(page, 'Shared catalog data')
    await openDisclosure(page, 'Torrents')
    const policy = page.getByRole('form', { name: 'Shared catalog data', exact: true })
    await policy
      .getByRole('switch', { name: 'Audio and subtitle languages', exact: true })
      .uncheck()
    await policy.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(policy.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    await expect(visitor.getByLabel('Catalog URL', { exact: true })).toHaveCount(0)
    await openDisclosure(visitor, 'Torrents')
    await expect(
      visitor.getByRole('switch', { name: 'Audio and subtitle languages', exact: true }),
    ).toHaveCount(0)
    await expect(
      visitor.getByRole('button', { name: 'Create connection link', exact: true }),
    ).toBeEnabled()
  } finally {
    await context.close()
  }
})
