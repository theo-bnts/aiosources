export const runtimeSections = {
  debrid: {
    title: 'Operation capacity',
    help: 'Limits per application process. Active work finishes when a limit is reduced; new operations wait or remain available for retry. Each cache update checks a requested title and may make several provider calls. Provider request pacing is unchanged.',
    fields: [
      ['cache_jobs', 'Simultaneous cache updates', 1, 32],
      ['download_jobs', 'Simultaneous download preparations', 1, 32],
      ['cleanup_jobs', 'Simultaneous cleanup operations', 1, 32],
      ['public_checks', 'Simultaneous public availability checks', 1, 16],
    ],
  },
  outgoing: {
    title: 'Request timeouts',
    help: 'Maximum time for each outgoing request, including its response body. Source timeouts remain configurable in each source. Changes apply to new requests. The remaining time budget of an operation can be shorter.',
    fields: [
      ['metadata_seconds', 'Metadata request timeout (seconds)', 5, 600],
      ['debrid_seconds', 'Debrid request timeout (seconds)', 5, 600],
      ['inventory_seconds', 'Metadata inventory download timeout (seconds)', 30, 3600],
    ],
  },
  connections: {
    title: 'Request timeouts',
    help: 'Maximum processing and delivery time for new incoming requests, shared by private and public connections. A provider request also respects its outgoing timeout. This does not change the lifetime of an active file download.',
    fields: [
      ['stremio_seconds', 'Stremio request timeout (seconds)', 10, 600],
      ['torznab_seconds', 'Torznab request timeout (seconds)', 5, 600],
      ['catalog_seconds', 'Shared catalog request timeout (seconds)', 5, 600],
    ],
  },
  history: {
    title: 'History retention',
    help: 'Expired diagnostics are removed progressively. Published data and evidence needed to resume imports are preserved. Shortening retention can permanently remove older history; increasing it cannot restore deleted history. Stremio history also keeps its 10,000-request safety limit.',
    fields: [
      ['days', 'Keep history (days)', 1, 3650],
      ['cleanup_seconds', 'Cleanup interval (seconds)', 5, 3600],
      ['batch_size', 'Rows per table cleanup batch', 100, 20000],
      ['pass_seconds', 'Maximum cleanup pass (seconds)', 1, 60],
    ],
  },
  identification: {
    title: 'Identification processing',
    help: 'Controls local identification, independently of the TMDB request rate. Use 0 workers for automatic capacity based on available database connections. Smaller batches reduce memory and checkpoint more often. Changes apply to the next batch; active batches finish.',
    fields: [
      ['workers', 'Maximum identification workers (0 = automatic)', 0, 32],
      ['queue_batch', 'Queued torrents per pass', 10, 2000],
      ['transaction_batch', 'Torrents per saved batch', 1, 100],
    ],
  },
  matching: {
    title: 'Matching reliability',
    help: 'Higher values make automatic identification more conservative. Saving rechecks automatic results; manual reviews are preserved. Conflicting identifiers, incompatible years and invalid numbering remain rejected. Pending rechecks run automatically when identification is enabled.',
    fields: [
      ['minimum_similarity', 'Minimum close-title similarity (%)', 92, 100],
      ['ambiguity_margin', 'Ambiguity margin (%)', 8, 25],
    ],
  },
  metadata: {
    title: 'Information processing',
    help: 'Controls concurrent information requests and bounded processing batches. The configured TMDB request rate still applies. Smaller saved batches reduce memory and commit progress more often. Changes apply to the next batch.',
    fields: [
      ['workers', 'Simultaneous information requests', 1, 32],
      ['queue_batch', 'Information records per pass', 1, 1000],
      ['publication_batch', 'Information records per saved batch', 1, 100],
    ],
  },
  metadata_retries: {
    title: 'Information retries',
    help: 'Temporary provider errors use a limited number of immediate attempts, then a saved retry clock. Delays increase up to the configured maximum; provider quota deadlines are always respected. Authentication errors are not retried within the same request.',
    fields: [
      ['request_attempts', 'Attempts per information request', 1, 10],
      ['request_retry_seconds', 'Initial request retry delay (seconds)', 1, 60],
      ['sync_retry_seconds', 'Initial update retry delay (seconds)', 10, 3600],
      ['sync_retry_max_seconds', 'Maximum update retry delay (seconds)', 60, 86400],
      ['detail_retry_seconds', 'Initial deferred-record retry delay (seconds)', 10, 3600],
      ['detail_retry_max_seconds', 'Maximum deferred-record retry delay (seconds)', 60, 604800],
    ],
  },
  debrid_retries: {
    title: 'Availability retries',
    help: 'Controls retries for incomplete availability checks. Delays increase up to the maximum; valid cached results remain usable. Temporary-item cleanup keeps its ownership checks and manual attention for failed deletions.',
    fields: [
      ['delay_minutes', 'Initial availability retry delay (minutes)', 1, 1440],
      ['maximum_hours', 'Maximum availability retry delay (hours)', 1, 168],
    ],
  },
  source_retries: {
    title: 'Temporary source errors',
    help: 'New imports capture these settings for temporary GET/HEAD failures, including HTTP 500, 502, 503, 504 and transient HTML responses. Existing imports keep their original retry policy across pauses and restarts. Source pacing, quota deadlines and separately configured HTTP 429 handling remain enforced. A retry count of 0 keeps the failed response for manual recovery.',
    fields: [
      ['retries', 'Automatic retries after a temporary source error', 0, 10],
      ['initial_seconds', 'Initial source retry delay (seconds)', 1, 60],
      ['maximum_seconds', 'Maximum source retry delay (seconds)', 1, 900],
    ],
  },
  downloads: {
    title: 'Browser downloads',
    help: 'Applies to public and administrator downloads through this application. Transfer duration starts when a download begins. Link validity controls how long a newly generated link can be opened; existing links retain their expiry. Session revocation, public-access changes and destination checks still apply.',
    fields: [
      ['duration_hours', 'Maximum transfer duration (hours)', 1, 168],
      ['ticket_minutes', 'New download link validity (minutes)', 1, 60],
    ],
  },
  security: {
    title: 'Authentication settings',
    help: 'Session duration applies to new sign-ins. Reducing the session limit does not revoke existing sessions. Sign-in limits apply per minute across application replicas; global capacity must cover the per-address limit. Two-step enrollment duration applies to new setups. Password, CSRF and factor replay protections remain enforced.',
    fields: [
      ['session_hours', 'New administrator session duration (hours)', 1, 168],
      ['session_limit', 'Maximum active administrator sessions', 1, 1024],
      ['login_per_minute', 'Sign-in attempts per address per minute', 1, 60],
      ['login_global_per_minute', 'Total sign-in attempts per minute', 1, 600],
      ['enrollment_minutes', 'Two-step setup validity (minutes)', 5, 30],
    ],
  },
  payloads: {
    title: 'Configuration limits',
    help: 'Limits newly saved source definitions and newly generated public links. Existing definitions and links remain readable. Smaller limits reduce accepted configuration sizes; protocol ceilings, encryption, TLS validation and destination restrictions remain enforced.',
    fields: [
      ['source_kib', 'Source definition limit (KiB)', 16, 128],
      ['public_stremio_bytes', 'Public Search / Stremio configuration limit (bytes)', 1024, 8192],
      ['public_torznab_bytes', 'Public Torznab configuration limit (bytes)', 1024, 6000],
    ],
  },
} as const

export type RuntimeSection = keyof typeof runtimeSections
