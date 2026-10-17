import test from 'node:test'
import assert from 'node:assert/strict'
import {
  torznabPreferenceKey,
  torznabPreferenceError,
  torznabCachePreferences,
} from '../src/lib/torznab-preferences.ts'

test('reverting saved cache filters restores the same defaults as URL options', () => {
  assert.deepEqual(
    torznabCachePreferences({ services: [], cached_only: false }),
    torznabCachePreferences({}),
  )
  assert.deepEqual(
    torznabCachePreferences({
      sources: ['alpha'],
      media: ['tv'],
      services: ['torbox'],
      cached_only: true,
    }),
    { services: ['torbox'], cached_only: true },
  )
  assert.equal(
    torznabPreferenceKey('encrypted-secret', { services: [], cached_only: false }),
    'encrypted-secret',
  )
})

test('Torznab URL filters are replaceable without resubmitting personal credentials', () => {
  const first = torznabPreferenceKey('encrypted-secret', {
    sources: ['alpha'],
    services: ['torbox'],
    cached_only: true,
  })
  const second = torznabPreferenceKey(first, { sources: ['beta'], media: ['tv'] })
  assert.equal(second.split('~')[0], 'encrypted-secret')
  assert.equal(second.split('~').length, 3)
  assert.deepEqual(JSON.parse(Buffer.from(second.split('~')[2], 'base64url').toString()), {
    sources: ['beta'],
    media: ['tv'],
  })
  assert.equal(torznabPreferenceKey(second, {}), 'encrypted-secret')
  assert.equal(torznabPreferenceError(second, { sources: ['beta'] }), '')
})

test('Torznab URL filters reject configurations beyond the shared request budget', () => {
  assert.notEqual(
    torznabPreferenceError('encrypted-secret', {
      sources: Array.from({ length: 101 }, (_, i) => 'source-' + i),
    }),
    '',
  )
  assert.notEqual(torznabPreferenceError('a'.repeat(6001), {}), '')
  assert.equal(torznabPreferenceKey('', {}), '')
})
