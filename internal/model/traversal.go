package model

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
	// after successful Incremental completion. Existing snapshots default to off.
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
