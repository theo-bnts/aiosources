import { join } from 'node:path'
import { test, expect, addRemotePassword } from './fixtures'

test('catalog share additional data is opt-in, persisted and safe on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/sharing/settings')
  await page.getByRole('button', { name: 'Create share', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Recipient / share name', { exact: true }).fill('Example recipient')
  await dialog.getByText('Additional data', { exact: true }).click()
  for (const name of ['Film and series information', 'Debrid cache observations'])
    await expect(dialog.getByRole('switch', { name, exact: true })).not.toBeChecked()
  await dialog.getByRole('switch', { name: 'Film and series information', exact: true }).check()
  await dialog.getByRole('switch', { name: 'Debrid cache observations', exact: true }).check()
  await dialog.getByText('Filters', { exact: true }).click()
  await dialog.getByLabel('info_hash', { exact: true }).uncheck()
  await expect(
    dialog.getByRole('switch', { name: 'Debrid cache observations', exact: true }),
  ).toBeDisabled()
  await expect(
    dialog.getByRole('switch', { name: 'Debrid cache observations', exact: true }),
  ).not.toBeChecked()
  await dialog.getByLabel('info_hash', { exact: true }).check()
  await dialog.getByRole('switch', { name: 'Debrid cache observations', exact: true }).check()
  for (const width of [390, 1280]) {
    await page.setViewportSize({ width, height: 844 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    if (process.env.QA_DIRECTORY)
      await page.screenshot({
        path: join(process.env.QA_DIRECTORY, `catalog-sharing-${width}.png`),
      })
  }
  await dialog.getByRole('button', { name: 'Create disabled share', exact: true }).click()
  await page.getByRole('button', { name: 'I have saved the password', exact: true }).click()
  const saved = (await (await page.request.get('/api/shares')).json()).items
  expect(saved).toHaveLength(1)
  expect(saved[0].enabled).toBe(false)
  expect(saved[0].catalog_entities).toEqual({ metadata: true, availability: true })
  await expect(page.getByText('Torrents · Metadata · Debrid cache', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await dialog.getByText('Additional data', { exact: true }).click()
  await expect(
    dialog.getByRole('switch', { name: 'Film and series information', exact: true }),
  ).toBeChecked()
  await expect(
    dialog.getByRole('switch', { name: 'Debrid cache observations', exact: true }),
  ).toBeChecked()
  await expect(dialog.getByRole('button', { name: 'Save permissions', exact: true })).toBeDisabled()
})

test('remote catalogs request additional data only after an explicit choice', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/remotes/settings')
  await page.getByRole('button', { name: 'Add remote catalog', exact: true }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add remote catalog', exact: true })
  await dialog.getByLabel('Local source identifier', { exact: true }).fill('example-remote')
  await dialog.getByLabel('Display name', { exact: true }).fill('Example remote')
  await dialog
    .getByLabel('HTTPS catalog URL', { exact: true })
    .fill('https://catalog.example/api/catalogs/example')
  await addRemotePassword(page, 'synthetic-sharing-password')
  await dialog.getByText('Additional data', { exact: true }).click()
  for (const name of ['Film and series information', 'Debrid cache observations']) {
    const control = dialog.getByRole('switch', { name, exact: true })
    await expect(control).not.toBeChecked()
    await control.check()
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  if (process.env.QA_DIRECTORY)
    await page.screenshot({ path: join(process.env.QA_DIRECTORY, 'catalog-reception-390.png') })
  await dialog.getByRole('button', { name: 'Create remote catalog', exact: true }).click()
  await expect(dialog).toBeHidden()
  const document = await (await page.request.get('/api/providers/example-remote')).json()
  expect(document.provider.enabled).toBe(false)
  expect(document.provider.catalog_entities).toEqual({ metadata: true, availability: true })
  const card = page.getByRole('region', { name: 'Remote catalog Example remote', exact: true })
  await expect(card.getByText('Not synchronized yet', { exact: true })).toBeVisible()
  await card.getByRole('button', { name: 'Edit', exact: true }).click()
  const edit = page.getByRole('dialog', { name: 'Edit remote catalog', exact: true })
  await edit.getByText('Additional data', { exact: true }).click()
  await expect(
    edit.getByRole('switch', { name: 'Film and series information', exact: true }),
  ).toBeChecked()
  await expect(edit.getByRole('button', { name: 'Save remote', exact: true })).toBeDisabled()
})
