package aiosource

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/text/unicode/norm"
)

type Candidate struct {
	EntryID      string            `json:"entryId"`
	WorkID       string            `json:"workId"`
	Source       string            `json:"source"`
	SourceID     string            `json:"sourceId"`
	Type         string            `json:"mediaType"`
	Titles       []string          `json:"titles"`
	Years        []int             `json:"years"`
	SeasonYears  map[int]int       `json:"seasonYears"`
	EpisodeYears map[string]int    `json:"episodeYears"`
	IDs          map[string]string `json:"externalIds"`
}
type SourceIDs struct {
	TMDB string `json:"tmdb"`
	IMDb string `json:"imdb"`
	TVDB string `json:"tvdb"`
}
type SourceEpisode struct {
	Season  *int `json:"season"`
	Episode *int `json:"episode"`
}
type Decision struct {
	Status, Reason string
	Winner         *Candidate
}
type evidence struct {
	c                         Candidate
	score, titleScore         float64
	exact, year, id, eligible bool
	reason                    string
}

var numberRE = regexp.MustCompile(`\p{N}+`)

func canonicalIMDb(s string) string {
	s = strings.TrimPrefix(strings.ToLower(s), "tt")
	if s != "" {
		return "tt" + s
	}
	return ""
}
func similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	grams := func(s string) map[string]bool {
		r := []rune(s)
		out := map[string]bool{}
		for i := 0; i+2 < len(r); i++ {
			out[string(r[i:i+3])] = true
		}
		return out
	}
	x, y := grams(a), grams(b)
	if len(x)+len(y) == 0 {
		return 0
	}
	n := 0
	for g := range x {
		if y[g] {
			n++
		}
	}
	return 2 * float64(n) / float64(len(x)+len(y))
}
func ChooseMatch(p Release, candidates []Candidate, ids SourceIDs, overflow bool, observed SourceEpisode) Decision {
	p = withSourceNumbering(p, observed)
	if p.InvalidNumbering {
		return Decision{Status: "unmatched", Reason: "invalid or unsupported numbering range"}
	}
	conflict := func(n *int, values []int) bool {
		return n != nil && (*n < 0 || (len(values) > 0 && !contains(values, *n)))
	}
	sc, ec := conflict(observed.Season, p.Seasons), conflict(observed.Episode, p.Episodes)
	es := []evidence{}
	numbers := strings.Join(numberRE.FindAllString(p.Normalized, -1), ",")
	for _, c := range candidates {
		e := evidence{c: c}
		compatible := 0.0
		hasCompatible := false
		exactChar := false
		for _, title := range c.Titles {
			alias := Normalize(title)
			s := similarity(p.Normalized, alias)
			e.titleScore = max(e.titleScore, s)
			if alias == p.Normalized {
				e.exact = true
				if utf8.RuneCountInString(norm.NFC.String(strings.TrimSpace(title))) == 1 && utf8.RuneCountInString(norm.NFC.String(strings.TrimSpace(p.Title))) == 1 {
					exactChar = true
				}
			}
			if strings.Join(numberRE.FindAllString(alias, -1), ",") == numbers {
				hasCompatible = true
				compatible = max(compatible, s)
			}
		}
		e.id = (ids.TMDB != "" && c.Source == "tmdb" && ids.TMDB == c.SourceID) || (ids.IMDb != "" && c.IDs["imdb"] != "" && canonicalIMDb(ids.IMDb) == canonicalIMDb(c.IDs["imdb"])) || (ids.TVDB != "" && ids.TVDB == c.IDs["tvdb"])
		idConflict := (ids.TMDB != "" && c.Source == "tmdb" && ids.TMDB != c.SourceID) || (ids.IMDb != "" && c.IDs["imdb"] != "" && canonicalIMDb(ids.IMDb) != canonicalIMDb(c.IDs["imdb"])) || (ids.TVDB != "" && c.IDs["tvdb"] != "" && ids.TVDB != c.IDs["tvdb"])
		unverified := (ids.TMDB != "" && c.Source != "tmdb") || (ids.IMDb != "" && c.IDs["imdb"] == "") || (ids.TVDB != "" && c.IDs["tvdb"] == "")
		if p.Year != nil {
			e.year = contains(c.Years, *p.Year)
			if c.Type == "tv" {
				for _, season := range p.Seasons {
					if c.SeasonYears[season] == *p.Year {
						e.year = true
					}
				}
				if !p.Pack && len(p.Seasons) == 1 && len(p.Episodes) == 1 {
					key := strconvPair(p.Seasons[0], p.Episodes[0])
					if c.EpisodeYears[key] == *p.Year {
						e.year = true
					}
				}
				if p.Pack && !e.year {
					for key, year := range c.EpisodeYears {
						season, _, valid := strings.Cut(key, ":")
						n, err := strconv.Atoi(season)
						if valid && err == nil && year == *p.Year && contains(p.Seasons, n) {
							e.year = true
							break
						}
					}
				}
			}
		}
		wrongType := (p.Type != "" && p.Type != c.Type) || ((observed.Season != nil || observed.Episode != nil) && c.Type != "tv")
		wrongYear := p.Year != nil && !e.year
		numberConflict := !hasCompatible || (!e.exact && e.titleScore >= .92 && compatible < .92)
		short := utf8.RuneCountInString(strings.ReplaceAll(p.Normalized, " ", "")) < 5
		e.eligible = (utf8.RuneCountInString(p.Normalized) >= 2 || (exactChar && e.year && e.id)) && !sc && !ec && !wrongType && !wrongYear && !numberConflict && !idConflict && !unverified && ((e.exact && (e.year || e.id || (!short && p.Type != ""))) || (utf8.RuneCountInString(p.Normalized) >= 10 && compatible >= .92 && e.year && e.id))
		switch {
		case sc:
			e.reason = "source season conflict"
		case ec:
			e.reason = "source episode conflict"
		case wrongType:
			e.reason = "media type conflict"
		case wrongYear:
			e.reason = "year conflict"
		case numberConflict:
			e.reason = "numbered title conflict"
		case idConflict:
			e.reason = "source ID conflict"
		case unverified:
			e.reason = "source ID unverified"
		case e.eligible && e.exact:
			e.reason = "exact alias with compatible evidence"
		case e.eligible:
			e.reason = "close alias corroborated by year and source ID"
		default:
			e.reason = "insufficient evidence"
		}
		e.score = e.titleScore
		if e.year {
			e.score += .2
		}
		if e.id {
			e.score += .15
		}
		es = append(es, e)
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].score > es[j].score })
	works, exactWorks, identifiedWorks := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var winner, identified *evidence
	for i := range es {
		e := &es[i]
		if e.eligible {
			works[e.c.WorkID] = true
			if winner == nil {
				winner = e
			}
			if e.exact && e.id && (p.Year == nil || e.year) {
				identifiedWorks[e.c.WorkID] = true
				if identified == nil {
					identified = e
				}
			}
		}
		if e.exact && (e.eligible || e.reason == "insufficient evidence" || e.reason == "source ID conflict" || e.reason == "source ID unverified") {
			exactWorks[e.c.WorkID] = true
		}
	}
	if len(identifiedWorks) == 1 {
		winner = identified
	} else {
		identified = nil
	}
	homonyms := len(works) > 1 || len(exactWorks) > 1
	closeCompetitor := false
	if winner != nil {
		for _, e := range es {
			if e.c.WorkID != winner.c.WorkID && (e.reason == "insufficient evidence" || e.reason == "source ID unverified" || (e.reason == "source ID conflict" && !e.exact && identified == nil)) && e.titleScore >= winner.titleScore-.08 && e.year {
				closeCompetitor = true
			}
		}
	}
	if overflow || (homonyms && identified == nil) || closeCompetitor {
		return Decision{Status: "ambiguous", Reason: "multiple plausible works"}
	}
	if winner == nil {
		reason := "no supported correspondence"
		// Explain the best supported rejection without changing eligibility or
		// choosing a work. Unrelated weak candidates are not useful diagnostics.
		if len(es) > 0 && es[0].titleScore >= .5 {
			reason = es[0].reason
		}
		return Decision{Status: "unmatched", Reason: reason}
	}
	reason := winner.reason
	if homonyms {
		reason = "exact alias disambiguated by compatible source ID"
	}
	return Decision{Status: "matched", Reason: reason, Winner: &winner.c}
}
func WorkCandidate(w model.AIOWork) Candidate {
	return Candidate{EntryID: itoa64(w.ID), WorkID: itoa64(w.ID), Source: "tmdb", SourceID: w.TMDB, Type: w.Type, Titles: w.Titles, Years: w.Years, SeasonYears: w.SeasonYears, EpisodeYears: w.EpisodeYears, IDs: map[string]string{"imdb": w.IMDb, "tvdb": w.TVDB}}
}

func (s *SourceEpisode) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for key, dst := range map[string]**int{"season": &s.Season, "episode": &s.Episode} {
		v := raw[key]
		if len(v) == 0 || string(v) == "null" {
			continue
		}
		var n int
		if json.Unmarshal(v, &n) != nil {
			n = -1
		}
		*dst = &n
	}
	return nil
}
