export type Tracker = { private: boolean; announce_secret: string }
export type Config = {
  revision: number
  tmdb_secret: string
  alldebrid_secret: string
  torbox_secret: string
  debridlink_secret: string
  deepbrid_secret: string
  premiumize_secret: string
  realdebrid_secret: string
  auto_sync: boolean
  auto_match: boolean
  openlibrary_enabled: boolean
  openlibrary_minutes: number
  sync_minutes: number
  match_seconds: number
  tmdb_requests_per_second: number
  cache_minutes: number
  response_seconds: number
  trackers: Record<string, Tracker>
}
export type BookStatus = {
  snapshot_at: string | null
  last_import: string | null
  changes_through: string | null
  pending: number
  catching_up: boolean
  needs_export: boolean
  skipped: number
  complete: boolean
  position: number
  imported: number
  works: number
  editions: number
  authors: number
  last_sync: string | null
  error?: string
  running: boolean
  next_update?: string | null
}
export type Status = {
  books?: BookStatus
  config: Config
  status: {
    works: number
    video_works: number
    matched: number
    ambiguous: number
    unmatched: number
    matching_pending: number
    pending: number
    cleanup_pending: number
    cleanup_failed: number
    last_sync: string | null
    last_match: string | null
    sync_error: string
    inventory_date: string
    applied_through: string | null
    recovery_started_at: string | null
  }
  syncing: boolean
  matching: boolean
  automation: {
    updates: 'disabled' | 'missing_token' | 'running' | 'scheduled' | 'retry' | 'due'
    next_update: string | null
    identification:
      'disabled' | 'waiting_information' | 'pending' | 'up_to_date' | 'running' | 'scheduled'
    next_identification: string | null
  }
  error: string
  matching_error?: string
  recovery_required: boolean
}

export type Controls = Omit<Status, 'status'> & {
  status: Pick<
    Status['status'],
    'cleanup_pending' | 'cleanup_failed' | 'last_sync' | 'last_match' | 'sync_error'
  > & {
    has_information: boolean
  }
}

export type InformationStatus = Omit<Status, 'status' | 'automation'> & {
  status: Pick<
    Status['status'],
    | 'video_works'
    | 'pending'
    | 'last_sync'
    | 'sync_error'
    | 'inventory_date'
    | 'applied_through'
    | 'recovery_started_at'
  >
  automation: Pick<Status['automation'], 'updates' | 'next_update'>
}

export type MatchDiagnostic = {
  occurrence_id: string
  provider_id: string
  source_id: string
  title: string
  status: 'matched' | 'ambiguous' | 'unmatched' | 'pending'
  reason: string
  imdb: string
  updated_at: string | null
}

export type ReviewWork = {
  display_title?: string
  id: number
  type: 'movie' | 'tv' | 'book'
  openlibrary?: string
  book?: { authors?: string[]; cover?: number }
  tmdb: string
  imdb: string
  tvdb: string
  titles: string[]
  years: number[]
  revision: number
}
export type MatchReview = {
  source_changed: boolean
  active: boolean
  revision: number
  fingerprint: string
  type: 'movie' | 'tv' | 'book'
  openlibrary?: string
  book?: { authors?: string[]; cover?: number }
  tmdb: string
  override_episode: boolean
  season: number | null
  episode: number | null
}
export type ReviewDetail = {
  book_evidence?: { isbn?: string; isbns?: string[]; openlibrary?: string; author?: string }
  suggested_query: string
  suggested_year: number | null
  record: {
    title: string
    provider: string
    source_id: string
    fingerprint: string
    imdb: string
    tmdb: string
    tvdb: string
    season: number | null
    episode: number | null
  }
  work: ReviewWork | null
  review: MatchReview | null
}
