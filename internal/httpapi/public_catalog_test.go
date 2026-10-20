package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicSearchCredentialReusePrivacyAndDownloadReadiness(t *testing.T) {
	f, addon, conn := addonAPI(t)
	const key = "synthetic-public-search-key"
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var ready, deleted atomic.Bool
	var creations atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("Search used another account")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/user/me":
			fmt.Fprint(w, `{"success":true,"data":{"id":1,"email":"synthetic-private-email@example.invalid"}}`)
		case "/torrents/checkcached":
			fmt.Fprintf(w, `{"success":true,"data":{%q:{"hash":%q,"files":[{"name":"Example.Movie.2021.mkv","size":100}]}}}`, hash, hash)
		case "/torrents/createtorrent":
			creations.Add(1)
			if r.ParseMultipartForm(4096) != nil || r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") {
				t.Error("Private cached-only policy bypassed")
			}
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, hash)
		case "/torrents/mylist":
			if deleted.Load() {
				fmt.Fprint(w, `{"success":true,"error":"ITEM_NOT_FOUND","data":null}`)
				return
			}
			fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":%t,"download_present":%t,"files":[{"id":0,"name":"Example.Movie.2021.mkv","size":100}]}}`, hash, ready.Load(), ready.Load())
		case "/torrents/requestdl":
			fmt.Fprint(w, `{"success":true,"data":"https://files.example/ephemeral-visitor-download"}`)
		default:
			t.Error("Unexpected provider request", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer upstream.Close()
	addon.Providers.TBBase, addon.Providers.Client = upstream.URL, upstream.Client()
	if err := f.db.SetPublicAccess(f.ctx, model.PublicAccess{Revision: 1, Enabled: true}); err != nil {
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
		URL        string   `json:"manifest_url"`
		Credential string   `json:"credential"`
		Services   []string `json:"services"`
	}
	response := send("POST", "/api/public/install", `{"services":{"torbox":"`+key+`"}}`, "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &installed) != nil || len(installed.Services) != 1 || installed.Credential == "" {
		t.Fatal("Could not install Search services", response.Code, response.Body)
	}
	saved, err := addon.OpenPublicInstallation(f.ctx, "https://addon.example", installed.Credential)
	if err != nil || saved.Services["torbox"] != key || len(saved.Announces) != 0 {
		t.Fatal("Browser credential is not keys-only", err)
	}
	response = send("POST", "/api/public/install", `{"credential":"`+installed.Credential+`","services":{}}`, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), key) {
		t.Fatal("Saved credential could not be reused", response.Code)
	}
	if got := send("GET", "/api/public/catalog/search?q=Example", "", installed.Credential); got.Code != 404 {
		t.Fatal("Search should be opt-in", got.Code)
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.SearchEnabled = true
	if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if err = f.db.SaveAIOWork(f.ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie"}, Years: []int{2021}}, []string{"example movie"}); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
 ('hidden-source','native-id-private',jsonb_build_object('title','Example.Movie.2021','info_hash',$1::text,'imdb_id','tt1234567')),
 ('hidden-source','native-id-uncached','{"title":"Example.Movie.2021.other","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","imdb_id":"tt1234567"}')`, hash); err != nil {
		t.Fatal(err)
	}
	if err = addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	generation, _ := f.db.AIOCacheGeneration(f.ctx)
	for _, h := range []string{hash, strings.Repeat("b", 40)} {
		if err = f.db.SaveSharedAvailability(f.ctx, "torbox", model.AIOAvailability{Hash: h, Cached: h == hash, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Example.Movie.2021.mkv", Size: 100}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, credential := range []string{"", installed.Credential + "x"} {
		if got := send("GET", "/api/public/catalog/search?q=Example", "", credential); got.Code != 401 {
			t.Fatal("Invalid credential accepted", got.Code)
		}
	}
	response = send("GET", "/api/public/catalog/search?q=Example.Movie&year=2021", "", installed.Credential)
	var titles model.List[model.CatalogTitle]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &titles) != nil || titles.Total != 1 || len(titles.Items[0].Providers) != 0 {
		t.Fatal("Public offline search failed or exposed sources", response.Code, response.Body)
	}
	path := fmt.Sprintf("/api/public/catalog/%d", titles.Items[0].ID)
	response = send("GET", path+"/releases?cached_only=false", "", installed.Credential)
	if response.Code != 200 {
		_, detailErr := addon.PublicCatalogReleases(f.ctx, saved, titles.Items[0].ID, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
		t.Fatalf("Public release service: %v", detailErr)
	}
	var releases model.List[model.CatalogRelease]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &releases) != nil || releases.Total != 1 || len(releases.Items) != 1 || releases.Items[0].Provider != "" || releases.Items[0].Fingerprint != "" || releases.Items[0].DownloadToken == "" || releases.Items[0].Services[0].CanAdd {
		t.Fatal("Private uncached row or identity exposed", response.Code, response.Body)
	}
	for _, private := range []string{"hidden-source", "native-id", hash, key} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatal("Private release data exposed", private)
		}
	}
	for _, query := range []string{"provider=hidden-source", "sort=source"} {
		if got := send("GET", path+"/releases?"+query, "", installed.Credential); got.Code != 422 {
			t.Fatal("Unconsented source filter accepted", got.Code)
		}
	}
	body, _ := json.Marshal(model.CatalogDownloadInput{DownloadToken: releases.Items[0].DownloadToken, Service: "torbox", AllowUncached: true})
	response = send("POST", path+"/download", string(body), installed.Credential)
	var pending model.CatalogDownload
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &pending) != nil || pending.PreparationID == "" || pending.PreparationID == "7" || pending.OperationID == "" {
		t.Fatal("Safe preparation missing", response.Code, response.Body)
	}
	continuation, _ := json.Marshal(model.CatalogDownloadInput{PreparationID: pending.PreparationID})
	ready.Store(true)
	response = send("POST", path+"/download", string(continuation), installed.Credential)
	var download model.CatalogDownload
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &download) != nil || download.OperationID != pending.OperationID || !strings.HasPrefix(download.URL, "/api/public/catalog-downloads/") || creations.Load() != 1 {
		t.Fatal("Public readiness lost the original operation", response.Code, response.Body, creations.Load())
	}
	deleted.Store(true)
	response = send("POST", path+"/download", string(continuation), installed.Credential)
	if response.Code != 404 || creations.Load() != 1 {
		t.Fatal("Deleted provider file retried or became 502", response.Code)
	}
	var stored int
	if err = conn.QueryRow(f.ctx, `SELECT count(*) FROM ingest.operation_downloads`).Scan(&stored); err != nil || stored != 0 {
		t.Fatal("Visitor continuation persisted", err)
	}
	detail := f.request("GET", "/api/operations/"+pending.OperationID, "", true, "")
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"trigger":"public"`) || strings.Contains(detail.Body.String(), `"download":`) {
		t.Fatal("Public download history is inconsistent", detail.Code)
	}
	for _, table := range []string{"operation_runs", "operation_events", "activity_logs", "aio_shared_availability", "secrets"} {
		rows, e := conn.Query(f.ctx, `SELECT row_to_json(t)::text FROM ingest.`+table+` t`)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var text string
			if e = rows.Scan(&text); e != nil {
				t.Fatal(e)
			}
			for _, private := range []string{key, installed.Credential, pending.PreparationID, "ephemeral-visitor-download", "synthetic-private-email"} {
				if strings.Contains(text, private) {
					t.Fatal("Visitor data saved", table)
				}
			}
		}
		rows.Close()
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.SearchEnabled = false
	if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if got := send("POST", path+"/download", string(continuation), installed.Credential); got.Code != 404 {
		t.Fatal("Search revocation ineffective", got.Code)
	}
	if _, err = addon.OpenPublicInstallation(f.ctx, "https://addon.example", installed.Credential); err != nil {
		t.Fatal("Turning off Search broke Stremio", err)
	}
}

func TestPublicSearchRechecksPermissionsBeforeReturningSlowResults(t *testing.T) {
	f, addon, _ := addonAPI(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"success":true,"data":{"id":1}}`) }))
	defer upstream.Close()
	addon.Providers.TBBase = upstream.URL
	auth, err := newAuthentication("synthetic-admin-password", "https://addon.example", f.db, addon.Vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"search", "sources", "generation"} {
		t.Run(change, func(t *testing.T) {
			access, _ := f.db.PublicAccess(f.ctx)
			access.Enabled, access.SearchEnabled, access.ExposeSources = true, true, true
			if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
				t.Fatal(err)
			}
			credential, err := addon.CreatePublicInstallation(f.ctx, "https://addon.example", map[string]string{"torbox": "synthetic-race-key"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			s := &server{public: &publicAdmission{}, auth: auth, options: Options{Store: f.db, AIOSource: addon, Vault: addon.Vault}}
			handler := s.publicCatalogHandler(time.Second, func(w http.ResponseWriter, r *http.Request, _ *aiosource.PublicInstallation) (any, error) {
				current, _ := f.db.PublicAccess(r.Context())
				switch change {
				case "search":
					current.SearchEnabled = false
				case "sources":
					current.ExposeSources = false
				case "generation":
					current.Enabled = false
				}
				if err := f.db.SetPublicAccess(r.Context(), current); err != nil {
					t.Fatal(err)
				}
				if change == "generation" {
					current, _ = f.db.PublicAccess(r.Context())
					current.Enabled = true
					if err := f.db.SetPublicAccess(r.Context(), current); err != nil {
						t.Fatal(err)
					}
				}
				return map[string]string{"source": "must-not-escape"}, nil
			})
			req := httptest.NewRequest("GET", "/api/public/catalog/search?q=Example", nil).WithContext(f.ctx)
			req.Header.Set("Authorization", "Bearer "+credential)
			out := httptest.NewRecorder()
			handler(out, req)
			want := 404
			if change == "sources" {
				want = 409
			} else if change == "generation" {
				want = 401
			}
			if out.Code != want || strings.Contains(out.Body.String(), "must-not-escape") {
				t.Fatal("Late public revocation ignored", out.Code)
			}
		})
	}
}

func TestPublicSearchFileTicketsExpireAndRecheckAccessWithoutForwardingKeys(t *testing.T) {
	f, addon, _ := addonAPI(t)
	auth, err := newAuthentication("synthetic-admin-password", "https://addon.example", f.db, addon.Vault)
	if err != nil {
		t.Fatal(err)
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled, access.SearchEnabled = true, true
	if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = f.db.PublicAccess(f.ctx)
	calls := 0
	withdraw := false
	s := &server{public: &publicAdmission{}, auth: auth, options: Options{Store: f.db, AIOSource: addon, Vault: addon.Vault}, files: make(chan struct{}, 1), fileClient: &http.Client{Transport: catalogFileRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
			t.Error("Private browser headers forwarded")
		}
		if withdraw {
			current, _ := f.db.PublicAccess(r.Context())
			current.SearchEnabled = false
			if err := f.db.SetPublicAccess(r.Context(), current); err != nil {
				t.Fatal(err)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Disposition": []string{`inline; filename="example.mkv"`}, "Set-Cookie": []string{"private-cookie=never-share"}}, Body: io.NopCloser(strings.NewReader("file-body"))}, nil
	})}}
	ticket := publicCatalogFileTicket{Service: "torbox", URL: "https://cdn.example/visitor-secret-download", Audience: "https://addon.example", Generation: access.Generation, Expires: time.Now().Add(time.Minute).Unix()}
	request := func(value publicCatalogFileTicket, purpose string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(value)
		ciphertext, e := addon.Vault.Seal(purpose, data)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest("GET", "/api/public/catalog-downloads/ticket", nil).WithContext(f.ctx)
		req.SetPathValue("token", base64.RawURLEncoding.EncodeToString(ciphertext))
		req.Header.Set("Authorization", "Bearer synthetic-visitor-key")
		req.AddCookie(f.cookie)
		out := httptest.NewRecorder()
		s.publicCatalogFile(out, req)
		return out
	}
	good := request(ticket, "public-catalog-file-v1")
	if good.Code != 200 || good.Body.String() != "file-body" || !strings.HasPrefix(good.Header().Get("Content-Disposition"), "attachment;") || good.Header().Get("Set-Cookie") != "" {
		t.Fatal("Public browser attachment failed", good.Code)
	}
	for _, value := range []publicCatalogFileTicket{func() publicCatalogFileTicket { v := ticket; v.Expires = 1; return v }(), func() publicCatalogFileTicket { v := ticket; v.Generation++; return v }(), func() publicCatalogFileTicket { v := ticket; v.Audience = "https://other.example"; return v }()} {
		if got := request(value, "public-catalog-file-v1"); got.Code != 404 {
			t.Fatal("Invalid file ticket accepted", got.Code)
		}
	}
	if got := request(ticket, "public-installation-v1"); got.Code != 404 || calls != 1 {
		t.Fatal("File capability crossed purposes", got.Code, calls)
	}
	withdraw = true
	if got := request(ticket, "public-catalog-file-v1"); got.Code != 404 || strings.Contains(got.Body.String(), "file-body") {
		t.Fatal("File delivery outlived access", got.Code)
	}
	if got := request(ticket, "public-catalog-file-v1"); got.Code != 404 || calls != 2 {
		t.Fatal("Revoked download contacted the file host", got.Code, calls)
	}
}
