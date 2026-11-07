import fs from 'node:fs/promises'
import path from 'node:path'
import crypto from 'node:crypto'
import pg from '../../web/node_modules/pg/lib/index.js'

const root = process.env.PERFORMANCE_WORKSPACE
if (!root || !process.env.PERFORMANCE_DATABASE_URL || process.env.CI)
  throw new Error('Local performance workspace required')
const db = new pg.Client({ connectionString: process.env.PERFORMANCE_DATABASE_URL })
db.on('error', () => {})
try {
  await db.connect()
  await db.query('BEGIN')
  const definitions = await db.query(`SELECT DISTINCT ON(provider_id) config FROM ingest.runs
    WHERE config->>'id'<>'' ORDER BY provider_id,created_at DESC`)
  const providers = path.join(root, 'providers')
  const state = path.join(root, 'state')
  await fs.mkdir(providers, { recursive: true, mode: 0o700 })
  await fs.mkdir(state, { recursive: true, mode: 0o700 })
  for (const { config } of definitions.rows) {
    if (!/^[a-zA-Z0-9_.-]+$/.test(config.id)) throw new Error('Source identity is invalid')
    // Keep the exact editor shape. Jobs are disabled and the global outgoing
    // route is replaced below; the browser runner also denies remote resources.
    config.enabled = false
    if (config.schedule) config.schedule.enabled = false
    if (config.catalog_sync) config.catalog_sync.enabled = false
    await fs.writeFile(path.join(providers, config.id + '.json'), JSON.stringify(config), {
      mode: 0o600,
    })
  }
  const row = (await db.query(`SELECT value FROM ingest.settings WHERE key='aiosource.config.v2'`))
    .rows[0]
  if (row) {
    const config = JSON.parse(row.value)
    config.auto_sync = config.auto_match = config.auto_cache = config.openlibrary_enabled = false
    for (const key of Object.keys(config)) if (key.endsWith('_secret')) config[key] = ''
    for (const tracker of Object.values(config.trackers ?? {})) {
      delete tracker.announce
      delete tracker.passkey_secret
      tracker.announce_secret = ''
    }
    await db.query(`UPDATE ingest.settings SET value=$1 WHERE key='aiosource.config.v2'`, [
      JSON.stringify(config),
    ])
  }
  await db.query(`UPDATE ingest.collection_settings SET auto_resume_interrupted=false,workers=1;
    UPDATE ingest.runs SET status='paused',pause_reason='manual' WHERE status IN('queued','running');
    UPDATE ingest.operation_runs SET status='cancelled',finished_at=now() WHERE status IN('queued','running');
    ALTER TABLE ingest.aio_public_access DISABLE TRIGGER USER;
    UPDATE ingest.aio_public_access SET enabled=true,expose_sources=true,
      stremio_enabled=true,torznab_enabled=true,search_enabled=true,catalog_enabled=true;
    ALTER TABLE ingest.aio_public_access ENABLE TRIGGER USER;
    UPDATE ingest.runtime_settings SET values=values || '{"days":3650,"cleanup_seconds":3600}'::jsonb WHERE section='history';
    UPDATE ingest.admin_security SET password_hash=NULL,active_secret=NULL,pending_secret=NULL,
      pending_expires_at=NULL,pending_session_id=NULL,recovery_hashes='{}';
    DELETE FROM ingest.admin_sessions; DELETE FROM ingest.security_attempts;
    UPDATE ingest.network_settings SET default_route='performance-offline',
      proxies='[{"id":"performance-offline","name":"Offline measurement","url":"http://127.0.0.1:9","enabled":true}]',
      overrides='{}',incoming='{"enabled":false,"ip_header":"X-Forwarded-For","format":"chain","trusted_proxies":[]}';`)
  if ((await db.query(`SELECT to_regclass('ingest.remote_catalogs') AS name`)).rows[0].name)
    await db.query('UPDATE ingest.remote_catalogs SET enabled=false')
  const key = crypto.randomBytes(32)
  await fs.writeFile(path.join(state, 'vault.key'), key.toString('base64') + '\n', { mode: 0o600 })
  const fingerprint = crypto
    .createHash('sha256')
    .update('scraper/vault/master-key-fingerprint/v1\0')
    .update(key)
    .digest('hex')
  await db.query(
    `UPDATE ingest.settings SET value=$1 WHERE key='vault.master_key_fingerprint.v1'`,
    [fingerprint],
  )
  for (const { name } of (await db.query('SELECT name FROM ingest.secrets')).rows) {
    const nonce = crypto.randomBytes(12)
    const cipher = crypto.createCipheriv('aes-256-gcm', key, nonce)
    cipher.setAAD(Buffer.from(name))
    const value = /announce|passkey/.test(name)
      ? 'https://announce.example.invalid/synthetic/announce'
      : 'synthetic-performance-value'
    const ciphertext = Buffer.concat([
      Buffer.from([1]),
      nonce,
      cipher.update(value),
      cipher.final(),
      cipher.getAuthTag(),
    ])
    await db.query('UPDATE ingest.secrets SET ciphertext=$2 WHERE name=$1', [name, ciphertext])
  }
  await db.query('COMMIT')
  await fs.writeFile(path.join(root, 'prepared.json'), JSON.stringify({ complete: true }), {
    mode: 0o600,
  })
  console.log(JSON.stringify({ prepared: true }))
} catch {
  await db.query('ROLLBACK').catch(() => {})
  console.error('The private snapshot could not be isolated. No application was started.')
  process.exitCode = 1
} finally {
  await db.end().catch(() => {})
}
