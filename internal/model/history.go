package model

import "time"

type PublicationChange struct {
	ID           string         `json:"id"`
	RunID        string         `json:"run_id,omitempty"`
	Kind         string         `json:"kind"`
	OccurredAt   time.Time      `json:"occurred_at"`
	Before       map[string]any `json:"before"`
	After        map[string]any `json:"after"`
	Origin       *CatalogOrigin `json:"origin,omitempty"`
	BeforeOrigin *CatalogOrigin `json:"before_origin,omitempty"`
}

type OccurrenceChange struct {
	PublicationChange
	OccurrenceID string `json:"occurrence_id"`
	ProviderID   string `json:"provider_id"`
	SourceID     string `json:"source_id"`
}

// HistoryBaseline describes state retained when semantic tracking began, not
// an invented addition or a claim about its original publication timestamp.
type HistoryBaseline struct {
	Fields     map[string]any `json:"fields"`
	Origin     *CatalogOrigin `json:"origin,omitempty"`
	RecordedAt time.Time      `json:"recorded_at"`
	Deleted    bool           `json:"deleted"`
}

type OccurrenceHistory struct {
	OccurrenceID string              `json:"occurrence_id"`
	ProviderID   string              `json:"provider_id"`
	SourceID     string              `json:"source_id"`
	Origin       *CatalogOrigin      `json:"origin,omitempty"`
	Baseline     *HistoryBaseline    `json:"baseline,omitempty"`
	HistorySince time.Time           `json:"history_since"`
	Items        []PublicationChange `json:"items"`
	Total        int64               `json:"total"`
	Limit        int                 `json:"limit"`
	Offset       int                 `json:"offset"`
}

type PublicationCounts struct {
	Added   int64 `json:"added"`
	Updated int64 `json:"updated"`
	Deleted int64 `json:"deleted"`
}

type RunChanges struct {
	Counts          PublicationCounts  `json:"counts"`
	HistorySince    time.Time          `json:"history_since"`
	HistoryComplete bool               `json:"history_complete"`
	Items           []OccurrenceChange `json:"items"`
	Total           int64              `json:"total"`
	Limit           int                `json:"limit"`
	Offset          int                `json:"offset"`
}
