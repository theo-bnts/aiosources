package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

// Complete HTTP/authentication/storage/formatting paths over synthetic data.
// Provider-backed cases use an actual loopback HTTP server, never an account.
func BenchmarkAddonReadFlows(b *testing.B) {
	f, addon, conn := addonAPI(b)
	hash := strings.Repeat("a", 40)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/me":
			fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
		case "/torrents/checkcached":
			fmt.Fprintf(w, `{"success":true,"data":{%q:{"hash":%q,"files":[{"name":"Synthetic.Feature.2024.mkv","size":100}]}}}`, hash, hash)
		case "/torrents/createtorrent":
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, hash)
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":true,"download_present":true,"files":[{"id":0,"name":"Synthetic.Feature.2024.mkv","size":100}]}}`, hash)
		case "/torrents/requestdl":
			fmt.Fprint(w, `{"success":true,"data":"https://files.example/performance"}`)
		default:
			b.Errorf("Unexpected synthetic provider path")
			w.WriteHeader(500)
		}
	}))
	b.Cleanup(upstream.Close)
	addon.Providers.TBBase, addon.Providers.Client = upstream.URL, upstream.Client()
	if err := addon.Vault.Put(f.ctx, "performance_api_key", "synthetic-provider-key"); err != nil {
		b.Fatal(err)
	}
	config, err := f.db.AIOConfig(f.ctx)
	if err != nil {
		b.Fatal(err)
	}
	config.TorBoxSecret, config.AutoCache = "performance_api_key", false
	if err := f.db.SetAIOConfig(f.ctx, config); err != nil {
		b.Fatal(err)
	}
	if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", DisplayTitle: "Synthetic Feature", Titles: []string{"Synthetic Feature"}, Years: []int{2024}}, []string{"synthetic feature"}); err != nil {
		b.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
  SELECT 'synthetic',n::text,jsonb_build_object('title','Synthetic.Feature.2024.1080p','info_hash',$1::text,'size',100,'imdb_id','tt1234567') FROM generate_series(1,20)n`, hash); err != nil {
		b.Fatal(err)
	}
	if err := addon.Match(f.ctx); err != nil {
		b.Fatal(err)
	}
	var work int64
	if err := conn.QueryRow(f.ctx, `SELECT id FROM ingest.aio_works WHERE tmdb_id='101'`).Scan(&work); err != nil {
		b.Fatal(err)
	}
	generation, err := f.db.AIOCacheGeneration(f.ctx)
	if err != nil {
		b.Fatal(err)
	}
	if err := f.db.SaveSharedAvailability(f.ctx, "torbox", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Synthetic.Feature.2024.mkv", Size: 100}}}); err != nil {
		b.Fatal(err)
	}
	access, err := f.db.PublicAccess(f.ctx)
	if err != nil {
		b.Fatal(err)
	}
	access.Enabled, access.SearchEnabled, access.StremioEnabled, access.TorznabEnabled, access.ExposeSources, access.CatalogEnabled = true, true, true, true, true, true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		b.Fatal(err)
	}
	private, err := testPrivateInstallation(b, f.ctx, addon)
	if err != nil {
		b.Fatal(err)
	}
	tz, err := f.db.CreateShare(f.ctx, model.ShareInput{Format: "torznab", Name: "Synthetic Torznab", Enabled: true, Scope: "all", Fields: store.CatalogFields()})
	if err != nil {
		b.Fatal(err)
	}
	share, err := f.db.CreateShare(f.ctx, model.ShareInput{Name: "Synthetic catalog", Enabled: true, Scope: "all", Fields: []string{"title", "info_hash", "size"}})
	if err != nil {
		b.Fatal(err)
	}
	publicRequest := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(f.ctx)
		r.Header.Set("Origin", "https://addon.example")
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	installed := publicRequest("POST", "/api/public/install", `{"services":{"torbox":"synthetic-visitor-key"}}`, "")
	var public struct {
		Credential string `json:"credential"`
		Manifest   string `json:"manifest_url"`
	}
	if installed.Code != 200 || json.Unmarshal(installed.Body.Bytes(), &public) != nil || public.Credential == "" {
		b.Fatal("Public synthetic install failed", installed.Code)
	}
	installed = publicRequest("POST", "/api/public/torznab/install", `{"options":{}}`, "")
	var publicTZ struct {
		Key string `json:"api_key"`
	}
	if installed.Code != 200 || json.Unmarshal(installed.Body.Bytes(), &publicTZ) != nil || publicTZ.Key == "" {
		b.Fatal("Public Torznab install failed", installed.Code)
	}
	installed = publicRequest("POST", "/api/public/catalogs/install", `{}`, "")
	var publicShare struct {
		URL      string `json:"url"`
		Password string `json:"password"`
	}
	if installed.Code != 200 || json.Unmarshal(installed.Body.Bytes(), &publicShare) != nil {
		b.Fatal("Public catalog install failed", installed.Code)
	}
	manifest := strings.TrimPrefix(public.Manifest, "https://addon.example")
	publicStreams := strings.TrimSuffix(manifest, "manifest.json") + "stream/movie/tt1234567.json"
	cases := []struct {
		name, route, key string
		admin            bool
		kind             string
		minimum          int
	}{
		{"private-search", "/api/catalog/search?q=Synthetic", "", true, "list", 1},
		{"public-search", "/api/public/catalog/search?q=Synthetic", public.Credential, false, "list", 1},
		{"private-releases", fmt.Sprintf("/api/catalog/%d/releases", work), "", true, "list", 20},
		{"public-releases", fmt.Sprintf("/api/public/catalog/%d/releases", work), public.Credential, false, "list", 20},
		{"private-stremio", "/stremio/" + private + "/stream/movie/tt1234567.json", "", false, "streams", 1},
		{"public-stremio", publicStreams, "", false, "streams", 1},
		{"private-manifest", "/stremio/" + private + "/manifest.json", "", false, "manifest", 0},
		{"public-manifest", manifest, "", false, "manifest", 0},
		{"private-torznab", "/torznab/" + tz.Share.ID + "/api?t=search&apikey=" + tz.Password, "", false, "xml", 20},
		{"public-torznab", "/public/torznab/api?t=search&apikey=" + publicTZ.Key, "", false, "xml", 20},
		{"private-sharing", "/api/catalogs/" + share.Share.ID, share.Password, false, "catalog", 20},
		{"public-sharing", strings.TrimPrefix(publicShare.URL, "https://addon.example"), publicShare.Password, false, "catalog", 20},
		{"identification", "/api/aiosource/matches?q=Synthetic", "", true, "list", 20},
		{"authentication", "/api/session", "", true, "session", 0},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				var response *httptest.ResponseRecorder
				if strings.HasPrefix(tc.name, "public-") {
					response = publicRequest("GET", tc.route, "", tc.key)
				} else {
					response = f.request("GET", tc.route, "", tc.admin, tc.key)
				}
				if response.Code != 200 {
					b.Fatal("Complete HTTP flow failed", response.Code)
				}
				if tc.kind == "xml" {
					if strings.Count(response.Body.String(), "<item>") < tc.minimum {
						b.Fatal("Torznab lost source rows")
					}
					continue
				}
				var data map[string]json.RawMessage
				if json.Unmarshal(response.Body.Bytes(), &data) != nil {
					b.Fatal("Response is not JSON")
				}
				switch tc.kind {
				case "list", "streams", "catalog":
					key := "items"
					if tc.kind == "streams" {
						key = "streams"
					}
					var items []json.RawMessage
					if json.Unmarshal(data[key], &items) != nil || len(items) < tc.minimum {
						b.Fatal("Complete flow lost results", tc.name, len(items))
					}
				case "manifest":
					if len(data["logo"]) == 0 || len(data["resources"]) == 0 {
						b.Fatal("Manifest lost required branding or resources")
					}
				case "session":
					if string(data["authenticated"]) != "true" {
						b.Fatal("Session lost authentication")
					}
				}
			}
		})
	}
	b.Run("public-download", func(b *testing.B) {
		for b.Loop() {
			releases := publicRequest("GET", fmt.Sprintf("/api/public/catalog/%d/releases", work), "", public.Credential)
			var data struct {
				Items []model.CatalogRelease `json:"items"`
			}
			if releases.Code != 200 || json.Unmarshal(releases.Body.Bytes(), &data) != nil || len(data.Items) == 0 {
				b.Fatal("Download release fixture missing")
			}
			payload, _ := json.Marshal(map[string]any{"download_token": data.Items[0].DownloadToken, "service": "torbox", "allow_uncached": false})
			response := publicRequest("POST", fmt.Sprintf("/api/public/catalog/%d/download", work), string(payload), public.Credential)
			var download model.CatalogDownload
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &download) != nil || download.Status != "ready" || !strings.HasPrefix(download.URL, "/api/public/catalog-downloads/") {
				b.Fatal("Complete public download did not resolve", response.Code)
			}
		}
	})
}
