package model

import "time"

// CatalogSearchFilters apply to local metadata, independently of release filters.
// A year selects any known release year, or a year within a series' known span.
type CatalogSearchFilters struct {
	Type     string
	Year     int
	Language string
}

type CatalogReleaseFilters struct {
	StreamPreferences StreamPreferences
	CachedOnly        bool
}

// CatalogTitle is a local, identified title. It never contains source payloads
// or account credentials. The media type determines the selection controls.
type CatalogTitle struct {
	OpenLibrary string           `json:"openlibrary,omitempty"`
	Cover       string           `json:"cover,omitempty"`
	Authors     []string         `json:"authors,omitempty"`
	Selection   CatalogSelection `json:"selection"`
	Seasons     []int            `json:"seasons"`
	ID          int64            `json:"id"`
	Type        string           `json:"type"`
	Title       string           `json:"title"`
	Years       []int            `json:"years"`
	TMDB        string           `json:"tmdb"`
	IMDb        string           `json:"imdb"`
	Releases    int64            `json:"releases"`
	Providers   []string         `json:"providers"`
}

type CatalogRelease struct {
	DownloadToken string                `json:"download_token,omitempty"`
	ProviderName  string                `json:"provider_name,omitempty"`
	Fingerprint   string                `json:"fingerprint"`
	OccurrenceID  string                `json:"occurrence_id"`
	Provider      string                `json:"provider"`
	Title         string                `json:"title"`
	Size          int64                 `json:"size"`
	PublishedAt   *time.Time            `json:"published_at,omitempty"`
	Languages     []string              `json:"languages"`
	Services      []CatalogAvailability `json:"services"`
}

type CatalogAvailability struct {
	FileCount int    `json:"file_count,omitempty"`
	Service   string `json:"service"`
	State     string `json:"state"`
	CanAdd    bool   `json:"can_add"`
	Filename  string `json:"filename,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

type CatalogSelection struct {
	File    string `json:"file,omitempty"`
	Season  *int   `json:"season,omitempty"`
	Episode *int   `json:"episode,omitempty"`
}

type CatalogDownloadInput struct {
	DownloadToken string `json:"download_token,omitempty"`
	CatalogSelection
	Fingerprint   string `json:"fingerprint"`
	OccurrenceID  string `json:"occurrence_id"`
	Service       string `json:"service"`
	AllowUncached bool   `json:"allow_uncached"`
	PreparationID string `json:"preparation_id,omitempty"`
}

type CatalogDownload struct {
	Files           []AIOFile `json:"files,omitempty"`
	Service         string    `json:"-"`
	Status          string    `json:"status"`
	URL             string    `json:"url,omitempty"`
	URLValidSeconds int       `json:"url_valid_seconds,omitempty"`
	OperationID     string    `json:"operation_id"`
	PreparationID   string    `json:"preparation_id,omitempty"`
}

type OperationDownload struct {
	WorkID  int64                `json:"work_id"`
	Input   CatalogDownloadInput `json:"input"`
	Account string               `json:"-"`
}
