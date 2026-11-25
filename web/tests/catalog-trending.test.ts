import test from 'node:test'
import assert from 'node:assert/strict'
import {
  defaultTrendingPeriod,
  formatTrendingMeta,
  parseTrendingPeriod,
  trendingCategories,
  trendingMetricLabel,
  trendingMetricTitle,
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

test('trending TV metrics format per-episode and per-season averages with explanatory tooltips', () => {
  const episodic = {
    id: 10,
    type: 'tv' as const,
    title: 'Weekly Hit',
    years: [2025, 2026],
    torrents: 140,
    episodes: 4,
    torrents_per_episode: 35,
  }
  assert.equal(trendingMetricLabel(episodic), '35/ep')
  assert.equal(formatTrendingMeta(episodic), '2025 · 35/ep')
  assert.equal(trendingMetricTitle(episodic), '140 torrents · 4 episodes (35/ep)')

  const fractional = {
    id: 11,
    type: 'tv' as const,
    title: 'Daily Soap',
    years: [2024],
    torrents: 25,
    episodes: 10,
    torrents_per_episode: 2.5,
  }
  assert.equal(trendingMetricLabel(fractional), '2.5/ep')
  assert.equal(formatTrendingMeta(fractional), '2024 · 2.5/ep')
  assert.equal(trendingMetricTitle(fractional), '25 torrents · 10 episodes (2.5/ep)')

  const singleEp = {
    id: 12,
    type: 'tv' as const,
    title: 'Premiere',
    years: [2026],
    torrents: 125.4,
    episodes: 1,
    torrents_per_episode: 125.4,
  }
  assert.equal(trendingMetricLabel(singleEp), '125/ep')
  assert.equal(trendingMetricTitle(singleEp), '125.4 torrents · 1 episode (125/ep)')

  const multiSeason = {
    id: 13,
    type: 'tv' as const,
    title: 'Archive Packs',
    years: [2020],
    torrents: 11,
    seasons: 5,
    torrents_per_season: 2.2,
  }
  assert.equal(trendingMetricLabel(multiSeason), '2.2/season')
  assert.equal(formatTrendingMeta(multiSeason), '2020 · 2.2/season')
  assert.equal(trendingMetricTitle(multiSeason), '11 torrents · 5 seasons (2.2/season)')

  const movie = {
    id: 14,
    type: 'movie' as const,
    title: 'Regular Movie',
    years: [2026],
    torrents: 20,
  }
  assert.equal(trendingMetricLabel(movie), '20 torrents')
  assert.equal(trendingMetricTitle(movie), undefined)
})
