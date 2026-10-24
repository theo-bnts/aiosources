package model

import (
	"encoding/json"
	"regexp"
	"slices"

	"golang.org/x/text/language"
)

// StreamPreferences are installation and view preferences, never credentials.
// An empty mode preserves older installations which did not select a language.
type StreamPreferences struct {
	ResultPreferences
	Language   string   `json:"language"`
	Mode       string   `json:"mode"`
	Sources    []string `json:"sources,omitempty"`
	Services   []string `json:"services,omitempty"`
	Types      []string `json:"types,omitempty"`
	CachedOnly bool     `json:"cached_only,omitempty"`
}

var sourceIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func ValidSourceID(value string) bool { return sourceIdentifier.MatchString(value) }

func NormalizeLanguage(value string) string {
	if len(value) > 64 {
		return ""
	}
	tag, err := language.Parse(value)
	if err != nil {
		return ""
	}
	base, _ := tag.Base()
	if base.String() == "und" {
		return ""
	}
	return base.String()
}

func ValidStreamPreferences(p StreamPreferences) bool {
	if len(p.Sources) > 100 || !ValidPlaybackPreferences(p) {
		return false
	}
	encoded, err := json.Marshal(p)
	return err == nil && len(encoded) <= 4096
}

// Saved access can select more sources than fit into a public installation URL.
// The playback policy has the same source bound as other saved access grants.
func ValidPlaybackPreferences(p StreamPreferences) bool {
	if !p.ResultPreferences.Valid(true) || p.ResponseSeconds > 60 || len(p.Sources) > 1000 || len(p.Services) > len(DebridServices())+1 || len(p.Types) > 3 {
		return false
	}
	if p.Language == "" && slices.ContainsFunc(p.Sorting, func(rule ResultSort) bool { return rule.Field == "language" }) {
		return false
	}
	seen := map[string]bool{}
	for _, source := range p.Sources {
		if !ValidSourceID(source) || seen[source] {
			return false
		}
		seen[source] = true
	}
	for _, pair := range []struct{ values, allowed []string }{{p.Services, append(DebridServices(), "direct")}, {p.Types, []string{"movie", "series", "anime"}}} {
		clear(seen)
		for _, value := range pair.values {
			if !slices.Contains(pair.allowed, value) || seen[value] {
				return false
			}
			seen[value] = true
		}
	}
	return (p.Mode == "" || p.Mode == "all" || p.Mode == "preferred" || p.Mode == "multi" || p.Mode == "unknown") &&
		(p.Language == "" || NormalizeLanguage(p.Language) == p.Language) && (p.Mode == "" || p.Mode == "all" || p.Language != "")
}
