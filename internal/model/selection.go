package model

import "strings"

// SelectionValues preserves single-value URLs while allowing a bounded set of
// alternatives. An empty selection means no restriction.
func SelectionValues(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}

func ValidSelection(value string, allowed func(string) bool) bool {
	if len(value) > 16384 {
		return false
	}
	values := SelectionValues(value)
	if len(values) > 100 {
		return false
	}
	for _, item := range values {
		if item == "" || len(item) > 256 || !allowed(item) {
			return false
		}
	}
	return true
}
