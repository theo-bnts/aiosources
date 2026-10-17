package model

// PublicAccess contains no visitor or administrator credentials.
type PublicAccess struct {
	Revision          int64 `json:"revision"`
	Enabled           bool  `json:"enabled"`
	ExposeSources     bool  `json:"expose_sources"`
	TorznabEnabled    bool  `json:"torznab_enabled"`
	TorznabGeneration int64 `json:"-"`
	Generation        int64 `json:"-"`
}

// PublicTorznabOptions travel inside an encrypted client key, never a profile.
type PublicTorznabOptions struct {
	Sources    []string `json:"sources,omitempty"`
	MediaTypes []string `json:"media,omitempty"`
	Services   []string `json:"services,omitempty"`
	CachedOnly bool     `json:"cached_only,omitempty"`
}
