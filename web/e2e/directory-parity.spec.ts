import { remoteDraft, remoteJSON } from '../src/lib/remote-config'
import type { Page } from '@playwright/test'
import pg from 'pg'
import { test, expect } from './fixtures'

async function seedDirectories(page: Page, origin: string, sourceURL: string, longNames = false) {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { Origin: origin, 'X-CSRF-Token': session.csrf_token }
  for (const name of ['alpha_secret', 'beta_secret'])
    expect(
      (
        await page.request.put(`/api/secrets/${name}`, {
          headers,
          data: { value: 'synthetic-unused-credential', revision: 0 },
        })
      ).ok(),
    ).toBe(true)
  const records = []
  for (const [id, name] of [
    ['one', 'Beta 10'],
    ['two', longNames ? 'Alpha'.repeat(28) : 'Alpha 2'],
  ]) {
    const source = { version: 1, id, name, adapter: 'json', enabled: false, url: sourceURL }
    expect(
      (
        await page.request.put(`/api/providers/${id}`, {
          headers,
          data: { revision: '', json: JSON.stringify(source) },
        })
      ).ok(),
    ).toBe(true)
    const remote = JSON.parse(
      remoteJSON(
        undefined,
        {
          ...remoteDraft(),
          id: `remote-${id}`,
          name,
          url: `https://catalog.example/api/catalogs/${id}`,
          enabled: false,
          autoSync: false,
          intervalMinutes: '30',
        },
        'alpha_secret',
      ),
    ) as typeof source
    expect(
      (
        await page.request.put(`/api/providers/${remote.id}`, {
          headers,
          data: { revision: '', json: JSON.stringify(remote) },
        })
      ).ok(),
    ).toBe(true)
    records.push({ source, remote })
    for (const endpoint of ['/api/stremio/connections', '/api/torznab/endpoints', '/api/shares']) {
      const options = await (await page.request.get(endpoint)).json()
      expect(
        (
          await page.request.post(endpoint, {
            headers,
            data: {
              name: name.slice(0, 115),
              enabled: false,
              scope: 'all',
              source_ids: [],
              fields:
                endpoint === '/api/shares'
                  ? ['title', 'info_hash', 'published_at']
                  : options.fields,
              ...(endpoint === '/api/stremio/connections'
                ? { stream_preferences: { language: 'en', mode: 'all' } }
                : {}),
              expires_at: null,
              requests_per_minute: 120,
              max_concurrent_downloads: 2,
            },
          })
        ).ok(),
      ).toBe(true)
    }
  }
  const network = await (await page.request.get('/api/settings/network')).json()
  expect(
    (
      await page.request.put('/api/settings/network', {
        headers,
        data: {
          ...network,
          proxies: records.map(({ source }) => ({
            id: `proxy-${source.id}`,
            name: source.name,
            url: `socks5://proxy-${source.id}:40000`,
            enabled: false,
          })),
        },
      })
    ).ok(),
  ).toBe(true)
  return { headers, records }
}

const directories = [
  ['/providers', 'Source filters', 'Find a source', 'sources'],
  ['/providers/settings', 'Source filters', 'Find a source', 'sources'],
  ['/remotes', 'Remote catalog filters', 'Find a remote catalog', 'remote catalogs'],
  ['/remotes/settings', 'Remote catalog filters', 'Find a remote catalog', 'remote catalogs'],
  ['/addon/settings', 'Access filters', 'Find an access key', 'access keys'],
  ['/torznab', 'Access filters', 'Find an access key', 'access keys'],
  ['/sharing', 'Share filters', 'Find a share', 'shares'],
  ['/sharing/settings', 'Share filters', 'Find a share', 'shares'],
  ['/network/outgoing', 'Proxy filters', 'Find a proxy', 'proxies'],
  ['/security/settings#secrets', 'Secret filters', 'Find a secret', 'secrets'],
] as const

test('directories share query, status, counts, clearing and reload behavior without saving configuration', async ({
  page,
  server,
}) => {
  await seedDirectories(page, server.origin, server.sourceURL)
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  for (const [path, label, queryLabel, plural] of directories) {
    await page.goto(path)
    const controls = page.getByRole('region', { name: label, exact: true })
    await expect(controls.getByRole('status')).toHaveText(`Showing 2 of 2 ${plural}`)
    await controls.getByLabel(queryLabel, { exact: true }).fill('ALPHA')
    await expect(controls.getByRole('status')).toHaveText(`Showing 1 of 2 ${plural}`)
    if (plural !== 'secrets')
      await controls.getByLabel('Status', { exact: true }).selectOption('disabled')
    await page.reload()
    await expect(controls.getByLabel(queryLabel, { exact: true })).toHaveValue('ALPHA')
    await expect(controls.getByRole('status')).toHaveText(`Showing 1 of 2 ${plural}`)
    await controls.getByLabel(queryLabel, { exact: true }).fill('unavailable fixture')
    await expect(controls.getByRole('status')).toHaveText(`Showing 0 of 2 ${plural}`)
    await controls.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect(controls.getByRole('status')).toHaveText(`Showing 2 of 2 ${plural}`)
    await expect(controls.getByLabel(queryLabel, { exact: true })).toHaveValue('')
  }
  expect(writes).toEqual([])
})

test('directory names and controls stay bounded on mobile and desktop including statistics', async ({
  page,
  server,
}) => {
  await seedDirectories(page, server.origin, server.sourceURL, true)
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    for (const [path, label] of directories) {
      await page.goto(path)
      await expect(page.getByRole('region', { name: label, exact: true })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
  }
})

test('saved connection summaries expose the same applied controls and credential actions', async ({
  page,
  server,
}) => {
  const { headers } = await seedDirectories(page, server.origin, server.sourceURL)
  const preferences = {
    max_results: 80,
    max_cached: 20,
    response_seconds: 12,
    sorting: [
      { field: 'cached', direction: 'desc' },
      { field: 'size', direction: 'desc' },
    ],
  }
  const base = {
    enabled: false,
    scope: 'all',
    source_ids: [],
    fields: ['title', 'info_hash', 'published_at'],
    expires_at: null,
    requests_per_minute: 0,
    max_concurrent_downloads: 0,
  }
  const stremioFields = (await (await page.request.get('/api/stremio/connections')).json()).fields
  const torznabFields = (await (await page.request.get('/api/torznab/endpoints')).json()).fields
  const stremio = await page.request.post('/api/stremio/connections', {
    headers,
    data: {
      ...base,
      fields: stremioFields,
      name: 'Configured Stremio',
      stream_preferences: { language: 'en', mode: 'all', ...preferences },
    },
  })
  expect(stremio.ok()).toBe(true)
  const body = {
    ...base,
    fields: torznabFields,
    name: 'Configured Torznab',
    media_types: ['movie', 'tv'],
    torznab_preferences: preferences,
  }
  const response = await page.request.post('/api/torznab/endpoints', { headers, data: body })
  expect(response.ok()).toBe(true)
  const saved = await response.json()
  for (const [path, name, limits, timeout] of [
    [
      '/addon/settings',
      'Configured Stremio',
      'Up to 80 results · Cache target: 20',
      'Stream response timeout',
    ],
    ['/torznab', 'Configured Torznab', 'Up to 80 results', 'Response timeout'],
  ]) {
    await page.goto(path)
    const card = page.getByRole('region', { name: `Access ${name}`, exact: true })
    await expect(card.getByText(limits, { exact: true })).toBeVisible()
    await expect(
      card.getByText('Cached status (descending) → Size (descending)', { exact: true }),
    ).toBeVisible()
    await expect(card.getByText(timeout, { exact: true })).toBeVisible()
    await expect(card.getByText('12 seconds', { exact: true })).toBeVisible()
    await expect(card.getByText(/Responses are limited to/)).toHaveCount(0)
  }
  expect(
    (
      await page.request.put(`/api/torznab/endpoints/${saved.share.id}`, {
        headers,
        data: {
          ...body,
          revision: saved.share.revision,
          torznab_preferences: { ...preferences, cached_only: true },
        },
      })
    ).ok(),
  ).toBe(true)
  await page.reload()
  const card = page.getByRole('region', { name: 'Access Configured Torznab', exact: true })
  await expect(card.getByText('Up to 20 cached results', { exact: true })).toBeVisible()
  await expect(card.getByText('Up to 80 results', { exact: true })).toHaveCount(0)
  const replace = card.getByRole('button', { name: 'Replace API key', exact: true })
  await expect(replace).toHaveAttribute('data-variant', 'warning')
  await replace.click()
  const confirmation = page.getByRole('alertdialog')
  await expect(confirmation).toContainText('previous API key')
  await confirmation.getByRole('button', { name: 'Back', exact: true }).click()
  await page.goto('/sharing/settings')
  const share = page.getByRole('region', { name: 'Share Beta 10', exact: true })
  await expect(share.getByText('Published fields', { exact: true })).toBeVisible()
  await expect(share.getByText('Title, Info hash, Publication date', { exact: true })).toBeVisible()
  await expect(
    share.getByRole('button', { name: 'Replace password', exact: true }),
  ).toHaveAttribute('data-variant', 'warning')
})

test('history filters keep visible labels and align their clear action with the selected fields', async ({
  page,
}) => {
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    for (const [path, ids] of [
      ['/runs?status=failed', ['run-provider', 'run-status']],
      ['/remotes/history?status=failed', ['run-provider', 'run-status']],
      ['/metadata/videos/history?status=failed', ['operation-status']],
      [
        '/debrid/history?status=failed',
        ['operation-type', 'debrid-operation-kind', 'operation-status'],
      ],
      ['/addon/history?status=empty', ['stremio-resource', 'stremio-outcome']],
    ] as const) {
      await page.goto(path)
      for (const id of ids) {
        const label = page.locator(`label[for="${id}"]`)
        await expect(label).toBeVisible()
        expect((await label.boundingBox())!.height).toBeGreaterThanOrEqual(16)
      }
      const field = await page.locator(`[id="${ids[ids.length - 1]}"]`).boundingBox()
      const clear = await page
        .getByRole('button', { name: 'Clear filters', exact: true })
        .boundingBox()
      if (width > 1000)
        expect(Math.abs(field!.y + field!.height - clear!.y - clear!.height)).toBeLessThan(2)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
  }
})

test('secret JSON help belongs to Advanced and settings search reveals it without changing credentials', async ({
  page,
}) => {
  const writes: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'DELETE'].includes(request.method()))
      writes.push(new URL(request.url()).pathname)
  })
  await page.goto('/security/settings')
  const help = page.locator('#secret-reference-help')
  await expect(help.locator('xpath=ancestor::details')).toHaveCount(1)
  await expect(help.locator(':scope > summary')).toBeHidden()
  await page.getByRole('button', { name: 'Search settings', exact: true }).click()
  const search = page.getByRole('dialog', { name: 'Search settings', exact: true })
  await search.getByRole('combobox').fill('auth.secret_ref')
  await search.getByRole('option', { name: /Using references in JSON/ }).click()
  await expect(help).toHaveAttribute('open', '')
  await expect(help).toBeFocused()
  await expect(help).toContainText('auth.secret_ref')
  expect(writes).toEqual([])
})

test('private Torznab accepts the configured HTTP origin while catalog sharing still requires HTTPS', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { Origin: server.origin, 'X-CSRF-Token': session.csrf_token }
  const options = await (await page.request.get('/api/torznab/endpoints')).json()
  expect(options.public_url).toBe(server.origin)
  expect(
    (
      await page.request.post('/api/torznab/endpoints', {
        headers,
        data: {
          name: 'Local access',
          enabled: false,
          scope: 'all',
          fields: options.fields,
          requests_per_minute: 0,
          max_concurrent_downloads: 0,
        },
      })
    ).status(),
  ).toBe(201)
  await page.goto('/torznab')
  await expect(
    page.getByRole('heading', { name: 'HTTPS public address required', exact: true }),
  ).toHaveCount(0)
  const card = page.getByRole('region', { name: 'Access Local access', exact: true })
  const enable = card.getByRole('switch', { name: 'Enable Local access', exact: true })
  await expect(enable).toBeEnabled()
  await enable.click()
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Enable access', exact: true })
    .click()
  await expect(card.getByRole('switch')).toBeChecked()
  const saved = await (await page.request.get('/api/torznab/endpoints')).json()
  expect(saved.items.find((item: { name: string }) => item.name === 'Local access').enabled).toBe(
    true,
  )
  expect(
    (
      await page.request.post('/api/shares', {
        headers,
        data: {
          name: 'Catalog access',
          enabled: false,
          scope: 'all',
          fields: ['title'],
          requests_per_minute: 120,
          max_concurrent_downloads: 2,
        },
      })
    ).status(),
  ).toBe(201)
  await page.goto('/sharing/settings')
  await expect(
    page.getByRole('heading', { name: 'HTTPS public address required', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('region', { name: 'Share Catalog access', exact: true }).getByRole('switch'),
  ).toBeDisabled()
})

test.describe('import directory summaries', () => {
  test.use({ collectionSource: true })
  test('sources and remote catalogs present the last import and its error in the same place', async ({
    page,
    server,
  }) => {
    const { records } = await seedDirectories(page, server.origin, server.sourceURL)
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      for (const [kind, config] of [
        ['source', records[0].source],
        ['remote', records[0].remote],
      ] as const)
        await db.query(
          `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,error,finished_at) VALUES($1,$2,$3,'full','failed','',$4,'synthetic import failure.',now())`,
          [`${kind}-failure`, config.id, config.name, JSON.stringify(config)],
        )
    } finally {
      await db.end()
    }
    for (const [path, name, id] of [
      ['/providers/settings', 'Source Beta 10', 'source-failure'],
      ['/remotes/settings', 'Remote catalog Beta 10', 'remote-failure'],
    ]) {
      await page.goto(path)
      const card = page.getByRole('region', { name, exact: true })
      const last = card
        .locator('dl > div')
        .filter({ has: page.getByText('Last run', { exact: true }) })
      await expect(last.getByText('Synthetic import failure.', { exact: true })).toBeVisible()
      await expect(card.getByText('Synthetic import failure.', { exact: true })).toHaveCount(1)
      await expect(last.getByRole('link')).toHaveAttribute(
        'href',
        `${path.startsWith('/remotes') ? '/remotes/history' : '/runs'}/${id}`,
      )
    }
    expect(server.sourceRequests()).toBe(0)
  })
})
