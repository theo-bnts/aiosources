import test from 'node:test'
import assert from 'node:assert/strict'

import { parseProviderJSON } from '../src/lib/provider-json.ts'
import {
  applyTorznabCategories,
  applyTorznabCapabilityField,
  applyUseAdvertisedCategories,
  countTorznabCategories,
  filterTorznabCategories,
  findUnknownCategoryIDs,
  isTorznabCapabilitiesStale,
  readSelectedCategoryIDs,
  readTorznabCapabilityFieldValue,
  resolveTorznabCapabilityFieldValue,
  torznabConnectionKey,
  torznabSearchModeOptionLabels,
  type TorznabCapabilitiesResult,
} from '../src/lib/torznab-capabilities.ts'

const sampleCaps: TorznabCapabilitiesResult = {
  endpoint: 'https://tracker.example/api',
  title: 'Cinema Archive',
  suggested_id: 'cinema_archive',
  limits: { default: 50, max: 200 },
  recommended_page_size: 200,
  searches: [
    { mode: 'search', available: true, supported_params: ['q', 'cat'] },
    { mode: 'tvsearch', available: true, supported_params: ['q', 'tvdbid', 'season', 'ep'] },
    { mode: 'movie', available: false, supported_params: ['imdbid'] },
  ],
  recommended_search_mode: 'search',
  categories: [
    {
      id: 2000,
      name: 'Movies',
      subcategories: [
        { id: 2040, name: 'HD' },
        { id: 2045, name: 'UHD' },
      ],
    },
    {
      id: 5000,
      name: 'TV',
      subcategories: [{ id: 5070, name: 'Anime' }],
    },
  ],
}

const templateJSON = `{
  "version": 1,
  "id": "example_torznab",
  "name": "Example Torznab provider",
  "adapter": "torznab",
  "url": "https://tracker.example",
  "enabled": false,
  "auth": {
    "type": "query",
    "name": "apikey",
    "secret_ref": "tracker_key"
  },
  "page_size": 100,
  "search": {
    "categories": []
  },
  "options": {
    "category_scope": "combined",
    "search_mode": "search"
  }
}`

test('per-field capability resolution extracts values, clamps page_size and handles missing metadata', () => {
  assert.equal(resolveTorznabCapabilityFieldValue(sampleCaps, 'name')?.value, 'Cinema Archive')
  assert.equal(resolveTorznabCapabilityFieldValue(sampleCaps, 'id')?.value, 'cinema_archive')
  assert.equal(
    resolveTorznabCapabilityFieldValue(sampleCaps, 'url')?.value,
    'https://tracker.example/api',
  )
  assert.equal(resolveTorznabCapabilityFieldValue(sampleCaps, 'page_size')?.value, 200)
  assert.equal(resolveTorznabCapabilityFieldValue(sampleCaps, 'search_mode')?.value, 'search')

  const oversized: TorznabCapabilitiesResult = {
    endpoint: 'https://tracker.example/api',
    limits: { default: 50000, max: 99999 },
    recommended_page_size: 99999,
    searches: [],
    categories: [],
  }
  assert.equal(resolveTorznabCapabilityFieldValue(oversized, 'page_size')?.value, 10000)
  assert.equal(resolveTorznabCapabilityFieldValue(oversized, 'name'), null)
  assert.equal(resolveTorznabCapabilityFieldValue(oversized, 'id'), null)
  assert.equal(resolveTorznabCapabilityFieldValue(oversized, 'search_mode'), null)
})

test('applying individual fields overrides existing values cleanly', () => {
  const doc = parseProviderJSON(`{
    "version": 1,
    "id": "existing_source",
    "name": "Existing Custom Name",
    "adapter": "torznab",
    "url": "https://tracker.example/api",
    "enabled": true,
    "page_size": 50,
    "options": { "search_mode": "movie" }
  }`)

  assert.equal(readTorznabCapabilityFieldValue(doc, 'name'), 'Existing Custom Name')
  assert.equal(readTorznabCapabilityFieldValue(doc, 'page_size'), 50)
  assert.equal(readTorznabCapabilityFieldValue(doc, 'search_mode'), 'movie')

  applyTorznabCapabilityField(doc, 'name', 'Cinema Archive')
  applyTorznabCapabilityField(doc, 'page_size', 200)
  applyTorznabCapabilityField(doc, 'search_mode', 'search')

  assert.equal(doc.get('id'), 'existing_source')
  assert.equal(doc.get('name'), 'Cinema Archive')
  assert.equal(doc.get('page_size'), 200)
  assert.equal(doc.getIn(['options', 'search_mode']), 'search')
})

test('search mode option labels mark unavailable modes without extra banners', () => {
  const labels = torznabSearchModeOptionLabels(sampleCaps)
  assert.equal(labels.search, 'General search')
  assert.equal(labels.tvsearch, 'Series')
  assert.equal(labels.movie, 'Films (unavailable)')
})

test('category selection keeps category_scope valid and preserves unknown IDs', () => {
  const doc = parseProviderJSON(`{
    "version": 1,
    "id": "existing_source",
    "name": "Existing",
    "adapter": "torznab",
    "url": "https://tracker.example/api",
    "enabled": true,
    "search": { "categories": [99999] },
    "options": { "category_scope": "advertised" }
  }`)

  assert.deepEqual(readSelectedCategoryIDs(doc), [99999])
  assert.deepEqual(findUnknownCategoryIDs([99999, 2040], sampleCaps.categories), [99999])

  // Picking categories while scope=advertised switches scope to combined.
  const switched = applyTorznabCategories(doc, [99999, 5070, 2040])
  assert.equal(switched.switchedScope, 'combined')
  assert.equal(doc.getIn(['options', 'category_scope']), 'combined')
  assert.deepEqual(readSelectedCategoryIDs(doc), [2040, 5070, 99999])

  // Clearing categories while scope=each switches scope back to combined.
  doc.setIn(['options', 'category_scope'], 'each')
  const cleared = applyTorznabCategories(doc, [])
  assert.equal(cleared.switchedScope, 'combined')
  assert.equal(doc.getIn(['options', 'category_scope']), 'combined')
  assert.deepEqual(readSelectedCategoryIDs(doc), [])

  // Switching to all advertised clears categories and sets scope=advertised.
  applyTorznabCategories(doc, [2000])
  applyUseAdvertisedCategories(doc)
  assert.equal(doc.getIn(['options', 'category_scope']), 'advertised')
  assert.deepEqual(readSelectedCategoryIDs(doc), [])
})

test('category filtering matches names and IDs while keeping parent hierarchy', () => {
  assert.equal(countTorznabCategories(sampleCaps.categories), 5)

  const bySubcat = filterTorznabCategories(sampleCaps.categories, 'anime')
  assert.equal(bySubcat.length, 1)
  assert.equal(bySubcat[0].id, 5000)
  assert.deepEqual(
    bySubcat[0].subcategories?.map((item) => item.id),
    [5070],
  )

  const byID = filterTorznabCategories(sampleCaps.categories, '2045')
  assert.equal(byID.length, 1)
  assert.equal(byID[0].id, 2000)
  assert.deepEqual(
    byID[0].subcategories?.map((item) => item.id),
    [2045],
  )

  const byParent = filterTorznabCategories(sampleCaps.categories, 'movies')
  assert.equal(byParent.length, 1)
  assert.equal(byParent[0].subcategories?.length, 2)
})

test('cache stays valid after canonical endpoint adoption and invalidates on connection edits', () => {
  const doc = parseProviderJSON(templateJSON)
  const fetchedKey = torznabConnectionKey(doc)

  assert.equal(isTorznabCapabilitiesStale(fetchedKey, doc, sampleCaps), false)

  applyTorznabCapabilityField(doc, 'url', sampleCaps.endpoint)
  assert.equal(isTorznabCapabilitiesStale(fetchedKey, doc, sampleCaps), false)

  doc.setIn(['auth', 'secret_ref'], 'different_secret')
  assert.equal(isTorznabCapabilitiesStale(fetchedKey, doc, sampleCaps), true)
})

test('minimal Torznab JSON without search, options or page_size applies capabilities cleanly', () => {
  const doc = parseProviderJSON(`{
    "version": 1,
    "id": "minimal_source",
    "name": "Minimal",
    "adapter": "torznab",
    "url": "https://tracker.example/api",
    "enabled": false
  }`)

  applyTorznabCategories(doc, [5070, 2040])
  assert.deepEqual(readSelectedCategoryIDs(doc), [2040, 5070])

  applyTorznabCapabilityField(doc, 'page_size', 200)
  applyTorznabCapabilityField(doc, 'search_mode', 'tvsearch')
  assert.equal(doc.get('page_size'), 200)
  assert.equal(doc.getIn(['options', 'search_mode']), 'tvsearch')
  assert.deepEqual(doc.errors, [])
})
