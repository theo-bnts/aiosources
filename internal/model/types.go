// Package model defines the shared contracts of the ingestion platform.
package model

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrBusy     = errors.New("an operation is already active")
	ErrInvalid  = errors.New("invalid configuration")
	ErrStalled  = errors.New("pagination did not advance")
)

type Provider struct {
	Version         int            `json:"version"`
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Adapter         string         `json:"adapter"`
	URL             string         `json:"url"`
	Enabled         bool           `json:"enabled"`
	Auth            Auth           `json:"auth"`
	RequestInterval string         `json:"request_interval"`
	RequestLimits   *RequestLimits `json:"request_limits,omitempty"`
	RequestTimeout  string         `json:"request_timeout,omitempty"`
	RateLimitReset  string         `json:"rate_limit_reset,omitempty"`
	PageSize        int            `json:"page_size"`
	Search          Search         `json:"search"`
	HTTP            HTTPConfig     `json:"http"`
	HTML            *HTMLConfig    `json:"html,omitempty"`
	Pagination      Pagination     `json:"pagination"`
	Mapping         Mapping        `json:"mapping"`
	Traversal       *JSONTraversal `json:"traversal,omitempty"`
	Output          Output         `json:"output"`
	Schedule        Schedule       `json:"schedule"`
	CatalogSync     *CatalogSync   `json:"catalog_sync,omitempty"`
	Options         map[string]any `json:"options,omitempty"`
}

const DefaultCatalogSyncMinutes = 60
const MaxCatalogSyncMinutes = 525600

type CatalogSync struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"interval_minutes"`
}

func (p Provider) IsRemoteCatalog() bool { return p.Adapter == "http_json" && p.HTTP.Catalog }

// RequestLimits bound primary-origin request attempts in rolling time windows.
// Zero disables that window; admission history is shared by all runs of a source.
type RequestLimits struct {
	PerMinute int `json:"per_minute,omitempty"`
	PerHour   int `json:"per_hour,omitempty"`
	PerDay    int `json:"per_day,omitempty"`
}

// SupportsMetadata identifies native sources with a configured detail mapping.
func (p Provider) SupportsMetadata() bool {
	return p.Adapter == "http_json" && !p.HTTP.Catalog && p.Traversal != nil &&
		p.Traversal.IDRecovery != nil && len(p.Traversal.EnrichFields) > 0
}

// MetadataFields excludes retired counters without changing saved definitions
// or immutable run snapshots. Old counter-only jobs finish with no candidates.
func (p Provider) MetadataFields() []string {
	if p.Traversal == nil {
		return nil
	}
	fields := p.Traversal.EnrichFields
	if !slices.ContainsFunc(fields, swarmCount) {
		return fields
	}
	return slices.DeleteFunc(slices.Clone(fields), swarmCount)
}

// SecretReferences gives admission, execution and deletion the same dependency set.
func (p Provider) SecretReferences() ([]string, error) {
	var refs []string
	for _, ref := range []string{p.Auth.SecretRef, p.Auth.UsernameRef, p.Auth.PasswordRef} {
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	for _, ref := range p.HTTP.SecretHeaders {
		if ref == "" {
			return nil, ErrInvalid
		}
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	return slices.Compact(refs), nil
}

type Auth struct {
	Type        string `json:"type"`
	SecretRef   string `json:"secret_ref,omitempty"`
	UsernameRef string `json:"username_ref,omitempty"`
	PasswordRef string `json:"password_ref,omitempty"`
	In          string `json:"in,omitempty"`
	Name        string `json:"name,omitempty"`
}

type Search struct {
	Query      string `json:"query,omitempty"`
	Categories []int  `json:"categories"`
}

type HTTPConfig struct {
	Method           string            `json:"method"`
	Headers          map[string]string `json:"headers,omitempty"`
	SecretHeaders    map[string]string `json:"secret_headers,omitempty"`
	Query            map[string]any    `json:"query,omitempty"`
	IncrementalQuery map[string]any    `json:"incremental_query,omitempty"`
	Body             any               `json:"body,omitempty"`
	ItemsPath        string            `json:"items_path"`
	Catalog          bool              `json:"catalog,omitempty"`
}

type Pagination struct {
	Type        string `json:"type"`
	In          string `json:"in,omitempty"`
	PageParam   string `json:"page_param,omitempty"`
	OffsetParam string `json:"offset_param,omitempty"`
	SizeParam   string `json:"size_param,omitempty"`
	CursorParam string `json:"cursor_param,omitempty"`
	Start       int    `json:"start"`
	NextPath    string `json:"next_path,omitempty"`
	TotalPath   string `json:"total_path,omitempty"`
	CurrentPath string `json:"current_path,omitempty"`
}

type Mapping struct {
	ID     string            `json:"id"`
	Fields map[string]string `json:"fields"`
}

type Output struct {
	Fields []string `json:"fields,omitempty"`
}

type Schedule struct {
	Enabled    *bool   `json:"enabled,omitempty"`
	Every      string  `json:"every,omitempty"`
	FullEvery  string  `json:"full_every,omitempty"`
	Cron       string  `json:"cron,omitempty"`
	Timezone   string  `json:"timezone,omitempty"`
	MaxPages   int     `json:"max_pages,omitempty"`
	Mode       RunMode `json:"mode,omitempty"`
	KnownPages int     `json:"known_pages,omitempty"`
}

type ScheduleSummary struct {
	ProviderID      string     `json:"provider_id"`
	ProviderName    string     `json:"provider_name"`
	ProviderEnabled bool       `json:"provider_enabled"`
	Valid           bool       `json:"valid"`
	Revision        string     `json:"revision"`
	Schedule        Schedule   `json:"schedule"`
	Enabled         bool       `json:"enabled"`
	State           string     `json:"state"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	NextFullAt      *time.Time `json:"next_full_at,omitempty"`
	LastRun         *Run       `json:"last_run,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type ProviderDocument struct {
	Provider Provider `json:"provider"`
	JSON     string   `json:"json"`
	Revision string   `json:"revision"`
	Issues   []string `json:"issues"`
}

type ProviderSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Adapter       string   `json:"adapter"`
	Enabled       bool     `json:"enabled"`
	Revision      string   `json:"revision"`
	Valid         bool     `json:"valid"`
	Issues        []string `json:"issues"`
	OutputFields  []string `json:"output_fields,omitempty"`
	LastRun       *Run     `json:"last_run,omitempty"`
	RemoteCatalog bool     `json:"remote_catalog,omitempty"`
}

type AdapterInfo struct {
	Type        string `json:"type"`
	Profile     string `json:"profile,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Template    string `json:"template"`
}

type Validation struct {
	Valid    bool      `json:"valid"`
	ID       string    `json:"id,omitempty"`
	Issues   []string  `json:"issues"`
	Provider *Provider `json:"provider,omitempty"`
}

type SecretResolver func(context.Context, string) (string, error)

type SecretInfo struct {
	Name      string    `json:"name"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RunMode string

const (
	ModePreview     RunMode = "preview"
	ModeIncremental RunMode = "incremental"
	ModeFull        RunMode = "full"
	ModeMetadata    RunMode = "metadata"
)

type RunStatus string

const (
	StatusQueued    RunStatus = "queued"
	StatusRunning   RunStatus = "running"
	StatusPaused    RunStatus = "paused"
	StatusSucceeded RunStatus = "succeeded"
	StatusFailed    RunStatus = "failed"
	StatusCancelled RunStatus = "cancelled"
)

type StartRun struct {
	ProviderID         string  `json:"provider_id"`
	Mode               RunMode `json:"mode"`
	MaxPages           *int    `json:"max_pages,omitempty"`
	MaxDurationSeconds *int    `json:"max_duration_seconds,omitempty"`
	KnownPages         *int    `json:"known_pages,omitempty"`
}

type RunTrigger string

const (
	TriggerManual    RunTrigger = "manual"
	TriggerScheduled RunTrigger = "scheduled"
)

type Run struct {
	RemoteCatalog         bool              `json:"remote_catalog,omitempty"`
	ID                    string            `json:"id"`
	ProviderID            string            `json:"provider_id"`
	ProviderName          string            `json:"provider_name"`
	Mode                  RunMode           `json:"mode"`
	InitialFull           bool              `json:"initial_full,omitempty"`
	Trigger               RunTrigger        `json:"trigger"`
	MetadataParentRunID   string            `json:"metadata_parent_run_id,omitempty"`
	Status                RunStatus         `json:"status"`
	CancelRequested       bool              `json:"cancel_requested"`
	PauseRequested        bool              `json:"pause_requested"`
	PauseReason           PauseReason       `json:"pause_reason,omitempty"`
	Policy                *CollectionPolicy `json:"policy,omitempty"`
	StorageMode           StorageMode       `json:"storage_mode"`
	RequestsWithoutNewIDs int64             `json:"requests_without_new_ids"`
	ProgressWarningSent   bool              `json:"progress_warning_sent"`
	TraversalDone         bool              `json:"traversal_done"`
	MaxPages              int               `json:"max_pages"`
	Pages                 int               `json:"pages"`
	Records               int               `json:"records"`
	DistinctRecords       int64             `json:"distinct_records"`
	Errors                int               `json:"errors"`
	Revision              string            `json:"revision"`
	Cursor                json.RawMessage   `json:"-"` // Adapter state may contain signed URLs or continuation credentials.
	KnownPageStreak       int               `json:"-"`
	Config                Provider          `json:"-"`
	CreatedAt             time.Time         `json:"created_at"`
	StartedAt             *time.Time        `json:"started_at,omitempty"`
	FinishedAt            *time.Time        `json:"finished_at,omitempty"`
	Error                 string            `json:"error,omitempty"`
}

// Record keeps the original item separate from its interpreted fields.
// A missing source identity or invalid mapping never discards Raw.
type Record struct {
	SourceID    string         `json:"source_id"`
	Raw         []byte         `json:"-"`
	ContentType string         `json:"content_type"`
	Fields      map[string]any `json:"fields"`
	Error       string         `json:"error,omitempty"`
	Ignored     bool           `json:"ignored,omitempty"`
	Auxiliary   bool           `json:"auxiliary,omitempty"`
	Deleted     bool           `json:"deleted,omitempty"`
	Origin      *CatalogOrigin `json:"origin,omitempty"`
	// Complete latest memberships, not an additive history of earlier matches.
	// Nil means the adapter does not provide coverage for this observation.
	// A valid explicit empty set retires prior derived coverage, even when
	// Ignored marks the observation as outside the selected dataset.
	CoverageScopes []string `json:"-"`
}

// Page includes the unmodified response even if record normalization fails.
// Next is opaque, JSON-serializable adapter state. Done means the complete
// requested traversal ended, not merely that a short page was returned.
type Page struct {
	Body             []byte
	ContentType      string
	Items            []Record
	Next             json.RawMessage
	Done             bool
	Position         string
	Error            string
	ResetStaging     bool
	RequireUniqueIDs bool
	// Adapter-supplied repeat evidence when native IDs are unavailable. This
	// must not establish native membership or an Incremental known-page boundary.
	FallbackFingerprint string
	// Nil preserves the generic Incremental streak for auxiliary/error responses.
	KnownPageStreak *int
	// RefreshScopes retires only these scopes' derived coverage and orphaned
	// staging, atomically with Next. Raw observations remain append-only.
	RefreshScopes []string
	Metadata      map[string]any
}

type Event struct {
	ID        int64          `json:"id"`
	RunID     string         `json:"run_id"`
	Kind      string         `json:"kind"`
	Message   string         `json:"message"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type RawRecord struct {
	ID              int64          `json:"id"`
	RunID           string         `json:"run_id"`
	ProviderID      string         `json:"provider_id"`
	SourceID        string         `json:"source_id"`
	Page            int            `json:"page"`
	ContentType     string         `json:"content_type"`
	Fields          map[string]any `json:"fields"`
	Error           string         `json:"error,omitempty"`
	Ignored         bool           `json:"ignored,omitempty"`
	Auxiliary       bool           `json:"auxiliary,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	Raw             []byte         `json:"-"`
	PageID          int64          `json:"page_id"`
	PayloadRetained bool           `json:"payload_retained"`
}

type Torrent struct {
	OccurrenceID string         `json:"occurrence_id"`
	ProviderID   string         `json:"provider_id"`
	SourceID     string         `json:"source_id"`
	Fields       map[string]any `json:"fields"`
	RawID        *int64         `json:"raw_id,omitempty"`
	FirstSeenAt  time.Time      `json:"first_seen_at"`
	LastSeenAt   time.Time      `json:"last_seen_at"`
	Historical   bool           `json:"historical"`
	Origin       *CatalogOrigin `json:"origin,omitempty"`
}

type ListOptions struct {
	SourceKind      string
	ProviderID      string
	RunID           string
	Status          string
	Query           string
	Providers       []string
	Category        string
	MinSize         string
	MaxSize         string
	PublishedAfter  string
	PublishedBefore string
	InfoHash        string
	Sort            string
	Limit           int
	Offset          int
}

type List[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

type RawProcessingCounts struct {
	Accepted   int64 `json:"accepted"`
	Errors     int64 `json:"error"`
	Ignored    int64 `json:"ignored"`
	Enrichment int64 `json:"enrichment"`
}
type RawListing struct {
	List[RawRecord]
	Counts RawProcessingCounts `json:"counts"`
}
