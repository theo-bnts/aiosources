import test from 'node:test'
import assert from 'node:assert/strict'
import {
  defaultLanguagePreferences,
  preferenceManifest,
  preferenceManifestError,
  resolvedLanguage,
} from '../src/lib/language-preferences.ts'

test('installation preferences use the browser locale and replace prior choices without touching credentials', () => {
  Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: { languages: ['fr-CA'], language: 'en-US' },
  })
  assert.equal(resolvedLanguage({ language: '', mode: 'unknown' }), 'fr')
  assert.equal(
    preferenceManifest('/stremio/opaque-token/manifest.json', { language: '', mode: 'unknown' }),
    '/stremio/opaque-token~fr~unknown/manifest.json',
  )
  assert.equal(
    preferenceManifest(
      'https://example.invalid/public/stremio/encrypted-token~fr~unknown/manifest.json',
      { language: 'de', mode: 'all' },
    ),
    'https://example.invalid/public/stremio/encrypted-token~de~all/manifest.json',
  )
})

test('connection filters travel in independent URLs and replace prior URL options', () => {
  const first = preferenceManifest(
    '/stremio/opaque-token/manifest.json',
    { language: 'fr', mode: 'unknown' },
    { sources: ['alpha', 'beta'], services: ['torbox'], types: ['movie'], cached_only: true },
  )
  assert.match(first, /^\/stremio\/opaque-token~v1~[A-Za-z0-9_-]+\/manifest.json$/)
  const decoded = JSON.parse(Buffer.from(first.split('~')[2].split('/')[0], 'base64url').toString())
  assert.deepEqual(decoded, {
    language: 'fr',
    mode: 'unknown',
    sources: ['alpha', 'beta'],
    services: ['torbox'],
    types: ['movie'],
    cached_only: true,
  })
  const second = preferenceManifest(first, { language: 'en', mode: 'all' }, { sources: ['beta'] })
  assert.equal((second.match(/~v1~/g) || []).length, 1)
  assert.equal(
    preferenceManifest(second, { language: 'en', mode: 'all' }, {}),
    '/stremio/opaque-token~en~all/manifest.json',
  )
  assert.notEqual(first, second)
})

test('oversized connection selections cannot produce an installable link', () => {
  const options = { sources: Array.from({ length: 101 }, (_, i) => 'source-' + i) }
  assert.ok(preferenceManifestError('', options))
  assert.ok(preferenceManifestError('/stremio/token~v1~' + 'a'.repeat(5463) + '/manifest.json', {}))
  assert.equal(preferenceManifestError('/stremio/token~fr~all/manifest.json', {}), null)
})

test('every new language filter starts with the browser locale, MULTI and unknown languages', () => {
  assert.deepEqual(defaultLanguagePreferences(), { language: '', mode: 'unknown' })
  const first = defaultLanguagePreferences()
  first.mode = 'all'
  assert.equal(defaultLanguagePreferences().mode, 'unknown')
})
