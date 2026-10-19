import { spawn, type ChildProcess } from 'node:child_process'
import { randomBytes, randomInt } from 'node:crypto'
import { constants } from 'node:fs'
import {
  access,
  chmod,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  realpath,
  rm,
  writeFile,
} from 'node:fs/promises'
import { createServer as createHTTPServer } from 'node:http'
import { createServer, type Server } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'
import pg from 'pg'
import { launchBackend } from './backend-readiness'

const webDirectory = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const databasePrefix = 'ingest_e2e_'
const directoryPrefix = 'ingest-e2e-'

type DatabaseIdentity = { oid: number; owner: number }

export type TestServer = {
  origin: string
  databaseURL: string
  password: string
  sourceURL: string
  sourceRequests: () => number
  close: () => Promise<void>
}

function maintenanceURL(): URL {
  // Compatibility for the retained workflow; never passed to the application.
  const raw = process.env.TEST_DATABASE_URL || process.env.INGEST_TEST_DATABASE_URL
  if (!raw)
    throw new Error('TEST_DATABASE_URL is required for browser regressions; no tests were skipped.')
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    throw new Error('TEST_DATABASE_URL must be a PostgreSQL maintenance URI.')
  }
  // An omitted port means PostgreSQL's default, never an inherited PGPORT.
  url.port ||= '5432'
  if (
    !['postgres:', 'postgresql:'].includes(url.protocol) ||
    !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) ||
    url.pathname !== '/postgres' ||
    [55485, 55486].includes(Number(url.port)) ||
    url.hash
  ) {
    throw new Error(
      'Browser tests require a disposable loopback PostgreSQL /postgres maintenance database, never ports 55485 or 55486.',
    )
  }
  // Prevent driver-specific query options from overriding the guarded host/database.
  for (const key of url.searchParams.keys()) {
    if (!['sslmode', 'sslcert', 'sslkey', 'sslrootcert', 'ssl'].includes(key)) {
      throw new Error(
        'The browser-test maintenance URI only accepts SSL query options, not connection overrides.',
      )
    }
  }
  return url
}

function client(url: URL) {
  const connection = new pg.Client({
    connectionString: url.toString(),
    connectionTimeoutMillis: 10_000,
    query_timeout: 15_000,
    statement_timeout: 10_000,
    application_name: 'ingest-browser-regression',
  })
  // Connection failures are surfaced by the awaited operation, without an unhandled
  // event printing a credential-bearing driver error to a CI log.
  connection.on('error', () => {})
  return connection
}

async function listen(server: Server): Promise<number> {
  // Stay below both reserved live-service ports, rather than trusting the OS's
  // ephemeral range. EADDRINUSE is a retry, never permission to reuse a service.
  for (let attempt = 0; attempt < 20; attempt++) {
    const port = randomInt(20_000, 49_000)
    const started = await new Promise<boolean>((resolveListen, reject) => {
      const failed = (error: NodeJS.ErrnoException) => {
        server.off('listening', ready)
        if (error.code === 'EADDRINUSE') resolveListen(false)
        else reject(new Error('Could not allocate a loopback browser-test port.'))
      }
      const ready = () => {
        server.off('error', failed)
        resolveListen(true)
      }
      server.once('error', failed)
      server.once('listening', ready)
      server.listen(port, '127.0.0.1')
    })
    if (started) return port
  }
  throw new Error('Could not reserve an unused browser-test port.')
}

async function closeListener(server: Server) {
  if (server.listening) {
    await new Promise<void>((resolveClose, reject) =>
      server.close((error) => (error ? reject(error) : resolveClose())),
    )
  }
}

async function stopProcess(child: ChildProcess) {
  if (child.exitCode !== null || child.signalCode !== null || !child.pid) return
  const exited = new Promise<void>((resolveExit) => child.once('exit', () => resolveExit()))
  child.kill('SIGTERM')
  const graceful = await Promise.race([
    exited.then(() => true),
    delay(35_000, false, { ref: false }),
  ])
  if (!graceful) {
    child.kill('SIGKILL')
    const stopped = await Promise.race([
      exited.then(() => true),
      delay(5_000, false, { ref: false }),
    ])
    if (!stopped)
      throw new Error('Could not stop the owned browser-test backend; refusing storage cleanup.')
  }
}

export async function startServer({
  collectionSource = false,
}: { collectionSource?: boolean } = {}): Promise<TestServer> {
  const maintenance = maintenanceURL()
  const binary = resolve(webDirectory, process.env.E2E_BINARY || '../bin/ingest')
  try {
    await access(binary, constants.X_OK)
  } catch {
    throw new Error('E2E_BINARY must name an executable production binary; run make build first.')
  }
  const parent = await realpath(tmpdir())
  const root = await mkdtemp(join(parent, directoryPrefix))
  const ownershipToken = randomBytes(24).toString('hex')
  const name = databasePrefix + randomBytes(12).toString('hex')
  const password = randomBytes(24).toString('base64url')
  let identity: DatabaseIdentity | undefined
  let created = false
  let child: ChildProcess | undefined
  let requests = 0
  let closing: Promise<void> | undefined
  const tripwire = createHTTPServer((_request, response) => {
    requests++
    if (collectionSource && _request.url?.startsWith('/source/html')) {
      const url = new URL(_request.url, 'http://fixture.invalid')
      if (url.pathname.startsWith('/source/html/detail/')) {
        const hash = url.pathname.endsWith('/two') ? 'b'.repeat(40) : 'a'.repeat(40)
        response
          .writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
          .end(`<main id="item-details"><span class="hash">${hash}</span></main>`)
        return
      }
      const second = url.searchParams.get('page') === '2'
      const id = second ? 'two' : 'one'
      response
        .writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
        .end(
          `<main id="catalog"><article data-id="${id}"><span class="title">Example HTML ${id} 2026</span><span class="size">1 KiB</span><time datetime="2026-01-01T00:00:00Z"></time><a class="details" href="/source/html/detail/${id}">Details</a></article></main>` +
            (second ? '' : '<a rel="next" href="?page=2">Next</a>'),
        )
    } else if (collectionSource) {
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(
        JSON.stringify({
          items: [
            { id: 'one', title: 'Synthetic movie 2026', info_hash: 'a'.repeat(40), size: 1024 },
          ],
        }),
      )
    } else response.writeHead(503).end('Collection is forbidden in browser editor regressions.')
  })
  const reservation = createServer()

  const close = () => {
    closing ??= (async () => {
      // A surviving process must never see its private database or files removed.
      if (child) await stopProcess(child)
      const failures: string[] = []
      try {
        await closeListener(reservation)
        await closeListener(tripwire)
      } catch {
        failures.push('Could not close an owned test listener.')
      }
      if (created) {
        const admin = client(maintenance)
        try {
          await admin.connect()
          const result = await admin.query(
            'SELECT oid, datdba AS owner FROM pg_database WHERE datname = $1 AND datdba = (SELECT oid FROM pg_roles WHERE rolname = current_user)',
            [name],
          )
          const current = result.rows[0] as DatabaseIdentity | undefined
          if (
            !/^ingest_e2e_[a-f0-9]{24}$/.test(name) ||
            !identity ||
            !current ||
            current.oid !== identity.oid ||
            current.owner !== identity.owner
          ) {
            throw new Error('ownership mismatch')
          }
          await admin.query(`DROP DATABASE "${name}" WITH (FORCE)`)
        } catch {
          failures.push(
            `Could not safely drop owned test database ${name}; inspect its ownership before manual cleanup.`,
          )
        } finally {
          await admin.end().catch(() => {})
        }
      }
      try {
        const stat = await lstat(root)
        if (
          dirname(root) !== parent ||
          !root.startsWith(join(parent, directoryPrefix)) ||
          !stat.isDirectory() ||
          stat.isSymbolicLink() ||
          (await readFile(join(root, '.owner'), 'utf8')) !== ownershipToken
        ) {
          throw new Error('ownership mismatch')
        }
        await rm(root, { recursive: true })
      } catch {
        failures.push('Could not safely remove the owned private browser-test directory.')
      }
      if (requests && !collectionSource)
        failures.push('The backend attempted source collection during an editor-only browser test.')
      if (failures.length) throw new Error(failures.join('\n'))
    })()
    return closing
  }

  try {
    await chmod(root, 0o700)
    await writeFile(join(root, '.owner'), ownershipToken, { mode: 0o600, flag: 'wx' })
    const state = join(root, 'state')
    const providers = join(root, 'providers')
    const home = join(root, 'home')
    const temporary = join(root, 'tmp')
    for (const directory of [state, providers, home, temporary]) {
      await mkdir(directory, { mode: 0o700 })
    }
    const admin = client(maintenance)
    try {
      await admin.connect()
      await admin.query(`CREATE DATABASE "${name}" TEMPLATE template0`)
      created = true
      const result = await admin.query(
        'SELECT oid, datdba AS owner FROM pg_database WHERE datname = $1',
        [name],
      )
      identity = result.rows[0] as DatabaseIdentity | undefined
      if (!identity) throw new Error('Missing database ownership record')
    } catch {
      throw new Error(
        'Could not create an isolated browser-test database. TEST_DATABASE_URL needs a disposable PostgreSQL service and CREATEDB permission.',
      )
    } finally {
      await admin.end().catch(() => {})
    }
    const database = new URL(maintenance)
    database.pathname = `/${name}`
    const sourcePort = await listen(tripwire)
    const { origin } = await launchBackend(async () => {
      const port = await listen(reservation)
      await closeListener(reservation)
      const origin = `http://127.0.0.1:${port}`
      // Deliberately do not inherit deployment credentials, PG*, proxies, HOME or
      // provider credentials. Neither the binary's cwd nor its state is the repo.
      child = spawn(
        binary,
        [
          'serve',
          '--data-dir',
          state,
          '--providers',
          providers,
          '--bind',
          `127.0.0.1:${port}`,
          '--public-url',
          origin,
          '--workers',
          '1',
        ],
        {
          cwd: root,
          env: {
            PATH: process.env.PATH || '/usr/bin:/bin',
            HOME: home,
            TMPDIR: temporary,
            TMP: temporary,
            TEMP: temporary,
            LANG: 'C.UTF-8',
            TZ: 'UTC',
            DATABASE_URL: database.toString(),
            ADMIN_PASSWORD: password,
          },
          // Startup output is used only for readiness. Never persist or publish logs,
          // environment, database URLs, cookies or temporary administrator passwords.
          stdio: ['ignore', 'pipe', 'pipe'],
        },
      )
      return { child, origin }
    })
    return {
      origin,
      databaseURL: database.toString(),
      password,
      sourceURL: `http://127.0.0.1:${sourcePort}/source`,
      sourceRequests: () => requests,
      close,
    }
  } catch (error) {
    try {
      await close()
    } catch (cleanupError) {
      throw new AggregateError([error, cleanupError], 'Browser-test setup and cleanup failed.')
    }
    throw error
  }
}
