import type { BookStatus } from './aiosource-types'

export function bookSyncLabel(status: BookStatus, automatic = true): string {
  if (!status.complete)
    return status.running
      ? 'Importing catalog'
      : status.position > 0
        ? 'Import interrupted'
        : 'Not started'
  if (status.needs_export) return 'Waiting for a newer export'
  if (status.error) return 'Retry pending'
  if (status.catching_up) return 'Catching up'
  if (status.pending > 0) return status.running ? 'Applying changes' : 'Changes pending'
  if (!automatic && !status.running) return 'Automatic updates disabled'
  return status.changes_through ? 'Following changes' : 'Preparing change tracking'
}
