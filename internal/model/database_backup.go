package model

import "time"

// DatabaseBackupTools reports whether PostgreSQL client utilities matching the
// connected server are available for native custom-format export and restore.
type DatabaseBackupTools struct {
	ExportAvailable bool   `json:"export_available"`
	ImportAvailable bool   `json:"import_available"`
	Version         string `json:"version,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// DatabaseRestoreState tracks the current or most recent database restore.
type DatabaseRestoreState struct {
	Status     string     `json:"status"`
	Phase      string     `json:"phase,omitempty"`
	Filename   string     `json:"filename,omitempty"`
	Bytes      int64      `json:"bytes,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// DatabaseBackupStatus summarizes backup tool availability, active transfers,
// restore progress and blocking background operations.
type DatabaseBackupStatus struct {
	Tools         DatabaseBackupTools  `json:"tools"`
	ExportRunning int                  `json:"export_running"`
	Restore       DatabaseRestoreState `json:"restore"`
	Running       int                  `json:"running"`
	Indexing      bool                 `json:"indexing"`
	Busy          bool                 `json:"busy"`
}
