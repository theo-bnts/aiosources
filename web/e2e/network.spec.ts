import type { Page } from '@playwright/test'
import type { NetworkSettings } from '../src/lib/network-types'
import { test, expect } from './fixtures'

async function settings(page: Page): Promise<NetworkSettings> {
  const response = await page.request.get('/api/settings/network')
  expect(response.status()).toBe(200)
  return response.json()
}

async function mutationHeaders(page: Page) {
  const session = await (await page.request.get('/api/session')).json()
  return { Origin: new URL(page.url()).origin, 'X-CSRF-Token': session.csrf_token }
}

test('proxy profiles use Secrets and route services independently without activating on test', async ({
  page,
  server,
}) => {
  await page.goto('/network')
  await expect(page.getByRole('heading', { name: 'Network', exact: true, level: 1 })).toBeVisible()
  await page.getByRole('button', { name: 'Add proxy', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
  await editor.getByLabel('Name', { exact: true }).fill('Personal gateway')
  await editor.getByLabel('Proxy URL', { exact: true }).fill('socks5://127.0.0.1:1')
  await editor.getByText('Authentication', { exact: true }).click()
  for (const [label, value] of [
    ['username', 'synthetic-proxy-user'],
    ['password', 'synthetic-proxy-password'],
  ]) {
    await editor.getByRole('button', { name: `Add ${label}`, exact: true }).click()
    const secret = page.getByRole('dialog', { name: `Add ${label}`, exact: true })
    await secret.getByLabel(`Proxy ${label}`, { exact: true }).fill(value)
    await secret.getByRole('button', { name: 'Store and select', exact: true }).click()
    await expect(secret).toBeHidden()
    await expect(editor.getByRole('button', { name: `Add ${label}`, exact: true })).toBeDisabled()
  }
  await editor.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(editor).toBeHidden()
  const card = page.getByRole('region', { name: 'Proxy Personal gateway', exact: true })
  await expect(card.getByRole('switch', { name: 'Enable Personal gateway' })).not.toBeChecked()
  const initial = await settings(page)
  expect(initial.default).toBe('system')
  expect(initial.proxies).toHaveLength(1)
  expect(initial.proxies[0].enabled).toBe(false)
  expect(initial.proxies[0].username_ref).toBeTruthy()
  expect(initial.proxies[0].password_ref).toBeTruthy()
  expect(JSON.stringify(initial)).not.toContain('synthetic-proxy-')
  const probe = page.waitForResponse((r) => r.url().endsWith('/api/settings/network/test'))
  await card.getByRole('button', { name: 'Test connection', exact: true }).click()
  expect((await probe).status()).toBe(502)
  await expect(
    page.getByText(
      'Proxy connection test failed. Check the proxy address, credentials and network access.',
      { exact: true },
    ),
  ).toBeVisible()
  expect((await settings(page)).proxies[0].enabled).toBe(false)
  await card.getByRole('switch', { name: 'Enable Personal gateway' }).click()
  await expect(card.getByRole('switch', { name: 'Disable Personal gateway' })).toBeChecked()

  const headers = await mutationHeaders(page)
  expect(
    (
      await page.request.put('/api/providers/sample', {
        headers,
        data: {
          json: JSON.stringify({
            version: 1,
            id: 'sample',
            name: 'Sample connection',
            adapter: 'http_json',
            url: server.sourceURL,
            enabled: false,
          }),
          revision: '',
        },
      })
    ).status(),
  ).toBe(200)
  await page.getByRole('tab', { name: 'Routing', exact: true }).click()
  await expect(page.locator('main .section-title')).toHaveText([
    'Default connection',
    'Sources',
    'Metadata',
    'Debrid',
  ])
  await page.getByLabel('AllDebrid', { exact: true }).selectOption(initial.proxies[0].id)
  await page.getByLabel('Sample connection', { exact: true }).selectOption('direct')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.reload()
  await expect(page.getByLabel('AllDebrid', { exact: true })).toHaveValue(initial.proxies[0].id)
  await expect(page.getByLabel('TorBox', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Browser downloads', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Sample connection', { exact: true })).toHaveValue('direct')
  await page.getByRole('tab', { name: 'Proxies', exact: true }).click()
  await expect(card.getByRole('button', { name: 'Delete', exact: true })).toBeDisabled()
  await page.getByRole('tab', { name: 'Routing', exact: true }).click()
  await page.getByLabel('AllDebrid', { exact: true }).selectOption('')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.getByRole('tab', { name: 'Proxies', exact: true }).click()
  await card.getByRole('button', { name: 'Delete', exact: true }).click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Delete proxy', exact: true })
    .click()
  await expect(card).toBeHidden()
  expect((await (await page.request.get('/api/secrets')).json()).items).toHaveLength(2)
  await page.goto('/logs?service=network')
  await expect(page.getByLabel('Source or service', { exact: true })).toHaveValue('service:network')
  await expect(page.getByText('Proxy connection test failed', { exact: true })).toBeVisible()
  expect(await page.locator('main').innerText()).not.toContain('synthetic-proxy-')
})

test('network drafts survive concurrent changes and mobile proxy forms keep shared protections', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/network')
  await page.getByRole('button', { name: 'Add proxy', exact: true }).click()
  const editor = page.getByRole('dialog', { name: 'Add proxy', exact: true })
  await editor.getByLabel('Name', { exact: true }).fill('WARP gateway')
  await editor.getByLabel('Proxy URL', { exact: true }).fill('socks5://warp:40000')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await editor.getByRole('button', { name: 'Back', exact: true }).click()
  const discard = page.getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
  await expect(discard).toBeVisible()
  await discard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(editor.getByLabel('Name', { exact: true })).toHaveValue('WARP gateway')
  await editor.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(editor).toBeHidden()
  await page.getByRole('tab', { name: 'Routing', exact: true }).click()
  await page.getByLabel('Outgoing connections', { exact: true }).selectOption('direct')
  const current = await settings(page)
  expect(
    (
      await page.request.put('/api/settings/network', {
        headers: await mutationHeaders(page),
        data: current,
      })
    ).status(),
  ).toBe(200)
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(
    page.getByText(
      'Network settings changed. Your draft is preserved; discard it to load the saved settings.',
      { exact: true },
    ),
  ).toBeVisible()
  await expect(page.getByLabel('Outgoing connections', { exact: true })).toHaveValue('direct')
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(page.getByLabel('Outgoing connections', { exact: true })).toHaveValue('system')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
