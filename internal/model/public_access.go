package model

// PublicAccess contains no visitor or administrator credentials.
type PublicAccess struct {
	Revision          int64        `json:"revision"`
	Enabled           bool         `json:"enabled"`
	ExposeSources     bool         `json:"expose_sources"`
	TorznabEnabled    bool         `json:"torznab_enabled"`
	TorznabGeneration int64        `json:"-"`
	Generation        int64        `json:"-"`
	Limits            PublicLimits `json:"limits"`
}

type PublicLimits struct {
	SetupPerMinute    int `json:"setup_per_minute"`
	RequestsPerMinute int `json:"requests_per_minute"`
	Concurrent        int `json:"concurrent"`
}

func DefaultPublicLimits() PublicLimits {
	return PublicLimits{SetupPerMinute: 12, RequestsPerMinute: 120, Concurrent: 50}
}

func (l PublicLimits) Valid() bool {
	return l.SetupPerMinute >= 1 && l.SetupPerMinute <= 100000 && l.RequestsPerMinute >= 1 && l.RequestsPerMinute <= 100000 && l.Concurrent >= 1 && l.Concurrent <= 1000
}

// PublicTorznabOptions travel in the client URL, never a saved visitor profile.
// Personal credentials are encrypted separately from these optional filters.
type PublicTorznabOptions struct {
	Sources    []string `json:"sources,omitempty"`
	MediaTypes []string `json:"media,omitempty"`
	Services   []string `json:"services,omitempty"`
	CachedOnly bool     `json:"cached_only,omitempty"`
}
