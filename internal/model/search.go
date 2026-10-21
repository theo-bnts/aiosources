package model

import (
	"bytes"
	"encoding/json"
)

// SearchFilters is the server-validated torrent search contract. Numeric
// bounds stay decimal strings; they must never pass through floating point.
type SearchFilters struct {
	Query           string   `json:"q,omitempty"`
	Providers       []string `json:"providers,omitempty"`
	Category        string   `json:"category,omitempty"`
	MinSize         string   `json:"min_size,omitempty"`
	MaxSize         string   `json:"max_size,omitempty"`
	PublishedAfter  string   `json:"published_after,omitempty"`
	PublishedBefore string   `json:"published_before,omitempty"`
	InfoHash        string   `json:"info_hash,omitempty"`
	Sort            string   `json:"sort,omitempty"`
}

// Null is not an omitted scalar bound. Reject it, and reject unknown keys even
// when this contract is decoded outside the HTTP layer.
func (f *SearchFilters) UnmarshalJSON(data []byte) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil || members == nil {
		return ErrInvalid
	}
	for _, value := range members {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	type fields SearchFilters
	var decoded fields
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return ErrInvalid
	}
	*f = SearchFilters(decoded)
	return nil
}

func (f SearchFilters) ListOptions() ListOptions {
	return ListOptions{Query: f.Query, Providers: f.Providers, Category: f.Category,
		MinSize: f.MinSize, MaxSize: f.MaxSize, PublishedAfter: f.PublishedAfter,
		PublishedBefore: f.PublishedBefore, InfoHash: f.InfoHash, Sort: f.Sort}
}
