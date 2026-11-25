package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogTrendingAdminPublicAndValidation(t *testing.T) {
	f, addon, conn := addonAPI(t)

	// Requires admin authentication.
	if got := f.request("GET", "/api/catalog/trending", "", false, ""); got.Code != 401 {
		t.Fatalf("anonymous admin trending status = %d, want 401", got.Code)
	}

	// Seed a movie work and a multi-episode TV series with private fields that must never leak.
	if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{
		Type:   "movie",
		TMDB:   "4242",
		IMDb:   "tt0004242",
		Titles: []string{"Trending Film"},
		Years:  []int{2026},
	}, []string{"trending film"}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{
		Type:   "tv",
		TMDB:   "4243",
		IMDb:   "tt0004243",
		Titles: []string{"Trending Series"},
		Years:  []int{2026},
	}, []string{"trending series"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES
 ('hidden-source','secret-native-id','{"title":"Trending.Film.2026.1080p","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","imdb_id":"tt0004242","announce":"https://tracker.invalid/secret"}'),
 ('hidden-source','secret-native-id-2','{"title":"Trending.Film.2026.2160p","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","imdb_id":"tt0004242"}'),
 ('hidden-source','secret-tv-1','{"title":"Trending.Series.S01E01.1080p","info_hash":"1111111111111111111111111111111111111111","imdb_id":"tt0004243"}'),
 ('hidden-source','secret-tv-2','{"title":"Trending.Series.S01E02.1080p","info_hash":"2222222222222222222222222222222222222222","imdb_id":"tt0004243"}'),
 ('hidden-source','secret-tv-3','{"title":"Trending.Series.S01E03.1080p","info_hash":"3333333333333333333333333333333333333333","imdb_id":"tt0004243"}')`); err != nil {
		t.Fatal(err)
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}

	// Admin default period is week: movie (2 torrents) ranks ahead of 3-episode series (3 torrents / 3 eps = 1/ep).
	res := f.request("GET", "/api/catalog/trending", "", true, "")
	var payload model.CatalogTrending
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &payload) != nil {
		t.Fatalf("admin trending failed: %d %s", res.Code, res.Body)
	}
	if payload.Period != "week" || len(payload.Categories) != 5 || payload.Categories[0].ID != "videos" || len(payload.Categories[0].Items) != 2 {
		t.Fatalf("unexpected admin trending payload: %+v", payload)
	}
	if got := payload.Categories[0].Items[0]; got.Type != "movie" || got.Torrents != 2 || got.Episodes != 0 || got.TorrentsPerEpisode != 0 {
		t.Fatalf("expected movie ranked first, got %+v", got)
	}
	if got := payload.Categories[0].Items[1]; got.Type != "tv" || got.Torrents != 3 || got.Episodes != 3 || got.TorrentsPerEpisode != 1 {
		t.Fatalf("expected 3-episode TV series ranked second at 1/ep, got %+v", got)
	}
	for _, leaked := range []string{"hidden-source", "secret-native-id", "tracker.invalid", "aaaaaaaa"} {
		if strings.Contains(res.Body.String(), leaked) {
			t.Fatalf("trending leaked private field %q", leaked)
		}
	}

	// Validation errors.
	for _, query := range []string{"period=invalid", "period=day&period=week", "limit=0", "limit=51", "limit=abc"} {
		out := f.request("GET", "/api/catalog/trending?"+query, "", true, "")
		if out.Code < 400 || out.Code >= 500 {
			t.Fatalf("invalid trending query %q accepted with %d", query, out.Code)
		}
	}

	// Public access opt-in and credential enforcement.
	const key = "synthetic-trending-key"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/me" {
			fmt.Fprint(w, `{"success":true,"data":{"id":1,"email":"user@example.invalid"}}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer upstream.Close()
	addon.Providers.TBBase, addon.Providers.Client = upstream.URL, upstream.Client()

	if err := f.db.SetPublicAccess(f.ctx, model.PublicAccess{Revision: 1, Enabled: true, StremioEnabled: true, SearchEnabled: false}); err != nil {
		t.Fatal(err)
	}
	send := func(method, path, body, credential string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(f.ctx)
		req.Header.Set("Origin", "https://addon.example")
		req.Header.Set("Content-Type", "application/json")
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		out := httptest.NewRecorder()
		f.handler.ServeHTTP(out, req)
		return out
	}
	var installed struct {
		Credential string `json:"credential"`
	}
	inst := send("POST", "/api/public/install", `{"services":{"torbox":"`+key+`"}}`, "")
	if inst.Code != 200 || json.Unmarshal(inst.Body.Bytes(), &installed) != nil || installed.Credential == "" {
		t.Fatalf("install failed: %d %s", inst.Code, inst.Body)
	}
	if got := send("GET", "/api/public/catalog/trending?period=day", "", installed.Credential); got.Code != 404 {
		t.Fatalf("public trending should be disabled when search_enabled=false, got %d", got.Code)
	}

	access, _ := f.db.PublicAccess(f.ctx)
	access.SearchEnabled = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if got := send("GET", "/api/public/catalog/trending?period=day", "", ""); got.Code != 401 {
		t.Fatalf("unauthenticated public trending accepted: %d", got.Code)
	}
	pub := send("GET", "/api/public/catalog/trending?period=month", "", installed.Credential)
	var pubPayload model.CatalogTrending
	if pub.Code != 200 || json.Unmarshal(pub.Body.Bytes(), &pubPayload) != nil || pubPayload.Period != "month" || len(pubPayload.Categories[0].Items) != 2 {
		t.Fatalf("public trending failed: %d %s", pub.Code, pub.Body)
	}
	for _, leaked := range []string{"hidden-source", "secret-native-id", "tracker.invalid", key} {
		if strings.Contains(pub.Body.String(), leaked) {
			t.Fatalf("public trending leaked %q", leaked)
		}
	}
}
