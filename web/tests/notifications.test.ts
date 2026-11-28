import assert from 'node:assert/strict'
import test from 'node:test'

import {
  alertCardMetric,
  alertCardSummary,
  formatAlertByteSize,
  formatAlertEpisode,
  formatAlertFiltersSummary,
  formatAlertSizeInput,
  groupAlertsByCategory,
  hasActiveAlertFilters,
  notificationChannelTypeLabel,
  parseAlertSizeInput,
  type ReleaseAlert,
} from '../src/lib/notification-types.ts'

test('parseAlertSizeInput accepts human decimal/binary sizes and exact byte counts', () => {
  assert.deepEqual(parseAlertSizeInput(''), {})
  assert.deepEqual(parseAlertSizeInput('0'), {})
  assert.deepEqual(parseAlertSizeInput('1048576'), { bytes: 1048576 })
  assert.deepEqual(parseAlertSizeInput('1 GB'), { bytes: 1_000_000_000 })
  assert.deepEqual(parseAlertSizeInput('1.5 GiB'), { bytes: 1_610_612_736 })
  assert.deepEqual(parseAlertSizeInput('500 MB'), { bytes: 500_000_000 })
  assert.deepEqual(parseAlertSizeInput('1,5 Go'), { bytes: 1_500_000_000 })
  assert.ok(parseAlertSizeInput('-5 GB').error)
  assert.ok(parseAlertSizeInput('lots').error)
})

test('formatAlertSizeInput and formatAlertByteSize preserve clean round-trip units', () => {
  assert.equal(formatAlertSizeInput(1_000_000_000), '1 GB')
  assert.equal(formatAlertSizeInput(1024 ** 3), '1 GiB')
  assert.equal(formatAlertByteSize(1_500_000_000), '1.5 GB')
  assert.equal(formatAlertByteSize(0), '')
})

test('alert episode and filter summaries format cleanly for display', () => {
  assert.equal(formatAlertEpisode(1, 2), 'S01E02')
  assert.equal(formatAlertEpisode(undefined, 2), '')
  assert.equal(hasActiveAlertFilters({}), false)
  assert.equal(hasActiveAlertFilters({ language_mode: 'all' }), false)
  assert.equal(hasActiveAlertFilters({ min_size: 1_000_000_000 }), true)
  assert.deepEqual(
    formatAlertFiltersSummary(
      {
        providers: ['src-a'],
        language: 'fr',
        language_mode: 'multi',
        min_size: 1_000_000_000,
        cached_only: true,
      },
      (id) => (id === 'src-a' ? 'Tracker A' : id),
    ),
    ['Tracker A', 'FR + MULTI', 'Min 1 GB', 'Cached only'],
  )
  assert.equal(notificationChannelTypeLabel('discord'), 'Discord')
  assert.equal(notificationChannelTypeLabel('webhook'), 'Generic webhook')
})

test('groupAlertsByCategory mirrors Search category order and hides empty categories', () => {
  const sample = (partial: Partial<ReleaseAlert>): ReleaseAlert => ({
    id: 'a-1',
    owner_type: 'admin',
    work_id: 1,
    media_type: 'movie',
    title: 'Sample',
    channel_id: 'ch-1',
    channel_name: 'Discord Movies',
    channel_type: 'discord',
    filters: {},
    status: 'watching',
    attempts: 0,
    next_attempt: '2026-11-28T12:00:00Z',
    revision: 1,
    created_at: '2026-11-28T12:00:00Z',
    updated_at: '2026-11-28T12:00:00Z',
    ...partial,
  })
  const movie = sample({
    id: 'a-movie',
    work_id: 10,
    media_type: 'movie',
    title: 'Dune 3',
    years: [2027],
  })
  const episode = sample({
    id: 'a-tv',
    work_id: 11,
    media_type: 'tv',
    title: 'Chronicle',
    season: 2,
    episode: 4,
    filters: { min_size: 1_000_000_000 },
  })
  const album = sample({
    id: 'a-music',
    work_id: 12,
    media_type: 'music',
    title: 'Next Album',
    status: 'retrying',
    last_error: 'HTTP 500',
  })

  const grouped = groupAlertsByCategory([album, episode, movie])
  assert.deepEqual(
    grouped.map((section) => section.category.id),
    ['videos', 'music'],
  )
  assert.equal(grouped[0].category.frame, 'portrait')
  assert.equal(grouped[1].category.frame, 'square')
  assert.deepEqual(
    grouped[0].items.map((item) => item.id),
    ['a-tv', 'a-movie'],
  )
  assert.equal(alertCardSummary(movie), 'Movie · 2027')
  assert.equal(alertCardSummary(episode), 'Series · S02E04')
  assert.equal(alertCardMetric(episode).label, 'Discord Movies · Min 1 GB')
  assert.equal(alertCardMetric(album).label, 'Retrying delivery')
  assert.match(alertCardMetric(album).title, /HTTP 500/)
})
