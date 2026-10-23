import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect, createStremioAccess } from './fixtures'
import { runtimeSections } from '../src/lib/runtime-settings-definitions'
import { settingsCatalog, settingsHash } from '../src/lib/settings-catalog'

test('settings search is local, keyboard accessible and reveals lazy advanced controls', async ({
  page,
}) => {
  const calls: string[] = []
  page.on('request', (request) =>
    calls.push(`${request.method()} ${new URL(request.url()).pathname}`),
  )
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Search settings', exact: true })
  const input = dialog.getByRole('combobox')
  await expect(input).toBeFocused()
  const before = calls.length
  await input.fill('Queued torrents')
  await expect(dialog.getByRole('option').first()).toContainText('Queued torrents per pass')
  expect(calls.slice(before)).toEqual([])
  await input.press('Enter')
  const field = page.getByLabel('Queued torrents per pass', { exact: true })
  await expect(field).toBeVisible()
  await expect(field).toBeFocused()
  await expect(field).toHaveAttribute('data-setting-highlight', '')
  await expect(page).toHaveURL(/\/metadata\/settings#setting=runtime.identification.queue_batch$/)
  expect(calls.filter((call) => /^(POST|PUT|DELETE) /.test(call))).toEqual([])
  await page.reload()
  await expect(field).toBeVisible()
  await expect(field).toBeFocused()
  await page.keyboard.press('Control+k')
  await expect(dialog).toBeVisible()
  await input.fill('unavailable xyz setting')
  await expect(dialog).toContainText('No settings match')
  await input.press('Escape')
  await expect(page.getByRole('button', { name: 'Search settings', exact: true })).toBeFocused()
})

test('every operational search target opens its field without changing saved settings', async ({
  page,
}) => {
  for (const section of Object.keys(runtimeSections)) {
    const setting = settingsCatalog.find((setting) => setting.id.startsWith(`runtime.${section}.`))!
    await page.goto(`${setting.route}${settingsHash(setting.id)}`)
    const field = page.locator(`[id="${setting.control}"]`)
    await expect(field).toBeVisible()
    await expect(field).toBeFocused()
  }
  await page.goto('/providers/settings?settings=advanced#setting=imports.workers')
  await expect(page.getByLabel('Simultaneous imports', { exact: true })).toBeFocused()
})

test('general settings keep resolvable destinations including disabled controls and security sections', async ({
  page,
}) => {
  test.setTimeout(60_000)
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  for (const setting of settingsCatalog.filter(
    (setting) => !setting.public && !setting.entity && !setting.id.startsWith('runtime.'),
  )) {
    await page.goto(`${setting.route}${settingsHash(setting.id)}`)
    const target = setting.control
      ? page.locator(`[id="${setting.control}"]`)
      : page.getByLabel(setting.label ?? setting.title, { exact: true })
    const button = page.getByRole('button', { name: setting.label ?? setting.title, exact: true })
    await expect(target.or(button)).toBeVisible()
    if (await target.count()) {
      await expect(target).toBeVisible()
      await expect
        .poll(() =>
          target.evaluate(
            (element) =>
              element === document.activeElement ||
              element.parentElement?.contains(document.activeElement),
          ),
        )
        .toBe(true)
    } else await expect(button).toBeFocused()
  }
  expect(writes).toEqual([])
})

test('search navigation keeps the ordinary unsaved-changes guard and preferences open on the chosen control', async ({
  page,
}) => {
  await page.goto('/metadata/settings')
  const rate = page.getByLabel('TMDB requests per second', { exact: true })
  await rate.fill('17')
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
  await search.getByRole('combobox').fill('Cache validity')
  await search.getByRole('option').first().click()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes' })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(rate).toHaveValue('17')
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  await search.getByRole('combobox').fill('Date format')
  await search.getByRole('option').first().click()
  const preferences = page.getByRole('dialog', { name: 'Interface preferences', exact: true })
  await expect(preferences.getByLabel('Date format', { exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(rate).toHaveValue('17')
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  await search.getByRole('combobox').fill('Cache validity')
  await search.getByRole('option').first().click()
  await guard.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(page.getByLabel('Cache validity (minutes)', { exact: true })).toBeFocused()
})

test('a connection setting chooses the existing access and opens its exact filter', async ({
  page,
}) => {
  await createStremioAccess(page, 'Living room')
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
  await search.getByRole('combobox').fill('stremio preferred language')
  await search.getByRole('option').first().click()
  const choose = page.getByRole('dialog', { name: 'Choose Stremio access', exact: true })
  await choose.getByRole('option', { name: /Living room/ }).click()
  const editor = page.getByRole('dialog', { name: 'Edit permissions & limits', exact: true })
  await expect(editor.getByLabel('Preferred language', { exact: true })).toBeVisible()
  await expect(editor.getByLabel('Preferred language', { exact: true })).toBeDisabled()
  await expect(editor.getByLabel('Preferred language', { exact: true }).locator('..')).toBeFocused()
  await expect(editor.getByRole('button', { name: 'Save permissions', exact: true })).toBeDisabled()
})

test('settings search remains reachable on narrow screens and does not overflow', async ({
  page,
}) => {
  for (const width of [320, 390, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/health/settings')
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))
    const trigger = page.getByRole('button', { name: 'Search settings', exact: true })
    await expect(trigger).toBeInViewport()
    for (const name of ['Interface preferences', 'About AIOSources']) {
      const searchBox = (await trigger.boundingBox())!
      const toolBox = (await page.getByRole('button', { name, exact: true }).boundingBox())!
      expect(
        searchBox.x + searchBox.width <= toolBox.x ||
          searchBox.y >= toolBox.y + toolBox.height ||
          searchBox.y + searchBox.height <= toolBox.y,
      ).toBe(true)
    }
    await trigger.click()
    const dialog = page.getByRole('dialog', { name: 'Search settings', exact: true })
    await dialog.getByRole('combobox').fill('timeout')
    await expect(dialog.getByRole('option').first()).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(
      false,
    )
    if (process.env.QA_DIRECTORY) {
      await mkdir(process.env.QA_DIRECTORY, { recursive: true })
      await page.screenshot({
        path: join(process.env.QA_DIRECTORY, `settings-search-${width}.png`),
        animations: 'disabled',
      })
    }
    await page.keyboard.press('Escape')
  }
})

test('source results select the source and stage, and field drafts cannot be discarded silently', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const definition = {
    version: 1,
    id: 'search-example',
    name: 'Example archive',
    adapter: 'json',
    url: server.sourceURL,
    enabled: false,
    schedule: { every: '30m', enabled: false },
  }
  expect(
    (
      await page.request.put('/api/providers/search-example', {
        headers: { 'X-CSRF-Token': session.csrf_token },
        data: { revision: '', json: JSON.stringify(definition) },
      })
    ).status(),
  ).toBe(200)
  const saved = await (await page.request.get('/api/providers/search-example')).json()
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  await page.getByRole('combobox', { name: 'Find a setting' }).fill('source page budget per tick')
  await page.getByRole('option').first().click()
  const chooser = page.getByRole('dialog', { name: 'Choose source', exact: true })
  await chooser.getByRole('option', { name: /Example archive/ }).click()
  await expect(page.getByRole('tab', { name: 'Schedules', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  const budget = page.getByLabel('Page budget per tick', { exact: true })
  await expect(budget).toBeFocused()
  await budget.fill('broken')
  await page.keyboard.press('Control+k')
  await page.getByRole('combobox', { name: 'Find a setting' }).fill('source request timeout')
  await page.getByRole('option').first().click()
  await chooser.getByRole('option', { name: /Example archive/ }).click()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes' })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await page.keyboard.press('Escape')
  await expect(budget).toHaveValue('broken')
  await page.keyboard.press('Control+k')
  await page.getByRole('combobox', { name: 'Find a setting' }).fill('source request timeout')
  await page.getByRole('option').first().click()
  await chooser.getByRole('option', { name: /Example archive/ }).click()
  await guard.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(page.getByLabel('Request timeout', { exact: true })).toBeFocused()
  expect(await (await page.request.get('/api/providers/search-example')).json()).toEqual(saved)
  // Subsequent same-source navigation preserves ordinary, valid unsaved edits.
  await page.getByLabel('Request timeout', { exact: true }).selectOption('60s')
  await page.keyboard.press('Control+k')
  await page.getByRole('combobox', { name: 'Find a setting' }).fill('source page budget per tick')
  await page.getByRole('option').first().click()
  await chooser.getByRole('option', { name: /Example archive/ }).click()
  await expect(budget).toBeFocused()
  await page.getByRole('tab', { name: 'Imports', exact: true }).click()
  await expect(page.getByLabel('Request timeout', { exact: true })).toHaveValue('60s')
})

test('same-page navigation closes a proxy editor only after its draft is reviewed', async ({
  page,
}) => {
  await page.clock.install()
  await page.goto('/network/outgoing')
  await page.getByRole('button', { name: 'Add proxy', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
  await editor.getByLabel('Name', { exact: true }).fill('Unsaved gateway')
  await page.keyboard.press('Control+k')
  await page.getByRole('combobox', { name: 'Find a setting' }).fill('Metadata request timeout')
  await page.getByRole('option').first().click()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes' })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(editor.getByLabel('Name', { exact: true })).toHaveValue('Unsaved gateway')
  await page.keyboard.press('Control+k')
  await page.getByRole('combobox', { name: 'Find a setting' }).fill('Metadata request timeout')
  await page.getByRole('option').first().click()
  await guard.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(editor).toBeHidden()
  await expect(page.getByLabel('Metadata request timeout (seconds)', { exact: true })).toBeFocused()
  expect((await (await page.request.get('/api/settings/network')).json()).proxies).toEqual([])
  await page.getByRole('button', { name: 'Add proxy', exact: true }).click()
  await editor.getByLabel('Name', { exact: true }).fill('Keep this new draft')
  const session = await (await page.request.get('/api/session')).json()
  const network = await (await page.request.get('/api/settings/network')).json()
  expect(
    (
      await page.request.put('/api/settings/network', {
        headers: { 'X-CSRF-Token': session.csrf_token },
        data: {
          ...network,
          proxies: [
            {
              id: 'other-gateway',
              name: 'Other gateway',
              url: 'http://127.0.0.1:9',
              enabled: false,
            },
          ],
        },
      })
    ).status(),
  ).toBe(200)
  const refreshed = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/settings/network' &&
      response.request().method() === 'GET',
  )
  await page.clock.fastForward(31000)
  await refreshed
  await expect(editor.getByLabel('Name', { exact: true })).toHaveValue('Keep this new draft')
})

test('empty connection lists can open a new configuration through search without creating access', async ({
  page,
}) => {
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  await page.getByRole('combobox', { name: 'Find a setting' }).fill('torznab requests per minute')
  await page.getByRole('option').first().click()
  await page.getByRole('option', { name: 'Create access…', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Create disabled access', exact: true })
  await expect(editor.getByLabel('Requests per minute', { exact: true })).toBeFocused()
  expect(writes).toEqual([])
})

test('remote catalogs, shares and proxy authentication use their existing editors without writes', async ({
  page,
}) => {
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  for (const [query, add, label] of [
    ['remote catalog sharing password', 'Add remote catalog…', 'Sharing password'],
    ['shared catalogs recipient name', 'Create share…', 'Recipient / share name'],
    ['proxy username', 'Add proxy…', 'Username'],
  ]) {
    await page.getByRole('button', { name: 'Search settings', exact: true }).click()
    await page.getByRole('combobox', { name: 'Find a setting' }).fill(query)
    await page.getByRole('option').first().click()
    await page.getByRole('option', { name: add, exact: true }).click()
    await expect(page.getByLabel(label, { exact: true })).toBeFocused()
    await page.keyboard.press('Escape')
  }
  expect(writes).toEqual([])
})

test('mobile section links remain visible below the responsive sticky header', async ({ page }) => {
  for (const width of [320, 768, 1024, 1440, 1920]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/security/settings#verification')
    const section = page.locator('#verification')
    await expect(section).toBeInViewport()
    await expect
      .poll(async () => {
        const header = await page.locator('[data-app-shell] header').first().boundingBox()
        const target = await section.boundingBox()
        return target!.y >= header!.y + header!.height - 1
      })
      .toBe(true)
    const search = (await page
      .getByRole('button', { name: 'Search settings', exact: true })
      .boundingBox())!
    const preferences = (await page
      .getByRole('button', { name: 'Interface preferences', exact: true })
      .boundingBox())!
    expect(
      search.x + search.width <= preferences.x ||
        search.y >= preferences.y + preferences.height ||
        search.y + search.height <= preferences.y,
    ).toBe(true)
  }
})

test('public settings search follows enabled features and never fetches administrator data', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public access', exact: true })
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.getByLabel('Allow public Torznab connections', { exact: true }).check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  const privateCalls: string[] = []
  await context.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (url.origin !== server.origin) return route.abort('blockedbyclient')
    if (
      url.pathname.startsWith('/api/') &&
      !['/api/session', '/api/about'].includes(url.pathname) &&
      !url.pathname.startsWith('/api/public/')
    )
      privateCalls.push(url.pathname)
    return route.continue()
  })
  try {
    const visitor = await context.newPage()
    await visitor.goto('/public/torznab')
    await visitor.getByRole('button', { name: 'Search settings', exact: true }).click()
    const search = visitor.getByRole('dialog', { name: 'Search settings', exact: true })
    await search.getByRole('combobox').fill('minimum close title similarity')
    await expect(search.getByRole('option')).toHaveCount(0)
    await search.getByRole('combobox').fill('sources')
    await expect(search.getByRole('option')).toHaveCount(0)
    await search.getByRole('combobox').fill('torznab cached')
    await search.getByRole('option').first().click()
    await expect(visitor.getByLabel('Cached results only', { exact: true })).toBeFocused()
    await visitor.getByRole('button', { name: 'Search settings', exact: true }).click()
    await search.getByRole('combobox').fill('stremio preferred language')
    await search.getByRole('option').first().click()
    await expect(visitor).toHaveURL(/\/public#setting=visitor.stremio.language$/)
    await expect(visitor.getByLabel('Preferred language', { exact: true })).toBeFocused()
    expect(privateCalls).toEqual([])
  } finally {
    await context.close()
  }
})
