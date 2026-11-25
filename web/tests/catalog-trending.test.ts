import test from 'node:test'
import assert from 'node:assert/strict'
import {
  defaultTrendingPeriod,
  formatTrendingMeta,
  parseTrendingPeriod,
  trendingCategories,
  trendingPeriodLabel,
  trendingPeriods,
  trendingPrimaryYear,
} from '../src/lib/catalog-trending.ts'

test('trending period parsing validates supported periods and falls back to week', () => {
  assert.equal(defaultTrendingPeriod, 'week')
  assert.deepEqual(
    trendingPeriods.map((option) => option.value),
    ['day', 'week', 'month', 'quarter', 'year'],
  )
  for (const period of ['day', 'week', 'month', 'quarter', 'year'] as const) {
    assert.equal(parseTrendingPeriod(period), period)
    assert.ok(trendingPeriodLabel(period).length > 0)
  }
  assert.equal(parseTrendingPeriod(null), 'week')
  assert.equal(parseTrendingPeriod(''), 'week')
  assert.equal(parseTrendingPeriod('decade'), 'week')
})

test('trending categories keep Videos first followed by the remaining catalog categories', () => {
  assert.deepEqual(
    trendingCategories.map((category) => category.id),
    ['videos', 'games', 'software', 'music', 'books'],
  )
  assert.deepEqual(
    trendingCategories.map((category) => category.label),
    ['Videos', 'Games', 'Software', 'Music', 'Books'],
  )
})

test('trending card captions surface primary year or fallback context alongside torrent counts', () => {
  assert.equal(trendingPrimaryYear([2026, 2024, 0, -5]), 2024)
  assert.equal(trendingPrimaryYear([]), undefined)
  assert.equal(
    formatTrendingMeta({
      id: 1,
      type: 'movie',
      title: 'Example Film',
      years: [2026],
      torrents: 1,
    }),
    '2026 · 1 torrent',
  )
  assert.equal(
    formatTrendingMeta({
      id: 2,
      type: 'tv',
      title: 'Example Series',
      years: [2025, 2024],
      torrents: 12,
    }),
    '2024 · 12 torrents',
  )
  assert.equal(
    formatTrendingMeta({
      id: 3,
      type: 'software',
      title: 'MediaPlayer',
      years: [],
      vendor: 'VideoLAN',
      torrents: 5,
    }),
    'VideoLAN · 5 torrents',
  )
  assert.equal(
    formatTrendingMeta({
      id: 4,
      type: 'music',
      title: 'Midnight Echo',
      years: [],
      artists: ['The Synths'],
      torrents: 3,
    }),
    'The Synths · 3 torrents',
  )
  assert.equal(
    formatTrendingMeta({
      id: 5,
      type: 'book',
      title: 'Starlight Atlas',
      years: [],
      authors: ['Ada Lovelace'],
      torrents: 2,
    }),
    'Ada Lovelace · 2 torrents',
  )
  assert.equal(
    formatTrendingMeta({
      id: 6,
      type: 'game',
      title: 'Mystery Quest',
      years: [],
      torrents: 7,
    }),
    '7 torrents',
  )
})
