package model

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxMetadataFilterCategories    = 100
	MaxMetadataFilterCategoryBytes = 256
)

var validMetadataIdentificationStatuses = []string{"unmatched", "ambiguous", "pending", "matched"}

// MetadataFilters optionally narrow a standalone Metadata run to specific
// source categories and/or current identification statuses.
type MetadataFilters struct {
	Categories             []string `json:"categories,omitempty"`
	IdentificationStatuses []string `json:"identification_statuses,omitempty"`
}

func (f *MetadataFilters) Empty() bool {
	return f == nil || (len(f.Categories) == 0 && len(f.IdentificationStatuses) == 0)
}

// Normalize trims and deduplicates categories (ignoring case) and validates
// identification statuses against the canonical identification state names.
// Empty filters normalize to nil so callers omit empty objects in snapshots.
func (f *MetadataFilters) Normalize() (*MetadataFilters, error) {
	if f == nil {
		return nil, nil
	}
	if len(f.Categories) > MaxMetadataFilterCategories || len(f.IdentificationStatuses) > len(validMetadataIdentificationStatuses) {
		return nil, ErrInvalid
	}
	var categories []string
	if len(f.Categories) > 0 {
		seen := make(map[string]bool, len(f.Categories))
		categories = make([]string, 0, len(f.Categories))
		for _, raw := range f.Categories {
			value := strings.TrimSpace(raw)
			if value == "" || len(value) > MaxMetadataFilterCategoryBytes || !utf8.ValidString(value) {
				return nil, ErrInvalid
			}
			for _, r := range value {
				if unicode.IsControl(r) {
					return nil, ErrInvalid
				}
			}
			key := strings.ToLower(value)
			if !seen[key] {
				seen[key] = true
				categories = append(categories, value)
			}
		}
	}
	var statuses []string
	if len(f.IdentificationStatuses) > 0 {
		seen := make(map[string]bool, len(f.IdentificationStatuses))
		statuses = make([]string, 0, len(f.IdentificationStatuses))
		for _, status := range f.IdentificationStatuses {
			if !slices.Contains(validMetadataIdentificationStatuses, status) {
				return nil, ErrInvalid
			}
			if !seen[status] {
				seen[status] = true
				statuses = append(statuses, status)
			}
		}
	}
	if len(categories) == 0 && len(statuses) == 0 {
		return nil, nil
	}
	return &MetadataFilters{
		Categories:             categories,
		IdentificationStatuses: statuses,
	}, nil
}

// Valid checks that stored filters are non-empty, canonical and free of duplicates.
func (f *MetadataFilters) Valid() bool {
	if f == nil || f.Empty() {
		return false
	}
	normalized, err := f.Normalize()
	if err != nil || normalized == nil {
		return false
	}
	return slices.Equal(f.Categories, normalized.Categories) &&
		slices.Equal(f.IdentificationStatuses, normalized.IdentificationStatuses)
}

// DiagnosticStatusesOnly reports whether every selected identification status
// is covered by the partial aio_matches_diagnostic_status index.
func (f *MetadataFilters) DiagnosticStatusesOnly() bool {
	if f == nil || len(f.IdentificationStatuses) == 0 {
		return false
	}
	for _, status := range f.IdentificationStatuses {
		if status != "unmatched" && status != "ambiguous" {
			return false
		}
	}
	return true
}

// JSONTraversal describes overlapping searches of a bounded listing API.
// Completion compares committed unique source IDs in every selected scope with
// its advertised total; a search-window boundary is not EOF.
// TotalMode defaults to strict (exact refreshed totals); at_least instead
// keeps the first root total through growth and accepts coverage at or above it.
// A fully enumerated, validated single-page root can reconcile a lower target
// with native classifications; partial or invalid listings cannot do so.
type JSONTraversal struct {
	WindowPages   int              `json:"window_pages"`
	MinimumTotal  int              `json:"minimum_total,omitempty"`
	TotalMode     string           `json:"total_mode,omitempty"`
	TotalPaths    []string         `json:"total_paths"`
	Scopes        []JSONScope      `json:"scopes"`
	Partitions    []JSONPartition  `json:"partitions,omitempty"`
	QueryVariants []map[string]any `json:"query_variants,omitempty"`
	Options       *JSONOptions     `json:"options,omitempty"`
	IDRecovery    *JSONIDRecovery  `json:"id_recovery,omitempty"`
	// IncrementalOrder selects descending native-ID validation (id, default)
	// or a publication-sorted listing with identity-based boundaries (published_at).
	IncrementalOrder string `json:"incremental_order,omitempty"`
	// Metadata mode fills these missing fields; standalone runs cover the catalogue.
	EnrichFields []string `json:"enrich_fields,omitempty"`
	// MetadataAfterIncremental queues a separate Metadata run for new native IDs
	// after successful Incremental or Full discovery completion. Existing snapshots default to off.
	MetadataAfterIncremental bool `json:"metadata_after_incremental,omitempty"`
}

// MetadataCandidate retains native identity separately from its detail locator.
type MetadataCandidate struct {
	SourceID string `json:"source_id"`
	InfoHash string `json:"info_hash"`
}

// Match compares mapped scalar fields, not hashes or publication projections.
// Each matching scope receives an independent observation of the source ID.
type JSONScope struct {
	ID    string         `json:"id"`
	Query map[string]any `json:"query,omitempty"`
	Match map[string]any `json:"match"`
}

// A partition's inclusive range is frozen in the traversal checkpoint.
// Exactly one of End and EndYearOffset must be specified.
type JSONPartition struct {
	Parameter     string `json:"parameter"`
	Start         int    `json:"start"`
	End           *int   `json:"end,omitempty"`
	EndYearOffset *int   `json:"end_year_offset,omitempty"`
}

// URL may use {scope-query-name} placeholders. Each returned group contributes
// its ValuesPath array; each value's ValuePath becomes one QueryParam filter.
type JSONOptions struct {
	URL          string `json:"url"`
	GroupsPath   string `json:"groups_path"`
	ValuesPath   string `json:"values_path"`
	ValuePath    string `json:"value_path"`
	QueryParam   string `json:"query_param"`
	PriorityPath string `json:"priority_path,omitempty"`
}

// ID recovery uses observed numeric IDs to seed finite scan ranges, extending
// past the listing's apparent maximum until scope coverage meets its targets.
// It resolves unseen IDs using HEAD. The last path segment of a same-origin
// Location must match ResolvePattern before it may fill {value} in DetailURL.
// {id} is the requested numeric ID; returned items keep their actual identity.
type JSONIDRecovery struct {
	DiscoveryQuery map[string]any `json:"discovery_query"`
	First          int            `json:"first"`
	ResolveURL     string         `json:"resolve_url"`
	ResolvePattern string         `json:"resolve_pattern"`
	DetailURL      string         `json:"detail_url"`
	DetailPath     string         `json:"detail_path"`
	Mapping        Mapping        `json:"mapping"`
}
