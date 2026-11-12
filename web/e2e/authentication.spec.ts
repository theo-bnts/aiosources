import { test, expect } from './fixtures'

test('password-manager form values work with password-only administrator sign-in', async ({
  browser,
  server,
}) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  try {
    await page.goto(server.origin + '/login')
    await expect(page.getByRole('tab', { name: 'Trusted user', exact: true })).toHaveAttribute(
      'data-state',
      'active',
    )
    if (process.env.QA_DIRECTORY) {
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/login-desktop.png`,
        fullPage: true,
        animations: 'disabled',
      })
      await page.setViewportSize({ width: 390, height: 844 })
      await page.screenshot({
        path: `${process.env.QA_DIRECTORY}/login-mobile.png`,
        fullPage: true,
        animations: 'disabled',
      })
      await page.setViewportSize({ width: 1440, height: 1100 })
    }
    await page.getByRole('tab', { name: 'Administrator', exact: true }).click()
    const password = page.getByLabel('Administrator password', { exact: true })
    await expect(password).toHaveAttribute('type', 'password')
    await expect(password).toHaveAttribute('name', 'password')
    await expect(password).toHaveAttribute('autocomplete', 'current-password')
    await expect(page.getByLabel('Authenticator or recovery code', { exact: true })).toHaveCount(0)
    // Some credential managers set the native field without a React change.
    // Do not put the temporary password into a failed action's reporter output.
    try {
      await password.evaluate((element, value) => {
        ;(element as HTMLInputElement).value = value
      }, server.password)
    } catch {
      throw new Error('Could not populate the isolated sign-in credential.')
    }
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(
      page.getByRole('heading', { name: 'Torrents', exact: true, level: 1 }),
    ).toBeVisible()
  } finally {
    await context.close()
  }
})

test('switching login tabs clears credentials and keeps the trusted tab as default', async ({
  browser,
  server,
}) => {
  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/user-login')
    await visitor.getByLabel('Email', { exact: true }).fill('reader@example.invalid')
    await visitor.getByLabel('Password', { exact: true }).fill('synthetic personal password')
    await visitor.getByRole('tab', { name: 'Administrator', exact: true }).click()
    await expect(visitor.getByLabel('Administrator password', { exact: true })).toHaveValue('')
    await visitor
      .getByLabel('Administrator password', { exact: true })
      .fill('synthetic admin password')
    await visitor.getByRole('tab', { name: 'Trusted user', exact: true }).click()
    await expect(visitor.getByLabel('Email', { exact: true })).toHaveValue('')
    await expect(visitor.getByLabel('Password', { exact: true })).toHaveValue('')
    await expect(visitor.getByLabel('Authenticator or recovery code', { exact: true })).toHaveCount(
      0,
    )
  } finally {
    await context.close()
  }
})
