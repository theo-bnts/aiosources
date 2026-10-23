package model

import "time"

const MaxDownloadTicketMinutes = 60

// Runtime sections have independent revisions: saving network timeouts cannot
// overwrite a concurrent change to storage or Debrid capacity.
type RuntimeSection struct {
	Revision int64          `json:"revision"`
	Values   map[string]int `json:"values"`
}

type RuntimeField struct{ Default, Min, Max int }

func RuntimeFields(section string) map[string]RuntimeField {
	switch section {
	case "debrid":
		return map[string]RuntimeField{"cache_jobs": {4, 1, 32}, "download_jobs": {4, 1, 32}, "cleanup_jobs": {4, 1, 32}, "public_checks": {1, 1, 16}}
	case "outgoing":
		return map[string]RuntimeField{"metadata_seconds": {60, 5, 600}, "debrid_seconds": {60, 5, 600}, "inventory_seconds": {600, 30, 3600}}
	case "connections":
		return map[string]RuntimeField{"stremio_seconds": {70, 10, 600}, "torznab_seconds": {20, 5, 600}, "catalog_seconds": {20, 5, 600}}
	case "history":
		return map[string]RuntimeField{"days": {7, 1, 3650}, "cleanup_seconds": {30, 5, 3600}, "batch_size": {5000, 100, 20000}, "pass_seconds": {10, 1, 60}}
	case "identification":
		return map[string]RuntimeField{"workers": {0, 0, 32}, "queue_batch": {250, 10, 2000}, "transaction_batch": {25, 1, 100}}
	case "matching":
		return map[string]RuntimeField{"minimum_similarity": {92, 92, 100}, "ambiguity_margin": {8, 8, 25}}
	case "metadata":
		return map[string]RuntimeField{"workers": {16, 1, 32}, "queue_batch": {100, 1, 1000}, "publication_batch": {16, 1, 100}}
	case "metadata_retries":
		return map[string]RuntimeField{"request_attempts": {4, 1, 10}, "request_retry_seconds": {1, 1, 60}, "sync_retry_seconds": {60, 10, 3600}, "sync_retry_max_seconds": {900, 60, 86400}, "detail_retry_seconds": {60, 10, 3600}, "detail_retry_max_seconds": {86400, 60, 604800}}
	case "source_retries":
		return map[string]RuntimeField{"retries": {3, 0, 10}, "initial_seconds": {2, 1, 60}, "maximum_seconds": {8, 1, 900}}
	case "debrid_retries":
		return map[string]RuntimeField{"delay_minutes": {15, 1, 1440}, "maximum_hours": {48, 1, 168}}
	case "downloads":
		return map[string]RuntimeField{"duration_hours": {24, 1, 168}, "ticket_minutes": {10, 1, MaxDownloadTicketMinutes}}
	case "security":
		return map[string]RuntimeField{"session_hours": {12, 1, 168}, "login_per_minute": {10, 1, 60}, "login_global_per_minute": {60, 1, 600}, "session_limit": {256, 1, 1024}, "enrollment_minutes": {10, 5, 30}}
	case "payloads":
		return map[string]RuntimeField{"source_kib": {128, 16, 128}, "public_stremio_bytes": {8192, 1024, 8192}, "public_torznab_bytes": {6000, 1024, 6000}}
	}
	return nil
}

func DefaultRuntimeSection(section string) RuntimeSection {
	result := RuntimeSection{Revision: 1, Values: map[string]int{}}
	for key, field := range RuntimeFields(section) {
		result.Values[key] = field.Default
	}
	return result
}

func (s RuntimeSection) Valid(section string) bool {
	fields := RuntimeFields(section)
	if fields == nil || s.Revision < 1 || len(s.Values) != len(fields) {
		return false
	}
	for key, field := range fields {
		v, ok := s.Values[key]
		if !ok || v < field.Min || v > field.Max {
			return false
		}
	}
	if section == "identification" && s.Values["transaction_batch"] > s.Values["queue_batch"] {
		return false
	}
	if section == "metadata" && s.Values["publication_batch"] > s.Values["queue_batch"] {
		return false
	}
	if section == "metadata_retries" && (s.Values["sync_retry_seconds"] > s.Values["sync_retry_max_seconds"] || s.Values["detail_retry_seconds"] > s.Values["detail_retry_max_seconds"]) {
		return false
	}
	if section == "debrid_retries" && s.Values["delay_minutes"] > s.Values["maximum_hours"]*60 {
		return false
	}
	if section == "security" && s.Values["login_per_minute"] > s.Values["login_global_per_minute"] {
		return false
	}
	if section == "source_retries" && s.Values["initial_seconds"] > s.Values["maximum_seconds"] {
		return false
	}
	return true
}

func RuntimeSections() []string {
	return []string{"debrid", "outgoing", "connections", "history", "identification", "matching", "metadata", "metadata_retries", "source_retries", "debrid_retries", "downloads", "security", "payloads"}
}

type MatchingPolicy struct {
	MinimumSimilarity int
	AmbiguityMargin   int
}

func DefaultMatchingPolicy() MatchingPolicy { return MatchingPolicy{92, 8} }

func (p MatchingPolicy) Valid() bool {
	return p.MinimumSimilarity >= 92 && p.MinimumSimilarity <= 100 && p.AmbiguityMargin >= 8 && p.AmbiguityMargin <= 25
}

// Saturate before multiplying, so long-lived retry counters cannot overflow.
func RetryBackoff(attempt int, initial, maximum time.Duration) time.Duration {
	delay := min(initial, maximum)
	for range min(max(attempt, 0), 63) {
		if delay >= maximum-delay {
			return maximum
		}
		delay *= 2
	}
	return delay
}
