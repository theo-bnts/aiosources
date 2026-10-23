export type RunMode = 'preview' | 'incremental' | 'full' | 'metadata'
export type RunStatus = 'queued' | 'running' | 'paused' | 'succeeded' | 'failed' | 'cancelled'
export interface Schedule {
  enabled?: boolean
  every?: string
  full_every?: string
  cron?: string
  timezone?: string
  mode?: Exclude<RunMode, 'preview'>
  max_pages?: number
  known_pages?: number
}
export interface CatalogSync {
  enabled: boolean
  interval_minutes: number
}
export interface ScheduleSummary {
  provider_id: string
  provider_name: string
  provider_enabled: boolean
  valid: boolean
  revision: string
  schedule: Schedule
  enabled: boolean
  state: 'manual' | 'disabled' | 'waiting' | 'running' | 'paused' | 'blocked' | 'invalid'
  next_run_at?: string
  next_full_at?: string
  last_run?: Run
  last_error?: string
}
export interface Provider {
  version: number
  id: string
  name: string
  adapter: string
  url: string
  enabled: boolean
  catalog_sync?: CatalogSync
  auth: {
    type: string
    secret_ref?: string
    username_ref?: string
    password_ref?: string
    in?: string
    name?: string
  }
  request_interval: string
  request_timeout?: string
  request_limits?: {
    per_minute?: number
    per_hour?: number
    per_day?: number
  }
  rate_limit_reset?: 'epoch' | 'relative'
  page_size: number
  search: { query?: string; categories: number[] | null }
  http: {
    method: string
    headers?: Record<string, string>
    secret_headers?: Record<string, string>
    query?: Record<string, unknown>
    incremental_query?: Record<string, unknown>
    body?: unknown
    items_path: string
    catalog?: boolean
  }
  html?: {
    page_selector: string
    rows_selector: string
    empty_selector?: string
    next_selector?: string
    id: HTMLField
    fields: Record<string, HTMLField>
    total?: HTMLField
    current?: HTMLField
    details?: {
      url: HTMLField
      page_selector: string
      fields: Record<string, HTMLField>
    }
  }
  pagination: {
    type: string
    in?: string
    page_param?: string
    offset_param?: string
    size_param?: string
    cursor_param?: string
    start: number
    next_path?: string
    total_path?: string
    current_path?: string
  }
  mapping: { id: string; fields: Record<string, string> | null }
  traversal?: {
    window_pages: number
    incremental_order?: 'id' | 'published_at'
    enrich_fields?: string[]
    metadata_after_incremental?: boolean
    minimum_total?: number
    total_mode?: 'strict' | 'at_least'
    total_paths: string[]
    scopes: {
      id: string
      query?: Record<string, unknown>
      match: Record<string, unknown>
    }[]
    partitions?: {
      parameter: string
      start: number
      end?: number
      end_year_offset?: number
    }[]
    query_variants?: Record<string, unknown>[]
    options?: {
      url: string
      groups_path: string
      values_path: string
      value_path: string
      query_param: string
      priority_path?: string
    }
    id_recovery?: {
      discovery_query: Record<string, unknown>
      first: number
      resolve_url: string
      resolve_pattern: string
      detail_url: string
      detail_path: string
      mapping: Provider['mapping']
    }
  }
  output: { fields?: string[] }
  schedule: Schedule
  options?: Record<string, unknown>
}

export interface HTMLField {
  selector?: string
  attribute?: string
  pattern?: string
  format?: 'text' | 'integer' | 'size' | 'date' | 'url' | 'info_hash' | 'torrent' | ''
  all?: boolean
  required?: boolean
  date_layout?: string
  timezone?: string
}
export interface ProviderDocument {
  provider: Provider
  json: string
  revision: string
  issues: string[] | null
}
export interface ProviderSummary {
  remote_catalog?: boolean
  id: string
  name: string
  adapter: string
  enabled: boolean
  revision: string
  valid: boolean
  issues: string[] | null
  output_fields?: string[]
  last_run?: Run
}
export interface SourceTorrentStats {
  provider_id: string
  imported: number
  matched: number
  ambiguous: number
  unmatched: number
  pending: number
  pending_import?: { run_id: string; torrents: number; new: number; updates: number }
  storage?: { bytes: number; measured_at: string }
}

export type SourceTorrentOverview = {
  items: SourceTorrentStats[]
  totals: { imported: number; awaiting_import: number }
}
export interface AdapterInfo {
  type: string
  profile?: string
  name: string
  description: string
  template: string
}
export interface Validation {
  valid: boolean
  id?: string
  issues: string[] | null
  provider?: Provider
}
export interface SecretInfo {
  name: string
  revision: number
  updated_at: string
}
export interface CollectionPolicy {
  source_retries?: { retries: number; initial_seconds: number; maximum_seconds: number }
  max_quota_retries: number
  max_quota_wait_seconds: number
  no_progress_requests: number
  no_progress_action: 'warn' | 'pause'
  max_duration_seconds: number
}
export type PauseReason = '' | 'manual' | 'budget' | 'quota' | 'no_progress' | 'interrupted'
export interface Run {
  remote_catalog?: boolean
  storage_mode: 'compact' | 'full'
  id: string
  provider_id: string
  provider_name: string
  mode: RunMode
  initial_full?: boolean
  trigger?: 'manual' | 'scheduled'
  metadata_parent_run_id?: string
  status: RunStatus
  cancel_requested: boolean
  pause_requested: boolean
  traversal_done: boolean
  policy?: CollectionPolicy
  pause_reason?: PauseReason
  requests_without_new_ids: number
  progress_warning_sent: boolean
  max_pages: number
  pages: number
  records: number
  distinct_records: number
  errors: number
  revision: string
  created_at: string
  started_at?: string
  finished_at?: string
  error?: string
}
export interface StartRun {
  provider_id: string
  mode: RunMode
  max_pages?: number
  max_duration_seconds?: number
  known_pages?: number
}
export interface RunEvent {
  id: number
  run_id: string
  kind: string
  message: string
  data?: Record<string, unknown>
  created_at: string
}
export interface RawRecord {
  id: number
  run_id: string
  provider_id: string
  source_id: string
  page: number
  content_type: string
  payload_retained: boolean
  fields: Record<string, unknown> | null
  error?: string
  ignored?: boolean
  auxiliary?: boolean
  created_at: string
  page_id: number
}
export interface RawDetail {
  record: RawRecord
  raw: string | null
  byte_length: number | null
}
export interface CatalogOrigin {
  instance_id: string
  provider_id: string
  source_id: string
}
export interface Torrent {
  occurrence_id: string
  provider_id: string
  source_id: string
  fields: Record<string, unknown> | null
  raw_id?: number
  first_seen_at: string
  last_seen_at: string
  historical: boolean
  origin?: CatalogOrigin
}
export interface Items<T> {
  items: T[] | null
}
export interface Page<T> extends Items<T> {
  total: number
  limit: number
  offset: number
}

export interface Session {
  authenticated: boolean
  csrf_token?: string
}
export interface Health {
  database: 'ok'
  version: string
}
