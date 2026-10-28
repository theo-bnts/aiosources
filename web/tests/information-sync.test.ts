import test from 'node:test'
import assert from 'node:assert/strict'
import { bookSyncLabel, metadataSyncLabel } from '../src/lib/information-sync.ts'
import type { BookStatus } from '../src/lib/aiosource-types.ts'

const following: BookStatus = {
  snapshot_at: '2026-01-01T00:00:00Z',
  last_import: '2026-01-02T00:00:00Z',
  changes_through: '2026-01-03T00:00:00Z',
  pending: 0,
  catching_up: false,
  needs_export: false,
  skipped: 0,
  complete: true,
  position: 3,
  imported: 3,
  works: 1,
  editions: 1,
  authors: 1,
  last_sync: '2026-01-03T00:00:00Z',
  running: false,
}

test('book freshness does not hide gaps, unapplied changes or interrupted imports', () => {
  assert.equal(bookSyncLabel(following), 'Following changes')
  assert.equal(bookSyncLabel(following, false), 'Automatic updates disabled')
  assert.equal(bookSyncLabel({ ...following, pending: 2 }), 'Changes pending')
  assert.equal(bookSyncLabel({ ...following, pending: 2, running: true }), 'Applying changes')
  assert.equal(bookSyncLabel({ ...following, needs_export: true }), 'Waiting for a newer export')
  assert.equal(bookSyncLabel({ ...following, catching_up: true }), 'Catch-up pending')
  assert.equal(bookSyncLabel({ ...following, catching_up: true, running: true }), 'Catching up')
  assert.equal(bookSyncLabel({ ...following, running: true }), 'Checking changes')
  assert.equal(bookSyncLabel({ ...following, complete: false }), 'Import interrupted')
})

test('film and series freshness distinguishes recovery, discovery and applied information', () => {
  const status = {
    pending: 0,
    last_sync: following.last_sync,
    sync_error: '',
    applied_through: following.changes_through,
    recovery_started_at: null,
  }
  assert.equal(metadataSyncLabel(status, false), bookSyncLabel(following))
  assert.equal(metadataSyncLabel(status, true), bookSyncLabel({ ...following, running: true }))
  assert.equal(metadataSyncLabel(status, false, false), bookSyncLabel(following, false))
  assert.equal(metadataSyncLabel({ ...status, pending: 2 }, false), 'Changes pending')
  assert.equal(metadataSyncLabel({ ...status, pending: 2 }, true), 'Applying changes')
  assert.equal(metadataSyncLabel(status, false, true, true), 'Recovery pending')
  assert.equal(
    metadataSyncLabel({ ...status, recovery_started_at: following.last_sync }, true),
    'Recovering catalog',
  )
  assert.equal(
    metadataSyncLabel({ ...status, sync_error: 'A retry is scheduled.' }, false),
    'Retry pending',
  )
  assert.equal(
    metadataSyncLabel({ ...status, last_sync: null, applied_through: null }, true),
    'Importing catalog',
  )
})
