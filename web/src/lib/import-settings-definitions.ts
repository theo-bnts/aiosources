export const importNumberFields = {
  workers: {
    label: 'Simultaneous imports',
    min: 1,
    max: 32,
    help: 'Shared maximum across this database. Per-source request pacing is unchanged.',
  },
  max_quota_retries: {
    label: 'Automatic retries after HTTP 429',
    min: 0,
    max: 10,
    help: '0 = no automatic retry. Server quota deadlines are always respected.',
  },
  max_quota_wait_seconds: {
    label: 'Maximum automatic quota wait (seconds)',
    min: 0,
    max: 86400,
    help: '0 = unlimited. A longer server-requested wait pauses the run after saving its status and deadline, not its response body. Explicit Resume accepts that wait without shortening the server deadline.',
  },
  no_progress_requests: {
    label: 'Responses without new source IDs',
    min: 0,
    max: 100000,
    help: '0 = disabled. Counts successful data responses without a previously unseen native ID, not quota errors or auxiliary/control responses.',
  },
} as const
