package model

import "time"

// OperationRun records service work without fabricating a source import or
// copying provider credentials into history. Counts describe saved checkpoints.
type OperationRun struct {
	Download   *OperationDownload `json:"download,omitempty"`
	ID         string             `json:"id"`
	Kind       string             `json:"kind"`
	Service    string             `json:"service"`
	Trigger    string             `json:"trigger"`
	Resource   string             `json:"resource,omitempty"`
	Status     string             `json:"status"`
	Phase      string             `json:"phase"`
	Processed  int64              `json:"processed"`
	Errors     int64              `json:"errors"`
	Results    map[string]int64   `json:"results"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
	FinishedAt *time.Time         `json:"finished_at"`
	Error      string             `json:"error,omitempty"`
}

type OperationEvent struct {
	DurationMS int64     `json:"duration_ms,omitempty"`
	HTTPStatus int       `json:"http_status,omitempty"`
	ErrorCode  string    `json:"error_code,omitempty"`
	ID         int64     `json:"id"`
	RunID      string    `json:"run_id"`
	Phase      string    `json:"phase"`
	Processed  int64     `json:"processed"`
	Errors     int64     `json:"errors"`
	CreatedAt  time.Time `json:"created_at"`
}

type OperationFilter struct {
	ListOptions
	Kind    string
	Service string
	Group   string
}
