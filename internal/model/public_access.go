package model

// PublicAccess contains no visitor or administrator credentials.
// CredentialPolicy selects server-resolved instance credentials independently
// from feature permissions. Secret values never accompany this policy.
type CredentialPolicy struct {
	UseInstanceDebrid    bool `json:"use_instance_debrid"`
	UseInstanceAnnounces bool `json:"use_instance_announces"`
}

type PublicAccess struct {
	CredentialPolicy
	Revision          int64 `json:"revision"`
	Enabled           bool  `json:"enabled"`
	SearchEnabled     bool  `json:"search_enabled"`
	StremioEnabled    bool  `json:"stremio_enabled"`
	StremioGeneration int64 `json:"-"`
	TorznabEnabled    bool  `json:"torznab_enabled"`
	TorznabGeneration int64 `json:"-"`
	CatalogEnabled    bool  `json:"catalog_enabled"`
	CatalogGeneration int64 `json:"-"`
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
