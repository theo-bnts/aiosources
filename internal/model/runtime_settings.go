package model

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
		return map[string]RuntimeField{"days": {7, 1, 3650}, "cleanup_seconds": {30, 5, 3600}}
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
	return true
}
