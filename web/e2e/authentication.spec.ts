import { test, expect } from './fixtures'

test('password-manager form values work for sign-in and verification without change events', async ({
  browser,
  server,
}) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  try {
    await page.goto(server.origin + '/security')
    const password = page.getByLabel('Administrator password', { exact: true })
    await expect(password).toHaveAttribute('type', 'password')
    await expect(password).toHaveAttribute('name', 'password')
    await expect(password).toHaveAttribute('autocomplete', 'current-password')
    const code = page.getByLabel('Authenticator or recovery code', { exact: true })
    await expect(code).toHaveAttribute('type', 'text')
    await expect(code).toHaveAttribute('name', 'code')
    await expect(code).toHaveAttribute('autocomplete', 'one-time-code')
    await expect(code).toHaveAttribute('maxlength', '64')
    await expect(page.getByText(/Sessions use an HttpOnly cookie/)).toHaveCount(0)

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
    await expect(page.getByRole('heading', { name: 'Security', exact: true })).toBeVisible()
    await page.getByRole('tab', { name: 'Two-step verification', exact: true }).click()
    await page.getByRole('switch', { name: 'Two-step verification', exact: true }).click()
    const dialog = page.getByRole('dialog')
    const current = dialog.getByLabel('Current password', { exact: true })
    await expect(current).toHaveAttribute('type', 'password')
    await expect(current).toHaveAttribute('name', 'password')
    await expect(current).toHaveAttribute('autocomplete', 'current-password')
    try {
      await current.evaluate((element, value) => {
        ;(element as HTMLInputElement).value = value
      }, server.password)
    } catch {
      throw new Error('Could not populate the isolated verification credential.')
    }
    await dialog.getByRole('button', { name: 'Generate key', exact: true }).click()
    const factor = dialog.getByLabel('Authenticator code', { exact: true })
    await expect(factor).toHaveAttribute('type', 'text')
    await expect(factor).toHaveAttribute('name', 'code')
    await expect(factor).toHaveAttribute('autocomplete', 'one-time-code')
    await expect(factor).toHaveAttribute('inputmode', 'numeric')
    await expect(factor).toHaveAttribute('pattern', '[0-9]{6}')
    await factor.fill('012345')
    await expect(factor).toHaveValue('012345')
  } finally {
    await context.close()
  }
})
