package aiosource

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Precompute text and language keys once, rather than parsing releases inside
// an O(n log n) comparator. The same ordering drives probing and presentation.
type resultRank struct {
	size                   int64
	name, source, identity string
	published              *time.Time
	cached, language       int
}

func recordRank(rec model.AIORecord, source, language string) resultRank {
	if source == "" {
		source = rec.Provider
	}
	r := resultRank{size: rec.Size, name: strings.ToLower(rec.Title), source: strings.ToLower(source), identity: rec.Provider + ":" + rec.SourceID, published: rec.PublishedAt, cached: 1}
	if language == "" {
		return r
	}
	languages := ReleaseLanguageCodes(rec)
	if len(languages) == 0 {
		r.language = 1
	}
	if slices.Contains(languages, "multi") {
		r.language = 2
	}
	if language != "" && slices.Contains(languages, language) {
		r.language = 3
	}
	return r
}

func sortingLanguage(p model.StreamPreferences) string {
	if slices.ContainsFunc(p.Sorting, func(rule model.ResultSort) bool { return rule.Field == "language" }) {
		return p.Language
	}
	return ""
}

func compareResults(a, b resultRank, rules []model.ResultSort) int {
	if len(rules) == 0 {
		rules = []model.ResultSort{{Field: "size", Direction: "desc"}}
	}
	for _, rule := range rules {
		var order int
		switch rule.Field {
		case "size":
			order = cmp.Compare(a.size, b.size)
		case "name":
			order = strings.Compare(a.name, b.name)
		case "source":
			order = strings.Compare(a.source, b.source)
		case "cached":
			order = cmp.Compare(a.cached, b.cached)
		case "language":
			order = cmp.Compare(a.language, b.language)
		case "published", "age":
			// An absent date has no age and stays last in either direction.
			if a.published == nil && b.published != nil {
				return 1
			}
			if a.published != nil && b.published == nil {
				return -1
			}
			if a.published != nil && b.published != nil {
				order = a.published.Compare(*b.published)
			}
			if rule.Field == "age" {
				order = -order
			}
		}
		if rule.Direction == "desc" {
			order = -order
		}
		if order != 0 {
			return order
		}
	}
	return strings.Compare(a.identity, b.identity)
}

func rankCache(hash string, availability map[string]map[string]model.AIOAvailability) int {
	known := len(availability) > 0
	for _, states := range availability {
		item, ok := states[hash]
		if ok && item.Cached {
			return 2
		}
		known = known && ok
	}
	if known {
		return 0
	}
	return 1
}

func sortStreamResults(streams []Stream, history []model.StremioResult, ranks []resultRank, p model.StreamPreferences) ([]Stream, []model.StremioResult) {
	indices := make([]int, len(streams))
	for i := range indices {
		indices[i] = i
	}
	slices.SortStableFunc(indices, func(i, j int) int { return compareResults(ranks[i], ranks[j], p.Sorting) })
	if limit := p.ResultLimit(p.CachedOnly); limit > 0 && len(indices) > limit {
		indices = indices[:limit]
	}
	out := make([]Stream, 0, len(indices))
	results := make([]model.StremioResult, 0, len(indices))
	for _, i := range indices {
		out = append(out, streams[i])
		results = append(results, history[i])
	}
	return out, results
}
