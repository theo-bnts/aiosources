package model

import (
	"strings"
	"testing"
	"time"
)

func TestCatalogCacheObservationsExcludeAccountDataAndUnsafeFiles(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(*CatalogCacheObservation)
	}{
		{"account library", func(a *CatalogCacheObservation) { a.Service = "realdebrid" }},
		{"private account provider", func(a *CatalogCacheObservation) { a.Service = "deepbrid" }},
		{"provider identifier", func(a *CatalogCacheObservation) { a.Files[0].ID = 1 }},
		{"private link", func(a *CatalogCacheObservation) { a.Files[0].Link = "https://private.invalid/" }},
		{"url path", func(a *CatalogCacheObservation) { a.Files[0].Path = "https://private.invalid/file" }},
		{"traversal", func(a *CatalogCacheObservation) { a.Files[0].Path = "../file.mkv" }},
		{"future", func(a *CatalogCacheObservation) { a.CheckedAt = time.Now().Add(time.Hour) }},
		{"no file proof", func(a *CatalogCacheObservation) { a.Files = nil }},
		{"duplicate files", func(a *CatalogCacheObservation) { a.Files = append(a.Files, a.Files[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := CatalogCacheObservation{Service: "torbox", Hash: strings.Repeat("a", 40), CheckedAt: time.Now(), Files: []AIOFile{{Path: "Folder/file.mkv", Size: 1}}}
			if !a.Valid(time.Now()) {
				t.Fatal("valid proof rejected")
			}
			test.alter(&a)
			if a.Valid(time.Now()) {
				t.Fatal("unsafe or incomplete proof accepted")
			}
		})
	}
}

func TestCatalogMetadataValidationAndPermissionIntersection(t *testing.T) {
	now := time.Now()
	m := CatalogWork(AIOWork{Type: "tv", TMDB: "101", IMDb: "tt0000101", Titles: []string{"Example Series"}, DisplayTitle: "Example Series", SeasonYears: map[int]int{1: 2020}, EpisodeYears: map[string]int{"1:1": 2020}}, now)
	if !m.Valid(now) {
		t.Fatal("valid series metadata rejected")
	}
	m.EpisodeYears["01:2"] = 2020
	if m.Valid(now) {
		t.Fatal("ambiguous season numbering accepted")
	}
	for _, text := range []string{"metadata,metadata", "unknown", "metadata,,availability"} {
		if _, ok := ParseCatalogEntities(text); ok {
			t.Fatalf("ambiguous permissions accepted: %q", text)
		}
	}
	requested, ok := ParseCatalogEntities("metadata,availability")
	if !ok {
		t.Fatal("valid permissions rejected")
	}
	allowed := requested.Intersect(CatalogEntities{Metadata: true})
	if !allowed.Metadata || allowed.Availability {
		t.Fatal("recipient widened share permissions")
	}
}
