import assert from 'node:assert/strict'
import { test } from 'node:test'
import { bytes, dateTimeToISO, localDateTime } from '../src/lib/format.ts'

test('date fields follow browser local time, including daylight saving and expiration seconds', () => {
  const original = process.env.TZ
  try {
    process.env.TZ = 'Europe/Paris'
    assert.equal(localDateTime('2026-01-02T03:04:05Z'), '2026-01-02T04:04')
    assert.equal(localDateTime('2026-07-02T03:04:05Z'), '2026-07-02T05:04')
    assert.equal(localDateTime('2026-07-02T03:04:05Z', 'seconds'), '2026-07-02T05:04:05')
    const repeatedHour = '2026-10-25T01:30:10.123Z'
    assert.equal(
      dateTimeToISO(localDateTime(repeatedHour, 'seconds'), repeatedHour, 'seconds'),
      repeatedHour,
    )
    assert.equal(
      dateTimeToISO('2026-10-25T03:30:10', repeatedHour, 'seconds'),
      '2026-10-25T02:30:10.000Z',
    )
    assert.equal(dateTimeToISO('', repeatedHour, 'seconds'), null)
    process.env.TZ = 'UTC'
    assert.equal(localDateTime('2026-07-02T03:04:05Z', 'seconds'), '2026-07-02T03:04:05')
    for (const value of [undefined, null, '', 'invalid']) assert.equal(localDateTime(value), '')
  } finally {
    if (original === undefined) delete process.env.TZ
    else process.env.TZ = original
  }
})

test('storage values keep their existing units and distinguish unknown sizes from zero', () => {
  assert.equal(bytes(undefined), 'Not provided')
  assert.equal(bytes(null), 'Not provided')
  assert.equal(bytes(0), '0 B')
  assert.equal(bytes(1023), '1,023 B')
  assert.equal(bytes(1024), '1 KiB')
  assert.equal(bytes(1536), '1.5 KiB')
  assert.equal(bytes(1024 ** 4), '1 TiB')
})
