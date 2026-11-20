package aiosource

import (
	"slices"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestIsRoundedReleaseSize(t *testing.T) {
	rounded := []int64{
		0,
		5_000_000,          // < 10 MB
		1024 * 1024 * 1024, // 1.00 GiB exact
		4_509_715_661,      // round(4.20 * 1024^3)
		1_500_000_000,      // 1.50 GB exact
		734_003_200,        // 700.00 MiB exact
		100 * 1024 * 1024,  // 100 MiB
		1_234_567_168,      // divisible by 1024
		int64(4.25 * 1073741824),
	}
	for _, sz := range rounded {
		if !IsRoundedReleaseSize(sz) {
			t.Errorf("IsRoundedReleaseSize(%d) = false, want true", sz)
		}
	}

	exact := []int64{
		4_512_345_679,
		12_847_291_357,
		1_483_921_773,
		28_914_662_103,
	}
	for _, sz := range exact {
		if IsRoundedReleaseSize(sz) {
			t.Errorf("IsRoundedReleaseSize(%d) = true, want false", sz)
		}
	}
}

func TestBuildReleaseCorrespondencesTiersAndPropagation(t *testing.T) {
	workID := int64(42)

	t.Run("tier 1 exact info_hash propagates audio and subtitles", func(t *testing.T) {
		hash := strings.Repeat("a", 40)
		records := []model.AIORecord{
			{
				Provider: "source_a", SourceID: "1", Fingerprint: "fp1",
				Title: "Example.Film.2024.1080p.WEB-DL.H264-GRP", Hash: hash, Size: 0,
			},
			{
				Provider: "source_b", SourceID: "2", Fingerprint: "fp2",
				Title: "Example.Film.2024.MULTI.VFF.1080p.WEB-DL.H264-GRP", Hash: hash, Size: 0,
				Languages:         []string{"MULTI", "Français (VFF/TrueFrench)", "ANGLAIS"},
				SubtitleLanguages: []string{"fr", "en"},
			},
		}
		groups := BuildReleaseCorrespondences("work", "42", &workID, records, nil)
		if len(groups) != 1 {
			t.Fatalf("got %d groups, want 1", len(groups))
		}
		g := groups[0]
		if g.MatchRule != model.CorrespondenceRuleInfoHash || g.Confidence != model.CorrespondenceConfidenceExact {
			t.Fatalf("got rule=%q confidence=%q", g.MatchRule, g.Confidence)
		}
		if g.EnrichedCount != 1 {
			t.Fatalf("enriched = %d, want 1", g.EnrichedCount)
		}
		if !slices.Equal(g.PropagatedAudio, []string{"en", "fr", "multi"}) {
			t.Fatalf("propagated audio = %v, want [en fr multi]", g.PropagatedAudio)
		}
		if !slices.Equal(g.PropagatedSubtitles, []string{"en", "fr"}) {
			t.Fatalf("propagated subtitles = %v, want [en fr]", g.PropagatedSubtitles)
		}
		var mA model.ComputedReleaseMember
		for _, m := range g.Members {
			if m.ProviderID == "source_a" {
				mA = m
			}
		}
		if !slices.Equal(mA.ContributedAudio, []string{"en", "fr", "multi"}) {
			t.Fatalf("source_a contributed audio = %v", mA.ContributedAudio)
		}
		if !slices.Equal(mA.ContributedSubtitles, []string{"en", "fr"}) {
			t.Fatalf("source_a contributed subs = %v", mA.ContributedSubtitles)
		}
	})

	t.Run("tier 2 exact non-rounded byte size enriches multi-blind and unknown releases", func(t *testing.T) {
		exactSize := int64(8_431_912_347)
		records := []model.AIORecord{
			{
				Provider: "source_a", SourceID: "10", Fingerprint: "fp10",
				Title: "Example.Movie.2023.MULTI.1080p.BluRay.x264-EXGRP", Hash: strings.Repeat("1", 40), Size: exactSize,
			},
			{
				Provider: "source_b", SourceID: "20", Fingerprint: "fp20",
				Title: "Example.Movie.2023.MULTI.VFF.1080p.BluRay.x264-EXGRP", Hash: strings.Repeat("2", 40), Size: exactSize,
				Languages:         []string{"Français", "Anglais", "Allemand"},
				SubtitleLanguages: []string{"Français"},
			},
			{
				Provider: "source_c", SourceID: "30", Fingerprint: "fp30",
				Title: "Example Movie (2023) 1080p BluRay x264", Hash: strings.Repeat("3", 40), Size: exactSize,
			},
		}
		groups := BuildReleaseCorrespondences("work", "42", &workID, records, nil)
		if len(groups) != 1 {
			t.Fatalf("got %d groups, want 1", len(groups))
		}
		g := groups[0]
		if g.MatchRule != model.CorrespondenceRuleExactSize || g.Confidence != model.CorrespondenceConfidenceHigh {
			t.Fatalf("got rule=%q confidence=%q", g.MatchRule, g.Confidence)
		}
		if g.MemberCount != 3 || g.ProviderCount != 3 || g.EnrichedCount != 2 {
			t.Fatalf("counts members=%d providers=%d enriched=%d", g.MemberCount, g.ProviderCount, g.EnrichedCount)
		}
		if !slices.Equal(g.PropagatedAudio, []string{"de", "en", "fr", "multi"}) {
			t.Fatalf("propagated audio = %v", g.PropagatedAudio)
		}
	})

	t.Run("tier 3 exact release name with shared group matches rounded or nfo-padded size within 1pct", func(t *testing.T) {
		records := []model.AIORecord{
			{
				Provider: "source_a", SourceID: "1", Fingerprint: "fp1",
				Title: "Example.Film.2024.1080p.WEB-DL.DDP5.1.H264-EXGRP", Size: 4_509_715_661, // rounded 4.20 GiB
			},
			{
				Provider: "source_b", SourceID: "2", Fingerprint: "fp2",
				Title: "Example.Film.2024.1080p.WEB-DL.DDP5.1.H264-EXGRP.mkv", Size: 4_511_234_567, // exact bytes (+0.03%)
				Languages: []string{"Español (Latinoamérica)", "Inglés"},
			},
		}
		groups := BuildReleaseCorrespondences("work", "42", &workID, records, nil)
		if len(groups) != 1 {
			t.Fatalf("got %d groups, want 1", len(groups))
		}
		if groups[0].MatchRule != model.CorrespondenceRuleReleaseName || groups[0].Confidence != model.CorrespondenceConfidenceVerified {
			t.Fatalf("got rule=%q confidence=%q", groups[0].MatchRule, groups[0].Confidence)
		}
		if !slices.Equal(groups[0].PropagatedAudio, []string{"en", "es", "multi"}) {
			t.Fatalf("propagated audio = %v", groups[0].PropagatedAudio)
		}
	})
}

func TestBuildReleaseCorrespondencesRejectsUnsafeMatches(t *testing.T) {
	workID := int64(99)
	exactSize := int64(6_321_987_653)

	tests := []struct {
		name    string
		records []model.AIORecord
	}{
		{
			name: "rounded size without identical release name",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.WEB-DL.H264-GRP1", Size: 4_509_715_661},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.FRENCH.1080p.WEB-DL.H264-GRP2", Size: 4_509_715_661},
			},
		},
		{
			name: "resolution conflict on same exact size",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.WEB-DL.x264", Size: exactSize},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.720p.WEB-DL.x264", Size: exactSize, Languages: []string{"fr"}},
			},
		},
		{
			name: "codec conflict on same exact size",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.WEB-DL.x264", Size: exactSize},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.1080p.WEB-DL.x265", Size: exactSize, Languages: []string{"fr"}},
			},
		},
		{
			name: "source class conflict bluray vs web",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.BluRay.x264", Size: exactSize},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.1080p.WEB-DL.x264", Size: exactSize, Languages: []string{"fr"}},
			},
		},
		{
			name: "episode mismatch S01E01 vs S01E02",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Show.S01E01.1080p.WEB-DL.x264", Size: exactSize},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Show.S01E02.1080p.WEB-DL.x264", Size: exactSize, Languages: []string{"fr"}},
			},
		},
		{
			name: "pack vs single episode mismatch",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Show.S01.COMPLETE.1080p.WEB-DL.x264", Size: exactSize},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Show.S01E01.1080p.WEB-DL.x264", Size: exactSize, Languages: []string{"fr"}},
			},
		},
		{
			name: "non-generic release group conflict",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.WEB-DL.H264-FLUX", Size: exactSize},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.1080p.WEB-DL.H264-NTb", Size: exactSize, Languages: []string{"fr"}},
			},
		},
		{
			name: "non-multi concrete audio contradiction french vs german",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.FRENCH.1080p.WEB-DL.H264", Size: exactSize},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.GERMAN.1080p.WEB-DL.H264", Size: exactSize},
			},
		},
		{
			name: "tier 3 rejects generic NOTAG release group",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.WEB-DL.H264-NOTAG", Size: 4_509_715_661},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.1080p.WEB-DL.H264-NOTAG", Size: 4_511_234_567, Languages: []string{"fr"}},
			},
		},
		{
			name: "tier 3 rejects size difference greater than 1pct",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.WEB-DL.H264-FLUX", Size: 4_500_000_000},
				{Provider: "b", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.1080p.WEB-DL.H264-FLUX", Size: 4_700_000_000, Languages: []string{"fr"}},
			},
		},
		{
			name: "single source duplicates do not form cross-source group",
			records: []model.AIORecord{
				{Provider: "a", SourceID: "1", Fingerprint: "f1", Title: "Example.Film.2024.1080p.WEB-DL.H264-FLUX", Size: exactSize},
				{Provider: "a", SourceID: "2", Fingerprint: "f2", Title: "Example.Film.2024.FRENCH.1080p.WEB-DL.H264-FLUX", Size: exactSize},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			groups := BuildReleaseCorrespondences("work", "99", &workID, tc.records, nil)
			if len(groups) != 0 {
				t.Fatalf("expected 0 groups for unsafe case %q, got %+v", tc.name, groups)
			}
		})
	}
}

func TestBuildReleaseCorrespondencesRespectsManualExclusions(t *testing.T) {
	workID := int64(77)
	exactSize := int64(7_123_456_789)
	records := []model.AIORecord{
		{Provider: "source_a", SourceID: "1", Fingerprint: "f1", Title: "Example.2024.1080p.BluRay.x264-GRP", Size: exactSize},
		{Provider: "source_b", SourceID: "2", Fingerprint: "f2", Title: "Example.2024.FRENCH.1080p.BluRay.x264-GRP", Size: exactSize},
		{Provider: "source_c", SourceID: "3", Fingerprint: "f3", Title: "Example.2024.MULTI.VFF.1080p.BluRay.x264-GRP", Size: exactSize, Languages: []string{"fr", "en"}},
	}
	groups := BuildReleaseCorrespondences("work", "77", &workID, records, nil)
	if len(groups) != 1 || groups[0].MemberCount != 3 {
		t.Fatalf("initial groups = %+v", groups)
	}
	matchKey := groups[0].MatchKey
	exclusions := map[string]map[string]bool{
		matchKey: {memberExclusionKey("source_a", "1"): true},
	}
	after := BuildReleaseCorrespondences("work", "77", &workID, records, exclusions)
	if len(after) != 1 || after[0].MemberCount != 2 {
		t.Fatalf("after excluding source_a, got %+v", after)
	}
	for _, m := range after[0].Members {
		if m.ProviderID == "source_a" {
			t.Fatalf("excluded member source_a still present")
		}
	}

	// Excluding a second member leaves only 1 provider -> group dissolves completely.
	exclusions[matchKey][memberExclusionKey("source_b", "2")] = true
	dissolved := BuildReleaseCorrespondences("work", "77", &workID, records, exclusions)
	if len(dissolved) != 0 {
		t.Fatalf("expected group to dissolve when <2 providers remain, got %+v", dissolved)
	}
}

func TestPropagatedLanguagesIntegrateWithFilteringAndPresentation(t *testing.T) {
	rec := model.AIORecord{
		Provider:                    "source_a",
		SourceID:                    "1",
		Title:                       "Example.Film.2024.1080p.BluRay.x264-GRP",
		Hash:                        strings.Repeat("c", 40),
		Size:                        8_431_912_347,
		PropagatedLanguages:         []string{"fr", "en", "multi"},
		PropagatedSubtitleLanguages: []string{"fr"},
	}
	if !AllowsLanguage(rec, model.StreamPreferences{Language: "fr", Mode: "preferred"}) {
		t.Fatalf("expected strict preferred French filter to accept propagated French audio")
	}
	if AllowsLanguage(rec, model.StreamPreferences{Language: "de", Mode: "preferred"}) {
		t.Fatalf("expected strict preferred German filter to reject French/English release")
	}
	stream := baseStream(rec, Resource{Type: "movie"}, "Source A")
	if !strings.Contains(stream.Name, "French") || !strings.Contains(stream.Name, "🇫🇷") || !strings.Contains(stream.Name, "French subtitles") {
		t.Fatalf("propagated languages not reflected in stream presentation: %q", stream.Name)
	}
	// Native evidence must remain empty so recomputation cannot feed back into itself.
	nativeAudio, nativeSubs := nativeLanguageEvidence(rec)
	if len(nativeAudio) != 0 || len(nativeSubs) != 0 {
		t.Fatalf("native evidence leaked propagated languages: audio=%v subs=%v", nativeAudio, nativeSubs)
	}
}
