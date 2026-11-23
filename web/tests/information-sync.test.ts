import test from 'node:test'
import assert from 'node:assert/strict'
import {
  bookSyncLabel,
  metadataSyncLabel,
  musicRecoveryMessage,
  musicSyncLabel,
  softwareRecoveryMessage,
  softwareSyncLabel,
} from '../src/lib/information-sync.ts'
import type { BookStatus, MusicStatus, SoftwareStatus } from '../src/lib/aiosource-types.ts'

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

test('music freshness distinguishes live feed, full exports, recovery and schema blocks', () => {
  const base: MusicStatus = {
    running: false,
    complete: true,
    phase: 'idle',
    selection: 'v1',
    snapshot: '20261114-002230',
    snapshot_at: '2026-11-14T00:22:30Z',
    works: 10,
    releases: 20,
    artists: 5,
    labels: 2,
    covers: 8,
    imported: 30,
    position: 30,
    filtered: 1,
    skipped: 0,
    last_import: '2026-11-14T01:00:00Z',
    last_sync: '2026-11-14T02:00:00Z',
    changes_through: '2026-11-14T02:00:00Z',
    last_packet_at: '2026-11-14T02:00:00Z',
    next_update: null,
    retry_at: null,
    replication_sequence: 101,
    schema_sequence: 30,
    live_enabled: true,
    needs_export: false,
    schema_unsupported: false,
  }
  assert.equal(musicSyncLabel(base), 'Following Live Data Feed')
  assert.equal(musicSyncLabel({ ...base, live_enabled: false }), 'Following full exports')
  assert.equal(musicSyncLabel({ ...base, replication_sequence: 0 }), 'Following full exports')
  assert.equal(musicSyncLabel(base, false), 'Automatic updates disabled')
  const waiting = { ...base, needs_export: true, recovery_reason: 'replication_lag' }
  assert.equal(musicSyncLabel(waiting), 'Full export recovery pending')
  assert.equal(musicSyncLabel({ ...base, needs_export: true }), 'Import interrupted')
  assert.equal(musicRecoveryMessage({ ...waiting, running: true }), undefined)
  assert.match(musicRecoveryMessage(waiting) ?? '', /complete MusicBrainz export/)
  assert.match(musicRecoveryMessage({ ...base, needs_export: true }) ?? '', /unfinished/)
  assert.equal(
    musicSyncLabel({ ...base, schema_unsupported: true }),
    'Update required (new schema)',
  )
  assert.equal(musicSyncLabel({ ...base, running: true, phase: 'covers' }), 'Importing cover index')
  assert.equal(
    musicSyncLabel({ ...base, running: true, phase: 'projecting' }),
    'Building music catalog',
  )
  assert.equal(musicSyncLabel({ ...base, running: true, phase: 'recovery' }), 'Recovering catalog')
  assert.equal(musicSyncLabel({ ...base, complete: false, position: 0 }), 'Not started')
})

test('software freshness distinguishes phases, recovery, errors and disabled automatic updates', () => {
  const base: SoftwareStatus = {
    running: false,
    complete: true,
    phase: 'idle',
    works: 25,
    nvd_entries: 15,
    winget_entries: 10,
    homebrew_entries: 8,
    icons: 12,
    icons_indexed: 3000,
    imported: 33,
    position: 33,
    filtered: 5,
    skipped: 0,
    nvd_snapshot_at: '2026-11-14T00:00:00Z',
    nvd_changes_through: '2026-11-14T02:00:00Z',
    winget_synced_at: '2026-11-14T02:00:00Z',
    homebrew_synced_at: '2026-11-14T02:00:00Z',
    icons_synced_at: '2026-11-14T02:00:00Z',
    last_import: '2026-11-14T01:00:00Z',
    last_sync: '2026-11-14T02:00:00Z',
    next_update: null,
    retry_at: null,
    nvd_key_configured: true,
    needs_full_import: false,
  }
  assert.equal(softwareSyncLabel(base), 'Following updates')
  assert.equal(softwareSyncLabel(base, false), 'Automatic updates disabled')
  assert.equal(softwareSyncLabel({ ...base, needs_full_import: true }), 'Full re-import pending')
  assert.equal(
    softwareRecoveryMessage({ ...base, needs_full_import: true, running: true }),
    undefined,
  )
  assert.match(
    softwareRecoveryMessage({ ...base, needs_full_import: true }) ?? '',
    /recheck its sources/,
  )
  assert.equal(softwareSyncLabel({ ...base, error: 'NVD unavailable' }), 'Retry pending')
  assert.equal(
    softwareSyncLabel({ ...base, running: true, phase: 'nvd_dump' }),
    'Importing NVD CPE snapshot',
  )
  assert.equal(
    softwareSyncLabel({ ...base, running: true, phase: 'winget' }),
    'Importing WinGet packages',
  )
  assert.equal(
    softwareSyncLabel({ ...base, running: true, phase: 'homebrew' }),
    'Importing Homebrew Casks',
  )
  assert.equal(
    softwareSyncLabel({ ...base, running: true, phase: 'icons' }),
    'Importing icon index',
  )
  assert.equal(
    softwareSyncLabel({ ...base, running: true, phase: 'recovery' }),
    'Recovering catalog',
  )
  assert.equal(softwareSyncLabel({ ...base, complete: false, position: 0 }), 'Not started')
  assert.equal(softwareSyncLabel({ ...base, complete: false, position: 10 }), 'Import interrupted')
})
