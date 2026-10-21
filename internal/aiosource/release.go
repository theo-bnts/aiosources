// Package aiosource adds local metadata correspondence and Stremio playback to Ingest.
package aiosource

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/text/unicode/norm"
)

type Resource struct {
	Kind, ID, Type  string
	Season, Episode *int
}

var resourceRE = regexp.MustCompile(`(?i)^(tt|imdb|tmdb|tvdb|mal|kitsu|anilist)[:-]?(\d+)(?::(\d+))?(?::(\d+))?$`)

func ParseID(id, mediaType string) (Resource, error) {
	r := Resource{Type: mediaType}
	m := resourceRE.FindStringSubmatch(strings.TrimSpace(id))
	if len(m) == 0 || (mediaType != "movie" && mediaType != "series" && mediaType != "anime") {
		return r, errors.New("unsupported resource")
	}
	r.Kind = strings.ToLower(m[1])
	if r.Kind == "tt" {
		r.Kind = "imdb"
	}
	r.ID = m[2]
	n, err := strconv.ParseUint(r.ID, 10, 53)
	if err != nil || n == 0 {
		return r, errors.New("invalid ID")
	}
	if r.Kind == "imdb" {
		r.ID = "tt" + r.ID
	} else {
		r.ID = strconv.FormatUint(n, 10)
	}
	idx := func(s string) (*int, error) {
		if s == "" {
			return nil, nil
		}
		n, e := strconv.ParseUint(s, 10, 31)
		v := int(n)
		return &v, e
	}
	if r.Kind == "mal" || r.Kind == "kitsu" || r.Kind == "anilist" {
		if m[4] != "" {
			return r, errors.New("invalid absolute episode")
		}
		r.Episode, err = idx(m[3])
	} else {
		if (m[3] == "") != (m[4] == "") {
			return r, errors.New("season and episode required together")
		}
		r.Season, err = idx(m[3])
		if err == nil {
			r.Episode, err = idx(m[4])
		}
	}
	if mediaType == "movie" {
		r.Season = nil
		r.Episode = nil
	}
	return r, err
}
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

type Release struct {
	Title, Normalized string
	Year              *int
	Type              string
	Seasons, Episodes []int
	Pack              bool
	InvalidNumbering  bool
}

var seasonRE = regexp.MustCompile(`(?i)\bS(\d{1,3})(?:\s*[-–]\s*S?(\d{1,3}))?`)

const episodeMarker = `(?:episode\s*|ep\s*|E)`

var episodeRE = regexp.MustCompile(`(?i)` + episodeMarker + `(\d{1,4})(?:\s*[-–]\s*` + episodeMarker + `?(\d{1,4}))?`)
var episodePrefixRE = regexp.MustCompile(`(?i)\bS\d{1,3}(?:` + episodeMarker + `\d{1,4})*$`)
var crossRE = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
var seasonWordRE = regexp.MustCompile(`(?i)\b(?:season|saison)\s*(\d{1,3})(?:\s*[-–]\s*(?:season|saison)?\s*(\d{1,3}))?`)
var absoluteRE = regexp.MustCompile(`(?i)(?:\s-\s|\b(?:ep|episode)\s*)(\d{1,4})(?:\s*[-–]\s*(\d{1,4}))?\b`)
var ordinalSeasonRE = regexp.MustCompile(`(?i)\b(\d{1,3})(?:st|nd|rd|th)\s+season\b`)
var separatedSeasonEpisodeRE = regexp.MustCompile(`(?i)\b(?:S|season\s*|saison\s*)(\d{1,3})\s+[-–]\s+(\d{1,4})(?:\s*[-–]\s*(\d{1,4}))?\b`)
var localEpisodeRE = regexp.MustCompile(`(?i)\bS\d{1,3}\s*E\d{1,4}(?:(?:\s*[-–]\s*E?|\s*E)\d{1,4})*`)
var packMarkerRE = regexp.MustCompile(`(?i)\b(?:complete|integrale|intégrale|pack)\b`)
var yearRE = regexp.MustCompile(`\b(?:18|19|20|21)\d{2}\b`)
var technicalRE = regexp.MustCompile(`(?i)\b(?:2160p|1080[pi]|720p|576p|480p|4k|web[ ._-]?(?:dl|rip)?|bluray|blu[ ._-]?ray|bdrip|brrip|dvdrip|hdtv|remux|x26[45]|h[ .]?26[45]|hevc|avc|av1|dts(?:[ ._-]?hd)?|aac|e?ac3|ddp|flac|xvid|divx|french|vff|vfq|vostfr|multi|truefrench|english|complete|integrale|intégrale)\b`)
var leadingGroupRE = regexp.MustCompile(`^\s*\[[^\]]+\]\s*`)

func ints(matches [][]string) []int {
	out := []int{}
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		end := n
		if len(m) > 2 && m[2] != "" {
			end, _ = strconv.Atoi(m[2])
		}
		if end < n || end-n > 100 {
			continue
		}
		for i := n; i <= end; i++ {
			if !contains(out, i) {
				out = append(out, i)
			}
		}
	}
	return out
}
func contains(a []int, n int) bool {
	for _, v := range a {
		if v == n {
			return true
		}
	}
	return false
}

// A trailing e followed by digits inside a word is part of that title. Episode
// markers still support S01E02E03 as well as separated and ranged numbering.
func parsedEpisodes(s string) ([]int, bool) {
	matches := [][]string{}
	for _, at := range episodeRE.FindAllStringSubmatchIndex(s, -1) {
		if at[0] > 0 {
			before, _ := utf8.DecodeLastRuneInString(s[:at[0]])
			if (unicode.IsLetter(before) || unicode.IsNumber(before) || before == '_') && !episodePrefixRE.MatchString(s[:at[0]]) {
				continue
			}
		}
		match := []string{s[at[0]:at[1]], s[at[2]:at[3]], ""}
		if at[4] >= 0 {
			match[2] = s[at[4]:at[5]]
		}
		matches = append(matches, match)
	}
	return ints(matches), invalidRanges(matches)
}

func invalidRanges(matches [][]string) bool {
	for _, m := range matches {
		if len(m) < 3 || m[2] == "" {
			continue
		}
		first, _ := strconv.Atoi(m[1])
		last, _ := strconv.Atoi(m[2])
		if last < first || last-first > 100 {
			return true
		}
	}
	return false
}

// Structured source numbering supplies missing scope; conflicting title
// numbering is retained so the chooser can reject it rather than overwrite it.
func withSourceNumbering(p Release, observed SourceEpisode) Release {
	if len(p.Seasons) == 0 && observed.Season != nil && *observed.Season >= 0 {
		p.Seasons = []int{*observed.Season}
	}
	if len(p.Episodes) == 0 && !p.Pack && observed.Episode != nil && *observed.Episode >= 0 {
		p.Episodes = []int{*observed.Episode}
	}
	if p.Type == "" && (len(p.Seasons) > 0 || len(p.Episodes) > 0) {
		p.Type = "tv"
	}
	if len(p.Seasons) > 0 && len(p.Episodes) == 0 {
		p.Pack = true
	}
	return p
}

func ParseRelease(raw string) Release {
	// Strip the leading release group before interpreting years and seasons.
	// Trimming '[' first made this branch unreachable and polluted the title.
	if at := leadingGroupRE.FindStringIndex(raw); at != nil && at[1] < len(raw) {
		raw = raw[at[1]:]
	}
	s := strings.ReplaceAll(raw, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = ordinalSeasonRE.ReplaceAllString(s, "Season $1")
	// Without an explicit pack marker, "Season 3 - 09" denotes an episode.
	// Explicit season ranges retain their original pack semantics.
	s = separatedSeasonEpisodeRE.ReplaceAllStringFunc(s, func(text string) string {
		m := separatedSeasonEpisodeRE.FindStringSubmatch(text)
		n, _ := strconv.Atoi(m[2])
		if n >= 1800 && n <= 2199 {
			return "S" + m[1] + " " + m[2]
		}
		if m[3] == "" && packMarkerRE.MatchString(s) {
			return "S" + m[1] + "-S" + m[2]
		}
		value := "S" + m[1] + "E" + m[2]
		if m[3] != "" {
			value += "-E" + m[3]
		}
		return value
	})
	seasons := seasonRE.FindAllStringSubmatch(s, -1)
	episodeText := s
	if local := localEpisodeRE.FindAllString(s, -1); len(local) > 0 {
		// A second absolute annotation (S02E03 EP1158) must not turn a
		// relative episode into a second, unrelated playback candidate.
		episodeText = strings.Join(local, " ")
	}
	episodes, invalidEpisodes := parsedEpisodes(episodeText)
	p := Release{Seasons: ints(seasons), Episodes: episodes, InvalidNumbering: invalidRanges(seasons) || invalidEpisodes}
	if len(p.Seasons) == 0 {
		wordSeasons := seasonWordRE.FindAllStringSubmatch(s, -1)
		p.Seasons = ints(wordSeasons)
		p.InvalidNumbering = p.InvalidNumbering || invalidRanges(wordSeasons)
	}
	if c := crossRE.FindStringSubmatch(s); len(c) > 0 {
		a, _ := strconv.Atoi(c[1])
		b, _ := strconv.Atoi(c[2])
		p.Seasons = []int{a}
		p.Episodes = []int{b}
	}
	if len(p.Episodes) == 0 && len(p.Seasons) == 0 {
		absolute := [][]string{}
		for _, m := range absoluteRE.FindAllStringSubmatch(s, -1) {
			n, _ := strconv.Atoi(m[1])
			if n >= 1800 && n <= 2199 && strings.HasPrefix(strings.TrimSpace(m[0]), "-") {
				continue
			}
			absolute = append(absolute, m)
		}
		p.Episodes = ints(absolute)
		p.InvalidNumbering = p.InvalidNumbering || invalidRanges(absolute)
	}
	if len(p.Seasons) > 0 || len(p.Episodes) > 0 {
		p.Type = "tv"
	}
	p.Pack = len(p.Seasons) > 0 && len(p.Episodes) == 0
	cut := len(s)
	// A numeric movie title (1917) must not consume the actual release year.
	years := yearRE.FindAllStringIndex(s, -1)
	for _, at := range years {
		if at[0] == 0 && len(years) > 1 {
			continue
		}
		if at[0] > 0 {
			n, _ := strconv.Atoi(s[at[0]:at[1]])
			p.Year = &n
			if at[0] < cut {
				cut = at[0]
			}
			break
		}
	}
	for _, re := range []*regexp.Regexp{seasonRE, seasonWordRE, crossRE, technicalRE, absoluteRE} {
		if at := re.FindStringIndex(s); at != nil && at[0] < cut {
			cut = at[0]
		}
	}
	title := strings.Trim(s[:cut], " []()-")
	p.Title = norm.NFC.String(title)
	p.Normalized = Normalize(title)
	return p
}
func MatchesEpisode(rec model.AIORecord, p Release, r Resource) bool {
	if rec.ReviewedEpisode {
		p.Seasons, p.Episodes = nil, nil
		p.Pack = true
		p.InvalidNumbering = false
	}
	if r.Type == "movie" || r.Episode == nil {
		return true
	}
	if p.InvalidNumbering {
		return false
	}
	if r.Season != nil {
		if len(p.Seasons) > 0 && !contains(p.Seasons, *r.Season) {
			return false
		}
		if rec.Season != nil {
			packAnchor := p.Pack && len(p.Seasons) > 1 && contains(p.Seasons, *rec.Season)
			if (!packAnchor && *rec.Season != *r.Season) || (len(p.Seasons) > 0 && !contains(p.Seasons, *rec.Season)) {
				return false
			}
		}
	}
	if len(p.Episodes) > 0 && !contains(p.Episodes, *r.Episode) {
		return false
	}
	if rec.Episode != nil {
		return *rec.Episode == *r.Episode
	}
	// Listing eligibility rejects contradictory evidence. Missing numbering
	// remains eligible so provider files can establish the requested episode;
	// playback still requires exactly one matching video through SelectFile.
	return contains(p.Episodes, *r.Episode) || p.Pack || len(p.Episodes) == 0
}

var videoRE = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|mov|wmv|webm|m4v|mpg|mpeg|m2ts|ts|ogm)$`)
var sampleRE = regexp.MustCompile(`(?i)(?:^|[/ ._-])sample(?:[ ._-]|$)`)
var numberedFileRE = regexp.MustCompile(`^([0-9]{1,3})(?:[ ._-].*)?\.[^.]+$`)

func fileNumbering(path string) Release {
	path = strings.ReplaceAll(path, `\`, `/`)
	p := ParseRelease(filepath.Base(path))
	if len(p.Episodes) == 0 {
		// A bare number is usable only in an immediate, single-season folder.
		// An unscoped file, extras directory or multi-season folder is ambiguous.
		parent := ParseRelease(filepath.Base(filepath.Dir(path)))
		if parent.InvalidNumbering {
			p.InvalidNumbering = true
			return p
		}
		if m := numberedFileRE.FindStringSubmatch(filepath.Base(path)); len(m) > 0 && len(parent.Seasons) == 1 {
			n, _ := strconv.Atoi(m[1])
			p.Seasons, p.Episodes = parent.Seasons, []int{n}
		}
	}
	if len(p.Seasons) == 0 {
		for parent := filepath.Dir(path); parent != "." && parent != "/"; parent = filepath.Dir(parent) {
			scope := ParseRelease(filepath.Base(parent))
			if scope.InvalidNumbering {
				p.InvalidNumbering = true
				break
			}
			if len(scope.Seasons) > 0 {
				if len(scope.Seasons) == 1 {
					p.Seasons = scope.Seasons
				}
				break
			}
		}
	}
	return p
}

func SelectFile(files []model.AIOFile, r Resource) (model.AIOFile, bool) {
	var selected model.AIOFile
	count := 0
	for _, f := range files {
		if !videoRE.MatchString(f.Path) || sampleRE.MatchString(strings.ReplaceAll(f.Path, `\`, `/`)) {
			continue
		}
		if r.Type == "movie" || r.Episode == nil {
			if count == 0 || f.Size > selected.Size {
				selected = f
			}
			count++
			continue
		}
		p := fileNumbering(f.Path)
		if p.InvalidNumbering {
			continue
		}
		if !contains(p.Episodes, *r.Episode) {
			continue
		}
		if r.Season != nil {
			if !contains(p.Seasons, *r.Season) {
				continue
			}
		}
		selected = f
		count++
	}
	return selected, count > 0 && (r.Type == "movie" || r.Episode == nil || count == 1)
}
