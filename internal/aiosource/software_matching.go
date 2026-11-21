package aiosource

import (
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/software"
	"github.com/theo-bnts/aiosources/internal/store"
)

func ParseSoftwareRelease(r model.AIORecord) software.Release {
	return software.ParseRecord(r)
}

func decideSoftware(r model.AIORecord, p software.Release, find store.AIOCandidateSearch) (model.AIOMatchResult, error) {
	r.Normalized = p.Normalized
	r.IdentificationType = "software"
	result := model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "insufficient software evidence"}

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

	if r.SoftwareInformationPending {
		result.Reason = "software information not ready"
		return result, nil
	}
	if p.InvalidID {
		result.Reason = "conflicting or invalid software identifier"
		return result, nil
	}
	if p.Collection {
		result.Reason = "software bundle requires review"
		return result, nil
	}

	hasStrongID := p.SoftwareKey != "" || p.WinGetID != "" || p.HomebrewCask != "" || p.NVDCPE != ""
	if len([]rune(r.Normalized)) < 2 && !hasStrongID {
		return result, nil
	}

	versions := []string{}
	addVersion := func(v string) {
		if n := Normalize(v); n != "" && !slices.Contains(versions, n) {
			versions = append(versions, n)
		}
	}
	addVersion(r.Normalized)
	for _, variant := range p.Variants {
		addVersion(variant)
	}
	if p.Vendor != "" {
		addVersion(p.Vendor + " " + p.Title)
		if stripped := software.ProductNorm(p.Title, p.Vendor); stripped != "" {
			addVersion(stripped)
		}
	}

	filter := model.AIOCandidateFilter{
		Type:           "software",
		Title:          r.Normalized,
		Titles:         versions,
		SoftwareKey:    p.SoftwareKey,
		WinGetID:       p.WinGetID,
		HomebrewCask:   p.HomebrewCask,
		NVDCPE:         p.NVDCPE,
		SoftwareVendor: p.Vendor,
		ExactOnly:      len([]rune(r.Normalized)) < 5,
	}
	works, overflow, err := find(filter)
	if err != nil {
		return result, err
	}
	if len(works) == 0 {
		return result, nil
	}

	type scored struct {
		work          model.AIOWork
		score         float64
		exact         bool
		idMatch       bool
		vendorMatch   bool
		platformMatch bool
		reason        string
	}

	queryVendorNorm := software.VendorNorm(p.Vendor)
	queryNumbers := extractNumbers(r.Normalized)
	eligible := make([]scored, 0, len(works))

	for _, w := range works {
		if w.Type != "software" {
			continue
		}
		s := scored{work: w}

		switch {
		case p.SoftwareKey != "" && w.SoftwareKey == p.SoftwareKey:
			s.idMatch, s.reason = true, "verified software identifier"
		case p.WinGetID != "" && strings.EqualFold(w.Software.WinGetID, p.WinGetID):
			s.idMatch, s.reason = true, "verified WinGet identifier"
		case p.HomebrewCask != "" && strings.EqualFold(w.Software.HomebrewCask, p.HomebrewCask):
			s.idMatch, s.reason = true, "verified Homebrew Cask token"
		case p.NVDCPE != "" && strings.EqualFold(w.Software.NVDCPE, p.NVDCPE):
			s.idMatch, s.reason = true, "verified NVD CPE identifier"
		}

		workVendorNorm := software.VendorNorm(w.Software.Vendor)
		if queryVendorNorm != "" && workVendorNorm != "" {
			if queryVendorNorm == workVendorNorm || strings.Contains(queryVendorNorm, workVendorNorm) || strings.Contains(workVendorNorm, queryVendorNorm) {
				s.vendorMatch = true
			}
		} else if workVendorNorm != "" && strings.HasPrefix(r.Normalized, workVendorNorm+" ") {
			s.vendorMatch = true
		}

		if len(p.Platforms) > 0 && len(w.Software.Platforms) > 0 {
			for _, hp := range p.Platforms {
				if slices.Contains(w.Software.Platforms, hp) {
					s.platformMatch = true
					break
				}
			}
		}

		// Evaluate title/alias similarity while protecting versioned product numerals
		// (e.g. "Pro-Q 3" must not fuzzy-match "Pro-Q 2").
		for _, candTitle := range append([]string{w.DisplayTitle}, w.Titles...) {
			normCand := Normalize(candTitle)
			if normCand == "" {
				continue
			}
			for _, inputTitle := range versions {
				if inputTitle == normCand {
					s.exact = true
					s.score = 1
					continue
				}
				if queryNumbers != extractNumbers(normCand) {
					continue
				}
				sim := similarity(inputTitle, normCand)
				if sim > s.score {
					s.score = sim
				}
			}
		}

		if s.idMatch {
			s.score = max(s.score, 1.0)
			eligible = append(eligible, s)
			continue
		}

		// Reject when an explicit vendor hint directly contradicts the work's vendor.
		if queryVendorNorm != "" && workVendorNorm != "" && !s.vendorMatch {
			continue
		}

		// Short product names (< 4 runes, e.g. "VLC", "OBS", "Git") require exact
		// alias plus vendor or platform corroboration.
		if len([]rune(r.Normalized)) < 4 {
			if s.exact && (s.vendorMatch || s.platformMatch) {
				s.reason = "exact software title with corroborated metadata"
				eligible = append(eligible, s)
			}
			continue
		}

		if s.exact {
			switch {
			case s.vendorMatch && s.platformMatch:
				s.reason = "exact software title, vendor and platform"
			case s.vendorMatch:
				s.reason = "exact software title and vendor"
			case s.platformMatch:
				s.reason = "exact software title and platform"
			default:
				s.reason = "exact software title"
			}
			eligible = append(eligible, s)
			continue
		}

		if len([]rune(r.Normalized)) >= 5 && s.score >= minimum && (s.vendorMatch || s.platformMatch || len(p.Platforms) == 0) {
			s.reason = "similar software title"
			eligible = append(eligible, s)
		}
	}

	if len(eligible) == 0 {
		if overflow {
			result.Status, result.Reason = "ambiguous", "too many software candidates"
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
		if a.vendorMatch != b.vendorMatch {
			if a.vendorMatch {
				return -1
			}
			return 1
		}
		if a.platformMatch != b.platformMatch {
			if a.platformMatch {
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
		sameTier := best.idMatch == second.idMatch && best.exact == second.exact && best.vendorMatch == second.vendorMatch && best.platformMatch == second.platformMatch
		if sameTier && best.score-second.score < margin {
			result.Status = "ambiguous"
			result.Reason = "multiple matching software entries"
			return result, nil
		}
	}

	winner := best.work
	result.Status = "matched"
	result.Reason = best.reason
	result.Work = &winner
	return result, nil
}
