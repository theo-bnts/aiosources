import type { Page } from '@playwright/test'
import type { NetworkSettings } from '../src/lib/network-types'
import { test, expect } from './fixtures'

test('Public access separates Search and connection settings, while Network expands into Incoming and Outgoing', async ({
  page,
}) => {
  await page.goto('/public-access')
  const form = page.getByRole('form', { name: 'Public access', exact: true })
  const search = form.getByRole('group', { name: 'Search', exact: true })
  const connections = form.getByRole('group', { name: 'Connections', exact: true })
  await expect(
    search.getByRole('switch', { name: 'Allow public Search', exact: true }),
  ).toBeVisible()
  await expect(connections.getByRole('switch')).toHaveCount(2)
  await expect(connections.getByText('Allow public Search', { exact: true })).toHaveCount(0)
  if (process.env.QA_DIRECTORY)
    await page.screenshot({
      path: `${process.env.QA_DIRECTORY}/public-access-settings-desktop.png`,
    })
  const enabled = form.getByRole('switch', { name: 'Enable public access', exact: true })
  const initial = await enabled.isChecked()
  await enabled.click()
  await page.getByRole('tab', { name: 'Request limits', exact: true }).click()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(enabled).toBeChecked({ checked: !initial })
  await enabled.click()
  await page.getByRole('tab', { name: 'Request limits', exact: true }).click()
  await expect(page).toHaveURL(/\/public-access\/limits$/)
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toBeVisible()
  const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  const network = navigation.getByRole('button', { name: 'Network', exact: true })
  if ((await network.getAttribute('aria-expanded')) !== 'true') await network.click()
  await expect(navigation.getByRole('link', { name: 'Incoming', exact: true })).toBeVisible()
  await navigation.getByRole('link', { name: 'Outgoing', exact: true }).click()
  await expect(
    page.getByRole('tablist', { name: 'Outgoing views', exact: true }).getByRole('tab'),
  ).toHaveText(['Proxies', 'Routing'])
  await page.getByRole('tab', { name: 'Routing', exact: true }).click()
  await expect(navigation.getByRole('link', { name: 'Outgoing', exact: true })).toHaveAttribute(
    'aria-current',
    'page',
  )
  await expect(
    page.getByRole('navigation', { name: 'Breadcrumb', exact: true }).getByRole('listitem'),
  ).toHaveText(['Administration/', 'Network/', 'Outgoing'])
  if (process.env.QA_DIRECTORY)
    await page.screenshot({ path: `${process.env.QA_DIRECTORY}/network-outgoing-desktop.png` })
  await page.reload()
  await expect(network).toHaveAttribute('aria-expanded', 'true')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  if (process.env.QA_DIRECTORY) {
    await page.screenshot({ path: `${process.env.QA_DIRECTORY}/network-outgoing-mobile.png` })
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
    await page.screenshot({ path: `${process.env.QA_DIRECTORY}/network-navigation-mobile.png` })
  }
})

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
  await page.goto('/network/outgoing')
  await expect(page.getByRole('heading', { name: 'Outgoing', exact: true, level: 1 })).toBeVisible()
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
  await page.goto('/network/outgoing')
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
  await page.getByRole('button', { name: 'Discard changes and reload', exact: true }).click()
  await expect(page.getByLabel('Outgoing connections', { exact: true })).toHaveValue('system')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('incoming proxy trust and public quotas use coherent forms and apply to both public protocols', async ({
  page,
}) => {
  await page.goto('/network/incoming')
  await expect(page).toHaveURL(/\/network\/incoming$/)
  await expect(page.getByRole('heading', { name: 'Trusted proxy', exact: true })).toBeVisible()
  await expect(page.getByRole('tablist')).toHaveCount(0)
  await expect(
    page.getByRole('switch', { name: 'Trust an incoming proxy header', exact: true }),
  ).not.toBeChecked()
  await page.getByLabel('IP address header', { exact: true }).fill('CF-Connecting-IP')
  await expect(page.getByLabel('Header format', { exact: true })).toHaveValue('single')
  await page.getByLabel('Trusted proxy addresses', { exact: true }).fill('127.0.0.1\n::1')
  await page.getByRole('switch', { name: 'Trust an incoming proxy header', exact: true }).click()
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await settings(page)).incoming).toEqual({
    enabled: true,
    ip_header: 'CF-Connecting-IP',
    format: 'single',
    trusted_proxies: ['127.0.0.1', '::1'],
  })
  const diagnostics = await page.request.get('/api/settings/network/incoming', {
    headers: { 'CF-Connecting-IP': '203.0.113.7' },
  })
  expect(await diagnostics.json()).toEqual({
    connection_ip: '127.0.0.1',
    visitor_ip: '203.0.113.7',
  })
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toHaveCount(0)
  await page
    .getByRole('navigation', { name: 'Main navigation', exact: true })
    .getByRole('link', { name: 'Public access', exact: true })
    .click()
  await page.getByRole('tab', { name: 'Request limits', exact: true }).click()
  await expect(page.getByLabel('IP address header', { exact: true })).toHaveCount(0)
  const form = page.getByRole('form', { name: 'Request limits', exact: true })
  for (const [label, value] of [
    ['Link creations per minute', '12'],
    ['Public requests per minute', '120'],
    ['Simultaneous public requests', '50'],
  ])
    await expect(form.getByLabel(label, { exact: true })).toHaveValue(value)
  await form.getByLabel('Public requests per minute', { exact: true }).fill('2')
  await form.getByLabel('Simultaneous public requests', { exact: true }).fill('7')
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const saved = await settings(page)
  expect(saved.public_limits).toEqual({
    setup_per_minute: 12,
    requests_per_minute: 2,
    concurrent: 7,
  })
  expect(saved.incoming).toEqual({
    enabled: true,
    ip_header: 'CF-Connecting-IP',
    format: 'single',
    trusted_proxies: ['127.0.0.1', '::1'],
  })
  const visitor = { 'CF-Connecting-IP': '203.0.113.8' }
  expect(
    (
      await page.request.get('/public/stremio/invalid/manifest.json', { headers: visitor })
    ).status(),
  ).toBe(401)
  expect(
    (
      await page.request.get('/public/torznab/api?apikey=invalid&t=caps', { headers: visitor })
    ).status(),
  ).toBe(401)
  const limited = await page.request.get('/public/stremio/invalid/manifest.json', {
    headers: visitor,
  })
  expect(limited.status()).toBe(429)
  expect(Number(limited.headers()['retry-after'])).toBeGreaterThan(0)
  expect(
    (
      await page.request.get('/public/stremio/invalid/manifest.json', {
        headers: { 'CF-Connecting-IP': '203.0.113.9' },
      })
    ).status(),
  ).toBe(401)
  await page.reload()
  await expect(form.getByLabel('Public requests per minute', { exact: true })).toHaveValue('2')
  await page.goto('/public-access')
  await expect(
    page.getByRole('navigation', { name: 'Breadcrumb', exact: true }).getByRole('listitem'),
  ).toHaveText(['Administration/', 'Public access'])
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toHaveCount(0)
})

test('incoming settings preserve unsaved edits and stay within a mobile viewport', async ({
  page,
}) => {
  async function openSection(label: string) {
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
    const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
    if (label === 'Incoming') {
      const network = navigation.getByRole('button', { name: 'Network', exact: true })
      if ((await network.getAttribute('aria-expanded')) !== 'true') await network.click()
    }
    await navigation.getByRole('link', { name: label, exact: true }).click()
  }
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/network/incoming')
  await page.getByLabel('IP address header', { exact: true }).fill('X-Real-IP')
  await page
    .getByLabel('Trusted proxy addresses', { exact: true })
    .fill('2001:db8:1234:5678:90ab:cdef:1234:5678\n10.0.0.0/24')
  await openSection('Public access')
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByLabel('IP address header', { exact: true })).toHaveValue('X-Real-IP')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await openSection('Public access')
  await page.getByRole('tab', { name: 'Request limits', exact: true }).click()
  await expect(page.getByLabel('Simultaneous public requests', { exact: true })).toHaveValue('50')
  await page.getByLabel('Public requests per minute', { exact: true }).fill('80')
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toHaveValue('80')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await openSection('Incoming')
  await expect(page.getByLabel('IP address header', { exact: true })).toHaveValue('X-Real-IP')
  expect((await settings(page)).public_limits.requests_per_minute).toBe(80)
})

test('public access saves preserve network quotas and concurrent request limit edits require explicit review', async ({
  page,
}) => {
  await page.goto('/public-access')
  const exposure = page.getByRole('form', { name: 'Public access', exact: true })
  await exposure.getByRole('switch', { name: 'Enable public access', exact: true }).click()
  const network = await settings(page)
  const response = await page.request.put('/api/settings/network', {
    headers: await mutationHeaders(page),
    data: { ...network, public_limits: { ...network.public_limits, requests_per_minute: 333 } },
  })
  expect(response.status()).toBe(200)
  await exposure.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(exposure.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  const access = await (await page.request.get('/api/public-access')).json()
  expect(access.enabled).toBe(true)
  expect(access).not.toHaveProperty('limits')
  expect((await settings(page)).public_limits.requests_per_minute).toBe(333)
  await page.goto('/public-access/limits')
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toHaveValue('333')
  await page.getByLabel('Public requests per minute', { exact: true }).fill('444')
  const latest = await settings(page)
  expect(
    (
      await page.request.put('/api/settings/network', {
        headers: await mutationHeaders(page),
        data: { ...latest, public_limits: { ...latest.public_limits, concurrent: 70 } },
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
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toHaveValue('444')
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Discard changes and reload', exact: true }).click()
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toHaveValue('333')
  await expect(page.getByLabel('Simultaneous public requests', { exact: true })).toHaveValue('70')
})

test('each public request limit can be switched off while active limits and incomplete drafts stay protected', async ({
  page,
}) => {
  await page.goto('/public-access/limits')
  const limit = page.getByRole('switch', { name: 'Public requests per minute limit', exact: true })
  const requests = page.getByLabel('Public requests per minute', { exact: true })
  await requests.fill('1')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await limit.click()
  await expect(requests).toBeDisabled()
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await settings(page)).public_limits).toEqual({
    setup_per_minute: 12,
    requests_per_minute: 0,
    concurrent: 50,
  })
  for (let i = 0; i < 3; i++)
    expect((await page.request.get('/public/stremio/invalid/manifest.json')).status()).toBe(401)
  await page.reload()
  await expect(limit).not.toBeChecked()
  await expect(requests).toBeDisabled()
  await expect(requests).toHaveAttribute('placeholder', 'Unlimited')
  await limit.click()
  await requests.fill('')
  await expect(limit).toBeChecked()
  await expect(requests).toBeEditable()
  await page.getByRole('tab', { name: 'Settings', exact: true }).click()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved changes', exact: true })
  await expect(guard).toBeVisible()
  await guard.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await requests.fill('1')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await page.request.get('/public/stremio/invalid/manifest.json')).status()).toBe(401)
  expect((await page.request.get('/public/stremio/invalid/manifest.json')).status()).toBe(429)
  await page.getByRole('switch', { name: 'Link creations per minute limit', exact: true }).click()
  await page
    .getByRole('switch', { name: 'Simultaneous public requests limit', exact: true })
    .click()
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await settings(page)).public_limits).toEqual({
    setup_per_minute: 0,
    requests_per_minute: 1,
    concurrent: 0,
  })
})
