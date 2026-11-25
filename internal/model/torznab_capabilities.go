package model

import "time"

// TorznabCapabilitiesResult summarizes metadata discovered from a Torznab t=caps
// response for source configuration. It never contains secrets or raw upstream errors.
type TorznabCapabilitiesResult struct {
	Endpoint            string                        `json:"endpoint"`
	Title               string                        `json:"title,omitempty"`
	SuggestedID         string                        `json:"suggested_id,omitempty"`
	Limits              TorznabLimits                 `json:"limits"`
	RecommendedPageSize int                           `json:"recommended_page_size,omitempty"`
	Searches            []TorznabSearchModeCapability `json:"searches"`
	RecommendedMode     string                        `json:"recommended_search_mode,omitempty"`
	Categories          []TorznabCategoryCapability   `json:"categories"`
	RateLimit           *TorznabRateLimitHint         `json:"rate_limit,omitempty"`
}

type TorznabLimits struct {
	Default int `json:"default,omitempty"`
	Max     int `json:"max,omitempty"`
}

type TorznabSearchModeCapability struct {
	Mode            string   `json:"mode"`
	Available       bool     `json:"available"`
	SupportedParams []string `json:"supported_params"`
}

type TorznabCategoryCapability struct {
	ID            int                         `json:"id"`
	Name          string                      `json:"name"`
	Subcategories []TorznabCategoryCapability `json:"subcategories,omitempty"`
}

type TorznabRateLimitHint struct {
	Limit     *int64     `json:"limit,omitempty"`
	Remaining *int64     `json:"remaining,omitempty"`
	ResetAt   *time.Time `json:"reset_at,omitempty"`
}
