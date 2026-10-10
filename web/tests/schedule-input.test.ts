import test from 'node:test'
import assert from 'node:assert/strict'
import {
  dailyTime,
  dailyCron,
  timingMode,
  frequencyLabel,
  intervalLabel,
} from '../src/lib/schedule-input.ts'

test('daily time input translates to the server five-field Cron without a browser timezone conversion', () => {
  assert.equal(dailyCron('02:30'), '30 2 * * *')
  assert.equal(dailyCron('00:00'), '0 0 * * *')
  assert.equal(dailyCron('23:59'), '59 23 * * *')
  for (const invalid of ['', '2:30', '24:00', '00:60', '02:30:15', '02:30 Europe/Paris'])
    assert.equal(dailyCron(invalid), '')
})

test('existing daily expressions are recognized without mutating their exact stored text', () => {
  const stored = '  00  03  * * *  '
  assert.equal(dailyTime(stored), '03:00')
  assert.equal(timingMode(undefined, stored), 'daily')
  assert.equal(frequencyLabel(undefined, stored), 'Daily at 03:00')
})

test('custom Cron expressions and unsupported daily shapes stay in the complete advanced editor', () => {
  for (const value of [
    '0 */6 * * *',
    '15 5 * * 1-5',
    '@daily',
    '0 24 * * *',
    '60 0 * * *',
    '0 0 1 * *',
    '0 0 * * * *',
  ]) {
    assert.equal(dailyTime(value), undefined)
    assert.equal(timingMode('1h', value), 'cron')
    assert.equal(frequencyLabel('1h', value), `Custom schedule · ${value}`)
  }
})

test('elapsed intervals remain distinct from a daily wall-clock schedule and custom durations stay exact', () => {
  assert.equal(timingMode('24h', undefined), 'every')
  assert.equal(frequencyLabel('24h', undefined), 'Every 24 hours')
  assert.equal(intervalLabel('168h'), '1 week')
  for (const value of ['90m', '1h30m', '360m', '1.5h', '500ms', '0.000000001s']) {
    assert.equal(intervalLabel(value), value)
    assert.equal(frequencyLabel(value, undefined), `Every ${value}`)
  }
  assert.equal(timingMode(), 'manual')
  assert.equal(frequencyLabel(), 'Manual only')
})
