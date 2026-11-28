package model

import "time"

// Trusted users consume the catalog using the selected credential policy. These
// permissions never authorize administration, definitions or secret values.
type TrustedPermissions struct {
	CredentialPolicy
	TrendingEnabled bool `json:"trending_enabled"`
	SearchEnabled   bool `json:"search_enabled"`
	StremioEnabled  bool `json:"stremio_enabled"`
	TorznabEnabled  bool `json:"torznab_enabled"`
	CatalogEnabled  bool `json:"catalog_enabled"`
	AlertsEnabled   bool `json:"alerts_enabled"`
}

func (p TrustedPermissions) Allows(feature string) bool {
	switch feature {
	case "trending":
		return p.TrendingEnabled
	case "search":
		return p.SearchEnabled
	case "browse":
		return p.SearchEnabled || p.TrendingEnabled
	case "stremio":
		return p.StremioEnabled
	case "torznab":
		return p.TorznabEnabled
	case "catalog":
		return p.CatalogEnabled
	case "alerts":
		return p.AlertsEnabled
	}
	return false
}

type TrustedUser struct {
	TrustedPermissions
	ID         string    `json:"id"`
	Email      string    `json:"email"`
	Enabled    bool      `json:"enabled"`
	Revision   int64     `json:"revision"`
	Generation int64     `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// A grant is constructed inside the server after verifying a session or an
// authenticated encrypted connection. It is never decoded from an API body.
type TrustedGrant struct {
	UserID     string
	Generation int64
}
