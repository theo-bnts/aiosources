package model

import "time"

type LogEntry struct {
	ID         int64          `json:"id"`
	Level      string         `json:"level"`
	Title      string         `json:"title,omitempty"`
	Kind       string         `json:"kind"`
	Message    string         `json:"message"`
	ProviderID string         `json:"provider_id,omitempty"`
	Service    string         `json:"service,omitempty"`
	RunID      string         `json:"run_id,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	Data       map[string]any `json:"data,omitempty"`
}

type LogOptions struct {
	ListOptions
	Service string
	Levels  []string
	From    *time.Time
	To      *time.Time
}

func IsActivityLevel(level string) bool {
	return level == "debug" || level == "info" || level == "warn" || level == "error"
}
