package model

import "time"

// Credential values never enter configuration or API responses. References
// resolve through the same encrypted vault as collection credentials.
type AIOConfig struct {
	Revision              int64                 `json:"revision"`
	TMDBSecret            string                `json:"tmdb_secret"`
	AllDebridSecret       string                `json:"alldebrid_secret"`
	TorBoxSecret          string                `json:"torbox_secret"`
	AutoSync              bool                  `json:"auto_sync"`
	AutoMatch             bool                  `json:"auto_match"`
	SyncMinutes           int                   `json:"sync_minutes"`
	TMDBRequestsPerSecond int                   `json:"tmdb_requests_per_second"`
	CacheMinutes          int                   `json:"cache_minutes"`
	ResponseSeconds       int                   `json:"response_seconds"`
	Trackers              map[string]AIOTracker `json:"trackers"`
}
type AIOTracker struct {
	Private        bool   `json:"private"`
	AnnounceSecret string `json:"announce_secret"`
	// Read previous v2 settings only; the service converts them to an encrypted
	// full announce URL before publishing configuration or serving streams.
	Announce      string `json:"announce,omitempty"`
	PasskeySecret string `json:"passkey_secret,omitempty"`
}
type AIOWork struct {
	Adult        bool           `json:"adult"`
	ID           int64          `json:"id"`
	Type         string         `json:"type"`
	TMDB         string         `json:"tmdb"`
	IMDb         string         `json:"imdb"`
	TVDB         string         `json:"tvdb"`
	Titles       []string       `json:"titles"`
	Years        []int          `json:"years"`
	SeasonYears  map[int]int    `json:"season_years"`
	EpisodeYears map[string]int `json:"episode_years"`
	Revision     int64          `json:"revision"`
}
type AIORecord struct {
	ReviewedEpisode     bool       `json:"reviewed_episode,omitempty"`
	Normalized          string     `json:"-"`
	QueueVersion        int64      `json:"-"`
	CandidateGeneration int64      `json:"-"`
	Provider            string     `json:"provider"`
	SourceID            string     `json:"source_id"`
	Title               string     `json:"title"`
	Hash                string     `json:"hash"`
	Size                int64      `json:"size"`
	IMDb                string     `json:"imdb"`
	TMDB                string     `json:"tmdb"`
	TVDB                string     `json:"tvdb"`
	MAL                 string     `json:"mal"`
	Season              *int       `json:"season"`
	Episode             *int       `json:"episode"`
	Languages           []string   `json:"languages"`
	PublishedAt         *time.Time `json:"published_at,omitempty"`
	DurationSeconds     float64    `json:"duration_seconds,omitempty"`
	Fingerprint         string     `json:"fingerprint"`
}

// Eligibility belongs in candidate discovery, before the bounded safety cap.
// Otherwise impossible homonyms can hide the one year-compatible work.
type AIOCandidateFilter struct {
	Title, TMDB, IMDb, TVDB, Type string
	Year                          *int
	Seasons                       []int
	Episode                       *int
	ExactOnly                     bool
}

type AIOSeasonRequest struct {
	TMDB   string `json:"tmdb_id"`
	Season int    `json:"season"`
}

type AIOMatchResult struct {
	Record         AIORecord
	Status, Reason string
	Work           *AIOWork
	Seasons        []AIOSeasonRequest
}
type AIOFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	ID   int64  `json:"-"`
	Link string `json:"-"`
}
type AIOAvailability struct {
	Generation int64     `json:"-"`
	Hash       string    `json:"hash"`
	Cached     bool      `json:"cached"`
	CheckedAt  time.Time `json:"checked_at"`
	Files      []AIOFile `json:"files,omitempty"`
}
type AIOResource struct {
	Generation  int64
	Key         string
	Service     string
	Account     string
	Hashes      []string
	CheckedAt   time.Time
	LastAttempt time.Time
	Retry       int
	Results     []AIOAvailability
}
type AIOCleanup struct {
	Account string
	Hash    string
	ID      int64
	Failed  bool
}
type AIOStatus struct {
	MetadataDue     bool       `json:"-"`
	Works           int64      `json:"works"`
	Matched         int64      `json:"matched"`
	Ambiguous       int64      `json:"ambiguous"`
	Unmatched       int64      `json:"unmatched"`
	MatchingPending int64      `json:"matching_pending"`
	Pending         int64      `json:"pending"`
	CleanupPending  int64      `json:"cleanup_pending"`
	CleanupFailed   int64      `json:"cleanup_failed"`
	LastSync        *time.Time `json:"last_sync"`
	ChangesThrough  *time.Time `json:"changes_through"`
	InventoryDate   string     `json:"inventory_date"`
	SyncRetryAt     *time.Time `json:"sync_retry_at"`
	SyncFailures    int        `json:"sync_failures"`
	SyncError       string     `json:"sync_error"`
}

type AIOAutomation struct {
	Updates        string     `json:"updates"`
	NextUpdate     *time.Time `json:"next_update"`
	Identification string     `json:"identification"`
}

// Diagnostics expose only source identity, title and the current decision.
// Download URLs, source credentials and raw fields are deliberately absent.
type AIOMatchDiagnostic struct {
	OccurrenceID string     `json:"occurrence_id"`
	ProviderID   string     `json:"provider_id"`
	SourceID     string     `json:"source_id"`
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason"`
	IMDb         string     `json:"imdb"`
	UpdatedAt    *time.Time `json:"updated_at"`
}
