import { test, expect } from './fixtures'

test('Storage saving is the default, edits are explicit and data reset requires confirmation', async ({
  page,
}) => {
  await page.goto('/health')
  const retention = page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'Data retention', exact: true }) })
  const mode = retention.getByLabel('Stored torrent information', { exact: true })
  await expect(mode).toHaveValue('compact')
  await expect(
    retention.getByText('Applies to imports started after saving.', { exact: false }),
  ).toBeVisible()
  const save = retention.getByRole('button', { name: 'Save', exact: true })
  await expect(save).toBeDisabled()
  await mode.selectOption('full')
  await expect(
    retention.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toBeDisabled()
  await save.click()
  await expect(retention.getByText('Changes saved.', { exact: true })).toBeVisible()
  await page.reload()
  await expect(mode).toHaveValue('full')
  await retention.getByRole('button', { name: 'Reset all source data', exact: true }).click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog).toContainText(
    'Source definitions, secrets, schedules, sharing permissions, TMDB information and debrid caches are retained.',
  )
  await expect(dialog).toContainText('This cannot be undone.')
  await dialog.getByRole('button', { name: 'Back', exact: true }).click()
  await expect(dialog).toBeHidden()
  await mode.selectOption('compact')
  await save.click()
  await expect(retention.getByText('Changes saved.', { exact: true })).toBeVisible()
  await retention.getByRole('button', { name: 'Reset all source data', exact: true }).click()
  await dialog.getByRole('button', { name: 'Reset source data', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(mode).toHaveValue('compact')
  await expect(
    retention.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toBeEnabled()
})

test('Storage settings report stale edits without silently overwriting another administrator', async ({
  page,
  server,
}) => {
  await page.goto('/health')
  const mode = page.getByLabel('Stored torrent information', { exact: true })
  await expect(mode).toHaveValue('compact')
  await mode.selectOption('full')
  const current = await (await page.request.get('/api/settings/storage')).json()
  const session = await (await page.request.get('/api/session')).json()
  const updated = await page.request.put('/api/settings/storage', {
    headers: { Origin: server.origin, 'X-CSRF-Token': session.csrf_token },
    data: current.settings,
  })
  expect(updated.ok()).toBe(true)
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(
    page.getByText('Storage settings changed elsewhere.', { exact: false }),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Reload saved value', exact: true }).click()
  await expect(mode).toHaveValue('compact')
  await expect(page.getByText('Storage settings changed elsewhere.', { exact: false })).toBeHidden()
})
