package model

import "slices"

// ResultSort is ordered by priority. Direction always describes the field,
// including age (ascending means youngest first), rather than its display label.
type ResultSort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

// ResultPreferences are optional per-connection overrides. Zero leaves the
// connection's result count unlimited and uses the instance response budget.
type ResultPreferences struct {
	Sorting         []ResultSort `json:"sorting,omitempty"`
	MaxResults      int          `json:"max_results,omitempty"`
	MaxCached       int          `json:"max_cached,omitempty"`
	ResponseSeconds int          `json:"response_seconds,omitempty"`
}

func (p ResultPreferences) Valid(language bool) bool {
	if len(p.Sorting) > 3 || p.MaxResults < 0 || p.MaxResults > 10000 || p.MaxCached < 0 || p.MaxCached > 10000 || p.ResponseSeconds < 0 || p.ResponseSeconds > 600 {
		return false
	}
	fields := []string{"cached", "size", "published", "age", "name", "source"}
	if language {
		fields = append(fields, "language")
	}
	for i, rule := range p.Sorting {
		if !slices.Contains(fields, rule.Field) || (rule.Direction != "asc" && rule.Direction != "desc") {
			return false
		}
		for _, previous := range p.Sorting[:i] {
			// Date and age are the same ordering with opposite directions.
			if previous.Field == rule.Field || ((previous.Field == "age" || previous.Field == "published") && (rule.Field == "age" || rule.Field == "published")) {
				return false
			}
		}
	}
	return true
}

func (p ResultPreferences) Empty() bool {
	return len(p.Sorting) == 0 && p.MaxResults == 0 && p.MaxCached == 0 && p.ResponseSeconds == 0
}

func (p ResultPreferences) ResultLimit(cachedOnly bool) int {
	if cachedOnly {
		return p.MaxCached
	}
	return p.MaxResults
}

// WithHiddenSources removes name-based ordering when source disclosure is withdrawn.
func (p ResultPreferences) WithHiddenSources() ResultPreferences {
	p.Sorting = slices.DeleteFunc(slices.Clone(p.Sorting), func(rule ResultSort) bool { return rule.Field == "source" })
	return p
}
