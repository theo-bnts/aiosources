package aiosource

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCrossSourceCorrespondencePropagationAndUnlinkLifecycle(t *testing.T) {
	ctx, s, conn := testService(t)

	w := model.AIOWork{
		Type:         "movie",
		TMDB:         "555",
		IMDb:         "tt5550001",
		DisplayTitle: "Example Film",
		Titles:       []string{"Example Film"},
		Years:        []int{2024},
	}
	if err := s.DB.SaveAIOWork(ctx, w, []string{"example film"}); err != nil {
		t.Fatal(err)
	}

	exactSize := int64(8_431_912_347)
	insert := func(provider, sourceID string, fields map[string]any) {
		t.Helper()
		b, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES($1,$2,$3)`, provider, sourceID, b); err != nil {
			t.Fatal(err)
		}
	}

	// Source A has only a blind MULTI title (no concrete language).
	insert("source_a", "a1", map[string]any{
		"title":     "Example.Film.2024.MULTI.1080p.BluRay.x264-EXGRP",
		"info_hash": strings.Repeat("1", 40),
		"size":      exactSize,
		"tmdb_id":   "555",
	})
	// Source B has the exact same non-rounded byte size and explicit French + English audio + French subtitles.
	insert("source_b", "b1", map[string]any{
		"title":     "Example.Film.2024.MULTI.VFF.1080p.BluRay.x264-EXGRP",
		"info_hash": strings.Repeat("2", 40),
		"size":      exactSize,
		"tmdb_id":   "555",
		"languages": []string{"MULTI", "Français (VFF/TrueFrench)", "ANGLAIS"},
		"sublangs":  []string{"fr"},
	})
	// Source C has a different non-multi language (German) on a conflicting rounded size -> must NOT link.
	insert("source_c", "c1", map[string]any{
		"title":     "Example.Film.2024.GERMAN.1080p.BluRay.x264-OTHER",
		"info_hash": strings.Repeat("3", 40),
		"size":      int64(4_509_715_661),
		"tmdb_id":   "555",
	})

	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify status counts reflect the new cross-source correspondence group.
	status, err := s.DB.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.CorrespondenceGroups != 1 || status.CorrespondenceMembers != 2 || status.CorrespondenceEnriched != 1 || status.CorrespondencesPending != 0 {
		t.Fatalf("unexpected correspondence status: %+v", status)
	}

	// Verify Stremio streams for strict French filter now include source_a via cross-source propagation.
	token, err := testPrivateInstallation(t, ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	frToken, err := WithInstallationPreferences(token, model.StreamPreferences{Language: "fr", Mode: "preferred"})
	if err != nil {
		t.Fatal(err)
	}
	streams, err := s.Streams(ctx, "http://localhost", frToken, "movie", "tt5550001")
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 2 {
		t.Fatalf("expected 2 French streams (source_a enriched + source_b), got %d: %+v", len(streams), streams)
	}
	for _, st := range streams {
		if !strings.Contains(st.Name, "French") || !strings.Contains(st.Name, "French subtitles") {
			t.Fatalf("stream missing propagated language/subtitle label: %q", st.Name)
		}
	}

	// Inspect persisted correspondence group and verify unlink dissolves propagation cleanly.
	list, err := s.DB.ListReleaseCorrespondences(ctx, model.ReleaseCorrespondenceOptions{
		ListOptions:  model.ListOptions{Limit: 10},
		EnrichedOnly: true,
	})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("list correspondences: items=%d err=%v", len(list.Items), err)
	}
	group := list.Items[0]
	if group.MatchRule != model.CorrespondenceRuleExactSize || !slices.Equal(group.PropagatedAudio, []string{"en", "fr", "multi"}) {
		t.Fatalf("unexpected group: %+v", group)
	}

	detail, err := s.DB.ReleaseCorrespondenceDetail(ctx, group.ID, "en")
	if err != nil || len(detail.Members) != 2 {
		t.Fatalf("detail: members=%d err=%v", len(detail.Members), err)
	}
	var occA string
	for _, m := range detail.Members {
		if m.ProviderID == "source_a" {
			occA = m.OccurrenceID
		}
	}
	if occA == "" {
		t.Fatal("missing source_a occurrence in correspondence detail")
	}

	nextID, err := s.UnlinkCorrespondenceMember(ctx, group.ID, occA)
	if err != nil {
		t.Fatal(err)
	}
	if nextID != 0 {
		t.Fatalf("expected 2-member group to dissolve after unlink, got group_id=%d", nextID)
	}

	// After unlinking source_a, strict French Stremio filter only returns source_b.
	afterStreams, err := s.Streams(ctx, "http://localhost", frToken, "movie", "tt5550001")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterStreams) != 1 {
		t.Fatalf("expected only 1 French stream after unlinking source_a, got %d", len(afterStreams))
	}

	// Recomputing correspondences must respect the persisted manual exclusion.
	if err := s.DB.QueueAllReleaseCorrespondences(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	afterRecompute, err := s.DB.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterRecompute.CorrespondenceGroups != 0 {
		t.Fatalf("manual exclusion was not respected on recompute: %+v", afterRecompute)
	}
}
