package model

import (
	"reflect"
	"testing"
	"time"
)

func TestCatalogPostersShareOnlyCanonicalProviderPaths(t *testing.T) {
	now := time.Now()
	work := AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Example film"}, DisplayTitle: "Example film", PosterPath: "/default.jpg", LocalizedPosters: map[string]string{"fr": "/french.jpg"}}
	metadata := CatalogWork(work, now)
	if !metadata.Valid(now) || metadata.Work().PosterPath != work.PosterPath || !reflect.DeepEqual(metadata.Work().LocalizedPosters, work.LocalizedPosters) {
		t.Fatal("safe image references did not survive sharing")
	}
	for _, path := range []string{"https://example.invalid/private?key=secret", "//example.invalid/file.jpg", "/../private.jpg", "/file.jpg?key=secret"} {
		value := CatalogWork(work, now)
		value.LocalizedPosters = map[string]string{"fr": path}
		if value.Valid(now) {
			t.Fatal("sharing accepted an arbitrary image URL")
		}
		value.LocalizedPosters = nil
		value.PosterPath = path
		if value.Valid(now) {
			t.Fatal("sharing accepted an arbitrary default image URL")
		}
	}
}
