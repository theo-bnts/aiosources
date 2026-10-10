package model

import (
	"maps"
	"slices"
)

// WithoutSwarmCounts removes transient counts from interpreted torrent data without
// mutating the source record, changing exact numbers or rewriting original bytes.
func WithoutSwarmCounts(fields map[string]any) map[string]any {
	cleaned, _ := withoutSwarmCounts(fields)
	return cleaned.(map[string]any)
}

func withoutSwarmCounts(value any) (any, bool) {
	switch value := value.(type) {
	case map[string]any:
		var cleaned map[string]any
		for key, field := range value {
			var next any
			changed := key == "seeders" || key == "peers"
			if !changed {
				next, changed = withoutSwarmCounts(field)
			}
			if !changed {
				continue
			}
			if cleaned == nil {
				cleaned = maps.Clone(value)
			}
			if key == "seeders" || key == "peers" {
				delete(cleaned, key)
			} else {
				cleaned[key] = next
			}
		}
		if cleaned != nil {
			return cleaned, true
		}
	case map[string][]string:
		_, seeders := value["seeders"]
		_, peers := value["peers"]
		if seeders || peers {
			cleaned := maps.Clone(value)
			delete(cleaned, "seeders")
			delete(cleaned, "peers")
			return cleaned, true
		}
	case []any:
		var cleaned []any
		for index, field := range value {
			next, changed := withoutSwarmCounts(field)
			if !changed {
				continue
			}
			if cleaned == nil {
				cleaned = slices.Clone(value)
			}
			cleaned[index] = next
		}
		if cleaned != nil {
			return cleaned, true
		}
	}
	return value, false
}
