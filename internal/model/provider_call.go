package model

// ProviderCall contains diagnostics only. URLs, bodies and account identifiers
// cannot be represented by this type.
type ProviderCall struct {
	Phase      string `json:"phase"`
	DurationMS int64  `json:"duration_ms"`
	HTTPStatus int    `json:"http_status"`
	ErrorCode  string `json:"error_code"`
}

func (c ProviderCall) Valid() bool {
	if c.DurationMS < 0 || c.HTTPStatus < 0 || c.HTTPStatus > 599 {
		return false
	}
	switch c.Phase {
	case "provider_request", "provider_availability", "provider_inventory", "provider_add", "provider_files", "provider_cleanup", "provider_unlock", "provider_playback", "book_changes", "book_details", "music_snapshot", "music_dump", "music_covers", "music_replication", "software_nvd_meta", "software_nvd_dump", "software_nvd_incremental", "software_winget", "software_homebrew", "software_icons":
	default:
		return false
	}
	switch c.ErrorCode {
	case "", "provider_error", "invalid_response", "authentication", "cancelled", "timeout", "rate_limit", "account_limit", "subscription", "provider_unavailable", "invalid_torrent", "file_too_large", "transfer_failed", "not_found", "not_cached":
		return true
	default:
		return false
	}
}
