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

	if err := f.db.SetPublicAccess(f.ctx, model.PublicAccess{Revision: 1, Enabled: true, StremioEnabled: true, SearchEnabled: true, TrendingEnabled: false}); err != nil {
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
		t.Fatalf("public trending should be disabled when trending_enabled=false even if search_enabled=true, got %d", got.Code)
	}
	if got := send("GET", "/api/public/catalog/search?q=Trending", "", installed.Credential); got.Code != 200 {
		t.Fatalf("public search should remain enabled when trending_enabled=false, got %d %s", got.Code, got.Body)
	}

	// Enable Trending independently while disabling Search.
	access, _ := f.db.PublicAccess(f.ctx)
	access.SearchEnabled = false
	access.TrendingEnabled = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	// An older open settings form omitting trending_enabled must preserve it.
	access, _ = f.db.PublicAccess(f.ctx)
	if put := f.request("PUT", "/api/public-access", fmt.Sprintf(`{"revision":%d,"enabled":true,"search_enabled":false,"stremio_enabled":true}`, access.Revision), true, ""); put.Code != 204 {
		t.Fatalf("public-access update failed: %d %s", put.Code, put.Body)
	}
	access, _ = f.db.PublicAccess(f.ctx)
	if !access.TrendingEnabled || access.SearchEnabled {
		t.Fatalf("omitting trending_enabled unexpectedly changed flags: %+v", access)
	}
	var cfg struct {
		TrendingEnabled bool `json:"trending_enabled"`
		SearchEnabled   bool `json:"search_enabled"`
	}
	if got := send("GET", "/api/public/config", "", ""); got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &cfg) != nil || !cfg.TrendingEnabled || cfg.SearchEnabled {
		t.Fatalf("unexpected public config: %d %s", got.Code, got.Body)
	}
	if got := send("POST", "/api/public/credentials", `{"services":{"torbox":"`+key+`"}}`, ""); got.Code != 200 {
		t.Fatalf("public credentials should work when trending_enabled=true and search_enabled=false, got %d %s", got.Code, got.Body)
	}
	if got := send("GET", "/api/public/catalog/trending?period=day", "", ""); got.Code != 401 {
		t.Fatalf("unauthenticated public trending accepted: %d", got.Code)
	}
	if got := send("GET", "/api/public/catalog/search?q=Trending", "", installed.Credential); got.Code != 404 {
		t.Fatalf("public search should stay disabled when only trending_enabled=true, got %d", got.Code)
	}
	if got := send("GET", "/api/public/catalog/torrents?q=Trending", "", installed.Credential); got.Code != 404 {
		t.Fatalf("public direct torrents should stay disabled when only trending_enabled=true, got %d", got.Code)
	}
	pub := send("GET", "/api/public/catalog/trending?period=month", "", installed.Credential)
	var pubPayload model.CatalogTrending
	if pub.Code != 200 || json.Unmarshal(pub.Body.Bytes(), &pubPayload) != nil || pubPayload.Period != "month" || len(pubPayload.Categories[0].Items) != 2 {
		t.Fatalf("public trending failed: %d %s", pub.Code, pub.Body)
	}
	// Clicking a trending item opens its title detail even when search_enabled=false.
	workID := pubPayload.Categories[0].Items[0].ID
	if got := send("GET", fmt.Sprintf("/api/public/catalog/%d", workID), "", installed.Credential); got.Code != 200 {
		t.Fatalf("public title detail from trending failed when search_enabled=false: %d %s", got.Code, got.Body)
	}
	for _, leaked := range []string{"hidden-source", "secret-native-id", "tracker.invalid", key} {
		if strings.Contains(pub.Body.String(), leaked) {
			t.Fatalf("public trending leaked %q", leaked)
		}
	}

	// Trusted user permissions: Trending and Search are independent.
	created := f.request("POST", "/api/trusted-users", `{"email":"trend.only@example.invalid","password":"synthetic trusted password","enabled":true,"trending_enabled":true,"search_enabled":false,"stremio_enabled":false,"torznab_enabled":false,"catalog_enabled":false}`, true, "")
	if created.Code != 201 {
		t.Fatalf("create trusted user failed: %d %s", created.Code, created.Body)
	}
	login := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"trend.only@example.invalid","password":"synthetic trusted password"}`)
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == trustedSessionCookie {
			cookie = c
		}
	}
	if login.Code != 200 || cookie == nil {
		t.Fatalf("trusted login failed: %d %s", login.Code, login.Body)
	}
	var trustedCfg struct {
		TrendingEnabled bool `json:"trending_enabled"`
		SearchEnabled   bool `json:"search_enabled"`
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/config", ""); got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &trustedCfg) != nil || !trustedCfg.TrendingEnabled || trustedCfg.SearchEnabled {
		t.Fatalf("unexpected trusted config: %d %s", got.Code, got.Body)
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/catalog/trending?period=week", ""); got.Code != 200 {
		t.Fatalf("trusted trending failed: %d %s", got.Code, got.Body)
	}
	if got := trustedRequest(f, cookie, "GET", fmt.Sprintf("/api/trusted/catalog/%d", workID), ""); got.Code != 200 {
		t.Fatalf("trusted title detail from trending failed: %d %s", got.Code, got.Body)
	}
	if got := trustedRequest(f, cookie, "POST", "/api/trusted/credentials", `{}`); got.Code != 200 {
		t.Fatalf("trusted credentials should work with trending_enabled=true, got %d %s", got.Code, got.Body)
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/catalog/search?q=Trending", ""); got.Code != 403 {
		t.Fatalf("trusted search should be denied when search_enabled=false, got %d", got.Code)
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/catalog/torrents?q=Trending", ""); got.Code != 403 {
		t.Fatalf("trusted torrents should be denied when search_enabled=false, got %d", got.Code)
	}

	createdSearchOnly := f.request("POST", "/api/trusted-users", `{"email":"search.only@example.invalid","password":"synthetic trusted password","enabled":true,"trending_enabled":false,"search_enabled":true,"stremio_enabled":false,"torznab_enabled":false,"catalog_enabled":false}`, true, "")
	if createdSearchOnly.Code != 201 {
		t.Fatalf("create search-only trusted user failed: %d %s", createdSearchOnly.Code, createdSearchOnly.Body)
	}
	loginSearch := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"search.only@example.invalid","password":"synthetic trusted password"}`)
	var searchCookie *http.Cookie
	for _, c := range loginSearch.Result().Cookies() {
		if c.Name == trustedSessionCookie {
			searchCookie = c
		}
	}
	if loginSearch.Code != 200 || searchCookie == nil {
		t.Fatalf("search-only trusted login failed: %d %s", loginSearch.Code, loginSearch.Body)
	}
	if got := trustedRequest(f, searchCookie, "GET", "/api/trusted/catalog/trending?period=week", ""); got.Code != 403 {
		t.Fatalf("trusted trending should be denied when trending_enabled=false, got %d", got.Code)
	}
	if got := trustedRequest(f, searchCookie, "GET", "/api/trusted/catalog/search?q=Trending", ""); got.Code != 200 {
		t.Fatalf("trusted search should succeed when search_enabled=true, got %d %s", got.Code, got.Body)
	}
}
