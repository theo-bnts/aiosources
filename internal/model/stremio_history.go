package model

import "time"

// Only explicitly selected display fields are retained, never playback URLs,
// announce addresses, installation tokens, headers or account credentials.
type StremioResult struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	ProviderName string `json:"provider_name,omitempty"`
	SourceID     string `json:"source_id"`
	Title        string `json:"title"`
	Service      string `json:"service"`
	Cache        string `json:"cache"`
	Filename     string `json:"filename"`
	Size         int64  `json:"size"`
}

type StremioRequest struct {
	Origin             string          `json:"origin"`
	Kind               string          `json:"kind"`
	Service            string          `json:"service"`
	Calls              []ProviderCall  `json:"calls,omitempty"`
	ID                 int64           `json:"id"`
	CreatedAt          time.Time       `json:"created_at"`
	MediaType          string          `json:"media_type"`
	Resource           string          `json:"resource"`
	DurationMS         int64           `json:"duration_ms"`
	Outcome            string          `json:"outcome"`
	Matched            int             `json:"matched"`
	Eligible           int             `json:"eligible"`
	InvalidHash        int             `json:"invalid_hash"`
	LanguageExcluded   int             `json:"language_excluded"`
	WrongEpisode       int             `json:"wrong_episode"`
	PrivateUnavailable int             `json:"private_unavailable"`
	FileUnavailable    int             `json:"file_unavailable"`
	Returned           int             `json:"returned"`
	Truncated          bool            `json:"truncated"`
	Results            []StremioResult `json:"results,omitempty"`
}

type StremioStatistics struct {
	Requests  int64      `json:"requests"`
	Empty     int64      `json:"empty"`
	Failed    int64      `json:"failed"`
	Results   int64      `json:"results"`
	AverageMS int64      `json:"average_ms"`
	Since     *time.Time `json:"since"`
}
