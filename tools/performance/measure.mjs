import fs from 'node:fs/promises'
import path from 'node:path'
import crypto from 'node:crypto'
import { performance } from 'node:perf_hooks'
import { chromium, request } from '../../web/node_modules/@playwright/test/index.mjs'
import pg from '../../web/node_modules/pg/lib/index.js'
import { applicationPages } from '../../web/e2e/application-pages.ts'

const root = process.env.PERFORMANCE_WORKSPACE
const origin = process.env.PERFORMANCE_ORIGIN
const samples = Number(process.env.PERFORMANCE_SAMPLES || 5)
if (
  !root ||
  !origin ||
  !process.env.PERFORMANCE_DATABASE_URL ||
  process.env.CI ||
  !Number.isInteger(samples) ||
  samples < 3 ||
  samples > 20
)
  throw new Error('Local performance context required')
const output = []
const diagnostics = []
const digest = (value) => crypto.createHash('sha256').update(JSON.stringify(value)).digest('hex')
const finiteError = (error) =>
  /^(http_\d{3}|empty_result|wrong_result|readiness|browser_error|rendering_error|request_failed|missing_fixture)$/.test(
    error?.message ?? '',
  )
    ? error.message
    : 'operation_failed'

async function measure(id, budget, action, contract = () => 'ok', count = samples) {
  const values = []
  let expected
  try {
    // Warm-up is separate from the measured repetitions and still validates data.
    for (let i = -1; i < count; i++) {
      const started = performance.now()
      const value = await action()
      const elapsed = performance.now() - started
      const current = digest(contract(value))
      if (expected !== undefined && current !== expected) throw new Error('wrong_result')
      expected = current
      if (i >= 0) values.push(Math.round(elapsed * 1000) / 1000)
    }
    output.push({ id, budget_ms: budget, samples_ms: values, contract: expected })
  } catch (error) {
    diagnostics.push({ case: id, message: String(error), stack: error?.stack })
    output.push({
      id,
      budget_ms: budget,
      samples_ms: values.length ? values : [budget],
      error: finiteError(error),
    })
  }
  console.log(JSON.stringify({ case: id, passed: !output.at(-1).error }))
}

function listContract(body) {
  if (!Array.isArray(body?.items) || !Number.isInteger(body.total) || body.total < 0)
    throw new Error('wrong_result')
  if (body.total > 0 && body.items.length === 0) throw new Error('empty_result')
  return { has_items: body.items.length > 0, paginated: typeof body.limit === 'number' }
}

function searchContract(wanted) {
  return (body) => {
    listContract(body)
    if (!body.items.some((item) => String(item.id) === String(wanted)))
      throw new Error('empty_result')
    return { total: body.total, items: body.items.map((item) => [item.id, item.type]) }
  }
}

async function discover() {
  const db = new pg.Client({
    connectionString: process.env.PERFORMANCE_DATABASE_URL,
    statement_timeout: 30_000,
  })
  db.on('error', () => {})
  try {
    await db.connect()
    const works = (
      await db.query(`WITH ids AS MATERIALIZED(SELECT DISTINCT work_id FROM ingest.aio_matches WHERE status='matched' AND work_id IS NOT NULL)
      SELECT DISTINCT ON(w.media_type) w.id,w.media_type,w.imdb_id,w.tmdb_id,w.openlibrary_id,
      COALESCE(NULLIF(w.display_title,''),w.book_data->>'title',t.title) AS title
      FROM ids JOIN ingest.aio_works w ON w.id=ids.work_id
      LEFT JOIN LATERAL(SELECT title FROM ingest.aio_titles WHERE work_id=w.id ORDER BY title LIMIT 1)t ON true
      WHERE NOT w.adult ORDER BY w.media_type,w.id`)
    ).rows
    const result = {
      works,
      sources: (
        await db.query(
          `SELECT DISTINCT provider_id FROM ingest.runs WHERE config->>'id'<>'' ORDER BY provider_id`,
        )
      ).rows,
      runs: (await db.query(`SELECT id FROM ingest.runs ORDER BY pages DESC,id LIMIT 2`)).rows,
      operations: (
        await db.query(
          `SELECT DISTINCT ON(kind,service) id,kind,service FROM ingest.operation_runs ORDER BY kind,service,created_at DESC`,
        )
      ).rows,
      torrent: (
        await db.query(
          `SELECT occurrence_id,fields->>'title' AS title,fields->>'info_hash' AS hash FROM ingest.torrents WHERE length(fields->>'title') BETWEEN 2 AND 500 ORDER BY provider_id,source_id LIMIT 1`,
        )
      ).rows[0],
      raw: (await db.query(`SELECT id FROM ingest.raw_records ORDER BY id DESC LIMIT 1`)).rows[0],
    }
    await fs.writeFile(path.join(root, 'entities-private.json'), JSON.stringify(result), {
      mode: 0o600,
    })
    return result
  } finally {
    await db.end()
  }
}

let browser, api
try {
  const entities = await discover()
  api = await request.newContext({
    baseURL: origin,
    timeout: 12_000,
    extraHTTPHeaders: { Origin: origin },
  })
  let login
  try {
    login = await api.post('/api/login', {
      data: { password: process.env.PERFORMANCE_ADMIN_PASSWORD },
    })
  } catch {
    throw new Error('Private benchmark sign-in failed')
  }
  if (login.status() !== 200) throw new Error('Private benchmark sign-in failed')
  const session = await (await api.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token }
  const get = async (route, extra = {}) => {
    const response = await api.get(route, { timeout: 12_000, ...extra })
    if (response.status() !== 200) throw new Error('http_' + response.status())
    return response.json()
  }
  const q = (text) => encodeURIComponent(text)
  const selections = new Map()
  // Connection credentials remain private inside the disposable workspace.
  const credentialsPath = path.join(root, 'connections-private.json')
  let credentials
  try {
    credentials = JSON.parse(await fs.readFile(credentialsPath, 'utf8'))
  } catch {
    credentials = {}
  }
  for (const [kind, endpoint] of [
    ['stremio', '/api/stremio/connections'],
    ['torznab', '/api/torznab/endpoints'],
  ]) {
    if (!credentials[kind]) {
      const response = await api.post(endpoint, {
        headers,
        data: {
          name: 'Local performance fixture',
          scope: 'all',
          enabled: true,
          requests_per_minute: 0,
          max_concurrent_downloads: 0,
          ...(kind === 'stremio'
            ? { stream_preferences: { services: ['direct'], skip_cache_check: true } }
            : {}),
        },
      })
      if (response.status() !== 201) throw new Error('Could not prepare local access')
      credentials[kind] = await response.json()
    }
  }
  await fs.writeFile(credentialsPath, JSON.stringify(credentials), { mode: 0o600 })
  const stremioToken = 's1.' + credentials.stremio.share.id + '.' + credentials.stremio.password
  // Seed the connection's history before comparing read-only directory paths.
  await get(`/stremio/${stremioToken}/manifest.json`)
  const apiCases = [
    ['sources.definitions', '/api/providers'],
    ['sources.statistics', '/api/source-torrent-stats'],
    ['sources.execution', '/api/settings/collections'],
    ['sources.history', '/api/runs?source_kind=source'],
    ['remotes.history', '/api/runs?source_kind=remote'],
    ['torrents.list', '/api/torrents'],
    ['videos.books.information', '/api/aiosource/information'],
    ['metadata.controls', '/api/aiosource/controls'],
    ['identification.list', '/api/aiosource/matches'],
    ['identification.statistics', '/api/aiosource'],
    ['identification.matched', '/api/aiosource/matches?status=matched'],
    ['identification.pending', '/api/aiosource/matches?status=pending'],
    ['metadata.history', '/api/operations?group=metadata'],
    ['debrid.statistics', '/api/debrid/statistics'],
    ['debrid.history', '/api/operations?group=debrid'],
    ['stremio.statistics', '/api/stremio/statistics'],
    ['stremio.history', '/api/stremio/requests'],
    ['stremio.accesses', '/api/stremio/connections'],
    ['torznab.accesses', '/api/torznab/endpoints'],
    ['sharing.accesses', '/api/shares'],
    ['security.secrets', '/api/secrets'],
    ['security.verification', '/api/security'],
    ['security.sessions', '/api/security/sessions'],
    ['network.settings', '/api/settings/network'],
    ['public.settings', '/api/public-access'],
    ['storage.statistics', '/api/system-health'],
    ['runtime.settings', '/api/settings/runtime/history'],
    ['logs.list', '/api/logs'],
  ]
  for (const [id, route] of apiCases) {
    await measure(
      'api.' + id,
      id === 'identification.matched'
        ? 15_000
        : ['sources.statistics', 'identification.statistics'].includes(id)
          ? 10_000
          : 5000,
      () => get(route, { timeout: id === 'identification.matched' ? 20_000 : 12_000 }),
      (body) =>
        typeof body?.total === 'number' ? listContract(body) : { keys: Object.keys(body).sort() },
    )
  }
  for (const type of ['movie', 'tv', 'book']) {
    const work = entities.works.find((work) => work.media_type === type)
    if (!work?.title) {
      output.push({
        id: 'api.search.' + type,
        budget_ms: 5000,
        samples_ms: [0],
        error: 'missing_fixture',
      })
      continue
    }
    const query = work.title.slice(0, 500)
    for (const [kind, text] of [
      ['title', query],
      ['fragment', query.split(/[\s.:-]+/).sort((a, b) => b.length - a.length)[0]],
    ])
      await measure(
        `api.search.${type}.${kind}`,
        5000,
        () => get(`/api/catalog/search?q=${q(text)}&type=${type}`),
        (body) => {
          listContract(body)
          if (!body.items.length) throw new Error('empty_result')
          return { total: body.total, items: body.items.map((item) => [item.id, item.type]) }
        },
      )
    const identifier = work.openlibrary_id || work.imdb_id || work.tmdb_id
    if (identifier)
      await measure(
        `api.search.${type}.identifier`,
        5000,
        () => get(`/api/catalog/search?q=${q(identifier)}&type=${type}`),
        searchContract(work.id),
      )
    const detail = await get('/api/catalog/' + work.id)
    selections.set(work.id, detail.selection)
    await measure(
      `api.search.${type}.detail`,
      5000,
      () => get('/api/catalog/' + work.id),
      (body) => {
        if (String(body.id) !== String(work.id)) throw new Error('wrong_result')
        return [body.id, body.type]
      },
    )
    const selection = type === 'tv' ? '?' + new URLSearchParams(detail.selection) : ''
    await measure(
      `api.search.${type}.releases`,
      5000,
      () => get(`/api/catalog/${work.id}/releases${selection}`),
      (body) => {
        listContract(body)
        if (!body.items.length) throw new Error('empty_result')
        return body.items.map((item) => [item.occurrence_id, item.size])
      },
    )
  }
  if (entities.torrent) {
    const title = entities.torrent.title
    await measure(
      'api.torrents.query',
      5000,
      () => get('/api/torrents?q=' + q(title)),
      (body) => {
        listContract(body)
        if (!body.items.length) throw new Error('empty_result')
        return body.items.map((item) => item.occurrence_id)
      },
    )
    await measure(
      'api.identification.query',
      5000,
      () => get('/api/aiosource/matches?q=' + q(title)),
      (body) => {
        listContract(body)
        if (!body.items.length) throw new Error('empty_result')
        return body.items.map((item) => item.occurrence_id ?? item.id)
      },
    )
  }
  if (entities.raw)
    await measure(
      'api.sources.record_detail',
      5000,
      () => get('/api/raw/' + entities.raw.id),
      (body) => {
        if (
          String(body.id) !== String(entities.raw.id) &&
          String(body.record?.id) !== String(entities.raw.id)
        )
          throw new Error('wrong_result')
        return 'record'
      },
    )
  for (const [index, run] of entities.runs.entries()) {
    for (const suffix of ['', '/events', '/changes'])
      await measure(
        `api.sources.run${index}.${suffix.slice(1) || 'detail'}`,
        5000,
        () => get(`/api/runs/${q(run.id)}${suffix}`),
        (body) =>
          typeof body.total === 'number' ? listContract(body) : { keys: Object.keys(body).sort() },
      )
    await measure(
      `api.sources.run${index}.records`,
      5000,
      () => get('/api/raw?run=' + q(run.id)),
      listContract,
    )
  }
  for (const [index, operation] of entities.operations.entries())
    for (const suffix of ['', '/events'])
      await measure(
        `api.operations.${index}.${suffix.slice(1) || 'detail'}`,
        5000,
        () => get('/api/operations/' + q(operation.id) + suffix),
        (body) =>
          typeof body.total === 'number' ? listContract(body) : { keys: Object.keys(body).sort() },
      )

  await measure(
    'api.stremio.manifest',
    3000,
    () => get(`/stremio/${stremioToken}/manifest.json`),
    (body) => ({ id: body.id, resources: body.resources }),
  )
  for (const work of entities.works.filter((work) => work.media_type !== 'book')) {
    let id = work.imdb_id || 'tmdb:' + work.tmdb_id
    const type = work.media_type === 'tv' ? 'series' : 'movie'
    if (type === 'series') {
      const selection = selections.get(work.id)
      if (!Number.isInteger(selection?.season) || !Number.isInteger(selection?.episode))
        throw new Error('missing_fixture')
      id += ':' + selection.season + ':' + selection.episode
    }
    await measure(
      'api.stremio.' + work.media_type,
      5000,
      () => get(`/stremio/${stremioToken}/stream/${type}/${q(id)}.json`),
      (body) => {
        if (!Array.isArray(body.streams)) throw new Error('wrong_result')
        return body.streams.map((stream) => [
          stream.infoHash,
          stream.fileIdx,
          stream.behaviorHints?.videoSize,
        ])
      },
    )
  }
  const endpoint = '/torznab/' + credentials.torznab.share.id + '/api'
  for (const mode of ['caps', 'search', 'movie', 'tvsearch', 'book']) {
    await measure(
      'api.torznab.' + mode,
      ['movie', 'tvsearch', 'book'].includes(mode) ? 20_000 : 5000,
      async () => {
        const response = await api.get(endpoint, {
          params: { apikey: credentials.torznab.password, t: mode, limit: 50 },
          timeout: 25_000,
        })
        if (response.status() !== 200) throw new Error('http_' + response.status())
        const text = await response.text()
        if (!(mode === 'caps' ? /<caps[\s>]/.test(text) : text.includes('<rss')))
          throw new Error('wrong_result')
        return { items: (text.match(/<item>/g) ?? []).length }
      },
      (value) => value,
    )
  }

  const publicAccess = await get('/api/public-access')
  if (
    ![
      'enabled',
      'search_enabled',
      'stremio_enabled',
      'torznab_enabled',
      'catalog_enabled',
      'expose_sources',
    ].every((key) => publicAccess[key])
  )
    throw new Error('The isolated public interface fixture is incomplete')
  await measure(
    'api.public.configuration',
    3000,
    () => get('/api/public/config'),
    (body) => ({
      enabled: body.enabled,
      search: body.search_enabled,
      stremio: body.stremio_enabled,
      torznab: body.torznab_enabled,
      sharing: body.catalog_enabled,
    }),
  )

  const storageState = await api.storageState()
  browser = await chromium.launch({ headless: true })
  for (const width of [390, 1440]) {
    const context = await browser.newContext({
      baseURL: origin,
      storageState,
      viewport: { width, height: 1100 },
      locale: 'en-US',
      timezoneId: 'UTC',
      serviceWorkers: 'block',
      acceptDownloads: false,
    })
    await context.route('**/*', (route) => {
      const url = new URL(route.request().url())
      return url.origin === origin || ['data:', 'blob:'].includes(url.protocol)
        ? route.continue()
        : route.abort('blockedbyclient')
    })
    const page = await context.newPage()
    const pending = new Set()
    let changed = 0,
      requestFailure = false,
      pageFailure = false,
      badResponse = false
    page.on('pageerror', () => {
      pageFailure = true
    })
    page.on('request', (r) => {
      if (r.url().startsWith(origin + '/api/') && r.resourceType() !== 'eventsource') {
        pending.add(r)
        changed = performance.now()
      }
    })
    page.on('requestfinished', (r) => {
      if (pending.delete(r)) changed = performance.now()
    })
    page.on('requestfailed', (r) => {
      if (pending.delete(r)) {
        changed = performance.now()
        if (!r.failure()?.errorText.includes('ERR_ABORTED')) requestFailure = true
      }
    })
    page.on('response', (response) => {
      if (response.url().startsWith(origin + '/api/') && response.status() >= 400)
        badResponse = true
    })
    async function ready() {
      const until = performance.now() + 12_000
      for (;;) {
        if (pageFailure) throw new Error('browser_error')
        if (requestFailure) throw new Error('request_failed')
        if (badResponse) throw new Error('rendering_error')
        const loading = await page
          .locator('main [data-slot="skeleton"]:visible,main [aria-busy="true"]:visible')
          .count()
        if (!loading && pending.size === 0 && performance.now() - changed > 100) break
        if (performance.now() > until) throw new Error('readiness')
        await page.waitForTimeout(30)
      }
      if ((await page.getByRole('heading', { level: 1, includeHidden: true }).count()) !== 1)
        throw new Error('rendering_error')
      if (!(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)))
        throw new Error('rendering_error')
      await page.evaluate(() => document.fonts.ready)
    }
    async function navigate(route) {
      pending.clear()
      requestFailure = pageFailure = badResponse = false
      changed = performance.now()
      await page.goto(route, { waitUntil: 'domcontentloaded', timeout: 15_000 })
      await ready()
    }
    async function sections(scope, id) {
      const visited = new Set()
      for (;;) {
        const all = await scope.locator('details').all()
        let found = false
        for (let index = 0; index < all.length; index++) {
          const detail = all[index]
          if (visited.has(index) || !(await detail.isVisible())) continue
          visited.add(index)
          found = true
          await measure(
            `${id}.section.${index}`,
            1500,
            async () => {
              if ((await detail.getAttribute('open')) !== null)
                await detail.locator(':scope > summary').click()
              await detail.locator(':scope > summary').click()
              await ready()
              if ((await detail.getAttribute('open')) === null) throw new Error('rendering_error')
            },
            () => 'open',
            3,
          )
        }
        if (!found) break
        if (visited.size > 100) throw new Error('rendering_error')
      }
    }
    const pages = [
      ...applicationPages,
      ...entities.works.map((work) => ['search.detail.' + work.media_type, '/search/' + work.id]),
      ...entities.runs.map((run, index) => ['sources.run.' + index, '/runs/' + q(run.id)]),
      ...entities.sources.map((source, index) => [
        'sources.editor.' + index,
        '/providers/' + q(source.provider_id),
      ]),
    ]
    for (const [id, route] of pages) {
      await measure(
        `browser.${width}.${id}`,
        ['sources.statistics', 'identification.results'].includes(id) ? 10_000 : 5000,
        async () => {
          await navigate(route)
          return { heading: await page.getByRole('heading', { level: 1 }).textContent() }
        },
        () => 'ready',
      )
      await sections(page.locator('main'), `browser.${width}.${id}`)
      if (id.startsWith('sources.editor.')) {
        const tabs = page
          .getByRole('tablist', { name: 'Source configuration stages', exact: true })
          .getByRole('tab')
        for (let index = 0; index < (await tabs.count()); index++) {
          await measure(
            `browser.${width}.${id}.stage.${index}`,
            1500,
            async () => {
              await tabs.nth(index).click()
              await ready()
              if ((await tabs.nth(index).getAttribute('aria-selected')) !== 'true')
                throw new Error('rendering_error')
            },
            () => 'ready',
            3,
          )
          await sections(page.getByRole('tabpanel'), `browser.${width}.${id}.stage.${index}`)
        }
        await measure(
          `browser.${width}.${id}.json`,
          1500,
          async () => {
            await page.getByRole('button', { name: 'JSON', exact: true }).click()
            await ready()
            await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
            await ready()
          },
          () => 'ready',
          3,
        )
      }
    }
    for (const [id, route, button] of [
      ['stremio.create', '/addon/settings', 'Create access'],
      ['torznab.create', '/torznab', 'Create access'],
      ['sharing.create', '/sharing/settings', 'Create share'],
      ['remotes.create', '/remotes/settings', 'Add remote catalog'],
      ['network.create', '/network/outgoing', 'Add proxy'],
      ['security.create', '/security/secrets', 'Create secret'],
    ]) {
      await navigate(route)
      await measure(
        `browser.${width}.${id}`,
        1500,
        async () => {
          await page.getByRole('button', { name: button, exact: true }).click()
          try {
            await page.getByRole('dialog').waitFor()
            await ready()
          } finally {
            await page.keyboard.press('Escape')
            await page.getByRole('dialog').waitFor({ state: 'hidden' })
          }
        },
        () => 'ready',
        3,
      )
      await page.getByRole('button', { name: button, exact: true }).click()
      await page.getByRole('dialog').waitFor()
      await sections(page.getByRole('dialog'), `browser.${width}.${id}`)
      await page.keyboard.press('Escape')
    }
    await context.close()
    const visitor = await browser.newContext({
      baseURL: origin,
      viewport: { width, height: 1100 },
      locale: 'en-US',
      serviceWorkers: 'block',
    })
    await visitor.route('**/*', (route) =>
      new URL(route.request().url()).origin === origin
        ? route.continue()
        : route.abort('blockedbyclient'),
    )
    const publicPage = await visitor.newPage()
    for (const [id, route] of [
      ['stremio', '/public'],
      ['torznab', '/public/torznab'],
      ['sharing', '/public/sharing'],
      ['search', '/public/search'],
    ]) {
      await measure(
        `browser.${width}.public.${id}`,
        4000,
        async () => {
          await publicPage.goto(route, { waitUntil: 'networkidle' })
          if ((await publicPage.getByRole('heading', { level: 1 }).count()) !== 1)
            throw new Error('rendering_error')
          if (
            !(await publicPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
          )
            throw new Error('rendering_error')
        },
        () => 'ready',
        3,
      )
    }
    await visitor.close()
  }
} catch (error) {
  diagnostics.push({ case: 'suite.setup', message: String(error), stack: error?.stack })
  output.push({ id: 'suite.setup', budget_ms: 1, samples_ms: [0], error: 'operation_failed' })
  process.exitCode = 1
} finally {
  await browser?.close()
  await api?.dispose()
  await fs.writeFile(path.join(root, 'measurements-private.json'), JSON.stringify(output), {
    mode: 0o600,
  })
  await fs.writeFile(path.join(root, 'diagnostics-private.json'), JSON.stringify(diagnostics), {
    mode: 0o600,
  })
}
