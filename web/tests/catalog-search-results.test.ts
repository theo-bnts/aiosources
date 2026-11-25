import test from 'node:test'
import assert from 'node:assert/strict'
import {
  catalogSearchCategories,
  catalogSearchPlan,
  compareCatalogTitles,
  DEFAULT_SEARCH_RAIL_LIMIT,
  hasCatalogRanking,
  MAX_SEARCH_RAIL_LIMIT,
  mergeCatalogPages,
  nextSearchRailLimit,
} from '../src/lib/catalog-search-results.ts'
import { catalogCardSummary, catalogSubtypeLabel } from '../src/lib/catalog-cards.ts'
import type { CatalogTitle } from '../src/lib/catalog-search-types.ts'

function title(id: number, type: CatalogTitle['type'], year: number): CatalogTitle {
  return {
    id,
    type,
    title: `Example ${id}`,
    years: [2099],
    tmdb: '',
    imdb: '',
    releases: 1,
    providers: [],
    seasons: null,
    selection: {},
    ranking: { precision: 4, relevance: 1, year },
  }
}

test('category requests preserve selected formats and query books once without duplication', () => {
  const plan = catalogSearchPlan('', 50, 0)
  assert.equal(plan.parallel, true)
  assert.deepEqual(
    plan.requests.map(({ category, type, limit, offset }) => ({ category, type, limit, offset })),
    [
      { category: 'Videos', type: 'movie,tv', limit: 50, offset: 0 },
      { category: 'Games', type: 'game', limit: 50, offset: 0 },
      { category: 'Software', type: 'software', limit: 50, offset: 0 },
      { category: 'Music', type: 'music', limit: 50, offset: 0 },
      { category: 'Books', type: 'ebook,audiobook,book', limit: 50, offset: 0 },
    ],
  )
  assert.deepEqual(
    catalogSearchPlan('audiobook,movie,ebook,audiobook', 25, 25).requests.map((r) => r.type),
    ['movie', 'ebook,audiobook'],
  )
  assert.deepEqual(catalogSearchPlan('ebook,audiobook', 50, 100).requests, [
    { category: 'Books', type: 'ebook,audiobook', limit: 50, offset: 100, categories: 1 },
  ])
  for (const type of ['unknown', 'movie,unknown', 'movie,']) {
    const invalid = catalogSearchPlan(type, 50, 0)
    assert.equal(invalid.parallel, false)
    assert.equal(invalid.requests[0].type, type, 'invalid selections must still reach validation')
  }
})

test('late categories use SQL ranking, including stored years and stable identifiers', () => {
  const movie = title(1, 'movie', 2026)
  const game = title(2, 'game', 2010)
  const book = title(3, 'book', 2020)
  const page = (items: CatalogTitle[]) => ({ items, total: items.length, limit: 50, offset: 0 })
  assert.deepEqual(mergeCatalogPages([page([game]), page([movie])], 50, 0).items, [movie, game])
  assert.deepEqual(mergeCatalogPages([page([game]), page([movie]), page([book])], 50, 0).items, [
    movie,
    book,
    game,
  ])
  const precise = { ...title(4, 'tv', 2000), ranking: { precision: 5, relevance: 0, year: 2000 } }
  const partial = { ...title(5, 'book', 2100), ranking: { precision: 1, relevance: 1, year: 2100 } }
  const lowerSimilarity = {
    ...title(6, 'tv', 2100),
    ranking: { precision: 4, relevance: 0.8, year: 2100 },
  }
  const tied = title(7, 'book', 2010)
  assert.deepEqual([partial, game, tied, precise, lowerSimilarity].sort(compareCatalogTitles), [
    precise,
    game,
    tied,
    lowerSimilarity,
    partial,
  ])
  assert.equal(hasCatalogRanking(movie), true)
  assert.equal(hasCatalogRanking({ ...movie, ranking: undefined }), false)
  assert.equal(
    hasCatalogRanking({ ...movie, ranking: { precision: 4, relevance: NaN, year: 2020 } }),
    false,
  )
})

test('bounded category prefixes produce the same global page and total at every supported size', () => {
  const kinds = ['movie', 'game', 'software', 'music', 'book'] as const
  const all = Array.from({ length: 240 }, (_, i) => title(i + 1, kinds[i % 5], 2100 - i))
  for (const limit of [25, 50, 100]) {
    for (const offset of [0, 25, 100, 175]) {
      const plan = catalogSearchPlan('', limit, offset)
      if (!plan.parallel) {
        assert.ok(offset + limit > 200)
        assert.equal(plan.requests.length, 1)
        assert.equal(plan.requests[0].offset, offset)
        assert.equal(plan.requests[0].categories, 5)
        continue
      }
      const pages = kinds.map((kind) => {
        const items = all.filter((item) => item.type === kind)
        return {
          items: items.slice(0, offset + limit),
          total: items.length,
          offset: 0,
          limit: offset + limit,
        }
      })
      const combined = mergeCatalogPages(pages.reverse(), limit, offset)
      assert.deepEqual(combined.items, all.slice(offset, offset + limit))
      assert.equal(combined.total, all.length)
    }
  }
  assert.deepEqual(
    mergeCatalogPages([{ items: null, total: 0, limit: 50, offset: 0 }], 50, 0).items,
    [],
  )
})

test('catalogSearchCategories matches Trending category order and frames while preserving filters', () => {
  assert.deepEqual(catalogSearchCategories(''), [
    { id: 'videos', label: 'Videos', frame: 'portrait', type: 'movie,tv' },
    { id: 'games', label: 'Games', frame: 'portrait', type: 'game' },
    { id: 'software', label: 'Software', frame: 'icon', type: 'software' },
    { id: 'music', label: 'Music', frame: 'square', type: 'music' },
    { id: 'books', label: 'Books', frame: 'portrait', type: 'ebook,audiobook,book' },
  ])
  assert.deepEqual(catalogSearchCategories('tv,ebook'), [
    { id: 'videos', label: 'Videos', frame: 'portrait', type: 'tv' },
    { id: 'books', label: 'Books', frame: 'portrait', type: 'ebook' },
  ])
  assert.deepEqual(catalogSearchCategories('unknown'), [
    { id: 'catalog', label: 'Catalog', frame: 'portrait', type: 'unknown' },
  ])
  assert.equal(DEFAULT_SEARCH_RAIL_LIMIT, 24)
  assert.equal(nextSearchRailLimit(24), 48)
  assert.equal(nextSearchRailLimit(48), 96)
  assert.equal(nextSearchRailLimit(96), 150)
  assert.equal(nextSearchRailLimit(150), MAX_SEARCH_RAIL_LIMIT)
  assert.equal(catalogSubtypeLabel({ type: 'movie' }), 'Movie')
  assert.equal(catalogSubtypeLabel({ type: 'tv' }), 'Series')
  assert.equal(catalogSubtypeLabel({ type: 'music', music_type: 'EP' }), 'EP')
  assert.equal(catalogCardSummary({ type: 'tv', years: [2024, 2026] }), 'Series · 2024')
  assert.equal(catalogCardSummary({ type: 'movie', years: [2026] }), 'Movie · 2026')
})
