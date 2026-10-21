package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicRoutesResolveCachedPlaybackEndToEndWithoutSavingVisitorData(t *testing.T) {
	f, addon, conn := addonAPI(t)
	hash := strings.Repeat("a", 40)
	if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie"}, Years: []int{2021}}, []string{"example movie"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one',jsonb_build_object('title','Example.Movie.2021','info_hash',$1::text,'imdb_id','tt1234567'))`, hash); err != nil {
		t.Fatal(err)
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer visitor-key-never-save" {
			t.Error("wrong playback account")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/user/me":
			fmt.Fprint(w, `{"success":true,"data":{"id":1,"email":"visitor-private@example.invalid"}}`)
		case "/torrents/checkcached":
			fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example Movie","size":100,"files":[{"id":99,"name":"Example.Movie.mp4","size":100}]}}}`, hash, hash)
		case "/torrents/createtorrent":
			_ = r.ParseMultipartForm(1 << 20)
			if r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") {
				t.Error("private source download allowed")
			}
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":"%s"}}`, hash)
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":"%s","download_finished":true,"download_present":true,"files":[{"id":3,"name":"Example.Movie.mp4","size":100}]}}`, hash)
		case "/torrents/requestdl":
			if r.URL.Query().Get("file_id") != "3" {
				t.Error("shared file ID reused as account file ID")
			}
			fmt.Fprint(w, `{"success":true,"data":"https://video.example/visitor-download"}`)
		default:
			t.Error("unexpected upstream operation")
		}
	}))
	defer upstream.Close()
	addon.Providers.TBBase = upstream.URL
	if err := f.db.SetPublicAccess(f.ctx, model.PublicAccess{Revision: 1, Enabled: true, StremioEnabled: true}); err != nil {
		t.Fatal(err)
	}
	configuration, err := addon.CreatePublicInstallation(f.ctx, "https://addon.example", map[string]string{"torbox": "visitor-key-never-save"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := f.request("GET", "/public/stremio/"+configuration+"~en~unknown/stream/movie/tt1234567.json", "", false, "")
	var payload struct {
		Streams []aiosource.Stream `json:"streams"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Streams) != 1 || payload.Streams[0].InfoHash != "" || len(payload.Streams[0].Sources) != 0 || payload.Streams[0].Hints.Indexer != "" {
		t.Fatal("cached public stream unavailable or exposes torrent credentials", response.Code)
	}
	play := f.request("GET", strings.TrimPrefix(payload.Streams[0].URL, "https://addon.example"), "", false, "")
	if play.Code != 307 || play.Header().Get("Location") != "https://video.example/visitor-download" {
		t.Fatal("public playback URL does not resolve", play.Code)
	}
	requests, err := f.db.ListStremioRequestsFiltered(f.ctx, model.ListOptions{}, "public", "playback")
	if err != nil || requests.Total != 1 {
		t.Fatal("playback missing from history", err)
	}
	detail, err := f.db.StremioRequest(f.ctx, requests.Items[0].ID)
	if err != nil || len(detail.Calls) != 4 || detail.Outcome != "results" {
		t.Fatal("playback calls or outcome missing", err)
	}
	for _, table := range []string{"stremio_requests", "operation_runs", "operation_events", "aio_shared_availability", "aio_availability", "aio_resources", "aio_cleanup", "activity_logs"} {
		rows, e := conn.Query(f.ctx, `SELECT row_to_json(t)::text FROM ingest.`+table+` t`)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var row string
			if e = rows.Scan(&row); e != nil {
				t.Fatal(e)
			}
			for _, private := range []string{"visitor-key-never-save", configuration, "visitor-private@", "visitor-download"} {
				if strings.Contains(row, private) {
					t.Fatal("visitor data persisted", table)
				}
			}
		}
		rows.Close()
	}
}

func TestPublicInstanceRoutesCiphertextAndAdministrationIsolation(t *testing.T) {
	f, addon, conn := addonAPI(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"success":true,"data":{"id":1}}`) }))
	defer upstream.Close()
	addon.Providers.TBBase = upstream.URL
	if response := f.request("GET", "/api/public-access", "", false, ""); response.Code != 401 {
		t.Fatal("public settings are not protected")
	}
	if response := f.request("PUT", "/api/public-access", `{"revision":1,"enabled":true,"stremio_enabled":true}`, true, ""); response.Code != 204 {
		t.Fatal("cannot enable public access", response.Code)
	}
	install := func(body string, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/public/install", strings.NewReader(body)).WithContext(f.ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	if response := install(`{"services":{"torbox":"visitor-key-do-not-store"}}`, "https://evil.example"); response.Code != 403 {
		t.Fatal("cross-origin key submission accepted")
	}
	if response := install(`{"services":{}}`, "https://addon.example"); response.Code != 400 {
		t.Fatal("missing service key accepted")
	}
	if response := install(`{"services":{"torbox":"visitor-key-do-not-store","torbox":"another-value"}}`, "https://addon.example"); response.Code != 400 {
		t.Fatal("duplicate key accepted")
	}
	if response := install(`{"services":{"unsupported":"visitor-key-do-not-store"}}`, "https://addon.example"); response.Code != 400 {
		t.Fatal("unsupported service accepted")
	}
	if response := install(`{"services":{"torbox":"`+strings.Repeat("a", 4096)+`"}}`, "https://addon.example"); response.Code != 400 {
		t.Fatal("oversized submission accepted")
	}
	response := install(`{"services":{"torbox":"visitor-key-do-not-store"}}`, "https://addon.example")
	var installed struct {
		URL string `json:"manifest_url"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &installed) != nil || installed.URL == "" || strings.Contains(installed.URL, "visitor-key") {
		t.Fatal("public installation failed", response.Code)
	}
	path := strings.TrimPrefix(installed.URL, "https://addon.example")
	manifest := f.request("GET", path, "", false, "")
	if manifest.Code != 200 || manifest.Header().Get("Cache-Control") != "no-store" || manifest.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("public manifest does not work")
	}
	streamPath := strings.Replace(path, "manifest.json", "stream/movie/tt1234567.json", 1)
	if response := f.request("OPTIONS", streamPath, "", false, ""); response.Code != 204 || response.Header().Get("Access-Control-Allow-Headers") != "Content-Type" {
		t.Fatal("public addon preflight is incompatible")
	}
	streams := f.request("GET", streamPath, "", false, "")
	if streams.Code != 200 || strings.TrimSpace(streams.Body.String()) != `{"streams":[]}` {
		t.Fatal("public stream route failed", streams.Code)
	}
	configuration := strings.Split(path, "/")[3]
	for _, route := range []string{"/api/providers", "/api/secrets", "/api/stremio/requests", "/api/logs", "/api/public-access", "/stremio/" + configuration + "/manifest.json"} {
		if value := f.request("GET", route, "", false, configuration); value.Code != 401 {
			t.Fatal("public URL grants wider permissions", route)
		}
	}
	var publicRows int
	if err := conn.QueryRow(f.ctx, `SELECT count(*) FROM ingest.stremio_requests WHERE origin='public'`).Scan(&publicRows); err != nil || publicRows != 2 {
		t.Fatal("requests absent from history", err)
	}
	filtered := f.request("GET", "/api/stremio/requests?origin=public&kind=manifest", "", true, "")
	if filtered.Code != 200 || !strings.Contains(filtered.Body.String(), `"total":1`) {
		t.Fatal("history filters failed")
	}
	if value := f.request("GET", "/api/stremio/requests?origin=unknown", "", true, ""); value.Code != 400 {
		t.Fatal("invalid origin accepted")
	}
	for _, table := range []string{"settings", "secrets", "aio_public_access", "stremio_requests", "activity_logs"} {
		rows, err := conn.Query(f.ctx, `SELECT row_to_json(t)::text FROM ingest.`+table+` t`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value string
			_ = rows.Scan(&value)
			if strings.Contains(value, "visitor-key-do-not-store") || strings.Contains(value, configuration) {
				t.Fatal("visitor credentials retained", table)
			}
		}
		rows.Close()
	}
	if value := f.request("PUT", "/api/public-access", `{"revision":2,"enabled":false}`, true, ""); value.Code != 204 {
		t.Fatal("cannot disable public access")
	}
	if value := f.request("GET", path, "", false, ""); value.Code != 401 {
		t.Fatal("disabled public manifest still accessible")
	}
	if value := f.request("PUT", "/api/public-access", `{"revision":3,"enabled":true,"stremio_enabled":true}`, true, ""); value.Code != 204 {
		t.Fatal("cannot reenable public access")
	}
	if value := f.request("GET", path, "", false, ""); value.Code != 401 {
		t.Fatal("old public manifest reactivated")
	}
	privateToken, _ := testPrivateInstallation(t, f.ctx, addon)
	if value := f.request("GET", "/stremio/"+privateToken+"/manifest.json", "", false, ""); value.Code != 200 {
		t.Fatal("public revocation broke private installation")
	}
}

func TestPublicAdmissionIsBoundedAndUntrustedHeadersCannotBypassIt(t *testing.T) {
	admission := &publicAdmission{}
	for i := 0; i < 13; i++ {
		r := httptest.NewRequest("POST", "/api/public/install", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i+10))
		w := httptest.NewRecorder()
		accepted := func() bool {
			release, ok := admission.enter(w, r, publicSetup)
			if ok {
				release()
			}
			return ok
		}()
		if accepted != (i < 12) {
			t.Fatal("setup limit bypassed")
		}
	}
}

func TestEnabledPublicInstallationCannotReadOrChangeAdministration(t *testing.T) {
	f, addon, _ := addonAPI(t)
	if err := f.db.SetPublicAccess(f.ctx, model.PublicAccess{Revision: 1, Enabled: true, StremioEnabled: true}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
	}))
	defer upstream.Close()
	addon.Providers.TBBase = upstream.URL
	configuration, err := addon.CreatePublicInstallation(f.ctx, "https://addon.example", map[string]string{"torbox": "synthetic-visitor-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/providers"}, {"GET", "/api/secrets"}, {"GET", "/api/raw/1/download"},
		{"GET", "/api/operations"}, {"GET", "/api/stremio/requests"}, {"GET", "/api/logs"},
		{"GET", "/api/security/sessions"}, {"GET", "/api/aiosource"}, {"GET", "/api/remotes"},
		{"PUT", "/api/public-access"}, {"PUT", "/api/aiosource/config"},
		{"POST", "/api/aiosource/token"}, {"POST", "/api/runs"}, {"PUT", "/api/providers/example"},
		{"DELETE", "/api/secrets/example"}, {"POST", "/api/security/mfa/enroll"},
		{"GET", "/stremio/" + configuration + "/manifest.json"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			response := f.request(route.method, route.path, `{}`, false, configuration)
			if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("public credential granted administrator access: %d", response.Code)
			}
			if strings.Contains(response.Body.String(), configuration) || response.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("public credential was reflected or administrator cookies were exposed to other origins")
			}
		})
	}
	access, err := f.db.PublicAccess(f.ctx)
	if err != nil || !access.Enabled || access.Revision != 2 {
		t.Fatal("unauthorized public requests changed administration state", err)
	}
}
