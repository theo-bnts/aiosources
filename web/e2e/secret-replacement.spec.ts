import { test, expect } from './fixtures'
import type { SecretInfo } from '../src/lib/types'

test('inline key replacement preserves the reference and handles conflicts and lost responses', async ({
  page,
  server,
}) => {
  await page.goto('/debrid/settings')
  const form = page.getByRole('form', { name: 'Debrid configuration', exact: true })
  await form.getByRole('button', { name: 'Add AllDebrid key', exact: true }).click()
  let editor = page.getByRole('dialog')
  await editor.getByLabel('AllDebrid API key', { exact: true }).fill('synthetic-first-key')
  await editor.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(editor).toBeHidden()
  const reference = await form.getByLabel('AllDebrid API key', { exact: true }).inputValue()
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const original = (await (await page.request.get('/api/aiosource/controls')).json()).config
  const metadata = async () => {
    const response = await page.request.get('/api/secrets')
    const value: { items: SecretInfo[] } = await response.json()
    expect(value.items).toHaveLength(1)
    return value.items[0]
  }
  const initial = await metadata()
  await page.setViewportSize({ width: 320, height: 900 })
  await form.getByRole('button', { name: 'Replace AllDebrid key', exact: true }).click()
  editor = page.getByRole('dialog', { name: 'Replace AllDebrid key', exact: true })
  await expect(editor).toContainText('applies immediately to every configuration')
  await expect(editor.getByLabel('AllDebrid API key', { exact: true })).toHaveValue('')
  await editor.getByLabel('AllDebrid API key', { exact: true }).fill('synthetic-replacement-key')
  const replacement = page.waitForRequest(
    (request) => request.method() === 'PUT' && request.url().endsWith(`/secrets/${reference}`),
  )
  await editor.getByRole('button', { name: 'Confirm replacement', exact: true }).click()
  expect((await replacement).postDataJSON()).toEqual({
    value: 'synthetic-replacement-key',
    revision: initial.revision,
  })
  await expect(editor).toBeHidden()
  expect((await metadata()).revision).toBeGreaterThan(initial.revision)
  await expect(form.getByLabel('AllDebrid API key', { exact: true })).toHaveValue(reference)
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await (await page.request.get('/api/aiosource/controls')).json()).config).toEqual(
    original,
  )

  // A second administrator changes the same entry after the editor opens.
  await form.getByRole('button', { name: 'Replace AllDebrid key', exact: true }).click()
  editor = page.getByRole('dialog', { name: 'Replace AllDebrid key', exact: true })
  await expect(editor).toBeVisible()
  const observed = await metadata()
  const session = await (await page.request.get('/api/session')).json()
  expect(
    (
      await page.request.put(`/api/secrets/${reference}`, {
        headers: { Origin: server.origin, 'X-CSRF-Token': session.csrf_token },
        data: { value: 'synthetic-other-administrator-key', revision: observed.revision },
      })
    ).status(),
  ).toBe(204)
  await editor.getByLabel('AllDebrid API key', { exact: true }).fill('synthetic-reviewed-key')
  await editor.getByRole('button', { name: 'Confirm replacement', exact: true }).click()
  await expect(editor).toContainText('This secret changed elsewhere.')
  await expect(
    editor.getByRole('button', { name: 'Confirm replacement', exact: true }),
  ).toBeDisabled()
  await expect(editor.getByLabel('AllDebrid API key', { exact: true })).toHaveValue(
    'synthetic-reviewed-key',
  )
  await editor.getByRole('button', { name: 'Load latest revision', exact: true }).click()
  await expect(
    editor.getByRole('button', { name: 'Confirm replacement', exact: true }),
  ).toBeEnabled()

  // A lost response must not weaken the revision check. Review the latest
  // revision before confirming again, without creating a second reference.
  let lostResponse = false
  await page.route(`**/api/secrets/${reference}`, async (route) => {
    if (route.request().method() === 'PUT' && !lostResponse) {
      lostResponse = true
      expect((await route.fetch()).status()).toBe(204)
      await route.abort('failed')
    } else await route.continue()
  })
  await editor.getByRole('button', { name: 'Confirm replacement', exact: true }).click()
  await expect(page.locator('[data-connection-notice]')).toContainText('The server is unreachable.')
  await expect(editor.getByLabel('AllDebrid API key', { exact: true })).toHaveValue(
    'synthetic-reviewed-key',
  )
  const committed = await metadata()
  await editor.getByRole('button', { name: 'Confirm replacement', exact: true }).click()
  await expect(editor).toContainText('This secret changed elsewhere.')
  expect((await metadata()).revision).toBe(committed.revision)
  await editor.getByRole('button', { name: 'Load latest revision', exact: true }).click()
  await expect(
    editor.getByRole('button', { name: 'Confirm replacement', exact: true }),
  ).toBeEnabled()
  await editor.getByRole('button', { name: 'Confirm replacement', exact: true }).click()
  await expect(editor).toBeHidden()
  expect((await metadata()).revision).toBeGreaterThan(committed.revision)
  expect((await (await page.request.get('/api/aiosource/controls')).json()).config).toEqual(
    original,
  )
  const listing = await (await page.request.get('/api/secrets')).text()
  for (const value of [
    'synthetic-first-key',
    'synthetic-replacement-key',
    'synthetic-other-administrator-key',
    'synthetic-reviewed-key',
  ])
    expect(listing).not.toContain(value)
  await form.getByRole('button', { name: 'Replace AllDebrid key', exact: true }).click()
  await expect(
    page.getByRole('dialog').getByLabel('AllDebrid API key', { exact: true }),
  ).toHaveValue('')
  await page.keyboard.press('Escape')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
