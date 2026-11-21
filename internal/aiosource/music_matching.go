package aiosource

import (
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
	"github.com/theo-bnts/aiosources/internal/store"
)

func ParseMusicRelease(r model.AIORecord) music.Release {
	return music.ParseRecord(r)
}

func decideMusic(r model.AIORecord, p music.Release, find store.AIOCandidateSearch) (model.AIOMatchResult, error) {
	r.Normalized = p.Normalized
	r.IdentificationType = "music"
	result := model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "insufficient music evidence"}
	policy := r.MatchingPolicy
	if policy == (model.MatchingPolicy{}) {
		policy = model.DefaultMatchingPolicy()
	}
	if !policy.Valid() {
		result.Reason = "invalid identification policy"
		return result, nil
	}
	minimum := max(.92, float64(policy.MinimumSimilarity)/100)
	margin := float64(policy.AmbiguityMargin) / 100
	if r.MusicInformationPending {
		result.Reason = "music information not ready"
		return result, nil
	}
	if p.InvalidID {
		result.Reason = "conflicting or invalid music identifier"
		return result, nil
	}
	if p.Collection {
		result.Reason = "music collection requires review"
		return result, nil
	}

	mbid := p.MBID
	if mbid == "" {
		mbid = p.ReleaseMBID
	}
	hasStrongID := mbid != "" || p.DiscogsID != "" || p.WikidataID != "" || p.Barcode != "" || p.CatalogNumber != ""
	if len([]rune(r.Normalized)) < 2 && !hasStrongID {
		return result, nil
	}

	versions := []string{}
	if r.Normalized != "" {
		versions = append(versions, r.Normalized)
	}
	for _, variant := range p.Variants {
		if n := Normalize(variant); n != "" && !slices.Contains(versions, n) {
			versions = append(versions, n)
		}
	}

	var year *int
	if p.StrippedYear && p.Year > 0 {
		year = &p.Year
	}

	filter := model.AIOCandidateFilter{
		Type:              "music",
		Title:             r.Normalized,
		Titles:            versions,
		MusicBrainz:       mbid,
		DiscogsID:         p.DiscogsID,
		WikidataID:        p.WikidataID,
		Barcode:           p.Barcode,
		CatalogNumber:     p.CatalogNumber,
		MusicArtist:       p.Artist,
		MusicLabel:        p.Label,
		MusicMinimumScore: max(0, minimum-margin),
		Year:              year,
		ExactOnly:         len([]rune(r.Normalized)) < 5,
	}
	works, overflow, err := find(filter)
	if err != nil {
		return result, err
	}
	// If year was stripped from a reissue/remaster whose year wasn't listed on
	// the release group yet, retry with exact artist+title when we have an artist.
	if len(works) == 0 && year != nil && (p.Artist != "" || hasStrongID) {
		retry := filter
		retry.Year = nil
		works, overflow, err = find(retry)
		if err != nil {
			return result, err
		}
	}
	if len(works) == 0 {
		return result, nil
	}

	type scored struct {
		work        model.AIOWork
		score       float64
		exact       bool
		artistMatch bool
		idMatch     bool
		yearMatch   bool
		reason      string
	}

	artistSig := music.ArtistSignature(p.Artist)
	eligible := make([]scored, 0, len(works))
	for _, w := range works {
		s := scored{work: w}
		// Check strong identifier evidence.
		switch {
		case mbid != "" && (w.MusicBrainz == mbid):
			s.idMatch, s.reason = true, "verified MusicBrainz ID"
		case p.DiscogsID != "" && matchesDiscogs(w.Music.DiscogsIDs, p.DiscogsID):
			s.idMatch, s.reason = true, "verified Discogs ID"
		case p.WikidataID != "" && w.Music.WikidataID == p.WikidataID:
			s.idMatch, s.reason = true, "verified Wikidata ID"
		case p.Barcode != "" && slices.Contains(w.Music.Barcodes, p.Barcode):
			s.idMatch, s.reason = true, "verified barcode"
		case p.CatalogNumber != "" && matchesCatalogNumber(w.Music.CatalogNumbers, p.CatalogNumber):
			s.idMatch, s.reason = true, "verified catalog number"
		}

		// Evaluate artist corroboration.
		if artistSig != "" {
			s.artistMatch = matchesMusicArtist(w.Music, artistSig)
		}

		// Evaluate title match against release group title and aliases.
		for _, candTitle := range append([]string{w.DisplayTitle}, w.Titles...) {
			normCand := Normalize(candTitle)
			if normCand == "" {
				continue
			}
			for _, inputTitle := range versions {
				sim := similarity(inputTitle, normCand)
				if sim > s.score {
					s.score = sim
				}
				if inputTitle == normCand {
					s.exact = true
					s.score = 1
				}
			}
		}

		if p.Year > 0 && (w.Music.YearVerified || slices.Contains(w.Years, p.Year)) {
			s.yearMatch = true
		}

		// Eligibility rules:
		// 1. Verified MBID / Discogs / Wikidata / Barcode (or CatNo + artist/title)
		//    is authoritative unless explicitly contradicted by a different artist.
		if s.idMatch {
			if artistSig != "" && !s.artistMatch && p.CatalogNumber != "" && mbid == "" && p.DiscogsID == "" && p.WikidataID == "" && p.Barcode == "" {
				continue
			}
			s.score = max(s.score, 1.0)
			eligible = append(eligible, s)
			continue
		}

		// 2. Without a strong ID, require a strong title match AND artist
		//    corroboration (never guess homonymous album titles blindly).
		if artistSig == "" || !s.artistMatch {
			continue
		}
		if p.StrippedYear && p.Year > 0 && len(w.Years) > 0 && !s.yearMatch && !s.exact {
			continue
		}
		if s.exact || (len([]rune(r.Normalized)) >= 5 && s.score >= minimum) {
			if s.exact && s.yearMatch {
				s.reason = "exact artist, album and year"
			} else if s.exact {
				s.reason = "exact artist and album"
			} else {
				s.reason = "similar album title with artist"
			}
			eligible = append(eligible, s)
		}
	}

	if len(eligible) == 0 {
		if overflow {
			result.Status, result.Reason = "ambiguous", "too many music candidates"
		}
		return result, nil
	}

	slices.SortFunc(eligible, func(a, b scored) int {
		if a.idMatch != b.idMatch {
			if a.idMatch {
				return -1
			}
			return 1
		}
		if a.exact != b.exact {
			if a.exact {
				return -1
			}
			return 1
		}
		if a.yearMatch != b.yearMatch {
			if a.yearMatch {
				return -1
			}
			return 1
		}
		if a.score > b.score {
			return -1
		}
		if a.score < b.score {
			return 1
		}
		if a.work.ID < b.work.ID {
			return -1
		}
		return 1
	})

	best := eligible[0]
	if len(eligible) > 1 {
		second := eligible[1]
		// Two equally strong candidates (e.g. homonymous album/EP/single by the
		// same artist without distinguishing year or ID) require manual review.
		sameTier := best.idMatch == second.idMatch && best.exact == second.exact && best.yearMatch == second.yearMatch
		if sameTier && best.score-second.score < margin {
			result.Status = "ambiguous"
			result.Reason = "multiple matching music releases"
			return result, nil
		}
	}

	winner := best.work
	result.Status = "matched"
	result.Reason = best.reason
	result.Work = &winner
	return result, nil
}

func matchesMusicArtist(info model.MusicInformation, sig string) bool {
	if sig == "" {
		return false
	}
	if music.ArtistSignature(info.ArtistCredit) == sig {
		return true
	}
	for _, a := range info.Artists {
		if music.ArtistSignature(a) == sig {
			return true
		}
	}
	for _, al := range info.ArtistAliases {
		if music.ArtistSignature(al) == sig {
			return true
		}
	}
	return false
}

func matchesDiscogs(stored []string, query string) bool {
	for _, v := range stored {
		if v == query {
			return true
		}
		if _, bare, ok := strings.Cut(v, ":"); ok && bare == query {
			return true
		}
	}
	return false
}

func matchesCatalogNumber(stored []string, normQuery string) bool {
	for _, v := range stored {
		if music.CatalogNumber(v) == normQuery {
			return true
		}
	}
	return false
}
