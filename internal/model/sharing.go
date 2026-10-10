package model

import (
	"bytes"
	"encoding/json"
	"time"
)

// Share contains only owner-visible configuration, never credential material.
type Share struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	Enabled                bool       `json:"enabled"`
	Scope                  string     `json:"scope"`
	SourceIDs              []string   `json:"source_ids"`
	Fields                 []string   `json:"fields"`
	Revision               int64      `json:"revision"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	LastAccessAt           *time.Time `json:"last_access_at,omitempty"`
	LastSyncAt             *time.Time `json:"last_sync_at,omitempty"`
	ExpiresAt              *time.Time `json:"expires_at"`
	RequestsPerMinute      int        `json:"requests_per_minute"`
	MaxConcurrentDownloads int        `json:"max_concurrent_downloads"`
	URL                    string     `json:"url"`
}

type ShareInput struct {
	Name                   string     `json:"name"`
	Enabled                bool       `json:"enabled"`
	Scope                  string     `json:"scope"`
	SourceIDs              []string   `json:"source_ids"`
	Fields                 []string   `json:"fields"`
	Revision               int64      `json:"revision"`
	ExpiresAt              *time.Time `json:"expires_at"`
	RequestsPerMinute      *int       `json:"requests_per_minute,omitempty"`
	MaxConcurrentDownloads *int       `json:"max_concurrent_downloads,omitempty"`
}

// Null is meaningful only for expiration. Limit pointers distinguish an omitted
// create default from an explicit zero; explicit null is not a numeric policy.
func (input *ShareInput) UnmarshalJSON(data []byte) error {
	type plain ShareInput
	var value plain
	wire := struct {
		*plain
		Requests   json.RawMessage `json:"requests_per_minute"`
		Concurrent json.RawMessage `json:"max_concurrent_downloads"`
	}{plain: &value}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	for _, field := range []struct {
		raw    json.RawMessage
		target **int
	}{{wire.Requests, &value.RequestsPerMinute}, {wire.Concurrent, &value.MaxConcurrentDownloads}} {
		if len(field.raw) == 0 {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(field.raw), []byte("null")) {
			return ErrInvalid
		}
		if err := json.Unmarshal(field.raw, field.target); err != nil {
			return err
		}
	}
	*input = ShareInput(value)
	return nil
}

type ShareCredential struct {
	Share    Share  `json:"share"`
	Password string `json:"password"`
}
