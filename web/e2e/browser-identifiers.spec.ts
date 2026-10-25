import { test, expect } from './fixtures'

// HTTP LAN pages expose getRandomValues but not randomUUID. Loopback is a
// trusted browser context, so reproduce that API shape before the app starts.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true })
  })
})

for (const entry of ['button', 'settings link'] as const) {
  test(`proxy creation through a ${entry} works without secure-context browser APIs`, async ({
    page,
  }) => {
    await page.goto(
      entry === 'button'
        ? '/network/outgoing'
        : '/network/outgoing?configure=new#setting=proxy.url',
    )
    if (entry === 'button')
      await page.getByRole('button', { name: 'Add proxy', exact: true }).click()
    const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
    await expect(editor).toBeVisible()
    // A settings destination focuses its field after the dialog autofocus.
    // Wait for that navigation to finish before typing in a different field.
    if (entry === 'settings link')
      await expect(editor.getByLabel('Proxy URL', { exact: true })).toBeFocused()
    await editor.getByLabel('Name', { exact: true }).fill('Synthetic local gateway')
    await editor.getByLabel('Proxy URL', { exact: true }).fill('socks5://127.0.0.1:1')
    await expect(editor.getByLabel('Name', { exact: true })).toHaveValue('Synthetic local gateway')
    await expect(editor.getByLabel('Proxy URL', { exact: true })).toHaveValue(
      'socks5://127.0.0.1:1',
    )
    const saved = page.waitForResponse(
      (response) =>
        response.request().method() === 'PUT' &&
        new URL(response.url()).pathname === '/api/settings/network',
    )
    await editor.getByRole('button', { name: 'Save', exact: true }).click()
    expect((await saved).status()).toBe(200)
    await expect(editor).toBeHidden()
    const settings = await (await page.request.get('/api/settings/network')).json()
    expect(settings.proxies).toHaveLength(1)
    expect(settings.proxies[0]).toMatchObject({
      name: 'Synthetic local gateway',
      url: 'socks5://127.0.0.1:1',
      enabled: false,
    })
    await expect(
      page.getByRole('region', { name: 'Proxy Synthetic local gateway', exact: true }),
    ).toBeVisible()
  })
}

test('public Search opens without secure-context browser APIs', async ({ page, server }) => {
  const session = await (await page.request.get('/api/session')).json()
  const access = await (await page.request.get('/api/public-access')).json()
  expect(
    (
      await page.request.put('/api/public-access', {
        headers: { Origin: server.origin, 'X-CSRF-Token': session.csrf_token },
        data: { ...access, enabled: true, search_enabled: true },
      })
    ).status(),
  ).toBe(204)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/public/search')
  await expect(page.getByRole('button', { name: 'Start searching', exact: true })).toBeVisible()
  await expect(page.getByLabel('TorBox API key', { exact: true })).toBeVisible()
  expect(
    await page.evaluate(() => ({
      uuid: typeof crypto.randomUUID,
      random: typeof crypto.getRandomValues,
    })),
  ).toEqual({ uuid: 'undefined', random: 'function' })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
