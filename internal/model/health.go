package model

import "time"

type HealthStorageCategory struct {
	Key        string `json:"key"`
	Bytes      int64  `json:"bytes"`
	TableBytes int64  `json:"table_bytes"`
	IndexBytes int64  `json:"index_bytes"`
}

type HealthDatabase struct {
	Available          bool                    `json:"available"`
	MeasuredAt         *time.Time              `json:"measured_at,omitempty"`
	TotalBytes         int64                   `json:"total_bytes"`
	RelationBytes      int64                   `json:"relation_bytes"`
	OtherDatabaseBytes int64                   `json:"other_database_bytes"`
	Categories         []HealthStorageCategory `json:"categories"`
}

type HealthDisk struct {
	Available      bool      `json:"available"`
	Scope          string    `json:"scope"`
	MeasuredAt     time.Time `json:"measured_at"`
	TotalBytes     uint64    `json:"total_bytes"`
	FreeBytes      uint64    `json:"free_bytes"`
	AvailableBytes uint64    `json:"available_bytes"`
}

type SystemHealth struct {
	CheckedAt     time.Time               `json:"checked_at"`
	Status        string                  `json:"status"`
	Database      HealthDatabase          `json:"database"`
	Disk          HealthDisk              `json:"disk"`
	NextCheckAt   time.Time               `json:"next_check_at"`
	SourceStorage []SourceStorageEstimate `json:"source_storage,omitempty"`
}

type SourceStorageEstimate struct {
	ProviderID string `json:"provider_id"`
	Bytes      int64  `json:"bytes"`
}
