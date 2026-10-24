package model

import "slices"

// TorznabPreferences select known availability, never a recipient's credentials.
// Public connections carry these settings in their URL; private access saves them.
type TorznabPreferences struct {
	ResultPreferences
	Services   []string `json:"services,omitempty"`
	CachedOnly bool     `json:"cached_only,omitempty"`
}

func (p TorznabPreferences) Valid() bool {
	if !p.ResultPreferences.Valid(false) || len(p.Services) > len(DebridServices()) {
		return false
	}
	for i, service := range p.Services {
		if !IsDebridService(service) || slices.Contains(p.Services[:i], service) {
			return false
		}
	}
	return true
}

// Query parameters may narrow access settings, never relax a saved or public
// connection's cache policy. An empty intersection must stay empty, not become All.
func (q TorznabQuery) WithPreferences(p TorznabPreferences) TorznabQuery {
	q.ResultPreferences = p.ResultPreferences
	q.CachedOnly = q.CachedOnly || p.CachedOnly
	q.MaxResults = p.ResultLimit(q.CachedOnly)
	if len(p.Services) == 0 {
		return q
	}
	if q.Services == nil {
		q.Services = p.Services
		return q
	}
	selected := []string{}
	for _, service := range q.Services {
		if slices.Contains(p.Services, service) {
			selected = append(selected, service)
		}
	}
	q.Services = selected
	return q
}

// TorznabQuery contains validated protocol filters, never an API key or URL.
type TorznabQuery struct {
	SearchFilters
	ResultPreferences
	SourceNames      map[string]string
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
