import test from 'node:test'
import assert from 'node:assert/strict'
import { storageStatus } from '../src/lib/storage-status.ts'

test('storage labels distinguish available, unmeasured and outdated evidence', () => {
  assert.deepEqual(storageStatus(true, '2026-10-01T12:00:00Z'), {
    label: 'Available',
    variant: 'success',
  })
  assert.deepEqual(storageStatus(true, undefined), {
    label: 'Awaiting measurement',
    variant: 'info',
  })
  assert.deepEqual(storageStatus(false, undefined), {
    label: 'Unavailable',
    variant: 'destructive',
  })
  assert.deepEqual(storageStatus(false, '2026-10-01T12:00:00Z'), {
    label: 'Measurement unavailable',
    variant: 'warning',
  })
  assert.deepEqual(storageStatus(true, '2026-10-01T12:00:00Z', true), {
    label: 'Out of date',
    variant: 'warning',
  })
})
