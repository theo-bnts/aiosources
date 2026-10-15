package model

import "time"

type SourceTorrentOverview struct {
	Items  []SourceTorrentStats `json:"items"`
	Totals SourceTorrentTotals  `json:"totals"`
}

type SourceTorrentTotals struct {
	Imported       int64 `json:"imported"`
	AwaitingImport int64 `json:"awaiting_import"`
}

type SourceStorageUsage struct {
	Bytes      int64     `json:"bytes"`
	MeasuredAt time.Time `json:"measured_at"`
}

// SourceTorrentStats counts current published identities, independently of the
// number of observations or shared info hashes. Decisions use the same freshness
// rules as identification diagnostics and stream publication.
type SourceTorrentStats struct {
	ProviderID    string               `json:"provider_id"`
	Imported      int64                `json:"imported"`
	Matched       int64                `json:"matched"`
	Ambiguous     int64                `json:"ambiguous"`
	Unmatched     int64                `json:"unmatched"`
	Pending       int64                `json:"pending"`
	PendingImport *SourcePendingImport `json:"pending_import,omitempty"`
	Storage       *SourceStorageUsage  `json:"storage,omitempty"`
}

// An incomplete Full import or remote sync can contain updates to already
// published identities. Its progress must never be added to Imported.
type SourcePendingImport struct {
	RunID    string `json:"run_id"`
	Torrents int64  `json:"torrents"`
	New      int64  `json:"new"`
	Updates  int64  `json:"updates"`
}
