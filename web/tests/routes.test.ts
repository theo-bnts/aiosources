import test from 'node:test'
import assert from 'node:assert/strict'
import {
  routes,
  visitorRoutes,
  runDetailPath,
  rawRecordPath,
  rawPagePath,
} from '../src/lib/routes.ts'

function collectStrings(node: unknown, prefix = ''): [string, string][] {
  if (typeof node === 'string') return [[prefix, node]]
  if (node && typeof node === 'object') {
    return Object.entries(node).flatMap(([key, value]) =>
      collectStrings(value, prefix ? `${prefix}.${key}` : key),
    )
  }
  return []
}

test('all canonical routes follow clean slash-prefixed kebab-case paths with no duplicates', () => {
  const entries = collectStrings(routes)
  assert.ok(entries.length > 50)
  const seen = new Map<string, string>()
  for (const [key, path] of entries) {
    assert.match(path, /^\/[a-z0-9/-]+$/, `route ${key} has unexpected characters: ${path}`)
    assert.ok(!path.endsWith('/'), `route ${key} must not have a trailing slash: ${path}`)
    assert.ok(!path.includes('//'), `route ${key} must not contain double slashes: ${path}`)
    assert.equal(
      seen.has(path),
      false,
      `duplicate route path ${path} (${seen.get(path)} and ${key})`,
    )
    seen.set(path, key)
  }
})

test('visitor routes mirror public and trusted hierarchies identically', () => {
  const pub = visitorRoutes('/public')
  const trusted = visitorRoutes('/trusted')
  assert.deepEqual(Object.keys(pub), Object.keys(trusted))
  for (const key of Object.keys(pub) as (keyof typeof pub)[]) {
    assert.equal(pub[key].replace(/^\/public/, '/trusted'), trusted[key])
  }
})

test('detail path helpers encode identifiers under their canonical parent history', () => {
  assert.equal(runDetailPath('run-1'), '/sources/indexers/torrents/history/run-1')
  assert.equal(runDetailPath('run-1', true), '/sources/remote-catalogs/history/run-1')
  assert.equal(rawRecordPath(42), '/sources/indexers/torrents/history/raw/42')
  assert.equal(rawPagePath(99), '/sources/indexers/torrents/history/pages/99')
})
