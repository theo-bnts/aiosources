package model

// PublicAccess contains no visitor or administrator credentials.
type PublicAccess struct {
	Revision          int64 `json:"revision"`
	Enabled           bool  `json:"enabled"`
	SearchEnabled     bool  `json:"search_enabled"`
	StremioEnabled    bool  `json:"stremio_enabled"`
	StremioGeneration int64 `json:"-"`
	ExposeSources     bool  `json:"expose_sources"`
	TorznabEnabled    bool  `json:"torznab_enabled"`
	TorznabGeneration int64 `json:"-"`
	Generation        int64 `json:"-"`
}

// PublicTorznabOptions travel in the client URL, never a saved visitor profile.
// Personal credentials are encrypted separately from these optional filters.
type PublicTorznabOptions struct {
	ResultPreferences
	Sources    []string `json:"sources,omitempty"`
	MediaTypes []string `json:"media,omitempty"`
	Services   []string `json:"services,omitempty"`
	CachedOnly bool     `json:"cached_only,omitempty"`
}
