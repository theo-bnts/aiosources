import { test, expect } from './fixtures'

for (const id of ['new', 'settings', 'New', 'Settings']) {
  test(`a valid source identifier named ${id} opens its own settings rather than a navigation page`, async ({
    page,
    server,
  }) => {
    const name = `Reserved ${id} example`
    const session = await (await page.request.get('/api/session')).json()
    const response = await page.request.put(`/api/providers/${id}`, {
      headers: { 'X-CSRF-Token': session.csrf_token },
      data: {
        revision: '',
        json: JSON.stringify({
          version: 1,
          id,
          name,
          adapter: 'json',
          url: server.sourceURL,
          enabled: false,
        }),
      },
    })
    expect(response.status()).toBe(200)
    await page.goto('/providers/settings')
    await page.getByRole('link', { name, exact: true }).click()
    await expect(page.getByLabel('Display name', { exact: true })).toHaveValue(name)
    await page.getByRole('button', { name: 'Search settings', exact: true }).click()
    const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
    await expect(search.getByRole('combobox')).toBeFocused()
    await search.getByRole('combobox').fill('source request timeout')
    await expect(search.getByRole('option').first()).toContainText('Request timeout')
    await search.getByRole('option').first().click()
    await page
      .getByRole('dialog', { name: 'Choose source', exact: true })
      .getByRole('option', { name: new RegExp(name) })
      .click()
    await expect(page.getByLabel('Request timeout', { exact: true })).toBeFocused()
    expect(new URL(page.url()).pathname).toBe(`/providers/id/${id}`)
  })
}
