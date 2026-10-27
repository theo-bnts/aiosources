package model

import "time"

// Credential values never enter configuration or API responses. References
// resolve through the same encrypted vault as collection credentials.
type AIOConfig struct {
	Revision              int64                 `json:"revision"`
	TMDBSecret            string                `json:"tmdb_secret"`
	AllDebridSecret       string                `json:"alldebrid_secret"`
	TorBoxSecret          string                `json:"torbox_secret"`
	DebridLinkSecret      string                `json:"debridlink_secret"`
	DeepbridSecret        string                `json:"deepbrid_secret"`
	PremiumizeSecret      string                `json:"premiumize_secret"`
	RealDebridSecret      string                `json:"realdebrid_secret"`
	AutoSync              bool                  `json:"auto_sync"`
	AutoMatch             bool                  `json:"auto_match"`
	OpenLibraryEnabled    bool                  `json:"openlibrary_enabled"`
	OpenLibraryDays       int                   `json:"openlibrary_days"`
	SyncMinutes           int                   `json:"sync_minutes"`
	MatchSeconds          int                   `json:"match_seconds"`
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
	OpenLibrary     string            `json:"openlibrary,omitempty"`
	PosterPath      string            `json:"poster_path,omitempty"`
	Book            BookInformation   `json:"book,omitzero"`
	LocalizedTitles map[string]string `json:"localized_titles"`
	DisplayTitle    string            `json:"display_title"`
	Adult           bool              `json:"adult"`
	ID              int64             `json:"id"`
	Type            string            `json:"type"`
	TMDB            string            `json:"tmdb"`
	IMDb            string            `json:"imdb"`
	TVDB            string            `json:"tvdb"`
	Titles          []string          `json:"titles"`
	Years           []int             `json:"years"`
	SeasonYears     map[int]int       `json:"season_years"`
	EpisodeYears    map[string]int    `json:"episode_years"`
	Revision        int64             `json:"revision"`
}
type AIORecord struct {
	BookInformationPending bool                `json:"-"`
	BookHints              BookHints           `json:"-"`
	MatchingPolicy         MatchingPolicy      `json:"-"`
	Presentation           map[string][]string `json:"-"`
	SubtitleLanguages      []string            `json:"-"`
	Bitrate                float64             `json:"-"`
	ReviewedEpisode        bool                `json:"reviewed_episode,omitempty"`
	Normalized             string              `json:"-"`
	QueueVersion           int64               `json:"-"`
	CandidateGeneration    int64               `json:"-"`
	Provider               string              `json:"provider"`
	SourceID               string              `json:"source_id"`
	Title                  string              `json:"title"`
	Hash                   string              `json:"hash"`
	Size                   int64               `json:"size"`
	IMDb                   string              `json:"imdb"`
	TMDB                   string              `json:"tmdb"`
	TVDB                   string              `json:"tvdb"`
	MAL                    string              `json:"mal"`
	Season                 *int                `json:"season"`
	Episode                *int                `json:"episode"`
	Languages              []string            `json:"languages"`
	PublishedAt            *time.Time          `json:"published_at,omitempty"`
	DurationSeconds        float64             `json:"duration_seconds,omitempty"`
	Fingerprint            string              `json:"fingerprint"`
}

// Eligibility belongs in candidate discovery, before the bounded safety cap.
// Otherwise impossible homonyms can hide the one year-compatible work.
type AIOCandidateFilter struct {
	Titles                        []string
	ISBN, OpenLibrary             string
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

type BookHints struct {
	OpenLibrary   string   `json:"openlibrary,omitempty"`
	ISBNs         []string `json:"isbns,omitempty"`
	Title         string   `json:"title,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Author        string   `json:"author,omitempty"`
	ISBN          string   `json:"isbn,omitempty"`
	Format        string   `json:"format,omitempty"`
	Categories    []int    `json:"categories,omitempty"`
	CategoryNames []string `json:"category_names,omitempty"`
}

type BookInformation struct {
	YearVerified  bool     `json:"-"`
	AuthorAliases []string `json:"author_aliases,omitempty"`
	AuthorKeys    []string `json:"author_keys,omitempty"`
	Authors       []string `json:"authors,omitempty"`
	Cover         int64    `json:"cover,omitempty"`
	Languages     []string `json:"languages,omitempty"`
}

type OpenLibraryStatus struct {
	Skipped  int64      `json:"skipped"`
	Complete bool       `json:"complete"`
	Snapshot string     `json:"snapshot,omitempty"`
	Position int64      `json:"position"`
	Imported int64      `json:"imported"`
	Works    int64      `json:"works"`
	Editions int64      `json:"editions"`
	Authors  int64      `json:"authors"`
	LastSync *time.Time `json:"last_sync"`
	Error    string     `json:"error,omitempty"`
	Running  bool       `json:"running"`
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
	Shareable  bool      `json:"-"`
	Generation int64     `json:"-"`
	Hash       string    `json:"hash"`
	Cached     bool      `json:"cached"`
	CheckedAt  time.Time `json:"checked_at"`
	Files      []AIOFile `json:"files,omitempty"`
}

type AvailabilityCheckStats struct {
	Cached      int64      `json:"cached"`
	Uncached    int64      `json:"uncached"`
	LastChecked *time.Time `json:"last_checked"`
}

type SharedCacheStats struct {
	Checks      *AvailabilityCheckStats `json:"checks,omitempty"`
	Service     string                  `json:"service"`
	Cached      int64                   `json:"cached"`
	Uncached    int64                   `json:"uncached"`
	LastChecked time.Time               `json:"last_checked"`
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
	LastMatch       *time.Time `json:"last_match"`
	ChangesThrough  *time.Time `json:"changes_through"`
	InventoryDate   string     `json:"inventory_date"`
	SyncRetryAt     *time.Time `json:"sync_retry_at"`
	SyncFailures    int        `json:"sync_failures"`
	SyncError       string     `json:"sync_error"`
}

type AIOAutomation struct {
	Updates            string     `json:"updates"`
	NextUpdate         *time.Time `json:"next_update"`
	Identification     string     `json:"identification"`
	NextIdentification *time.Time `json:"next_identification"`
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
