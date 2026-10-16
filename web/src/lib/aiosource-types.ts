export type Tracker = { private: boolean; announce_secret: string }
export type Config = {
  revision: number
  tmdb_secret: string
  alldebrid_secret: string
  torbox_secret: string
  auto_sync: boolean
  auto_match: boolean
  sync_minutes: number
  tmdb_requests_per_second: number
  cache_minutes: number
  response_seconds: number
  trackers: Record<string, Tracker>
}
export type Status = {
  config: Config
  status: {
    works: number
    matched: number
    ambiguous: number
    unmatched: number
    matching_pending: number
    pending: number
    cleanup_pending: number
    cleanup_failed: number
    last_sync: string | null
    sync_error: string
  }
  syncing: boolean
  matching: boolean
  automation: {
    updates: 'disabled' | 'missing_token' | 'blocked' | 'running' | 'scheduled' | 'retry' | 'due'
    next_update: string | null
    identification: 'disabled' | 'waiting_information' | 'pending' | 'up_to_date' | 'running'
  }
  error: string
  rebuild_required: boolean
  manifest_path: string
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
  type: 'movie' | 'tv'
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
  type: 'movie' | 'tv'
  tmdb: string
  override_episode: boolean
  season: number | null
  episode: number | null
}
export type ReviewDetail = {
  suggested_query: string
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
