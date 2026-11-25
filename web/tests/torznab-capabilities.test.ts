import test from 'node:test'
import assert from 'node:assert/strict'

import { parseProviderJSON } from '../src/lib/provider-json.ts'
import {
  applyTorznabCategories,
  applyTorznabCapabilityChanges,
  applyTorznabCapabilityField,
  applyUseAdvertisedCategories,
  countTorznabCategories,
  filterTorznabCategories,
  findUnknownCategoryIDs,
  isTorznabCapabilitiesStale,
  planTorznabCapabilityChanges,
  readSelectedCategoryIDs,
  torznabConnectionKey,
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

test('new Torznab template treats placeholder defaults and canonical endpoint as safe fills', () => {
  const doc = parseProviderJSON(templateJSON)
  const changes = planTorznabCapabilityChanges(doc, sampleCaps, { isExistingSource: false })
  const pending = changes.filter((item) => !item.matches)

  assert.deepEqual(
    pending.map((item) => item.field),
    ['name', 'id', 'url', 'page_size'],
  )
  assert.equal(
    pending.some((item) => item.overwrites),
    false,
  )

  const applied = applyTorznabCapabilityChanges(doc, changes, { includeOverwrites: false })
  assert.equal(applied.length, 4)
  assert.equal(doc.get('name'), 'Cinema Archive')
  assert.equal(doc.get('id'), 'cinema_archive')
  assert.equal(doc.get('url'), 'https://tracker.example/api')
  assert.equal(doc.get('page_size'), 200)
})

test('new Torznab draft with custom name flags only that field as overwrite', () => {
  const doc = parseProviderJSON(templateJSON)
  doc.set('name', 'My Personal Tracker')

  const changes = planTorznabCapabilityChanges(doc, sampleCaps, { isExistingSource: false })
  const nameChange = changes.find((item) => item.field === 'name')
  const idChange = changes.find((item) => item.field === 'id')

  assert.equal(nameChange?.overwrites, true)
  assert.equal(idChange?.overwrites, false)

  applyTorznabCapabilityChanges(doc, changes, { includeOverwrites: false })
  assert.equal(doc.get('name'), 'My Personal Tracker')
  assert.equal(doc.get('id'), 'cinema_archive')
  assert.equal(doc.get('page_size'), 200)
})

test('existing source never suggests changing id and marks customized fields as overwrites', () => {
  const doc = parseProviderJSON(`{
    "version": 1,
    "id": "existing_source",
    "name": "Existing Custom Name",
    "adapter": "torznab",
    "url": "https://tracker.example/api",
    "enabled": true,
    "page_size": 50,
    "search": { "categories": [] },
    "options": { "category_scope": "combined", "search_mode": "tvsearch" }
  }`)

  const changes = planTorznabCapabilityChanges(doc, sampleCaps, { isExistingSource: true })
  assert.equal(
    changes.some((item) => item.field === 'id'),
    false,
  )
  // tvsearch is advertised available, so it should remain untouched.
  assert.equal(changes.find((item) => item.field === 'search_mode')?.matches, true)
  assert.equal(changes.find((item) => item.field === 'name')?.overwrites, true)
  assert.equal(changes.find((item) => item.field === 'page_size')?.overwrites, true)

  // Non-overwrite pass changes nothing on a customized existing source.
  const safeApplied = applyTorznabCapabilityChanges(doc, changes, { includeOverwrites: false })
  assert.equal(safeApplied.length, 0)
  assert.equal(doc.get('name'), 'Existing Custom Name')
  assert.equal(doc.get('page_size'), 50)

  // Explicit overwrite pass updates both fields while preserving id and tvsearch.
  const allApplied = applyTorznabCapabilityChanges(doc, changes, { includeOverwrites: true })
  assert.deepEqual(
    allApplied.map((item) => item.field),
    ['name', 'page_size'],
  )
  assert.equal(doc.get('id'), 'existing_source')
  assert.equal(doc.get('name'), 'Cinema Archive')
  assert.equal(doc.get('page_size'), 200)
  assert.equal(doc.getIn(['options', 'search_mode']), 'tvsearch')
})

test('existing source with unavailable search_mode flags it for replacement', () => {
  const doc = parseProviderJSON(`{
    "version": 1,
    "id": "movie_only",
    "name": "Cinema Archive",
    "adapter": "torznab",
    "url": "https://tracker.example/api",
    "enabled": true,
    "page_size": 200,
    "options": { "search_mode": "movie" }
  }`)

  const changes = planTorznabCapabilityChanges(doc, sampleCaps, { isExistingSource: true })
  const modeChange = changes.find((item) => item.field === 'search_mode')
  assert.equal(modeChange?.matches, false)
  assert.equal(modeChange?.overwrites, true)
  assert.equal(modeChange?.nextValue, 'search')
  assert.match(modeChange?.currentDisplay ?? '', /unavailable/i)
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

test('staleness ignores canonical endpoint adoption but flags other connection edits', () => {
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
