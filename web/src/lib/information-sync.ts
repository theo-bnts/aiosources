import type { BookStatus, MusicStatus, SoftwareStatus, Status } from './aiosource-types'

export function metadataSyncLabel(
  status: Pick<
    Status['status'],
    'pending' | 'last_sync' | 'sync_error' | 'applied_through' | 'recovery_started_at'
  >,
  running: boolean,
  automatic = true,
  recovery = false,
): string {
  if (recovery || status.recovery_started_at)
    return running ? 'Recovering catalog' : 'Recovery pending'
  if (status.sync_error && !running) return 'Retry pending'
  if (status.pending > 0) return running ? 'Applying changes' : 'Changes pending'
  if (!automatic && !running) return 'Automatic updates disabled'
  if (!status.last_sync && !status.applied_through)
    return running ? 'Importing catalog' : 'Not started'
  if (running) return 'Checking changes'
  return status.applied_through ? 'Following changes' : 'Preparing change tracking'
}

export function bookSyncLabel(status: BookStatus, automatic = true): string {
  if (!status.complete)
    return status.running
      ? 'Importing catalog'
      : status.position > 0
        ? 'Import interrupted'
        : 'Not started'
  if (status.needs_export) return 'Waiting for a newer export'
  if (status.error && !status.running) return 'Retry pending'
  if (status.catching_up) return status.running ? 'Catching up' : 'Catch-up pending'
  if (status.pending > 0) return status.running ? 'Applying changes' : 'Changes pending'
  if (!automatic && !status.running) return 'Automatic updates disabled'
  if (status.running) return 'Checking changes'
  return status.changes_through ? 'Following changes' : 'Preparing change tracking'
}

export function softwareSyncLabel(status: SoftwareStatus, automatic = true): string {
  if (status.running) {
    if (status.phase === 'recovery') return 'Recovering catalog'
    if (status.phase === 'nvd_meta') return 'Checking NVD feed'
    if (status.phase === 'nvd_dump') return 'Importing NVD CPE snapshot'
    if (status.phase === 'nvd_incremental') return 'Applying NVD CPE updates'
    if (status.phase === 'winget') return 'Importing WinGet packages'
    if (status.phase === 'homebrew') return 'Importing Homebrew Casks'
    if (status.phase === 'icons') return 'Importing icon index'
    if (status.phase === 'merging' || status.phase === 'projecting' || status.phase === 'cleanup')
      return 'Building software catalog'
    return 'Importing catalog'
  }
  if (!status.complete) return status.position > 0 ? 'Import interrupted' : 'Not started'
  if (status.needs_full_import) return 'Full re-import pending'
  if (status.error) return 'Retry pending'
  if (!automatic) return 'Automatic updates disabled'
  return status.last_sync ? 'Following updates' : 'Not started'
}

export function musicSyncLabel(status: MusicStatus, automatic = true): string {
  if (status.schema_unsupported) return 'Update required (new schema)'
  if (status.running) {
    if (status.phase === 'recovery') return 'Recovering catalog'
    if (status.phase === 'covers') return 'Importing cover index'
    if (status.phase === 'project') return 'Building music catalog'
    if (status.phase === 'replication') return 'Applying Live Data Feed'
    return 'Importing catalog'
  }
  if (!status.complete) return status.position > 0 ? 'Import interrupted' : 'Not started'
  if (status.needs_export) return 'Full export recovery pending'
  if (status.error) return 'Retry pending'
  if (!automatic) return 'Automatic updates disabled'
  if (status.live_enabled) return 'Following Live Data Feed'
  return status.snapshot ? 'Following full exports' : 'Not started'
}
