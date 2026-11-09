package aiosource

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogReleaseUsesTheStremioFormatterWithoutAdditionalRequests(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Trackers["public-one"] = model.AIOTracker{Private: false}
	if err = s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	s.Providers = providerTest(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("formatting a release contacted an upstream service")
		w.WriteHeader(http.StatusInternalServerError)
	})
	seedMovie(t, ctx, s, conn, "public-one", "one", strings.Repeat("b", 40))
	if _, err = conn.Exec(ctx, `UPDATE aiosources.torrents SET fields=fields||'{
 "title":"Example.Movie.2021.FRENCH.1080p.WEB-DL.H265.HDR10+.DTS.5.1.EXTENDED",
 "languages":["fra","eng"],"sublangs":["fra"],"duration":5400,
 "release_group":"ExampleGroup","published_at":"2026-10-09T14:00:00+02:00",
 "attributes":{"private_key":"forbidden-attribute"},
 "subtitles":[{"language":"fr","url":"https://subtitle.example/private-token"}]
 }' WHERE provider_id='public-one'`); err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var work int64
	if err = conn.QueryRow(ctx, `SELECT id FROM aiosources.aio_works`).Scan(&work); err != nil {
		t.Fatal(err)
	}
	token, _ := testPrivateInstallation(t, ctx, s)
	streams, err := s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(streams) != 1 {
		t.Fatal("direct stream fixture was unavailable", err)
	}
	releases, err := s.CatalogReleases(ctx, work, model.CatalogSelection{}, model.ListOptions{})
	if err != nil || len(releases.Items) != 1 {
		t.Fatal("Search release fixture was unavailable", err)
	}
	release := releases.Items[0]
	description := strings.Join(release.Presentation.Details, "\n")
	name := strings.Join(append(release.Presentation.Highlights, release.Presentation.Language), "\n")
	if description != streams[0].Title || name != streams[0].Name || streams[0].Hints.Filename != release.Title {
		t.Fatal("Search and Stremio formatted the same release differently")
	}
	for _, label := range []string{"🖼️ 1080P", "🎞️ WEB-DL", "🎨 HDR10+", "🔊 DTS", "✨ EXTENDED (1h 30m)", "💬 French subtitles", "📦", "🧭 Public Library", "🧩 ExampleGroup", "📅 2026-10-09"} {
		if !strings.Contains(name+"\n"+description, label) {
			t.Fatalf("known presentation detail was lost: %s", label)
		}
	}
	if !strings.Contains(release.Presentation.Language, "French / English") {
		t.Fatal("formatted audio language names were lost")
	}
	encoded, err := json.Marshal(releases)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"forbidden-attribute", "subtitle.example", "private-token", "administrator-announce"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private source metadata escaped the presentation boundary")
		}
	}
}
