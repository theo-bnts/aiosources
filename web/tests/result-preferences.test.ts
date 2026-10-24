import test from 'node:test'
import assert from 'node:assert/strict'
import {
  readResultSorting,
  resultPreferences,
  type SortRule,
} from '../src/lib/result-preferences.ts'
import { preferenceManifest } from '../src/lib/language-preferences.ts'
import { torznabPreferenceKey } from '../src/lib/torznab-preferences.ts'

test('empty result controls stay optional and sorting priorities round-trip in both URL formats', () => {
  assert.deepEqual(
    resultPreferences({ sorting: [], max_results: 0, max_cached: 0, response_seconds: 0 }),
    {},
  )
  const sorting: SortRule[] = [
    { field: 'cached', direction: 'desc' },
    { field: 'size', direction: 'asc' },
    { field: 'name', direction: 'asc' },
  ]
  const options = { sorting, max_results: 80, max_cached: 20, response_seconds: 12 }
  const manifest = preferenceManifest(
    '/public/stremio/sealed/manifest.json',
    { language: 'fr', mode: 'all' },
    options,
  )
  const stremio = JSON.parse(
    Buffer.from(manifest.split('~')[2].split('/')[0], 'base64url').toString(),
  )
  const torznab = JSON.parse(
    Buffer.from(torznabPreferenceKey('sealed', options).split('~')[2], 'base64url').toString(),
  )
  assert.deepEqual(stremio, { language: 'fr', mode: 'all', ...options })
  assert.deepEqual(torznab, options)
  assert.deepEqual(readResultSorting(JSON.stringify(sorting)), sorting)
})

test('malformed URL sorting cannot crash the search controls', () => {
  for (const raw of [
    'invalid',
    '{}',
    '[null]',
    '[1]',
    '[{"field":"private","direction":"asc"}]',
    '[{"field":"name","direction":"up"}]',
    '[{"field":"age","direction":"asc"},{"field":"published","direction":"desc"}]',
  ])
    assert.deepEqual(readResultSorting(raw), [])
})
