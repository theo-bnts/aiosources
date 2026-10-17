package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogSearchRequiresAdministrationAndDownloadCSRF(t *testing.T) {
	f, addon, conn := addonAPI(t)
	if got := f.request("POST", "/api/operations/missing/download", "{}", false, ""); got.Code != 401 {
		t.Fatal("download continuation exposed without administration", got.Code)
	}
	protected := httptest.NewRequest("POST", "/api/operations/missing/download", strings.NewReader("{}"))
	protected.AddCookie(f.cookie)
	rejected := httptest.NewRecorder()
	f.handler.ServeHTTP(rejected, protected)
	if rejected.Code != 403 {
		t.Fatal("download continuation bypassed CSRF", rejected.Code)
	}
	w := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie: New Day"}, Years: []int{2026}}
	if err := f.db.SaveAIOWork(f.ctx, w, []string{"example movie new day"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one','{"title":"Example.Movie.New.Day.2026","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","imdb_id":"tt1234567","private_note":"synthetic-source-secret","announce":"https://source.invalid/private-announce"}')`); err != nil {
		t.Fatal(err)
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	response := f.request("GET", "/api/catalog/search?q=Example.Movie", "", true, "")
	var titles model.List[model.CatalogTitle]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &titles) != nil || titles.Total != 1 {
		t.Fatalf("catalog search failed: %d %s", response.Code, response.Body)
	}
	workID := titles.Items[0].ID
	var id string
	if err := conn.QueryRow(f.ctx, `SELECT id::text FROM ingest.aio_works WHERE id=$1`, workID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/catalog/search?q=Example", "/api/catalog/" + id, "/api/catalog/" + id + "/releases"} {
		if res := f.request("GET", path, "", false, ""); res.Code != 401 {
			t.Fatalf("anonymous access to %s: %d", path, res.Code)
		}
	}
	// A public instance never grants the administration session needed here.
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if res := f.request("GET", "/api/catalog/"+id, "", false, ""); res.Code != 401 {
		t.Fatal("public mode bypassed administration")
	}
	res := f.request("GET", "/api/catalog/"+id+"/releases", "", true, "")
	if res.Code != 200 || strings.Contains(res.Body.String(), "synthetic-source-secret") || strings.Contains(res.Body.String(), "private-announce") || strings.Contains(res.Body.String(), "aaaaaaaa") || strings.Contains(res.Body.String(), "source_id") {
		t.Fatalf("catalog release projection unsafe: %d %s", res.Code, res.Body)
	}
	rows, err := f.db.CatalogRecords(f.ctx, workID)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	body := `{"occurrence_id":"` + rows[0].OccurrenceID + `","service":"torbox","allow_uncached":true}`
	for _, origin := range []string{"https://addon.example", "https://other.example"} {
		req := httptest.NewRequest("POST", "/api/catalog/"+id+"/download", strings.NewReader(body)).WithContext(f.ctx)
		req.AddCookie(f.cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if origin == "https://other.example" {
			req.Header.Set("X-CSRF-Token", f.csrf)
		}
		out := httptest.NewRecorder()
		f.handler.ServeHTTP(out, req)
		if out.Code != http.StatusForbidden {
			t.Fatalf("missing CSRF/foreign origin accepted: %d", out.Code)
		}
	}
	for _, query := range []string{"q=x", "q=Example&type=unsupported", "q=Example&q=Other", "q=%00Example", "q=Example&year=2020-2026", "q=Example&year=2026&year=2025", "q=Example&year=0", "q=Example&year=2026.0", "q=Example&year=10000", "q=Example&year=20e2"} {
		out := f.request("GET", "/api/catalog/search?"+query, "", true, "")
		if out.Code < 400 || out.Code >= 500 {
			t.Fatalf("invalid catalog search accepted: %s / %d", query, out.Code)
		}
	}
	for _, tc := range []struct {
		year string
		want int64
	}{{"2026", 1}, {"2025", 0}} {
		response := f.request("GET", "/api/catalog/search?q=Example&year="+tc.year, "", true, "")
		var page model.List[model.CatalogTitle]
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Total != tc.want {
			t.Fatalf("year filter %q: %d %s", tc.year, response.Code, response.Body)
		}
	}
	for _, body := range []string{`{"service":"unknown"}`, `{"service":"torbox","occurrence_id":"invalid"}`, `{"service":"torbox","url":"https://other.example"}`} {
		out := f.request("POST", "/api/catalog/"+id+"/download", body, true, "")
		if out.Code < 400 || out.Code >= 500 {
			t.Fatalf("invalid download accepted: %d", out.Code)
		}
	}
	var operations int
	for _, preparation := range []string{"-1", "00", "1.5", "1e2", "9223372036854775808", "https://other.example"} {
		body := fmt.Sprintf(`{"service":"torbox","occurrence_id":%q,"fingerprint":%q,"preparation_id":%q}`, rows[0].OccurrenceID, rows[0].Fingerprint, preparation)
		if out := f.request("POST", "/api/catalog/"+id+"/download", body, true, ""); out.Code != 422 {
			t.Fatalf("invalid preparation accepted: %d", out.Code)
		}
	}
	if err := conn.QueryRow(f.ctx, `SELECT count(*) FROM ingest.operation_runs WHERE kind='download'`).Scan(&operations); err != nil || operations != 0 {
		t.Fatal("invalid request started a download", err)
	}
	if cache := res.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("private catalog response could be cached: %q", cache)
	}
}

func TestCatalogDownloadHTTPReturnsReadyLinksAndPendingResponses(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			f, addon, conn := addonAPI(t)
			hash := strings.Repeat("a", 40)
			if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Film"}, Years: []int{2026}}, []string{"example film"}); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one','{"title":"Example.Film.2026","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","imdb_id":"tt1234567"}')`); err != nil {
				t.Fatal(err)
			}
			if err := addon.Match(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err := addon.Vault.Put(f.ctx, "debrid", "synthetic-download-key"); err != nil {
				t.Fatal(err)
			}
			c, _ := f.db.AIOConfig(f.ctx)
			if service == "alldebrid" {
				c.AllDebridSecret = "debrid"
			} else {
				c.TorBoxSecret = "debrid"
			}
			c.Trackers["source"] = model.AIOTracker{Private: true}
			if err := addon.SaveConfig(f.ctx, c); err != nil {
				t.Fatal(err)
			}
			var ready atomic.Bool
			var deleted atomic.Bool
			ready.Store(true)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					if deleted.Load() {
						fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
						return
					}
					status := 0
					if ready.Load() {
						status = 4
					}
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"statusCode":%d}]}}`, hash, status)
				case "/v4/magnet/files":
					if deleted.Load() {
						fmt.Fprint(w, `{"status":"error","error":{"code":"MAGNET_INVALID_ID","message":"synthetic-private-key"}}`)
						return
					}
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"files":[{"n":"Example.Film.2026.mkv","s":100,"l":"https://files.example/selected"}]}]}}`)
				case "/v4/link/unlock":
					fmt.Fprint(w, `{"status":"success","data":{"link":"https://files.example/download"}}`)
				case "/torrents/createtorrent":
					if r.ParseMultipartForm(4096) != nil || r.FormValue("add_only_if_cached") != "true" {
						t.Error("private HTTP download bypassed cached-only policy")
					}
					if deleted.Load() {
						w.WriteHeader(400)
						fmt.Fprint(w, `{"success":false,"error":"DOWNLOAD_NOT_CACHED","detail":"synthetic-private-key","data":null}`)
						return
					}
					fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, hash)
				case "/torrents/mylist":
					if deleted.Load() {
						fmt.Fprint(w, `{"success":true,"error":"ITEM_NOT_FOUND","detail":"synthetic-private-key","data":null}`)
						return
					}
					fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":%t,"download_present":%t,"files":[{"id":0,"name":"Example.Film.2026.mkv","size":100}]}}`, hash, ready.Load(), ready.Load())
				case "/torrents/requestdl":
					fmt.Fprint(w, `{"success":true,"data":"https://files.example/download"}`)
				default:
					t.Error("unexpected download request")
					w.WriteHeader(500)
				}
			}))
			defer upstream.Close()
			addon.Providers.ADBase, addon.Providers.TBBase, addon.Providers.Client = upstream.URL, upstream.URL, upstream.Client()
			var id int64
			if err := conn.QueryRow(f.ctx, `SELECT id FROM ingest.aio_works`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			rows, err := f.db.CatalogRecords(f.ctx, id)
			if err != nil || len(rows) != 1 {
				t.Fatal(err)
			}
			input := model.CatalogDownloadInput{OccurrenceID: rows[0].OccurrenceID, Fingerprint: rows[0].Fingerprint, Service: service, AllowUncached: true}
			body, _ := json.Marshal(input)
			path := fmt.Sprintf("/api/catalog/%d/download", id)
			response := f.request("POST", path, string(body), true, "")
			var result model.CatalogDownload
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Status != "ready" || !strings.HasPrefix(result.URL, "/api/catalog-downloads/") || result.OperationID == "" {
				t.Fatalf("ready download response: %d %s", response.Code, response.Body)
			}
			ready.Store(false)
			response = f.request("POST", path, string(body), true, "")
			result = model.CatalogDownload{}
			if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Status != "pending" || result.URL != "" || response.Header().Get("Retry-After") != "15" {
				t.Fatalf("pending download response: %d %s", response.Code, response.Body)
			}
			if result.PreparationID != "7" {
				t.Fatal("pending download lost its account item reference")
			}

			original := result.OperationID
			ready.Store(true)
			follow := f.request("POST", "/api/operations/"+original+"/download", "{}", true, "")
			var followed model.CatalogDownload
			if follow.Code != 200 || json.Unmarshal(follow.Body.Bytes(), &followed) != nil || followed.OperationID != original || !strings.HasPrefix(followed.URL, "/api/catalog-downloads/") {
				t.Fatalf("continuation did not update its original operation: %d %s", follow.Code, follow.Body)
			}
			detail := f.request("GET", "/api/operations/"+original, "", true, "")
			if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"download":`) || strings.Contains(detail.Body.String(), "synthetic-download-key") || strings.Contains(detail.Body.String(), "files.example") || strings.Contains(detail.Body.String(), `"account"`) {
				t.Fatal("unsafe or missing history continuation", detail.Code)
			}
			if err := addon.Vault.Put(f.ctx, "debrid", "replacement-account"); err != nil {
				t.Fatal(err)
			}
			changed := f.request("POST", "/api/operations/"+original+"/download", "{}", true, "")
			if changed.Code != 409 {
				t.Fatal("continuation used a different provider account", changed.Code)
			}
			if err := addon.Vault.Put(f.ctx, "debrid", "synthetic-download-key"); err != nil {
				t.Fatal(err)
			}
			// The account item can disappear between readiness checks. Never
			// turn its cache miss into a 502 or retry an uncached creation.
			ready.Store(true)
			deleted.Store(true)
			input.AllowUncached = false
			input.PreparationID = result.PreparationID
			body, _ = json.Marshal(input)
			response = f.request("POST", path, string(body), true, "")
			wantStatus, wantCode := 404, "not_found"
			var failure struct {
				Code string `json:"code"`
			}
			if response.Code != wantStatus || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != wantCode || strings.Contains(response.Body.String(), "synthetic-private-key") {
				t.Fatalf("deleted download response: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestCatalogLocaleAndPreferenceTokensDoNotChangeAuthorization(t *testing.T) {
	f, addon, conn := addonAPI(t)
	work := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie New Day"}, Years: []int{2026}, LocalizedTitles: map[string]string{"fr": "Film Exemple", "en": "Example Movie New Day"}}
	if err := f.db.SaveAIOWork(f.ctx, work, []string{"example movie new day"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one','{"title":"Example.Movie.New.Day.2026.FRENCH","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","imdb_id":"tt1234567"}')`); err != nil {
		t.Fatal(err)
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	response := f.request("GET", "/api/catalog/search?q=example&language=fr-CA", "", true, "")
	var page model.List[model.CatalogTitle]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Total != 1 || page.Items[0].Title != "Film Exemple" {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, path := range []string{"/api/torrents?q=example-movie+new+day", "/api/aiosource/matches?q=example-movie+new+day"} {
		res := f.request("GET", path, "", true, "")
		var list struct {
			Total int `json:"total"`
		}
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &list) != nil || list.Total != 1 {
			t.Fatal("shared HTTP search diverged", path, res.Code, res.Body.String())
		}
	}
	token, _ := testPrivateInstallation(t, f.ctx, addon)
	for _, suffix := range []string{""} {
		if res := f.request("GET", "/stremio/"+token+suffix+"/manifest.json", "", false, ""); res.Code != 200 {
			t.Fatal("valid installation rejected", suffix, res.Code)
		}
	}
	for _, token := range []string{"invalid~fr~all", token + "~fr~unknown", token + "~fr~all", token + "~fr~invalid", token + "~~unknown"} {
		if res := f.request("GET", "/stremio/"+token+"/manifest.json", "", false, ""); res.Code != 401 {
			t.Fatal("preferences bypassed authorization", res.Code)
		}
	}
}
