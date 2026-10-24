package model

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const CatalogEntitiesHeader = "X-AIOSources-Catalog-Entities"

// Both sides opt in. These are data permissions, never account configuration.
type CatalogEntities struct {
	Metadata     bool `json:"metadata,omitempty"`
	Availability bool `json:"availability,omitempty"`
}

func (e CatalogEntities) Empty() bool { return !e.Metadata && !e.Availability }
func (e CatalogEntities) Header() string {
	var names []string
	if e.Metadata {
		names = append(names, "metadata")
	}
	if e.Availability {
		names = append(names, "availability")
	}
	return strings.Join(names, ",")
}
func ParseCatalogEntities(value string) (CatalogEntities, bool) {
	var e CatalogEntities
	if len(value) > 64 {
		return e, false
	}
	if value == "" {
		return e, true
	}
	for _, name := range strings.Split(value, ",") {
		switch strings.TrimSpace(name) {
		case "metadata":
			if e.Metadata {
				return e, false
			}
			e.Metadata = true
		case "availability":
			if e.Availability {
				return e, false
			}
			e.Availability = true
		default:
			return e, false
		}
	}
	return e, true
}
func (e CatalogEntities) Intersect(other CatalogEntities) CatalogEntities {
	return CatalogEntities{Metadata: e.Metadata && other.Metadata, Availability: e.Availability && other.Availability}
}

// No local database IDs, match decisions, normalized aliases or remote URLs.
type CatalogMetadata struct {
	Type            string            `json:"type"`
	TMDB            string            `json:"tmdb"`
	IMDb            string            `json:"imdb,omitempty"`
	TVDB            string            `json:"tvdb,omitempty"`
	Adult           bool              `json:"adult"`
	Titles          []string          `json:"titles"`
	DisplayTitle    string            `json:"display_title"`
	LocalizedTitles map[string]string `json:"localized_titles,omitempty"`
	Years           []int             `json:"years"`
	SeasonYears     map[int]int       `json:"season_years,omitempty"`
	EpisodeYears    map[string]int    `json:"episode_years,omitempty"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func CatalogWork(w AIOWork, updated time.Time) CatalogMetadata {
	return CatalogMetadata{Type: w.Type, TMDB: w.TMDB, IMDb: w.IMDb, TVDB: w.TVDB, Adult: w.Adult, Titles: w.Titles,
		DisplayTitle: w.DisplayTitle, LocalizedTitles: w.LocalizedTitles, Years: w.Years, SeasonYears: w.SeasonYears, EpisodeYears: w.EpisodeYears, UpdatedAt: updated}
}
func (m CatalogMetadata) Work() AIOWork {
	return AIOWork{Type: m.Type, TMDB: m.TMDB, IMDb: m.IMDb, TVDB: m.TVDB, Adult: m.Adult, Titles: m.Titles, DisplayTitle: m.DisplayTitle,
		LocalizedTitles: m.LocalizedTitles, Years: m.Years, SeasonYears: m.SeasonYears, EpisodeYears: m.EpisodeYears}
}

type CatalogCacheObservation struct {
	Service   string    `json:"service"`
	Hash      string    `json:"hash"`
	CheckedAt time.Time `json:"checked_at"`
	Files     []AIOFile `json:"files"`
}

type CatalogSupplement struct {
	Metadata     []CatalogMetadata         `json:"metadata,omitempty"`
	Availability []CatalogCacheObservation `json:"availability,omitempty"`
}

var catalogNumber = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)
var catalogIMDb = regexp.MustCompile(`^tt[0-9]{7,12}$`)
var catalogLanguage = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})?$`)
var catalogHash = regexp.MustCompile(`^[0-9a-f]{40}$`)

func catalogLabel(s string, maximum int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= maximum && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n") && !strings.Contains(s, "://")
}
func catalogTime(t, now time.Time) bool {
	return !t.IsZero() && t.Year() >= 1970 && !t.After(now.Add(time.Minute))
}
func validCatalogYear(year int) bool { return year >= 1800 && year <= 2200 }

// Fail a malformed page atomically; do not quietly turn a partial proof into a
// successful import. Size limits also apply when callers bypass the connector.
func (m CatalogMetadata) Valid(now time.Time) bool {
	if (m.Type != "movie" && m.Type != "tv") || !catalogNumber.MatchString(m.TMDB) ||
		(m.IMDb != "" && !catalogIMDb.MatchString(m.IMDb)) || (m.TVDB != "" && !catalogNumber.MatchString(m.TVDB)) ||
		!catalogTime(m.UpdatedAt, now) || !catalogLabel(m.DisplayTitle, 1024) || len(m.Titles) < 1 || len(m.Titles) > 256 || len(m.LocalizedTitles) > 256 || len(m.Years) > 100 || len(m.SeasonYears) > 1000 || len(m.EpisodeYears) > 10000 {
		return false
	}
	for _, t := range m.Titles {
		if !catalogLabel(t, 1024) {
			return false
		}
	}
	for locale, t := range m.LocalizedTitles {
		if !catalogLanguage.MatchString(locale) || !catalogLabel(t, 1024) {
			return false
		}
	}
	for _, year := range m.Years {
		if !validCatalogYear(year) {
			return false
		}
	}
	if m.Type == "movie" && (len(m.SeasonYears) > 0 || len(m.EpisodeYears) > 0) {
		return false
	}
	for season, year := range m.SeasonYears {
		if season < 0 || season > 1000 || !validCatalogYear(year) {
			return false
		}
	}
	for key, year := range m.EpisodeYears {
		p := strings.Split(key, ":")
		if len(p) != 2 || !validCatalogYear(year) {
			return false
		}
		s, e1 := strconv.Atoi(p[0])
		e, e2 := strconv.Atoi(p[1])
		if e1 != nil || e2 != nil || s < 0 || s > 1000 || e < 1 || e > 10000 || key != strconv.Itoa(s)+":"+strconv.Itoa(e) {
			return false
		}
	}
	b, err := json.Marshal(m)
	return err == nil && len(b) <= 512*1024
}

// These providers expose independently reusable hash/file cache proofs. Account
// libraries (notably Real-Debrid and Deepbrid) are deliberately excluded.
func CatalogCacheService(service string) bool {
	return service == "alldebrid" || service == "debridlink" || service == "premiumize" || service == "torbox"
}
func (a CatalogCacheObservation) Valid(now time.Time) bool {
	if !CatalogCacheService(a.Service) || !catalogHash.MatchString(a.Hash) || !catalogTime(a.CheckedAt, now) || len(a.Files) < 1 || len(a.Files) > 10000 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range a.Files {
		if !catalogLabel(f.Path, 4096) || f.Size < 0 || f.ID != 0 || f.Link != "" || seen[f.Path] || strings.ContainsAny(f.Path, "\\?#") || strings.HasPrefix(f.Path, "/") {
			return false
		}
		for _, part := range strings.Split(f.Path, "/") {
			if part == "" || part == "." || part == ".." {
				return false
			}
		}
		for _, r := range f.Path {
			if unicode.IsControl(r) {
				return false
			}
		}
		seen[f.Path] = true
	}
	b, err := json.Marshal(a)
	return err == nil && len(b) <= 512*1024
}

func (s CatalogSupplement) Valid(permissions CatalogEntities, now time.Time) bool {
	if (!permissions.Metadata && len(s.Metadata) > 0) || (!permissions.Availability && len(s.Availability) > 0) || len(s.Metadata)+len(s.Availability) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, m := range s.Metadata {
		key := m.Type + ":" + m.TMDB
		if seen[key] || !m.Valid(now) {
			return false
		}
		seen[key] = true
	}
	for _, a := range s.Availability {
		key := a.Service + ":" + a.Hash
		if seen[key] || !a.Valid(now) {
			return false
		}
		seen[key] = true
	}
	data, err := json.Marshal(s)
	return err == nil && len(data) <= 7<<20
}
