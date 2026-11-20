package aiosource

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

const minExactCorrespondenceSize int64 = 10_000_000 // 10 MB minimum for size-based video correspondence.

var (
	corrResRE   = regexp.MustCompile(`(?i)\b(2160p|4k|1080[pi]|720p|576p|480p)\b`)
	corrCodecRE = regexp.MustCompile(`(?i)\b(hevc|[hx][ .]?265|[hx][ .]?264|avc|av1|xvid|divx|mpeg[ .-]?2|vc[- .]?1)\b`)
	corrSrcRE   = regexp.MustCompile(`(?i)\b(remux|blu[- .]?ray|bdrip|brrip|bdmv|web[- .]?dl|webrip|dvdrip|hdtv|pdtv|sdtv|hdcam|camrip|cam|telesync|tsrip)\b`)
	corrGroupRE = regexp.MustCompile(`(?i)\b(?:x26[45]|h[ .]?26[45]|hevc|avc|av1|xvid|divx|vc[- .]?1|mpeg[ .-]?[24]|web[- .]?dl|webrip|blu[- .]?ray|bdrip|brrip|dvdrip|hdrip|hdtv|remux|aac(?:[ .]?[1257]\.[01])?|e?ac3(?:[ .]?[1257]\.[01])?|ddp?(?:[ .]?[1257]\.[01])?|dts(?:[- .]?hd)?(?:[- .]?ma)?(?:[ .]?[1257]\.[01])?|dtshdma|truehd|atmos|flac(?:[ .]?[1257]\.[01])?|opus(?:[ .]?[1257]\.[01])?|10bits?|2160p|1080[pi]|720p|576p|480p|[1257]\.1|[12]\.0)-([A-Za-z0-9][A-Za-z0-9_]{1,20})(?:\.(?:mkv|mp4|avi|mov|m4v|ts|m2ts|webm))?$`)
	corrExtRE   = regexp.MustCompile(`(?i)\.(?:mkv|mp4|avi|mov|m4v|ts|m2ts|webm|wmv|flv|mpg|mpeg|iso|vob)$`)
	corrNormRE  = regexp.MustCompile(`[^a-z0-9]+`)
)

var genericReleaseGroups = map[string]bool{
	"notag": true, "no_tag": true, "nogrp": true, "nogroup": true,
	"unknown": true, "none": true, "raw": true, "tba": true, "na": true,
}

// ReleaseSignature captures the strictly normalized structural and native language
// identity of a single torrent occurrence for ultra-conservative cross-source matching.
type ReleaseSignature struct {
	InfoHash             string
	Size                 int64
	SizeRounded          bool
	Resolution           string
	Codec                string
	Source               string
	EpisodeKey           string
	ReleaseGroup         string
	CanonicalReleaseName string
	HasTechnicalMarker   bool
	OwnAudio             []string
	OwnSubtitles         []string
	HasMulti             bool
	ConcreteAudio        []string
}

// IsRoundedReleaseSize detects rounded or coarse display sizes (such as tracker
// sizes rounded to KiB/MiB/GiB/MB/GB with 0–2 decimals) so two unrelated encodes
// sharing a rounded display size are never linked by byte size.
func IsRoundedReleaseSize(size int64) bool {
	if size < minExactCorrespondenceSize {
		return true
	}
	if size%1024 == 0 || size%1_000_000 == 0 {
		return true
	}
	f := float64(size)
	for _, unit := range [...]float64{
		1024 * 1024 * 1024, // GiB
		1024 * 1024,        // MiB
		1_000_000_000,      // GB
		1_000_000,          // MB
	} {
		q := math.Round(f * 100 / unit)
		if q > 0 && math.Abs(f-math.Round(q*unit/100)) <= 1 {
			return true
		}
	}
	return false
}

func ExtractReleaseSignature(rec model.AIORecord) ReleaseSignature {
	title := strings.TrimSpace(rec.Title)
	cleaned := inlineTagsRE.ReplaceAllString(title, " ")
	if strings.Contains(cleaned, " | ") {
		cleaned = trailingAltTitleRE.ReplaceAllString(cleaned, "")
	}
	cleaned = strings.TrimSpace(cleaned)

	sig := ReleaseSignature{
		Size:        rec.Size,
		SizeRounded: IsRoundedReleaseSize(rec.Size),
	}
	if hashRE.MatchString(rec.Hash) {
		sig.InfoHash = strings.ToLower(rec.Hash)
	}
	if m := corrResRE.FindStringSubmatch(cleaned); len(m) == 2 {
		v := strings.ToLower(m[1])
		switch v {
		case "4k":
			v = "2160p"
		case "1080i":
			v = "1080p"
		}
		sig.Resolution = v
	}
	if m := corrCodecRE.FindStringSubmatch(cleaned); len(m) == 2 {
		v := strings.NewReplacer(".", "", " ", "", "-", "").Replace(strings.ToLower(m[1]))
		switch v {
		case "hevc", "h265", "x265":
			v = "h265"
		case "avc", "h264", "x264":
			v = "h264"
		case "xvid", "divx":
			v = "mpeg4"
		}
		sig.Codec = v
	}
	if m := corrSrcRE.FindStringSubmatch(cleaned); len(m) == 2 {
		v := strings.NewReplacer(".", "", " ", "", "-", "").Replace(strings.ToLower(m[1]))
		switch v {
		case "remux", "bluray", "bdrip", "brrip", "bdmv":
			v = "bluray"
		case "webdl", "webrip":
			v = "web"
		case "hdtv", "pdtv", "sdtv":
			v = "hdtv"
		case "cam", "camrip", "hdcam", "telesync", "tsrip":
			v = "cam"
		}
		sig.Source = v
	}
	sig.HasTechnicalMarker = sig.Resolution != "" || sig.Codec != "" || sig.Source != ""

	rel := withSourceNumbering(ParseRelease(cleaned), SourceEpisode{Season: rec.Season, Episode: rec.Episode})
	sig.EpisodeKey = canonicalEpisodeKey(rel)

	if m := corrGroupRE.FindStringSubmatch(cleaned); len(m) == 2 {
		g := strings.ToLower(m[1])
		if !genericReleaseGroups[g] && isTrailingReleaseGroup(m[1]) {
			sig.ReleaseGroup = g
		}
	}

	// Canonical release name strips leading [tag] prefix and container extension,
	// preserving all release tokens (title, year, resolution, codec, group).
	canonBase := leadingBracketPrefixRE.ReplaceAllString(cleaned, "")
	canonBase = corrExtRE.ReplaceAllString(canonBase, "")
	sig.CanonicalReleaseName = corrNormRE.ReplaceAllString(foldLanguageToken(canonBase), "")

	audio, subs := nativeLanguageEvidence(rec)
	for _, item := range audio {
		sig.OwnAudio = append(sig.OwnAudio, item.code)
		if item.code == "multi" {
			sig.HasMulti = true
		} else {
			sig.ConcreteAudio = append(sig.ConcreteAudio, item.code)
		}
	}
	for _, item := range subs {
		sig.OwnSubtitles = append(sig.OwnSubtitles, item.code)
	}
	slices.Sort(sig.OwnAudio)
	slices.Sort(sig.OwnSubtitles)
	slices.Sort(sig.ConcreteAudio)
	return sig
}

func canonicalEpisodeKey(rel Release) string {
	if len(rel.Seasons) == 0 && len(rel.Episodes) == 0 {
		return ""
	}
	seasons := append([]int(nil), rel.Seasons...)
	episodes := append([]int(nil), rel.Episodes...)
	slices.Sort(seasons)
	slices.Sort(episodes)
	var b strings.Builder
	for i, s := range seasons {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "S%02d", s)
	}
	if len(episodes) > 0 {
		b.WriteByte(':')
		for i, e := range episodes {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "E%02d", e)
		}
	} else if rel.Pack {
		b.WriteString(":pack")
	}
	return b.String()
}

// signaturesCompatible enforces all 6 safety invariants for non-hash matching.
func signaturesCompatible(a, b ReleaseSignature) bool {
	if a.Resolution != "" && b.Resolution != "" && a.Resolution != b.Resolution {
		return false
	}
	if a.Codec != "" && b.Codec != "" && a.Codec != b.Codec {
		return false
	}
	if a.Source != "" && b.Source != "" && a.Source != b.Source {
		return false
	}
	if (a.EpisodeKey != "" || b.EpisodeKey != "") && a.EpisodeKey != b.EpisodeKey {
		return false
	}
	if a.ReleaseGroup != "" && b.ReleaseGroup != "" && a.ReleaseGroup != b.ReleaseGroup {
		return false
	}
	// Reject non-multi concrete audio contradiction (e.g. French-only vs English-only).
	if !a.HasMulti && !b.HasMulti && len(a.ConcreteAudio) > 0 && len(b.ConcreteAudio) > 0 {
		if !slicesOverlap(a.ConcreteAudio, b.ConcreteAudio) {
			return false
		}
	}
	return true
}

func slicesOverlap(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func sizesWithinOnePercent(a, b int64) bool {
	if a < minExactCorrespondenceSize || b < minExactCorrespondenceSize {
		return false
	}
	hi := max(a, b)
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff*100 <= hi
}

func correspondenceRulePriority(rule string) int {
	switch rule {
	case model.CorrespondenceRuleInfoHash:
		return 3
	case model.CorrespondenceRuleExactSize:
		return 2
	case model.CorrespondenceRuleReleaseName:
		return 1
	default:
		return 0
	}
}

func correspondenceConfidenceForRule(rule string) string {
	switch rule {
	case model.CorrespondenceRuleInfoHash:
		return model.CorrespondenceConfidenceExact
	case model.CorrespondenceRuleExactSize:
		return model.CorrespondenceConfidenceHigh
	default:
		return model.CorrespondenceConfidenceVerified
	}
}

func memberExclusionKey(provider, sourceID string) string {
	return provider + "\x00" + sourceID
}

// BuildReleaseCorrespondences computes verified cross-source release equivalence
// groups for a single scope ('work' or 'hash'). Every group spans at least 2 distinct
// sources, respects manual exclusions, and rejects ambiguous/conflicting buckets.
func BuildReleaseCorrespondences(scope, scopeKey string, workID *int64, records []model.AIORecord, exclusions map[string]map[string]bool) []model.ComputedReleaseGroup {
	if len(records) < 2 {
		return nil
	}
	// Work on a deterministically ordered copy so group keys and canonical fields are reproducible.
	ordered := append([]model.AIORecord(nil), records...)
	slices.SortFunc(ordered, func(a, b model.AIORecord) int {
		if c := strings.Compare(a.Provider, b.Provider); c != 0 {
			return c
		}
		return strings.Compare(a.SourceID, b.SourceID)
	})
	n := len(ordered)
	sigs := make([]ReleaseSignature, n)
	for i, rec := range ordered {
		sigs[i] = ExtractReleaseSignature(rec)
	}

	parent := make([]int, n)
	memberRule := make([]string, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	componentMembers := func(root int) []int {
		out := []int{}
		for i := 0; i < n; i++ {
			if find(i) == root {
				out = append(out, i)
			}
		}
		return out
	}
	// Union all indices in bucket if compatible across already-existing components.
	unionBucket := func(indices []int, rule string, enforceCompat bool) {
		if enforceCompat {
			// Verify pairwise compatibility across the full union of all components touched by this bucket.
			roots := map[int]bool{}
			for _, idx := range indices {
				roots[find(idx)] = true
			}
			merged := []int{}
			for r := range roots {
				merged = append(merged, componentMembers(r)...)
			}
			for i := 0; i < len(merged); i++ {
				for j := i + 1; j < len(merged); j++ {
					if !signaturesCompatible(sigs[merged[i]], sigs[merged[j]]) {
						return
					}
				}
			}
		}
		base := find(indices[0])
		for _, idx := range indices {
			r := find(idx)
			if r != base {
				parent[r] = base
			}
			if correspondenceRulePriority(rule) > correspondenceRulePriority(memberRule[idx]) {
				memberRule[idx] = rule
			}
		}
	}

	isExcluded := func(matchKey string, idx int) bool {
		if len(exclusions) == 0 {
			return false
		}
		byKey := exclusions[matchKey]
		if len(byKey) == 0 {
			return false
		}
		return byKey[memberExclusionKey(ordered[idx].Provider, ordered[idx].SourceID)]
	}
	hasMultipleProviders := func(indices []int) bool {
		if len(indices) < 2 {
			return false
		}
		first := ordered[indices[0]].Provider
		for _, idx := range indices[1:] {
			if ordered[idx].Provider != first {
				return true
			}
		}
		return false
	}

	// Tier 1: Exact 40-char SHA-1 info_hash across distinct sources.
	byHash := map[string][]int{}
	for i, sig := range sigs {
		if sig.InfoHash != "" {
			byHash[sig.InfoHash] = append(byHash[sig.InfoHash], i)
		}
	}
	hashKeys := make([]string, 0, len(byHash))
	for h := range byHash {
		hashKeys = append(hashKeys, h)
	}
	slices.Sort(hashKeys)
	for _, h := range hashKeys {
		matchKey := "hash:" + h
		if scope == "work" {
			matchKey = "hash:" + scopeKey + ":" + h
		}
		eligible := []int{}
		for _, idx := range byHash[h] {
			if !isExcluded(matchKey, idx) {
				eligible = append(eligible, idx)
			}
		}
		if hasMultipleProviders(eligible) {
			unionBucket(eligible, model.CorrespondenceRuleInfoHash, false)
		}
	}

	if scope == "work" {
		// Tier 2: Same work_id + exact non-rounded byte size >= 10 MB + strict 6-guard compatibility.
		bySize := map[int64][]int{}
		for i, sig := range sigs {
			if !sig.SizeRounded && sig.Size >= minExactCorrespondenceSize {
				bySize[sig.Size] = append(bySize[sig.Size], i)
			}
		}
		sizeKeys := make([]int64, 0, len(bySize))
		for sz := range bySize {
			sizeKeys = append(sizeKeys, sz)
		}
		slices.Sort(sizeKeys)
		for _, sz := range sizeKeys {
			matchKey := "size:" + scopeKey + ":" + strconv.FormatInt(sz, 10)
			eligible := []int{}
			for _, idx := range bySize[sz] {
				if !isExcluded(matchKey, idx) {
					eligible = append(eligible, idx)
				}
			}
			if !hasMultipleProviders(eligible) {
				continue
			}
			// Reject entire bucket on any technical, episode, group or non-multi language conflict.
			clean := true
			for i := 0; i < len(eligible) && clean; i++ {
				for j := i + 1; j < len(eligible); j++ {
					if !signaturesCompatible(sigs[eligible[i]], sigs[eligible[j]]) {
						clean = false
						break
					}
				}
			}
			if clean {
				unionBucket(eligible, model.CorrespondenceRuleExactSize, true)
			}
		}

		// Tier 3: Same work_id + exact canonical release name (>=16 chars with tech marker)
		// + exact same non-generic release group + sizes within +-1% + strict compatibility.
		type nameGroupKey struct {
			name, group string
		}
		byName := map[nameGroupKey][]int{}
		for i, sig := range sigs {
			if len(sig.CanonicalReleaseName) >= 16 && sig.HasTechnicalMarker && sig.ReleaseGroup != "" && sig.Size >= minExactCorrespondenceSize {
				k := nameGroupKey{sig.CanonicalReleaseName, sig.ReleaseGroup}
				byName[k] = append(byName[k], i)
			}
		}
		nameKeys := make([]nameGroupKey, 0, len(byName))
		for k := range byName {
			nameKeys = append(nameKeys, k)
		}
		slices.SortFunc(nameKeys, func(a, b nameGroupKey) int {
			if c := strings.Compare(a.name, b.name); c != 0 {
				return c
			}
			return strings.Compare(a.group, b.group)
		})
		for _, k := range nameKeys {
			sum := sha256.Sum256([]byte(k.name + "|" + k.group))
			matchKey := "name:" + scopeKey + ":" + hex.EncodeToString(sum[:8])
			eligible := []int{}
			for _, idx := range byName[k] {
				if !isExcluded(matchKey, idx) {
					eligible = append(eligible, idx)
				}
			}
			if !hasMultipleProviders(eligible) {
				continue
			}
			clean := true
			for i := 0; i < len(eligible) && clean; i++ {
				for j := i + 1; j < len(eligible); j++ {
					a, b := sigs[eligible[i]], sigs[eligible[j]]
					if !sizesWithinOnePercent(a.Size, b.Size) || !signaturesCompatible(a, b) {
						clean = false
						break
					}
				}
			}
			if clean {
				unionBucket(eligible, model.CorrespondenceRuleReleaseName, true)
			}
		}
	}

	// Collect connected components that actually linked across >= 2 distinct providers.
	clusters := map[int][]int{}
	for i := 0; i < n; i++ {
		if memberRule[i] == "" {
			continue
		}
		r := find(i)
		clusters[r] = append(clusters[r], i)
	}

	out := make([]model.ComputedReleaseGroup, 0, len(clusters))
	for _, indices := range clusters {
		if !hasMultipleProviders(indices) {
			continue
		}
		matchRule, matchKey := resolveClusterIdentity(scope, scopeKey, indices, sigs, memberRule)
		// Apply exclusions keyed by the resolved cluster matchKey as well.
		filtered := make([]int, 0, len(indices))
		for _, idx := range indices {
			if !isExcluded(matchKey, idx) {
				filtered = append(filtered, idx)
			}
		}
		if !hasMultipleProviders(filtered) {
			continue
		}
		out = append(out, buildComputedGroup(scope, scopeKey, workID, matchRule, matchKey, filtered, ordered, sigs, memberRule))
	}

	slices.SortFunc(out, func(a, b model.ComputedReleaseGroup) int {
		return strings.Compare(a.MatchKey, b.MatchKey)
	})
	return out
}

func resolveClusterIdentity(scope, scopeKey string, indices []int, sigs []ReleaseSignature, memberRule []string) (string, string) {
	bestRule := ""
	for _, idx := range indices {
		if correspondenceRulePriority(memberRule[idx]) > correspondenceRulePriority(bestRule) {
			bestRule = memberRule[idx]
		}
	}
	switch bestRule {
	case model.CorrespondenceRuleInfoHash:
		// Pick the lowest shared hash among members with exact_info_hash.
		hashes := []string{}
		for _, idx := range indices {
			if memberRule[idx] == model.CorrespondenceRuleInfoHash && sigs[idx].InfoHash != "" {
				hashes = append(hashes, sigs[idx].InfoHash)
			}
		}
		slices.Sort(hashes)
		if len(hashes) > 0 {
			if scope == "work" {
				return bestRule, "hash:" + scopeKey + ":" + hashes[0]
			}
			return bestRule, "hash:" + hashes[0]
		}
	case model.CorrespondenceRuleExactSize:
		counts := map[int64]int{}
		for _, idx := range indices {
			if !sigs[idx].SizeRounded && sigs[idx].Size >= minExactCorrespondenceSize {
				counts[sigs[idx].Size]++
			}
		}
		var bestSize int64
		bestCount := 0
		for sz, c := range counts {
			if c > bestCount || (c == bestCount && sz > bestSize) {
				bestSize, bestCount = sz, c
			}
		}
		if bestSize > 0 {
			return bestRule, "size:" + scopeKey + ":" + strconv.FormatInt(bestSize, 10)
		}
	}
	// Release name fallback.
	names := []string{}
	for _, idx := range indices {
		if sigs[idx].CanonicalReleaseName != "" {
			names = append(names, sigs[idx].CanonicalReleaseName+"|"+sigs[idx].ReleaseGroup)
		}
	}
	slices.Sort(names)
	keyBase := scopeKey
	if len(names) > 0 {
		keyBase = names[0]
	}
	sum := sha256.Sum256([]byte(keyBase))
	return bestRule, "name:" + scopeKey + ":" + hex.EncodeToString(sum[:8])
}

func buildComputedGroup(
	scope, scopeKey string,
	workID *int64,
	matchRule, matchKey string,
	indices []int,
	ordered []model.AIORecord,
	sigs []ReleaseSignature,
	memberRule []string,
) model.ComputedReleaseGroup {
	providers := map[string]bool{}
	concreteAudioSet := map[string]bool{}
	concreteSubSet := map[string]bool{}
	anyMultiAudio := false
	anyMultiSub := false
	sizeCounts := map[int64]int{}
	var maxSize int64
	bestTitle := ""
	bestTitleScore := -1

	for _, idx := range indices {
		rec := ordered[idx]
		sig := sigs[idx]
		providers[rec.Provider] = true
		if sig.HasMulti {
			anyMultiAudio = true
		}
		for _, code := range sig.ConcreteAudio {
			concreteAudioSet[code] = true
		}
		for _, code := range sig.OwnSubtitles {
			if code == "multi" {
				anyMultiSub = true
			} else {
				concreteSubSet[code] = true
			}
		}
		if sig.Size > maxSize {
			maxSize = sig.Size
		}
		if !sig.SizeRounded && sig.Size > 0 {
			sizeCounts[sig.Size]++
		}
		score := len(sig.ConcreteAudio)*1000 + len(sig.OwnSubtitles)*100 + len(rec.Title)
		if score > bestTitleScore {
			bestTitleScore = score
			bestTitle = rec.Title
		}
	}

	canonicalSize := maxSize
	bestSizeCount := 0
	for sz, c := range sizeCounts {
		if c > bestSizeCount || (c == bestSizeCount && sz > canonicalSize) {
			canonicalSize, bestSizeCount = sz, c
		}
	}

	propagatedAudio := make([]string, 0, len(concreteAudioSet)+1)
	for code := range concreteAudioSet {
		propagatedAudio = append(propagatedAudio, code)
	}
	slices.Sort(propagatedAudio)
	if anyMultiAudio || len(concreteAudioSet) >= 2 {
		propagatedAudio = append(propagatedAudio, "multi")
	}

	propagatedSubs := make([]string, 0, len(concreteSubSet)+1)
	for code := range concreteSubSet {
		propagatedSubs = append(propagatedSubs, code)
	}
	slices.Sort(propagatedSubs)
	if len(propagatedSubs) == 0 && anyMultiSub {
		propagatedSubs = append(propagatedSubs, "multi")
	}

	members := make([]model.ComputedReleaseMember, 0, len(indices))
	enrichedCount := 0
	for _, idx := range indices {
		rec := ordered[idx]
		sig := sigs[idx]
		contributedAudio := []string{}
		for _, code := range propagatedAudio {
			if slices.Contains(sig.OwnAudio, code) {
				continue
			}
			// Do not push generic "multi" onto a torrent that already has concrete audio
			// unless at least 2 concrete languages exist or a peer explicitly carried "multi".
			contributedAudio = append(contributedAudio, code)
		}
		contributedSubs := []string{}
		ownHasConcreteSub := false
		for _, code := range sig.OwnSubtitles {
			if code != "multi" {
				ownHasConcreteSub = true
				break
			}
		}
		for _, code := range propagatedSubs {
			if slices.Contains(sig.OwnSubtitles, code) {
				continue
			}
			if code == "multi" && ownHasConcreteSub {
				continue
			}
			contributedSubs = append(contributedSubs, code)
		}
		gainedMeaningful := false
		for _, code := range contributedAudio {
			if code != "multi" || len(sig.OwnAudio) == 0 {
				gainedMeaningful = true
				break
			}
		}
		if !gainedMeaningful && len(contributedSubs) > 0 {
			gainedMeaningful = true
		}
		if gainedMeaningful {
			enrichedCount++
		}
		ownAudio := append([]string(nil), sig.OwnAudio...)
		if ownAudio == nil {
			ownAudio = []string{}
		}
		ownSubs := append([]string(nil), sig.OwnSubtitles...)
		if ownSubs == nil {
			ownSubs = []string{}
		}
		rule := memberRule[idx]
		if rule == "" {
			rule = matchRule
		}
		members = append(members, model.ComputedReleaseMember{
			ProviderID:           rec.Provider,
			SourceID:             rec.SourceID,
			Fingerprint:          rec.Fingerprint,
			MatchRule:            rule,
			OwnAudio:             ownAudio,
			OwnSubtitles:         ownSubs,
			ContributedAudio:     contributedAudio,
			ContributedSubtitles: contributedSubs,
		})
	}

	return model.ComputedReleaseGroup{
		Scope:               scope,
		ScopeKey:            scopeKey,
		WorkID:              workID,
		MatchRule:           matchRule,
		MatchKey:            matchKey,
		Confidence:          correspondenceConfidenceForRule(matchRule),
		CanonicalSize:       canonicalSize,
		CanonicalTitle:      bestTitle,
		MemberCount:         len(members),
		ProviderCount:       len(providers),
		EnrichedCount:       enrichedCount,
		PropagatedAudio:     propagatedAudio,
		PropagatedSubtitles: propagatedSubs,
		Members:             members,
	}
}
