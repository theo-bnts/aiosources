import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  browserPreferences,
  defaultBrowserPreferences,
  normalizeBrowserPreferences,
  refreshInterval,
  saveBrowserPreferences,
} from '../src/lib/browser-preferences.ts'
import { dateTimeToISO, localDateTime } from '../src/lib/format.ts'

test('invalid browser preferences fall back independently without blocking the interface', () => {
  assert.deepEqual(normalizeBrowserPreferences(null), defaultBrowserPreferences)
  assert.deepEqual(
    normalizeBrowserPreferences({
      pageSize: 50000,
      timeZone: 'invalid',
      refresh: false,
      dateFormat: 'broken',
    }),
    defaultBrowserPreferences,
  )
  assert.equal(normalizeBrowserPreferences({ pageSize: 25, timeZone: 'utc' }).pageSize, 25)
})

test('reduced activity changes every refresh class without disabling refresh', () => {
  try {
    saveBrowserPreferences({ ...defaultBrowserPreferences, refresh: 'reduced' })
    assert.equal(refreshInterval('live'), 15000)
    assert.equal(refreshInterval('statistics'), 45000)
    assert.equal(refreshInterval('ordinary'), 90000)
    assert.equal(browserPreferences().refresh, 'reduced')
  } finally {
    saveBrowserPreferences(defaultBrowserPreferences)
  }
})

test('UTC date inputs retain the exact saved instant and unchanged fractional seconds', () => {
  try {
    saveBrowserPreferences({ ...defaultBrowserPreferences, timeZone: 'utc' })
    const saved = '2026-10-22T10:21:33.456Z'
    assert.equal(localDateTime(saved, 'seconds'), '2026-10-22T10:21:33')
    assert.equal(dateTimeToISO('2026-10-22T10:21:33', saved, 'seconds'), saved)
    assert.equal(dateTimeToISO('2026-10-22T12:30'), '2026-10-22T12:30:00.000Z')
  } finally {
    saveBrowserPreferences(defaultBrowserPreferences)
  }
})
