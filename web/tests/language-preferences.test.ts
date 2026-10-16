import test from 'node:test'
import assert from 'node:assert/strict'
import { preferenceManifest, resolvedLanguage } from '../src/lib/language-preferences.ts'

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
