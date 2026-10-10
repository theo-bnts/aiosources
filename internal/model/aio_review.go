package model

import "time"

// Reviews are separate from imported fields and may be undone independently.
type AIOMatchReview struct {
	SourceChanged   bool      `json:"source_changed"`
	Active          bool      `json:"active"`
	Revision        int64     `json:"revision"`
	Fingerprint     string    `json:"fingerprint"`
	Type            string    `json:"type"`
	TMDB            string    `json:"tmdb"`
	Season          *int      `json:"season"`
	Episode         *int      `json:"episode"`
	OverrideEpisode bool      `json:"override_episode"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AIOReviewDetail struct {
	SuggestedQuery string          `json:"suggested_query"`
	Record         AIORecord       `json:"record"`
	Review         *AIOMatchReview `json:"review"`
	Work           *AIOWork        `json:"work"`
}

type AIOReviewInput struct {
	Fingerprint     string `json:"fingerprint"`
	Revision        int64  `json:"revision"`
	WorkID          int64  `json:"work_id"`
	WorkRevision    int64  `json:"work_revision"`
	Automatic       bool   `json:"automatic"`
	OverrideEpisode bool   `json:"override_episode"`
	Season          *int   `json:"season"`
	Episode         *int   `json:"episode"`
}
