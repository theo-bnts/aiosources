package music

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/text/language"
)

var (
	mbidRE       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	snapshotRE   = regexp.MustCompile(`^([0-9]{8}-[0-9]{6})/?$`)
	barcodeRE    = regexp.MustCompile(`^[0-9]{8,14}$`)
	wikidataRE   = regexp.MustCompile(`^Q[1-9][0-9]{0,14}$`)
	discogsNum   = regexp.MustCompile(`^[1-9][0-9]{0,14}$`)
	discogsPath  = regexp.MustCompile(`(?i)^/(?:[a-z]{2}/)?(master|release|artist)/([1-9][0-9]{0,14})(?:-|/|$)`)
	wikidataPath = regexp.MustCompile(`^/(?:wiki|entity)/(Q[1-9][0-9]{0,14})(?:/|$)`)
	catNoCleanRE = regexp.MustCompile(`[^a-z0-9]+`)
)

// DefaultPrimaryTypes lists the default MusicBrainz release-group primary types
// imported when an administrator enables music information.
var DefaultPrimaryTypes = []string{"album", "ep", "single"}

// ValidPrimaryTypes lists all MusicBrainz primary release-group types.
var ValidPrimaryTypes = []string{"album", "ep", "single", "broadcast", "other"}

// SpokenSecondaryTypes identifies non-music spoken-word release groups that
// belong to audiobooks/books rather than music by default.
var SpokenSecondaryTypes = []string{"audiobook", "audio drama", "spokenword"}

// MBID extracts and canonicalizes a lowercase MusicBrainz UUID from either a
// bare UUID or a musicbrainz.org entity URL.
func MBID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 256 {
		return ""
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return ""
		}
		host := strings.ToLower(u.Hostname())
		if host != "musicbrainz.org" && !strings.HasSuffix(host, ".musicbrainz.org") {
			return ""
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return ""
		}
		s = parts[1]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if mbidRE.MatchString(s) {
		return s
	}
	return ""
}

// Barcode normalizes an EAN/UPC/GTIN barcode (8-14 digits).
func Barcode(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 32 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r != '-' && r != ' ' {
			return ""
		}
	}
	out := b.String()
	if barcodeRE.MatchString(out) {
		return out
	}
	return ""
}

// CatalogNumber normalizes a record-label catalog number for matching.
func CatalogNumber(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 128 {
		return ""
	}
	norm := catNoCleanRE.ReplaceAllString(strings.ToLower(s), "")
	if len(norm) < 2 || len(norm) > 64 {
		return ""
	}
	hasDigit := false
	for i := 0; i < len(norm); i++ {
		if norm[i] >= '0' && norm[i] <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return ""
	}
	return norm
}

// DiscogsURL extracts ("master"|"release"|"artist", numericID) from a Discogs
// URL, discarding unrelated URLs immediately.
func DiscogsURL(raw string) (kind, id string) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 1024 {
		return "", ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "discogs.com" && !strings.HasSuffix(host, ".discogs.com") {
		return "", ""
	}
	m := discogsPath.FindStringSubmatch(u.Path)
	if len(m) != 3 {
		return "", ""
	}
	return strings.ToLower(m[1]), m[2]
}

// DiscogsID canonicalizes a Discogs identifier supplied either as a bare number
// (optionally prefixed with m/r or master:/release:) or as a discogs.com URL.
func DiscogsID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 256 {
		return ""
	}
	if strings.Contains(s, "://") {
		kind, id := DiscogsURL(s)
		if kind == "" || id == "" {
			return ""
		}
		return kind + ":" + id
	}
	low := strings.ToLower(s)
	for _, prefix := range []string{"master:", "release:", "artist:"} {
		if id, ok := strings.CutPrefix(low, prefix); ok && discogsNum.MatchString(id) {
			return prefix + id
		}
	}
	if strings.HasPrefix(low, "m") && discogsNum.MatchString(low[1:]) {
		return "master:" + low[1:]
	}
	if strings.HasPrefix(low, "r") && discogsNum.MatchString(low[1:]) {
		return "release:" + low[1:]
	}
	if discogsNum.MatchString(low) {
		return low
	}
	return ""
}

// WikidataURL extracts a canonical Q-identifier from a Wikidata URL.
func WikidataURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 1024 {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "wikidata.org" && !strings.HasSuffix(host, ".wikidata.org") {
		return ""
	}
	m := wikidataPath.FindStringSubmatch(u.Path)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

// WikidataID canonicalizes a bare Wikidata Q-identifier or URL.
func WikidataID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 256 {
		return ""
	}
	if strings.Contains(s, "://") {
		return WikidataURL(s)
	}
	up := strings.ToUpper(s)
	if wikidataRE.MatchString(up) {
		return up
	}
	return ""
}

// ExportSnapshot validates a MusicBrainz fullexport directory name (e.g.
// "20261114-002230") and returns its canonical name without trailing slash.
func ExportSnapshot(raw string) (string, bool) {
	m := snapshotRE.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) != 2 {
		return "", false
	}
	if _, err := time.Parse("20060102-150405", m[1]); err != nil {
		return "", false
	}
	return m[1], true
}

// ExportDate parses the UTC timestamp encoded in a MusicBrainz snapshot name.
func ExportDate(snapshot string) (time.Time, bool) {
	name, ok := ExportSnapshot(snapshot)
	if !ok {
		return time.Time{}, false
	}
	at, err := time.Parse("20060102-150405", name)
	if err != nil {
		return time.Time{}, false
	}
	return at.UTC(), true
}

// PrimaryType normalizes a MusicBrainz release-group primary type name.
func PrimaryType(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if slices.Contains(ValidPrimaryTypes, v) {
		return v
	}
	return ""
}

// ImportTypes validates and canonicalizes selected release-group primary types.
func ImportTypes(values []string) ([]string, bool) {
	if len(values) == 0 || len(values) > len(ValidPrimaryTypes) {
		return nil, false
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		t := PrimaryType(value)
		if t == "" {
			return nil, false
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out, true
}

// ImportLanguages validates ISO 639-1/639-3 language codes consistently with
// Open Library language filtering.
func ImportLanguages(values []string) ([]string, bool) {
	if len(values) > 30 {
		return nil, false
	}
	out := []string{}
	for _, value := range values {
		if value == "" || len(value) > 3 {
			return nil, false
		}
		tag, err := language.Parse(value)
		if err != nil || tag == language.Und {
			return nil, false
		}
		base, _ := tag.Base()
		code := base.String()
		if code == "und" || (len(code) != 2 && len(code) != 3) {
			return nil, false
		}
		if !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	slices.Sort(out)
	return out, true
}

// NormalizeLanguage converts a MusicBrainz ISO 639-1 or ISO 639-3 code into
// the canonical base language code used by catalog filters.
func NormalizeLanguage(iso1, iso3 string) string {
	for _, candidate := range []string{strings.TrimSpace(iso1), strings.TrimSpace(iso3)} {
		if candidate == "" || candidate == "und" || candidate == "zxx" || candidate == "mul" {
			continue
		}
		if tag, err := language.Parse(candidate); err == nil && tag != language.Und {
			base, _ := tag.Base()
			code := base.String()
			if code != "und" && (len(code) == 2 || len(code) == 3) {
				return code
			}
		}
	}
	return ""
}

// Normalize delegates to the shared catalog title normalizer.
func Normalize(s string) string {
	return model.NormalizeTitle(s)
}

// ArtistSignature normalizes an artist name into sorted distinct tokens so
// "Beatles, The" and "The Beatles" or reordered collaborations match reliably.
func ArtistSignature(s string) string {
	words := strings.Fields(Normalize(s))
	if len(words) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(words))
	for _, w := range words {
		if w == "the" || w == "and" || w == "feat" || w == "ft" || w == "featuring" || w == "vs" || w == "with" {
			continue
		}
		if !slices.Contains(filtered, w) {
			filtered = append(filtered, w)
		}
	}
	if len(filtered) == 0 {
		filtered = words
	}
	slices.Sort(filtered)
	return strings.Join(filtered, " ")
}

// TitleScore computes normalized title similarity for album matching.
func TitleScore(a, b string) float64 {
	return model.TitleSimilarity(Normalize(a), Normalize(b))
}

func parseYear(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1850 || n > 2200 {
		return 0
	}
	return n
}
