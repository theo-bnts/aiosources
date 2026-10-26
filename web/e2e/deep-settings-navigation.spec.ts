import { test, expect } from './fixtures'

const proxyAddress = 'socks5://gateway.example:1080'

test.beforeEach(async ({ page }) => {
  const session = await (await page.request.get('/api/session')).json()
  const network = await (await page.request.get('/api/settings/network')).json()
  const response = await page.request.put('/api/settings/network', {
    headers: { 'X-CSRF-Token': session.csrf_token },
    data: {
      ...network,
      proxies: [
        { id: 'proxy-alpha', name: 'Alpha gateway', url: proxyAddress, enabled: false },
        { id: 'proxy-beta', name: 'Beta gateway', url: proxyAddress, enabled: false },
      ],
    },
  })
  expect(response.status()).toBe(200)
})

test('closing a settings-linked editor keeps directory filters usable without reopening it', async ({
  page,
}) => {
  await page.goto('/network/outgoing?configure=new#setting=proxy.name')
  const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
  await expect(editor.getByLabel('Name', { exact: true })).toBeFocused()
  await editor.getByRole('button', { name: 'Close', exact: true }).click()
  await expect(editor).toHaveCount(0)
  await page.getByLabel('Find a proxy', { exact: true }).fill('Nothing')
  await expect(page.getByText('No proxies match these filters', { exact: true })).toBeVisible()
  await expect(editor).toHaveCount(0)
})

test('settings search preserves a new proxy draft when opening another field in that editor', async ({
  page,
}) => {
  await page.goto('/network/outgoing?configure=new#setting=proxy.name')
  const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
  await expect(editor.getByLabel('Name', { exact: true })).toBeFocused()
  await editor.getByLabel('Name', { exact: true }).fill('Example gateway')
  await editor.getByLabel('Proxy URL', { exact: true }).fill(proxyAddress)
  await page.keyboard.press('Control+k')
  const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
  await expect(search.getByRole('combobox')).toBeFocused()
  await search.getByRole('combobox').fill('proxy username')
  await expect(search.getByRole('option').first()).toContainText('Username')
  await search.getByRole('option').first().click()
  await page
    .getByRole('dialog', { name: 'Choose proxy', exact: true })
    .getByRole('option', { name: /Add proxy/ })
    .click()
  await expect(editor.getByLabel('Username', { exact: true })).toBeVisible()
  await expect(editor.getByLabel('Name', { exact: true })).toHaveValue('Example gateway')
  await expect(editor.getByLabel('Proxy URL', { exact: true })).toHaveValue(proxyAddress)
  await expect(page.getByRole('alertdialog', { name: 'Unsaved changes' })).toHaveCount(0)
})

test('Back between settings-linked editors protects the current unsaved connection', async ({
  page,
}) => {
  await page.goto('/network/outgoing?configure=proxy-alpha#setting=proxy.name')
  const editor = page.getByRole('dialog', { name: 'Edit proxy', exact: true })
  await expect(editor.getByLabel('Name', { exact: true })).toBeFocused()
  await editor.getByRole('button', { name: 'Close', exact: true }).click()
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
  await expect(search.getByRole('combobox')).toBeFocused()
  await search.getByRole('combobox').fill('proxy name')
  await expect(search.getByRole('option').first()).toContainText('Name')
  await search.getByRole('option').first().click()
  await page
    .getByRole('dialog', { name: 'Choose proxy', exact: true })
    .getByRole('option', { name: /Beta gateway/ })
    .click()
  await expect(editor.getByLabel('Name', { exact: true })).toBeFocused()
  await editor.getByLabel('Name', { exact: true }).fill('Unsaved gateway')
  await page.goBack()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(editor.getByLabel('Name', { exact: true })).toHaveValue('Unsaved gateway')
  await expect(page).toHaveURL(/configure=proxy-beta/)
  await page.goBack()
  await guard.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(editor.getByLabel('Name', { exact: true })).toHaveValue('Alpha gateway')
})

test('a field search preserves a manually opened new editor and can reopen it after closing', async ({
  page,
}) => {
  await page.goto('/network/outgoing')
  await page.getByRole('button', { name: 'Add proxy', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
  await expect(editor.getByLabel('Name', { exact: true })).toBeFocused()
  await editor.getByLabel('Name', { exact: true }).fill('Manual draft')
  for (let attempt = 0; attempt < 2; attempt++) {
    await page.keyboard.press('Control+k')
    const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
    await expect(search.getByRole('combobox')).toBeFocused()
    await search.getByRole('combobox').fill('proxy username')
    await expect(search.getByRole('option').first()).toContainText('Username')
    await search.getByRole('option').first().click()
    await page
      .getByRole('dialog', { name: 'Choose proxy', exact: true })
      .getByRole('option', { name: /Add proxy/ })
      .click()
    await expect(editor.getByLabel('Username', { exact: true })).toBeFocused()
    await expect(editor.getByLabel('Name', { exact: true })).toHaveValue(
      attempt === 0 ? 'Manual draft' : '',
    )
    await editor.getByRole('button', { name: 'Close', exact: true }).click()
    if (attempt === 0)
      await page
        .getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
        .getByRole('button', { name: 'Discard changes', exact: true })
        .click()
    await expect(editor).toHaveCount(0)
  }
})

for (const format of ['stremio', 'torznab'] as const) {
  test(`${format} protects unsaved access settings when navigating Back to another access`, async ({
    page,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const endpoint = format === 'stremio' ? '/api/stremio/connections' : '/api/torznab/endpoints'
    const route = format === 'stremio' ? '/addon/settings' : '/torznab'
    const ids: string[] = []
    for (const name of ['First access', 'Second access']) {
      const response = await page.request.post(endpoint, {
        headers: { 'X-CSRF-Token': session.csrf_token },
        data: {
          name,
          enabled: false,
          scope: 'all',
          fields: ['title', 'info_hash'],
          ...(format === 'stremio'
            ? { stream_preferences: { mode: 'all', language: '' } }
            : { torznab_preferences: {} }),
        },
      })
      expect(response.status()).toBe(201)
      ids.push((await response.json()).share.id)
    }
    await page.goto(`${route}?configure=${ids[0]}#setting=${format}.name`)
    const editor = page.getByRole('dialog', { name: 'Edit access', exact: true })
    await expect(editor.getByLabel('Access name', { exact: true })).toBeFocused()
    await editor.getByRole('button', { name: 'Close', exact: true }).click()
    await page.getByRole('button', { name: 'Search settings', exact: true }).click()
    const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
    await expect(search.getByRole('combobox')).toBeFocused()
    await search.getByRole('combobox').fill(`${format} access name`)
    await expect(search.getByRole('option').first()).toContainText('Access name')
    await search.getByRole('option').first().click()
    await page
      .getByRole('dialog', {
        name: format === 'stremio' ? 'Choose Stremio access' : 'Choose Torznab access',
        exact: true,
      })
      .getByRole('option', { name: /Second access/ })
      .click()
    await expect(editor.getByLabel('Access name', { exact: true })).toBeFocused()
    await editor.getByLabel('Access name', { exact: true }).fill('Unsaved access')
    await page.goBack()
    const guard = page.getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
    await expect(guard).toBeVisible()
    await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
    await expect(editor.getByLabel('Access name', { exact: true })).toHaveValue('Unsaved access')
    await page.goBack()
    await guard.getByRole('button', { name: 'Discard changes', exact: true }).click()
    await expect(editor.getByLabel('Access name', { exact: true })).toHaveValue('First access')
  })
}
