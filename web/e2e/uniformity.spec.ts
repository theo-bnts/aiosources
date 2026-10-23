import type { Page } from '@playwright/test'
import { test, expect, addRemotePassword } from './fixtures'

async function headers(page: Page, origin: string) {
  const session = await (await page.request.get('/api/session')).json()
  return { 'X-CSRF-Token': session.csrf_token, Origin: origin }
}

async function addSource(
  page: Page,
  origin: string,
  sourceURL: string,
  id: string,
  name: string,
  enabled = false,
) {
  const response = await page.request.put(`/api/providers/${id}`, {
    headers: await headers(page, origin),
    data: {
      revision: '',
      json: JSON.stringify({
        version: 1,
        id,
        name,
        enabled,
        adapter: 'http_json',
        url: sourceURL,
        ...(id === 'beta'
          ? { schedule: { enabled: true, every: '2h', mode: 'incremental', max_pages: 0 } }
          : {}),
      }),
    },
  })
  expect(response.status()).toBe(200)
  return response.json()
}

async function addRemote(page: Page) {
  await page.goto('/remotes/settings')
  await page.getByRole('button', { name: 'Add remote catalog', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Local source identifier', { exact: true }).fill('remote')
  await dialog.getByLabel('Display name', { exact: true }).fill('Remote example')
  await dialog
    .getByLabel('HTTPS catalog URL', { exact: true })
    .fill('https://catalog.example/api/catalogs/example')
  await addRemotePassword(page, 'synthetic-not-used-password')
  await dialog.getByRole('switch', { name: 'Connection enabled', exact: true }).uncheck()
  await dialog.getByRole('button', { name: 'Create remote catalog', exact: true }).click()
  await expect(dialog).toBeHidden()
}

test('every list uses 50 rows while retaining its filters and legacy bookmarks', async ({
  page,
}) => {
  for (const [route, filter, api] of [
    ['/runs', 'status=failed', '/api/runs'],
    ['/metadata/history', 'status=succeeded', '/api/operations'],
    ['/debrid/history', 'service=torbox', '/api/operations'],
    ['/logs', 'service=tmdb', '/api/logs'],
    ['/addon/history', 'status=empty', '/api/stremio/requests'],
    ['/torrents', 'q=missing', '/api/torrents'],
  ]) {
    const request = page.waitForRequest(
      (request) => request.method() === 'GET' && new URL(request.url()).pathname === api,
    )
    await page.goto(`${route}?${filter}&limit=25&offset=100`)
    expect(new URL((await request).url()).searchParams.get('limit')).toBe('50')
    await expect(page.getByLabel('Rows per page', { exact: true })).toHaveCount(0)
    const [key, value] = filter.split('=')
    expect(new URL(page.url()).searchParams.get(key)).toBe(value)
  }
})

test('source directories share filters and a stale activation is reloaded without replaying a write', async ({
  page,
  server,
}) => {
  const alpha = await addSource(page, server.origin, server.sourceURL, 'alpha', 'Alpha local', true)
  await addSource(page, server.origin, server.sourceURL, 'beta', 'Beta local')
  await addRemote(page)
  const views = [
    ['/providers', 'beta', 'disabled', 'Showing 1 of 2 sources'],
    ['/remotes/settings', 'remote', 'disabled', 'Showing 1 of 1 source'],
  ]
  for (const [route, query, state, count] of views) {
    await page.goto(`${route}?q=${query}&state=${state}`)
    const filters = page.getByRole('region', { name: 'Source filters', exact: true })
    await expect(filters.getByRole('status')).toHaveText(count)
    await page.reload()
    await expect(filters.getByLabel('Find a source', { exact: true })).toHaveValue(query)
    await filters.getByLabel('Find a source', { exact: true }).fill('missing source')
    await expect(filters.getByRole('status')).toHaveText(count.replace('Showing 1', 'Showing 0'))
    await filters.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect(filters.getByLabel('Find a source', { exact: true })).toHaveValue('')
    expect(new URL(page.url()).searchParams.has('state')).toBe(false)
  }
  await page.goto('/providers/settings')
  let writes = 0
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/providers/alpha' && request.method() === 'PUT')
      writes++
  })
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/providers/alpha', async (route) => {
    if (route.request().method() === 'GET') await gate
    await route.fallback()
  })
  try {
    const reading = page.waitForRequest(
      (request) =>
        new URL(request.url()).pathname === '/api/providers/alpha' && request.method() === 'GET',
    )
    await page.getByRole('switch', { name: 'Disable Alpha local', exact: true }).click()
    await reading
    const remote = await page.request.put('/api/providers/alpha', {
      headers: await headers(page, server.origin),
      data: { revision: alpha.revision, json: alpha.json.replace('Alpha local', 'Alpha latest') },
    })
    expect(remote.status()).toBe(200)
    release()
    await expect(page.getByRole('alert')).toContainText('This source was changed elsewhere')
    expect(writes).toBe(0)
  } finally {
    release()
    // Keep the forwarding handler until context teardown; the gate is released.
  }
  await page.getByRole('button', { name: 'Reload sources', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await page.getByRole('switch', { name: 'Disable Alpha latest', exact: true }).click()
  await expect
    .poll(
      async () => (await (await page.request.get('/api/providers/alpha')).json()).provider.enabled,
    )
    .toBe(false)
  expect(writes).toBe(1)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByLabel('Find a source', { exact: true }).fill('missing')
  await expect(
    page.getByRole('heading', { name: 'No sources match these filters', exact: true }),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('source, schedule and remote editors disable no-op saves and clear reverted drafts', async ({
  page,
  server,
}) => {
  const original = await addSource(page, server.origin, server.sourceURL, 'beta', 'Beta local')
  await page.goto('/providers/beta')
  const save = page.getByRole('button', { name: 'Save', exact: true })
  await expect(save).toBeDisabled()
  const name = page.getByLabel('Display name', { exact: true })
  await name.fill('Unsaved name')
  await expect(save).toBeEnabled()
  await name.fill('Beta local')
  await expect(save).toBeDisabled()
  await page.getByRole('tab', { name: 'Schedules', exact: true }).click()
  await expect(save).toBeDisabled()
  await page.getByText('Advanced', { exact: true }).click()
  const budget = page.getByLabel('Page budget per tick', { exact: true })
  await budget.fill('12')
  await expect(save).toBeEnabled()
  await budget.fill('0')
  await expect(save).toBeDisabled()
  await expect(page.getByRole('alertdialog')).toHaveCount(0)
  expect((await (await page.request.get('/api/providers/beta')).json()).revision).toBe(
    original.revision,
  )
  await addRemote(page)
  const remote = await (await page.request.get('/api/providers/remote')).json()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  const dialog = page.getByRole('dialog')
  const remoteSave = dialog.getByRole('button', { name: 'Save remote', exact: true })
  await expect(remoteSave).toBeDisabled()
  const remoteName = dialog.getByLabel('Display name', { exact: true })
  await remoteName.fill('Unsaved remote')
  await expect(remoteSave).toBeEnabled()
  await remoteName.fill('Remote example')
  await expect(remoteSave).toBeDisabled()
  const interval = dialog.getByLabel('Synchronization interval (minutes)', { exact: true })
  await interval.fill('120')
  await expect(remoteSave).toBeEnabled()
  await interval.fill('60')
  await expect(remoteSave).toBeDisabled()
  const automatic = dialog.getByRole('switch', { name: 'Automatic synchronization', exact: true })
  await automatic.uncheck()
  await expect(remoteSave).toBeEnabled()
  await automatic.check()
  await expect(remoteSave).toBeDisabled()
  await expect(dialog.getByRole('status')).toHaveText('No unsaved changes')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  expect((await (await page.request.get('/api/providers/remote')).json()).revision).toBe(
    remote.revision,
  )
})

test.describe('nested history pagination', () => {
  test.use({ collectionSource: true })
  test('run tabs and occurrence histories keep independent page sizes and preserve the parent view', async ({
    page,
    server,
  }) => {
    const authorization = await headers(page, server.origin)
    const saved = await page.request.put('/api/providers/paged-source', {
      headers: authorization,
      data: {
        revision: '',
        json: JSON.stringify({
          version: 1,
          id: 'paged-source',
          name: 'Paged source',
          enabled: true,
          adapter: 'http_json',
          url: server.sourceURL,
          pagination: { type: 'none' },
          http: { items_path: '/items' },
          mapping: {
            id: '/id',
            fields: { title: '/title', info_hash: '/info_hash', size: '/size' },
          },
        }),
      },
    })
    expect(saved.status()).toBe(200)
    const started = await page.request.post('/api/runs', {
      headers: authorization,
      data: { provider_id: 'paged-source', mode: 'incremental', max_pages: 1 },
    })
    expect(started.status()).toBe(202)
    const run = await started.json()
    await expect
      .poll(async () => (await (await page.request.get(`/api/runs/${run.id}`)).json()).status)
      .toBe('succeeded')
    await page.goto(
      `/runs/${run.id}?tab=raw&raw_q=Synthetic&raw_limit=25&raw_offset=100&changes_limit=25&changes_offset=100`,
    )
    await expect(page.getByLabel('Rows per page', { exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Return to first page', exact: true }).click()
    expect(new URL(page.url()).searchParams.get('raw_q')).toBe('Synthetic')
    expect(new URL(page.url()).searchParams.get('changes_offset')).toBe('100')
    await page.getByRole('tab', { name: 'Published changes', exact: true }).click()
    await page.getByRole('button', { name: 'Return to first page', exact: true }).click()
    expect(new URL(page.url()).searchParams.has('changes_offset')).toBe(false)
    const torrent = (await (await page.request.get('/api/torrents')).json()).items[0]
    await page.goto(
      `/torrents?limit=25&offset=100&occurrence=${torrent.occurrence_id}&occurrence_tab=history&history_limit=25&history_offset=100&related_limit=100`,
    )
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByLabel('Rows per page', { exact: true })).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Return to first page', exact: true }).click()
    expect(new URL(page.url()).searchParams.has('history_offset')).toBe(false)
    expect(new URL(page.url()).searchParams.get('offset')).toBe('100')
    await dialog.getByRole('tab', { name: 'Related occurrences', exact: true }).click()
    await expect(dialog.getByLabel('Rows per page', { exact: true })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    expect(new URL(page.url()).searchParams.has('history_limit')).toBe(false)
    expect(new URL(page.url()).searchParams.has('related_limit')).toBe(false)
    expect(server.sourceRequests()).toBe(1)
  })
})
