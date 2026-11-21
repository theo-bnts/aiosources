export type Tracker = { private: boolean; announce_secret: string }
export type Config = {
  revision: number
  igdb_client_id: string
  igdb_secret: string
  igdb_enabled: boolean
  igdb_minutes: number
  igdb_requests_per_second: number
  igdb_reconcile_days: number
  tmdb_secret: string
  alldebrid_secret: string
  torbox_secret: string
  debridlink_secret: string
  deepbrid_secret: string
  premiumize_secret: string
  realdebrid_secret: string
  auto_sync: boolean
  auto_match: boolean
  auto_cache: boolean
  openlibrary_enabled: boolean
  openlibrary_minutes: number
  openlibrary_relevant_only: boolean
  openlibrary_languages: string[]
  musicbrainz_enabled: boolean
  musicbrainz_minutes: number
  musicbrainz_secret: string
  musicbrainz_official_only: boolean
  musicbrainz_exclude_spoken: boolean
  musicbrainz_include_covers: boolean
  musicbrainz_types: string[]
  musicbrainz_languages: string[]
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
  filtered: number
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
export type GameStatus = {
  pending: boolean
  running: boolean
  complete: boolean
  works: number
  processed: number
  total: number
  phase: string
  last_sync: string | null
  changes_through: string | null
  last_reconciliation: string | null
  next_update: string | null
  retry_at: string | null
  error?: string
}
export type MusicStatus = {
  running: boolean
  complete: boolean
  phase: string
  selection: string
  snapshot: string
  snapshot_at: string | null
  works: number
  releases: number
  artists: number
  labels: number
  covers: number
  imported: number
  position: number
  filtered: number
  skipped: number
  last_import: string | null
  last_sync: string | null
  changes_through: string | null
  last_packet_at: string | null
  next_update: string | null
  retry_at: string | null
  replication_sequence: number
  schema_sequence: number
  live_enabled: boolean
  needs_export: boolean
  recovery_reason?: string
  schema_unsupported: boolean
  error?: string
}
export type Status = {
  games?: GameStatus
  books?: BookStatus
  music?: MusicStatus
  maintenance?: {
    scope: 'books' | 'videos' | 'games' | 'music' | 'metadata' | 'mapping' | 'cache' | ''
    status: 'running' | 'succeeded' | 'failed' | ''
    phase?: 'stopping' | 'resetting'
    error?: string
  }
  config: Config
  status: {
    works: number
    video_works: number
    matched: number
    ambiguous: number
    unmatched: number
    matching_pending: number
    correspondences_pending: number
    correspondence_groups: number
    correspondence_members: number
    correspondence_enriched: number
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

export type CorrespondenceRule = 'exact_info_hash' | 'work_exact_size' | 'work_release_name'
export type CorrespondenceConfidence = 'exact' | 'high' | 'verified'

export type ReleaseCorrespondenceGroup = {
  id: number
  scope: 'work' | 'hash'
  scope_key: string
  work_id?: number | null
  work_type?: string
  work_title?: string
  imdb?: string
  tmdb?: string
  match_rule: CorrespondenceRule
  match_key: string
  confidence: CorrespondenceConfidence
  canonical_size: number
  canonical_title: string
  member_count: number
  provider_count: number
  enriched_count: number
  providers: string[]
  propagated_audio: string[]
  propagated_subtitles: string[]
  updated_at: string
}

export type ReleaseCorrespondenceMember = {
  group_id: number
  occurrence_id: string
  provider_id: string
  source_id: string
  fingerprint: string
  title: string
  hash?: string
  size: number
  match_rule: CorrespondenceRule
  own_audio: string[]
  own_subtitles: string[]
  contributed_audio: string[]
  contributed_subtitles: string[]
}

export type ReleaseCorrespondenceExclusion = {
  occurrence_id: string
  provider_id: string
  source_id: string
  title: string
  match_key: string
  reason: string
  created_at: string
}

export type ReleaseCorrespondenceDetail = {
  group: ReleaseCorrespondenceGroup
  members: ReleaseCorrespondenceMember[]
  exclusions: ReleaseCorrespondenceExclusion[]
}

export type MusicInformation = {
  primary_type?: string
  secondary_types?: string[]
  artists?: string[]
  artist_aliases?: string[]
  labels?: string[]
  catalog_numbers?: string[]
  barcodes?: string[]
  formats?: string[]
  discogs_ids?: string[]
  wikidata_id?: string
  has_cover?: boolean
}
export type ReviewWork = {
  display_title?: string
  id: number
  type: 'movie' | 'tv' | 'book' | 'game' | 'music'
  igdb?: string
  game?: { platforms?: { id: number; name: string; slug: string }[] }
  openlibrary?: string
  book?: { authors?: string[]; cover?: number }
  musicbrainz?: string
  music?: MusicInformation
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
  type: 'movie' | 'tv' | 'book' | 'game' | 'music'
  igdb?: string
  game?: { platforms?: { id: number; name: string; slug: string }[] }
  openlibrary?: string
  book?: { authors?: string[]; cover?: number }
  musicbrainz?: string
  music?: MusicInformation
  tmdb: string
  override_episode: boolean
  season: number | null
  episode: number | null
}
export type ReviewDetail = {
  book_evidence?: { isbn?: string; isbns?: string[]; openlibrary?: string; author?: string }
  music_evidence?: {
    mbid?: string
    release_mbid?: string
    discogs_id?: string
    wikidata_id?: string
    barcode?: string
    catalog_number?: string
    artist?: string
    album?: string
    label?: string
  }
  suggested_query: string
  suggested_year: number | null
  record: {
    title: string
    igdb?: string
    musicbrainz?: string
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
