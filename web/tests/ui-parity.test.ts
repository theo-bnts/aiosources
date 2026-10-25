import test from 'node:test'
import assert from 'node:assert/strict'
import { compareListLabels, compareListNames, matchesListQuery } from '../src/lib/list-view.ts'
import {
  catalogFieldsSummary,
  catalogShareState,
  connectionSummary,
} from '../src/lib/catalog-display.ts'
import type { CatalogShare } from '../src/lib/catalog-types.ts'

test('directory matching and natural name order are shared without modifying their records', () => {
  const records = [
    { id: 'b', name: 'Beta' },
    { id: 'a10', name: 'Alpha 10' },
    { id: 'a2', name: 'Alpha 2' },
  ]
  assert.deepEqual(
    [...records].sort(compareListNames).map((item) => item.id),
    ['a2', 'a10', 'b'],
  )
  assert.deepEqual(
    records.map((item) => item.id),
    ['b', 'a10', 'a2'],
  )
  assert.deepEqual(
    records
      .map((item) => ({ label: item.name, value: item.id }))
      .sort(compareListLabels)
      .map((item) => item.value),
    ['a2', 'a10', 'b'],
  )
  assert.equal(matchesListQuery('  ALPHA   2 ', 'Alpha 2', 'a2'), true)
  assert.equal(matchesListQuery('Alpha Beta', 'Alpha 2', 'a2'), false)
  assert.equal(matchesListQuery('warp', undefined, 'socks5://warp:40000'), true)
})

function access(format: 'stremio' | 'torznab', cachedOnly = false): CatalogShare {
  const preferences = {
    max_results: 80,
    max_cached: 20,
    response_seconds: 12,
    cached_only: cachedOnly,
    sorting: [{ field: 'size' as const, direction: 'desc' as const }],
  }
  return {
    id: 'example',
    name: 'Example',
    enabled: false,
    scope: 'all',
    source_ids: [],
    fields: ['title'],
    revision: 1,
    created_at: '',
    updated_at: '',
    expires_at: null,
    requests_per_minute: 0,
    max_concurrent_downloads: 0,
    url: '',
    format,
    ...(format === 'stremio'
      ? { stream_preferences: { language: 'fr', mode: 'all', ...preferences } }
      : { torznab_preferences: preferences }),
  }
}

test('connection summaries show applied limits with the actual protocol semantics', () => {
  assert.equal(connectionSummary(access('stremio'))?.limits, 'Up to 80 results · Cache target: 20')
  assert.equal(connectionSummary(access('torznab'))?.limits, 'Up to 80 results')
  for (const format of ['stremio', 'torznab'] as const) {
    const record = access(format, true)
    assert.equal(connectionSummary(record)?.limits, 'Up to 20 cached results')
    assert.equal(connectionSummary(record)?.sorting, 'Size (descending)')
    assert.equal(connectionSummary(record)?.response, '12 seconds')
    assert.equal((record.stream_preferences ?? record.torznab_preferences)?.max_results, 80)
  }
})

test('connection summaries use the same media, service and language labels as their controls', () => {
  const record = access('stremio')
  record.stream_preferences = {
    language: 'fr',
    mode: 'multi',
    types: ['movie'],
    services: ['torbox'],
    cached_only: true,
  }
  assert.equal(connectionSummary(record)?.filters, 'Movies · TorBox · French + MULTI · Cached only')
  record.format = 'torznab'
  record.media_types = ['tv', 'other']
  record.torznab_preferences = { services: ['direct'] }
  assert.equal(
    connectionSummary(record)?.filters,
    'Series · Other / not identified · Direct torrents',
  )
})

test('catalog field labels stay readable while unrecognized future fields remain distinguishable', () => {
  assert.equal(
    catalogFieldsSummary(['title', 'info_hash', 'published_at']),
    'Title, Info hash, Publication date',
  )
  assert.equal(catalogFieldsSummary(['future_field']), 'future_field')
  assert.equal(catalogFieldsSummary(['constructor', 'toString']), 'constructor, toString')
  assert.equal(catalogFieldsSummary([]), 'No fields selected')
})

test('expired access has the same effective state in statistics and settings', () => {
  for (const enabled of [false, true])
    assert.equal(
      catalogShareState({ enabled, expires_at: new Date(Date.now() - 60_000).toISOString() }),
      'expired',
    )
  assert.equal(catalogShareState({ enabled: true, expires_at: null }), 'enabled')
  assert.equal(catalogShareState({ enabled: false, expires_at: null }), 'disabled')
})
