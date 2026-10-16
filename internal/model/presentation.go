package model

// StreamPreferences are installation and view preferences, never credentials.
// An empty mode preserves older installations which did not select a language.
type StreamPreferences struct {
	Language   string   `json:"language"`
	Mode       string   `json:"mode"`
	Sources    []string `json:"sources,omitempty"`
	Services   []string `json:"services,omitempty"`
	Types      []string `json:"types,omitempty"`
	CachedOnly bool     `json:"cached_only,omitempty"`
}
