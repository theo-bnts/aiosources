package model

// TorznabQuery contains validated protocol filters, never an API key or URL.
type TorznabQuery struct {
	SearchFilters
	Mode             string
	Services         []string
	CachedOnly       bool
	Categories       []int
	MediaTypes       []string
	IMDb, TMDB, TVDB string
	Season, Episode  *int
	Year             int
	Limit, Offset    int
}

type TorznabRecord struct {
	AIORecord
	OccurrenceID string
	Category     int
}
