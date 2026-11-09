package aiosource

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestTMDBPostersSelectValidatedPathsWithoutChangingMatchingData(t *testing.T) {
	for _, tc := range []struct{ kind, id, detail string }{{"movie", "101", movieDetail}, {"tv", "202", tvDetail}} {
		before, err := decodeWork(tc.kind, tc.id, []byte(tc.detail))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err = json.Unmarshal([]byte(tc.detail), &body); err != nil {
			t.Fatal(err)
		}
		body["poster_path"] = "/original.jpg"
		body["images"] = map[string]any{"posters": []map[string]any{
			{"iso_639_1": "fr", "file_path": "/french.jpg", "vote_average": 8, "vote_count": 5},
			{"iso_639_1": "en", "file_path": "/english.jpg", "vote_average": 9, "vote_count": 5},
			{"iso_639_1": "fr", "file_path": "/bestFrench.jpg", "vote_average": 8, "vote_count": 10},
			{"iso_639_1": "fr", "file_path": "/otherFrench.jpg", "vote_average": 7, "vote_count": 100},
			{"iso_639_1": "fr", "file_path": "https://untrusted.invalid/image.jpg", "vote_average": 10, "vote_count": 100},
			{"iso_639_1": nil, "file_path": "/neutral.jpg", "vote_average": 10, "vote_count": 100},
		}}
		data, _ := json.Marshal(body)
		after, err := decodeWork(tc.kind, tc.id, data)
		if err != nil {
			t.Fatal(err)
		}
		if after.PosterPath != "/original.jpg" || !reflect.DeepEqual(after.LocalizedPosters, map[string]string{"fr": "/bestFrench.jpg", "en": "/english.jpg"}) {
			t.Fatal("incorrect image selection", after.LocalizedPosters)
		}
		after.PosterPath = before.PosterPath
		after.LocalizedPosters = before.LocalizedPosters
		if !reflect.DeepEqual(before, after) {
			t.Fatal("images changed decision-relevant information")
		}
	}
}

func TestPublicAndAdministratorSearchUseTheSameLocalCoverWithoutProviderCalls(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "example-source", "example-release", strings.Repeat("a", 40))
	if _, err := conn.Exec(ctx, `UPDATE aiosources.aio_works SET poster_path='/default.jpg',localized_posters='{"fr":"/french.jpg","en":"/english.jpg"}'`); err != nil {
		t.Fatal(err)
	}
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.SearchEnabled = true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	visitor := &PublicInstallation{Generation: access.Generation, Audience: "https://example.invalid", Services: map[string]string{"torbox": "synthetic-visitor-key"}}
	for _, language := range []string{"fr", "ja"} {
		opts := model.ListOptions{Query: "Example Movie"}
		filters := model.CatalogSearchFilters{Language: language}
		admin, err := s.CatalogSearch(ctx, opts, filters)
		if err != nil || len(admin.Items) != 1 {
			t.Fatal("administrator search", err)
		}
		public, err := s.PublicCatalogSearch(ctx, visitor, opts, filters)
		if err != nil || len(public.Items) != 1 || public.Items[0].Cover != admin.Items[0].Cover || len(public.Items[0].Providers) != 0 {
			t.Fatal("public image differs or reveals a source", err)
		}
		detail, err := s.PublicCatalogTitle(ctx, visitor, public.Items[0].ID, language)
		if err != nil || detail.Cover != public.Items[0].Cover {
			t.Fatal("public detail image differs", err)
		}
	}
}
