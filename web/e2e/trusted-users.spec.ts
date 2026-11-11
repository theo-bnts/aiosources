import { test, expect } from './fixtures'

test('trusted users have the visitor workspace with instance services and no administration', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/trusted-users')
  await expect(page.getByRole('heading', { name: 'Trusted', exact: true, level: 1 })).toBeVisible()
  const breadcrumb = page.getByRole('navigation', { name: 'Breadcrumb', exact: true })
  await expect(breadcrumb).toContainText('Users')
  await page.getByRole('button', { name: 'Add user', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Add trusted user', exact: true })
  await expect(
    dialog.getByRole('switch', { name: 'Enable account', exact: true }),
  ).not.toBeChecked()
  await dialog.getByLabel('Email', { exact: true }).fill('reader@example.invalid')
  try {
    await dialog.getByLabel('Password', { exact: true }).fill('reader-' + server.password)
  } catch {
    throw new Error('Could not fill the isolated trusted-user credential.')
  }
  await dialog.getByRole('switch', { name: 'Enable account', exact: true }).check()
  const permissions = dialog
    .locator('details')
    .filter({ has: page.locator('summary').getByText('Permissions', { exact: true }) })
  await expect(permissions).not.toHaveAttribute('open', '')
  await permissions.locator('summary').click()
  await expect(
    dialog.getByRole('switch', { name: 'Allow shared catalogs', exact: true }),
  ).toBeDisabled()
  for (const name of [
    'Allow Search',
    'Allow Torznab connections',
    'Allow Stremio connections',
    'Expose source names and filters',
    'Allow shared catalogs',
  ])
    await dialog.getByRole('switch', { name, exact: true }).check()
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(
    page.getByRole('heading', { name: 'reader@example.invalid', exact: true }),
  ).toBeVisible()
  if (process.env.QA_DIRECTORY)
    await page.screenshot({
      path: `${process.env.QA_DIRECTORY}/trusted-settings-desktop.png`,
      fullPage: true,
      animations: 'disabled',
    })
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/user-login')
    const email = visitor.getByLabel('Email', { exact: true })
    await expect(email).toHaveAttribute('type', 'email')
    await expect(email).toHaveAttribute('autocomplete', 'username')
    await email.fill('reader@example.invalid')
    try {
      await visitor.getByLabel('Password', { exact: true }).fill('reader-' + server.password)
    } catch {
      throw new Error('Could not fill the isolated trusted-user credential.')
    }
    await visitor.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(
      visitor.getByRole('heading', { name: 'Stremio', exact: true, level: 1 }),
    ).toBeVisible()
    const navigation = visitor.getByRole('navigation', { name: 'Main navigation', exact: true })
    await expect(navigation.getByRole('link')).toHaveText([
      'Search',
      'Torznab',
      'Stremio',
      'Shared catalogs',
    ])
    await expect(navigation.getByRole('link', { name: 'Administration', exact: true })).toHaveCount(
      0,
    )
    await expect(
      visitor.getByRole('heading', { name: 'Connect your debrid services', exact: true }),
    ).toHaveCount(0)
    await visitor.getByRole('button', { name: 'Create installation link', exact: true }).click()
    const manifest = await visitor.getByLabel('Manifest URL', { exact: true }).inputValue()
    expect((await context.request.get(new URL(manifest).pathname)).status()).toBe(200)
    expect((await context.request.get('/api/secrets')).status()).toBe(401)
    // Restore the role after intentionally probing an admin-only endpoint.
    await visitor.reload()
    await navigation.getByRole('link', { name: 'Search', exact: true }).click()
    await expect(visitor.getByLabel('Title, author or identifier', { exact: true })).toBeVisible()
    await expect(visitor.getByLabel('AllDebrid API key', { exact: true })).toHaveCount(0)
    await visitor.getByLabel('Title, author or identifier', { exact: true }).fill('Example')
    await visitor.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(visitor.getByText('No matching titles', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    const edit = page.getByRole('dialog', { name: 'Edit trusted user', exact: true })
    await edit.getByRole('switch', { name: 'Enable account', exact: true }).uncheck()
    await edit.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(edit).toBeHidden()
    expect((await context.request.get(new URL(manifest).pathname)).status()).toBe(401)
    await visitor.reload()
    await expect(
      visitor.getByRole('heading', { name: 'Trusted user sign-in', exact: true }),
    ).toBeVisible()
  } finally {
    await context.close()
  }
})

test('trusted account controls fit on mobile and saved permissions remain explicit', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/trusted-users')
  await page.getByRole('button', { name: 'Add user', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Add trusted user', exact: true })
  await dialog.getByLabel('Email', { exact: true }).fill('mobile.reader@example.invalid')
  try {
    await dialog.getByLabel('Password', { exact: true }).fill('reader-' + server.password)
  } catch {
    throw new Error('Could not fill the isolated trusted-user credential.')
  }
  await dialog.locator('summary').filter({ hasText: 'Permissions' }).click()
  await dialog.getByRole('switch', { name: 'Allow Torznab connections', exact: true }).check()
  // Credential managers can populate the native field without a React event.
  try {
    await dialog.getByLabel('Password', { exact: true }).evaluate((element, password) => {
      ;(element as HTMLInputElement).value = password
    }, 'reader-' + server.password)
  } catch {
    throw new Error('Could not populate the isolated trusted-user credential.')
  }
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(dialog).toBeHidden()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  const edit = page.getByRole('dialog', { name: 'Edit trusted user', exact: true })
  await expect(edit.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(edit.getByRole('switch', { name: 'Enable account', exact: true })).not.toBeChecked()
  await expect(edit.getByLabel('Replace password', { exact: true })).toHaveAttribute(
    'autocomplete',
    'new-password',
  )
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
  ).toBeLessThanOrEqual(0)
  const widths = await edit.evaluate((node) => ({
    content: node.scrollWidth,
    visible: node.clientWidth,
  }))
  expect(widths.content - widths.visible).toBeLessThanOrEqual(1)
  if (process.env.QA_DIRECTORY)
    await page.screenshot({
      path: `${process.env.QA_DIRECTORY}/trusted-editor-mobile.png`,
      fullPage: true,
      animations: 'disabled',
    })
})

test('trusted personal credentials reuse the public forms and remember announces separately from anonymous visitors', async ({
  page,
  server,
  browser,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { Origin: server.origin, 'X-CSRF-Token': session.csrf_token }
  expect(
    (
      await page.request.put('/api/providers/private-example', {
        headers,
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'private-example',
            name: 'Example Library',
            adapter: 'json',
            url: server.sourceURL,
            enabled: false,
          }),
        },
      })
    ).status(),
  ).toBe(200)
  await page.goto('/trusted-users')
  await page.getByRole('button', { name: 'Add user', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Add trusted user', exact: true })
  await editor.getByLabel('Email', { exact: true }).fill('personal.reader@example.invalid')
  await editor.getByLabel('Password', { exact: true }).fill('synthetic personal password')
  await editor.getByRole('switch', { name: 'Enable account', exact: true }).check()
  await editor.locator('summary').filter({ hasText: 'Permissions' }).click()
  for (const name of [
    'Allow Search',
    'Allow Torznab connections',
    'Allow Stremio connections',
    'Expose source names and filters',
  ])
    await editor.getByRole('switch', { name, exact: true }).check()
  await editor.locator('summary').filter({ hasText: 'Credentials' }).click()
  for (const name of ['Use instance debrid accounts', 'Use instance announce URLs']) {
    const option = editor.getByRole('switch', { name, exact: true })
    await expect(option).toBeChecked()
    await option.uncheck()
  }
  await editor.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(editor).toBeHidden()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/user-login')
    await visitor.getByLabel('Email', { exact: true }).fill('personal.reader@example.invalid')
    await visitor.getByLabel('Password', { exact: true }).fill('synthetic personal password')
    await visitor.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(visitor.getByLabel('TorBox API key', { exact: true })).toBeVisible()
    const navigation = visitor.getByRole('navigation', { name: 'Main navigation', exact: true })
    await navigation.getByRole('link', { name: 'Torznab', exact: true }).click()
    await visitor.locator('summary').filter({ hasText: 'Private source playback' }).click()
    const own = 'https://announce.example.invalid/synthetic-personal-passkey/announce'
    const announce = visitor.getByLabel('Example Library announce URL', { exact: true })
    await announce.fill(own)
    await visitor.getByRole('button', { name: 'Create connection link', exact: true }).click()
    await expect(visitor.getByLabel('API key', { exact: true })).not.toHaveValue('')
    const key = await visitor.getByLabel('API key', { exact: true }).inputValue()
    expect(
      (
        await context.request.get('/trusted/torznab/api?t=caps&apikey=' + encodeURIComponent(key))
      ).status(),
    ).toBe(200)
    await expect(announce).toHaveValue('')
    await expect(announce).toHaveAttribute(
      'placeholder',
      'Saved on this browser · enter a URL to replace it',
    )
    const stored = await visitor.evaluate(() => JSON.stringify(localStorage))
    expect(stored).not.toContain(own)
    expect(stored).not.toContain('synthetic personal password')
    expect(stored).toContain('aiosources.public.credentials.trusted:')
    expect(
      await visitor.evaluate(() => localStorage.getItem('aiosources.public.credentials')),
    ).toBeNull()
    await navigation.getByRole('link', { name: 'Stremio', exact: true }).click()
    await visitor.locator('summary').filter({ hasText: 'Private source playback' }).click()
    await expect(
      visitor.getByLabel('Example Library announce URL', { exact: true }),
    ).toHaveAttribute('placeholder', 'Saved on this browser · enter a URL to replace it')
    await visitor.getByRole('button', { name: 'Create installation link', exact: true }).click()
    await expect(
      visitor.getByText('Enter at least one service key.', { exact: true }),
    ).toBeVisible()
    expect((await context.request.get('/api/secrets')).status()).toBe(401)
  } finally {
    await context.close()
  }
})

test('Public offers instance credentials only after explicit administrator authorization', async ({
  page,
  server,
  browser,
}) => {
  await page.goto('/public-access')
  const form = page.getByRole('form', { name: 'Public', exact: true })
  await form.getByRole('switch', { name: 'Enable public access', exact: true }).check()
  await form.locator('summary').filter({ hasText: 'Credentials' }).click()
  for (const name of ['Use instance debrid accounts', 'Use instance announce URLs']) {
    const option = form.getByRole('switch', { name, exact: true })
    await expect(option).not.toBeChecked()
    await option.check()
  }
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/public')
    await expect(
      visitor.getByRole('heading', { name: 'Create installation link', exact: true }),
    ).toBeVisible()
    await expect(visitor.getByLabel('TorBox API key', { exact: true })).toHaveCount(0)
    await expect(
      visitor.locator('summary').filter({ hasText: 'Private source playback' }),
    ).toHaveCount(0)
    await visitor.getByRole('button', { name: 'Create installation link', exact: true }).click()
    await expect(visitor.getByLabel('Manifest URL', { exact: true })).not.toHaveValue('')
    const manifest = await visitor.getByLabel('Manifest URL', { exact: true }).inputValue()
    expect((await context.request.get(new URL(manifest).pathname)).status()).toBe(200)
    await form.getByRole('switch', { name: 'Use instance debrid accounts', exact: true }).uncheck()
    await form.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
    expect((await context.request.get(new URL(manifest).pathname)).status()).toBe(401)
    await visitor.reload()
    await expect(visitor.getByLabel('TorBox API key', { exact: true })).toBeVisible()
  } finally {
    await context.close()
  }
})
