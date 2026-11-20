package model

import "time"

const (
	CorrespondenceRuleInfoHash    = "exact_info_hash"
	CorrespondenceRuleExactSize   = "work_exact_size"
	CorrespondenceRuleReleaseName = "work_release_name"
)

const (
	CorrespondenceConfidenceExact    = "exact"
	CorrespondenceConfidenceHigh     = "high"
	CorrespondenceConfidenceVerified = "verified"
)

func ValidCorrespondenceRule(rule string) bool {
	switch rule {
	case CorrespondenceRuleInfoHash, CorrespondenceRuleExactSize, CorrespondenceRuleReleaseName:
		return true
	default:
		return false
	}
}

// ReleaseCorrespondenceGroup records a verified cross-source equivalence group
// of torrents representing the exact same underlying release across distinct sources.
type ReleaseCorrespondenceGroup struct {
	ID                  int64     `json:"id"`
	Scope               string    `json:"scope"`
	ScopeKey            string    `json:"scope_key"`
	WorkID              *int64    `json:"work_id,omitempty"`
	WorkType            string    `json:"work_type,omitempty"`
	WorkTitle           string    `json:"work_title,omitempty"`
	IMDb                string    `json:"imdb,omitempty"`
	TMDB                string    `json:"tmdb,omitempty"`
	MatchRule           string    `json:"match_rule"`
	MatchKey            string    `json:"match_key"`
	Confidence          string    `json:"confidence"`
	CanonicalSize       int64     `json:"canonical_size"`
	CanonicalTitle      string    `json:"canonical_title"`
	MemberCount         int       `json:"member_count"`
	ProviderCount       int       `json:"provider_count"`
	EnrichedCount       int       `json:"enriched_count"`
	Providers           []string  `json:"providers"`
	PropagatedAudio     []string  `json:"propagated_audio"`
	PropagatedSubtitles []string  `json:"propagated_subtitles"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ReleaseCorrespondenceMember records one torrent occurrence participating in a
// cross-source equivalence group, preserving native vs propagated languages separately.
type ReleaseCorrespondenceMember struct {
	GroupID              int64    `json:"group_id"`
	OccurrenceID         string   `json:"occurrence_id"`
	ProviderID           string   `json:"provider_id"`
	SourceID             string   `json:"source_id"`
	Fingerprint          string   `json:"fingerprint"`
	Title                string   `json:"title"`
	Hash                 string   `json:"hash,omitempty"`
	Size                 int64    `json:"size"`
	MatchRule            string   `json:"match_rule"`
	OwnAudio             []string `json:"own_audio"`
	OwnSubtitles         []string `json:"own_subtitles"`
	ContributedAudio     []string `json:"contributed_audio"`
	ContributedSubtitles []string `json:"contributed_subtitles"`
}

// ReleaseCorrespondenceExclusion persists a manual unlink decision so automatic
// correspondence computation never re-links that occurrence to that group key.
type ReleaseCorrespondenceExclusion struct {
	OccurrenceID string    `json:"occurrence_id"`
	ProviderID   string    `json:"provider_id"`
	SourceID     string    `json:"source_id"`
	Title        string    `json:"title"`
	MatchKey     string    `json:"match_key"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}

type ReleaseCorrespondenceDetail struct {
	Group      ReleaseCorrespondenceGroup       `json:"group"`
	Members    []ReleaseCorrespondenceMember    `json:"members"`
	Exclusions []ReleaseCorrespondenceExclusion `json:"exclusions"`
}

type ReleaseCorrespondenceOptions struct {
	ListOptions
	Rule         string
	EnrichedOnly bool
	Language     string
}

type CorrespondenceScope struct {
	Scope    string
	ScopeKey string
	Version  int64
}

type ComputedReleaseGroup struct {
	Scope               string
	ScopeKey            string
	WorkID              *int64
	MatchRule           string
	MatchKey            string
	Confidence          string
	CanonicalSize       int64
	CanonicalTitle      string
	MemberCount         int
	ProviderCount       int
	EnrichedCount       int
	PropagatedAudio     []string
	PropagatedSubtitles []string
	Members             []ComputedReleaseMember
}

type ComputedReleaseMember struct {
	ProviderID           string
	SourceID             string
	Fingerprint          string
	MatchRule            string
	OwnAudio             []string
	OwnSubtitles         []string
	ContributedAudio     []string
	ContributedSubtitles []string
}
