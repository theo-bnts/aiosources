package model

import (
	"slices"
	"testing"
)

func TestMusicCoverURLAndValidation(t *testing.T) {
	valid := "11111111-2222-3333-4444-555555555555"
	if got := CoverURL("music", valid, 0); got != "https://coverartarchive.org/release-group/11111111-2222-3333-4444-555555555555/front-500" {
		t.Fatalf("CoverURL music = %q", got)
	}
	for _, invalid := range []string{
		"",
		"not-a-uuid",
		"11111111-2222-3333-4444-555555555555/../../secret",
		"https://evil.example/cover.jpg",
		"11111111-2222-3333-4444-555555555555?token=secret",
		"11111111-2222-3333-4444-55555555555G",
	} {
		if got := CoverURL("music", invalid, 0); got != "" {
			t.Fatalf("unsafe music cover accepted for %q: %q", invalid, got)
		}
	}
}

func TestMusicSourceHintsAndCompactStorage(t *testing.T) {
	fields := map[string]any{
		"title":      "Synthetic Duo - Neon Horizon (2024) [FLAC]",
		"categories": []any{3000, 3040},
		"artist":     "Synthetic Duo",
		"album":      "Neon Horizon",
		"label":      "Orbit Records",
		"catno":      "ORB-042",
		"barcode":    "0724349691728",
		"external_ids": map[string]any{
			"mbid":        "11111111-2222-3333-4444-555555555555",
			"discogs_id":  "26647",
			"wikidata_id": "Q43228",
		},
		"private_token": "must-be-dropped",
	}
	hints := MusicSourceHints(fields)
	if hints.Kind != "lossless" || hints.Artist != "Synthetic Duo" || hints.Album != "Neon Horizon" || hints.Label != "Orbit Records" ||
		hints.CatalogNumber != "ORB-042" || hints.Barcode != "0724349691728" ||
		hints.MBID != "11111111-2222-3333-4444-555555555555" || hints.DiscogsID != "26647" || hints.WikidataID != "Q43228" {
		t.Fatalf("unexpected music hints: %+v", hints)
	}

	// Audiobooks (3030) must never be claimed as music.
	audiobook := MusicSourceHints(map[string]any{
		"title":      "Morgan Vale - The Amber Orchard [Audiobook]",
		"categories": []any{3030},
	})
	if audiobook.Kind != "" {
		t.Fatalf("audiobook category 3030 became music hints: %+v", audiobook)
	}

	compacted := compactTorrentFields(fields)
	if compacted["artist"] != "Synthetic Duo" || compacted["album"] != "Neon Horizon" || compacted["barcode"] != "0724349691728" {
		t.Fatalf("compact storage dropped music fields: %+v", compacted)
	}
	ext, _ := compacted["external_ids"].(map[string]any)
	if ext["mbid"] != "11111111-2222-3333-4444-555555555555" || ext["discogs_id"] != "26647" || ext["wikidata_id"] != "Q43228" {
		t.Fatalf("compact storage dropped music external IDs: %+v", compacted)
	}
	if _, leaked := compacted["private_token"]; leaked {
		t.Fatal("compact storage kept unknown private field")
	}

	// Catalog details projection preserves music identifiers and rejects unsafe strings.
	projected := ProjectCatalogDetails(map[string]any{
		"mbid":        "11111111-2222-3333-4444-555555555555",
		"discogs_id":  "26647",
		"wikidata_id": "Q43228",
		"barcode":     "0724349691728",
		"external_ids": map[string]any{
			"mbid": "https://evil.example/mbid",
		},
	}, CatalogEntities{Identifiers: true})
	if projected["mbid"] != "11111111-2222-3333-4444-555555555555" || projected["discogs_id"] != "26647" || projected["wikidata_id"] != "Q43228" || projected["barcode"] != "0724349691728" {
		t.Fatalf("catalog details dropped valid music identifiers: %+v", projected)
	}
	if projected["external_ids"].(map[string]any)["mbid"] != catalogConflicting {
		t.Fatalf("unsafe mbid was not flagged as conflicting: %+v", projected)
	}

	cfg := AIOConfig{MusicBrainzSecret: "mb_live_token"}
	if !slices.Contains(cfg.SecretReferences(), "mb_live_token") {
		t.Fatal("AIOConfig.SecretReferences omitted MusicBrainzSecret")
	}
}
