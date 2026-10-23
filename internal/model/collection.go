package model

const (
	DefaultCollectionWorkers = 5
	MaxCollectionWorkers     = 32
	DefaultPreviewPages      = 3
	DefaultRequestTimeout    = "30s"
)

// CollectionSettings applies capacity immediately; execution defaults are captured at creation.
type CollectionSettings struct {
	AutoResumeInterrupted bool   `json:"auto_resume_interrupted"`
	Workers               int    `json:"workers"`
	MaxQuotaRetries       int    `json:"max_quota_retries"`
	MaxQuotaWaitSeconds   int    `json:"max_quota_wait_seconds"`
	NoProgressRequests    int    `json:"no_progress_requests"`
	NoProgressAction      string `json:"no_progress_action"`
	Revision              int64  `json:"revision"`
}

func (s CollectionSettings) Valid() bool {
	return s.Workers >= 1 && s.Workers <= MaxCollectionWorkers && s.Revision > 0 &&
		s.Policy().Valid()
}

// CollectionPolicy is immutable for the lifetime of a run, including all resumes.
type CollectionPolicy struct {
	SourceRetries       SourceRetryPolicy `json:"source_retries"`
	MaxQuotaRetries     int               `json:"max_quota_retries"`
	MaxQuotaWaitSeconds int               `json:"max_quota_wait_seconds"`
	NoProgressRequests  int               `json:"no_progress_requests"`
	NoProgressAction    string            `json:"no_progress_action"`
	MaxDurationSeconds  int               `json:"max_duration_seconds"`
}

func (p CollectionPolicy) Valid() bool {
	return p.MaxQuotaRetries >= 0 && p.MaxQuotaRetries <= 10 &&
		p.SourceRetries.Valid() &&
		p.MaxQuotaWaitSeconds >= 0 && p.MaxQuotaWaitSeconds <= 86400 &&
		p.NoProgressRequests >= 0 && p.NoProgressRequests <= 100000 &&
		(p.NoProgressAction == "warn" || p.NoProgressAction == "pause") &&
		p.MaxDurationSeconds >= 0 && p.MaxDurationSeconds <= 604800
}

func (s CollectionSettings) Policy() CollectionPolicy {
	return CollectionPolicy{SourceRetries: DefaultSourceRetryPolicy(), MaxQuotaRetries: s.MaxQuotaRetries, MaxQuotaWaitSeconds: s.MaxQuotaWaitSeconds,
		NoProgressRequests: s.NoProgressRequests, NoProgressAction: s.NoProgressAction}
}

// ApplyDefaults uses one settings snapshot and never overrides source-specific timeouts.
func (s CollectionSettings) ApplyDefaults(run *Run) {
	if run.Policy == nil {
		policy := s.Policy()
		run.Policy = &policy
	}
	if run.Config.RequestTimeout == "" {
		run.Config.RequestTimeout = DefaultRequestTimeout
	}
}

// EffectivePolicy preserves the behavior of pre-policy runs without rewriting them.
func (r Run) EffectivePolicy() CollectionPolicy {
	if r.Policy == nil {
		return CollectionPolicy{MaxQuotaRetries: 3, NoProgressAction: "warn"}
	}
	return *r.Policy
}

type SourceRetryPolicy struct {
	Retries        int `json:"retries"`
	InitialSeconds int `json:"initial_seconds"`
	MaximumSeconds int `json:"maximum_seconds"`
}

func DefaultSourceRetryPolicy() SourceRetryPolicy { return SourceRetryPolicy{3, 2, 8} }

// Older run snapshots contain no source retry policy. Keep their original
// behavior without changing the immutable snapshot or consulting live settings.
func (p SourceRetryPolicy) Effective() SourceRetryPolicy {
	if p == (SourceRetryPolicy{}) {
		return DefaultSourceRetryPolicy()
	}
	return p
}

func (p SourceRetryPolicy) Valid() bool {
	if p == (SourceRetryPolicy{}) {
		return true
	}
	return p.Retries >= 0 && p.Retries <= 10 && p.InitialSeconds >= 1 && p.InitialSeconds <= 60 && p.MaximumSeconds >= p.InitialSeconds && p.MaximumSeconds <= 900
}

type PauseReason string

const (
	PauseManual      PauseReason = "manual"
	PauseBudget      PauseReason = "budget"
	PauseQuota       PauseReason = "quota"
	PauseNoProgress  PauseReason = "no_progress"
	PauseInterrupted PauseReason = "interrupted"
)

// RequiresManualResume excludes budget continuation and technical recovery.
func (r Run) RequiresManualResume() bool {
	return r.PauseRequested || r.PauseReason == PauseManual || r.PauseReason == PauseQuota || r.PauseReason == PauseNoProgress
}

// CollectionOverview includes the local database connection budget and queue state.
type CollectionOverview struct {
	SourceRunning int                `json:"source_running"`
	SourceQueued  int                `json:"source_queued"`
	Settings      CollectionSettings `json:"settings"`
	Running       int                `json:"running"`
	Queued        int                `json:"queued"`
	Capacity      int                `json:"capacity"`
}
