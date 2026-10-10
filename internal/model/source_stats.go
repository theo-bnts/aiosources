package model

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
}

// An incomplete Full import or remote sync can contain updates to already
// published identities. Its progress must never be added to Imported.
type SourcePendingImport struct {
	RunID    string `json:"run_id"`
	Torrents int64  `json:"torrents"`
}
