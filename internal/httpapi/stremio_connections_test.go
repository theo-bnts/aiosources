package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func createStremioAccess(t *testing.T, f sharingAPIFixture, input model.ShareInput) model.ShareCredential {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	w := f.request("POST", "/api/stremio/connections", string(data), true, "")
	var grant model.ShareCredential
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &grant) != nil {
		t.Fatal("could not create Stremio access", w.Code, w.Body.String())
	}
	return grant
}

func TestStremioSavedInstallationsResolveBothPlaybackProviders(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			f, addon, conn := addonAPI(t)
			hash := strings.Repeat("a", 40)
			if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie"}, Years: []int{2021}}, []string{"example movie"}); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one',jsonb_build_object('title','Example.Movie.2021.FRENCH','info_hash',$1::text,'imdb_id','tt1234567'))`, hash); err != nil {
				t.Fatal(err)
			}
			if err := addon.Match(f.ctx); err != nil {
				t.Fatal(err)
			}
			config, _ := f.db.AIOConfig(f.ctx)
			config.Trackers = map[string]model.AIOTracker{"source": {Private: false}}
			if service == "alldebrid" {
				config.AllDebridSecret = "exampleKey"
			} else {
				config.TorBoxSecret = "exampleKey"
			}
			if err := addon.Vault.Put(f.ctx, "exampleKey", "synthetic-admin-provider-key"); err != nil {
				t.Fatal(err)
			}
			if err := f.db.SetAIOConfig(f.ctx, config); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer synthetic-admin-provider-key" {
					t.Error("wrong private playback account")
					w.WriteHeader(401)
					return
				}
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
				case "/v4/magnet/upload":
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s","ready":true}]}}`, hash)
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":77,"files":[{"n":"Example.Movie.mp4","s":100,"l":"https://provider.example/file"}]}]}}`)
				case "/v4/link/unlock":
					fmt.Fprint(w, `{"status":"success","data":{"link":"https://video.example/ready"}}`)
				case "/torrents/createtorrent":
					fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":"%s"}}`, hash)
				case "/torrents/mylist":
					fmt.Fprintf(w, `{"success":true,"data":{"hash":"%s","download_finished":true,"download_present":true,"files":[{"id":3,"name":"Example.Movie.mp4","size":100}]}}`, hash)
				case "/torrents/requestdl":
					fmt.Fprint(w, `{"success":true,"data":"https://video.example/ready"}`)
				default:
					t.Error("unexpected provider request", r.URL.Path)
					w.WriteHeader(400)
				}
			}))
			defer upstream.Close()
			addon.Providers.ADBase, addon.Providers.TBBase = upstream.URL, upstream.URL
			grant := createStremioAccess(t, f, model.ShareInput{Name: "Example playback", Scope: "all", Enabled: true, StreamPreferences: &model.StreamPreferences{Language: "fr", Mode: "preferred", Services: []string{service}}})
			path := stremioAccessPath(grant) + "/play/" + service + "/source/one/movie/tt1234567"
			w := f.request("GET", path, "", false, "")
			if w.Code != 307 || w.Header().Get("Location") != "https://video.example/ready" || calls.Load() == 0 {
				t.Fatal("saved access cannot resolve playback", w.Code, w.Body.String())
			}
			input := model.ShareInput{Format: "stremio", Name: grant.Share.Name, Scope: "all", Fields: grant.Share.Fields, Enabled: true, Revision: grant.Share.Revision, RequestsPerMinute: &grant.Share.RequestsPerMinute, MaxConcurrentDownloads: &grant.Share.MaxConcurrentDownloads, StreamPreferences: &model.StreamPreferences{Language: "de", Mode: "preferred", Services: []string{service}}}
			if _, err := f.db.UpdateShare(f.ctx, grant.Share.ID, input); err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			if w = f.request("GET", path, "", false, ""); w.Code != 422 || calls.Load() != before {
				t.Fatal("stale playback URL bypassed current filters", w.Code)
			}
		})
	}
}

func stremioAccessPath(g model.ShareCredential) string {
	return "/stremio/s1." + g.Share.ID + "." + g.Password
}

func TestStremioDeliveryRechecksConcurrentRevocationAndClosesItsLease(t *testing.T) {
	f, addon, conn := addonAPI(t)
	grant := createStremioAccess(t, f, model.ShareInput{Name: "Example", Scope: "all", Enabled: true, StreamPreferences: &model.StreamPreferences{}})
	s := &server{options: Options{AIOSource: addon, Store: f.db}, streams: make(chan struct{}, 64), streamAuth: make(chan struct{}, 64)}
	handler := s.aioProtect(func(w http.ResponseWriter, r *http.Request) {
		if err := f.db.DeleteShare(f.ctx, grant.Share.ID, grant.Share.Revision); err != nil {
			t.Fatal(err)
		}
		if validatePrivateConnection(w, r) {
			w.Header().Set("Location", "https://video.example/should-not-be-delivered")
			w.WriteHeader(307)
		}
	})
	r := httptest.NewRequest("GET", stremioAccessPath(grant)+"/manifest.json", nil).WithContext(f.ctx)
	r.SetPathValue("token", strings.TrimPrefix(stremioAccessPath(grant), "/stremio/"))
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != 401 || w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), grant.Password) {
		t.Fatal("revocation during a request did not block delivery", w.Code)
	}
	var active int
	if err := conn.QueryRow(f.ctx, `SELECT count(*) FROM ingest.share_requests WHERE lease_until>clock_timestamp()`).Scan(&active); err != nil || active != 0 {
		t.Fatal("request lease leaked", active, err)
	}
	w = httptest.NewRecorder()
	respondError(w, aiosource.ErrAuth)
	if w.Code != 401 {
		t.Fatal("authorization changes during a request are reported as server failures")
	}
}

func TestStremioConnectionsSaveIndependentSettingsAndPreserveLegacyLinks(t *testing.T) {
	f, addon, conn := addonAPI(t)
	for _, source := range []string{"alpha", "beta"} {
		definition := fmt.Sprintf(`{"version":1,"id":%q,"name":%q,"adapter":"http_json","url":"https://source.invalid/api","enabled":false}`, source, "Example "+source)
		body, _ := json.Marshal(map[string]any{"revision": "", "json": definition})
		if w := f.request("PUT", "/api/providers/"+source, string(body), true, ""); w.Code != 200 {
			t.Fatal("source fixture", w.Code)
		}
	}
	if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie"}, Years: []int{2021}}, []string{"example movie"}); err != nil {
		t.Fatal(err)
	}
	for i, row := range []struct{ provider, id, language string }{{"alpha", "one", "FRENCH"}, {"alpha", "two", "GERMAN"}, {"beta", "one", "FRENCH"}} {
		if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES($1,$2,jsonb_build_object('title',$3::text,'info_hash',$4::text,'imdb_id','tt1234567','size',$5::bigint))`, row.provider, row.id, "Example.Movie.2021."+row.language, strings.Repeat(string(rune('a'+i)), 40), i+1); err != nil {
			t.Fatal(err)
		}
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	input := model.ShareInput{Name: "Example installation", Enabled: true, Scope: "selected", SourceIDs: []string{"alpha"}, StreamPreferences: &model.StreamPreferences{Language: "fr", Mode: "preferred", Types: []string{"movie"}, Services: []string{"direct"}}}
	grant := createStremioAccess(t, f, input)
	path := stremioAccessPath(grant)
	manifest := func(path string) map[string]any {
		t.Helper()
		w := f.request("GET", path+"/manifest.json", "", false, "")
		var result map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("manifest unavailable or cacheable", w.Code)
		}
		return result
	}
	read := func(want int) {
		t.Helper()
		w := f.request("GET", path+"/stream/movie/tt1234567.json", "", false, "")
		var result struct {
			Streams []aiosource.Stream `json:"streams"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Streams) != want {
			t.Fatal("saved filters not applied", w.Code, len(result.Streams), want)
		}
	}
	firstID := manifest(path)["id"]
	other := createStremioAccess(t, f, input)
	if firstID == manifest(stremioAccessPath(other))["id"] {
		t.Fatal("independent installations overwrite each other in Stremio")
	}
	read(1)
	input.Format, input.Revision = "stremio", grant.Share.Revision
	input.RequestsPerMinute, input.MaxConcurrentDownloads = &grant.Share.RequestsPerMinute, &grant.Share.MaxConcurrentDownloads
	input.StreamPreferences.Mode = "all"
	update := func() {
		t.Helper()
		body, _ := json.Marshal(input)
		w := f.request("PUT", "/api/stremio/connections/"+grant.Share.ID, string(body), true, "")
		var current model.Share
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &current) != nil {
			t.Fatal("could not update saved settings", w.Code, w.Body.String())
		}
		input.Revision = current.Revision
	}
	update()
	read(2)
	input.StreamPreferences.Types = []string{"series"}
	update()
	read(0)
	input.StreamPreferences.Types = []string{"movie"}
	update()
	read(2)
	if firstID != manifest(path)["id"] {
		t.Fatal("changing settings requires reinstalling the manifest")
	}
	input.SourceIDs = []string{"beta"}
	update()
	read(1)
	for _, suffix := range []string{"/play/torbox/alpha/one/movie/tt1234567", "/play/torbox/beta/one/movie/tt1234567"} {
		if w := f.request("GET", path+suffix, "", false, ""); w.Code != 422 {
			t.Fatal("playback bypassed source or service filters", w.Code)
		}
	}
	if w := f.request("GET", path+"~fr~all/manifest.json", "", false, ""); w.Code != 401 {
		t.Fatal("URL settings replaced database permissions", w.Code)
	}
	legacy, err := addon.Token(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	manifest("/stremio/" + legacy + "~fr~unknown")
	if _, err = addon.RotateToken(f.ctx); err != nil {
		t.Fatal(err)
	}
	manifest(path)
	manifest(stremioAccessPath(other))
	if w := f.request("GET", "/stremio/"+legacy+"/manifest.json", "", false, ""); w.Code != 401 {
		t.Fatal("legacy rotation stopped revoking the legacy token")
	}
	for _, endpoint := range []string{"/api/shares", "/api/torznab/endpoints"} {
		if w := f.request("GET", endpoint, "", true, ""); w.Code != 200 || strings.Contains(w.Body.String(), grant.Share.ID) {
			t.Fatal("installation listed under another protocol")
		}
	}
	for _, endpoint := range []string{"/api/stremio/connections", "/api/providers", "/api/secrets"} {
		if w := f.request("GET", endpoint, "", false, grant.Password); w.Code != 401 {
			t.Fatal("installation authorized administration", w.Code)
		}
	}
	access, err := f.db.PublicAccess(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	access.Enabled, access.TorznabEnabled = true, true
	if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/torznab/" + grant.Share.ID + "/api?t=caps&apikey=" + grant.Password, "/api/catalogs/" + grant.Share.ID, "/public" + path + "/manifest.json", "/public/torznab/api?t=caps&apikey=" + grant.Password} {
		if w := f.request("GET", endpoint, "", false, grant.Password); w.Code != 401 {
			t.Fatal("installation authorized another protocol", w.Code)
		}
	}
	for _, endpoint := range []string{"/api/shares/", "/api/torznab/endpoints/"} {
		if w := f.request("POST", endpoint+grant.Share.ID+"/rotate", `{"revision":1}`, true, ""); w.Code != 404 {
			t.Fatal("another protocol changed this installation", w.Code)
		}
	}
	listed := f.request("GET", "/api/stremio/connections", "", true, "")
	if listed.Code != 200 || strings.Contains(listed.Body.String(), grant.Password) || strings.Contains(listed.Body.String(), other.Password) {
		t.Fatal("saved access revealed a credential")
	}
	rotated := f.request("POST", "/api/stremio/connections/"+grant.Share.ID+"/rotate", fmt.Sprintf(`{"revision":%d}`, input.Revision), true, "")
	var replacement model.ShareCredential
	if rotated.Code != 200 || json.Unmarshal(rotated.Body.Bytes(), &replacement) != nil {
		t.Fatal("rotation failed", rotated.Code)
	}
	if w := f.request("GET", path+"/manifest.json", "", false, ""); w.Code != 401 {
		t.Fatal("old installation survived rotation")
	}
	path = stremioAccessPath(replacement)
	manifest(path)
	manifest(stremioAccessPath(other))
	removed := f.request("DELETE", "/api/stremio/connections/"+grant.Share.ID, fmt.Sprintf(`{"revision":%d}`, replacement.Share.Revision), true, "")
	if removed.Code != 204 || f.request("GET", path+"/manifest.json", "", false, "").Code != 401 {
		t.Fatal("revocation did not remove access")
	}
	manifest(stremioAccessPath(other))
	var retained bool
	if err = conn.QueryRow(f.ctx, `SELECT enabled FROM ingest.catalog_tracking`).Scan(&retained); err != nil || retained {
		t.Fatal("Stremio enabled catalog sharing retention", err)
	}
}

func TestStremioConnectionsRejectInvalidSettingsAndRespectQuotas(t *testing.T) {
	f, _, _ := addonAPI(t)
	for _, prefs := range []string{`null`, `{"services":["other"]}`, `{"sources":["alpha"]}`, `{"mode":"preferred"}`, `{"mode":"all","key":"private"}`} {
		w := f.request("POST", "/api/stremio/connections", `{"name":"Example","scope":"all","stream_preferences":`+prefs+`}`, true, "")
		if w.Code < 400 || w.Code >= 500 {
			t.Fatal("invalid stored preferences accepted", w.Code)
		}
	}
	if w := f.request("POST", "/api/stremio/connections", `{"name":"Example","scope":"all","stream_preferences":{}}`, false, ""); w.Code != 401 {
		t.Fatal("unauthenticated create", w.Code)
	}
	r := httptest.NewRequest("POST", "/api/stremio/connections", strings.NewReader(`{"name":"Example","scope":"all","stream_preferences":{}}`)).WithContext(f.ctx)
	r.AddCookie(f.cookie)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("create did not require CSRF", w.Code)
	}
	limit := 1
	grant := createStremioAccess(t, f, model.ShareInput{Name: "Limited", Scope: "all", Enabled: true, RequestsPerMinute: &limit, StreamPreferences: &model.StreamPreferences{}})
	path := stremioAccessPath(grant) + "/manifest.json"
	if w := f.request("GET", path, "", false, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := f.request("GET", path, "", false, ""); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("rolling quota not enforced", w.Code)
	}
	expired := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, input := range []model.ShareInput{
		{Name: "Disabled", Scope: "all", StreamPreferences: &model.StreamPreferences{}},
		{Name: "Expired", Scope: "all", Enabled: true, StreamPreferences: &model.StreamPreferences{}, ExpiresAt: &expired},
	} {
		grant := createStremioAccess(t, f, input)
		for _, resource := range []string{"/manifest.json", "/stream/movie/tt1234567.json", "/play/torbox/alpha/one/movie/tt1234567"} {
			if w := f.request("GET", stremioAccessPath(grant)+resource, "", false, ""); w.Code != 401 {
				t.Fatal("disabled or expired access worked", w.Code)
			}
		}
	}
	local := sharingAPI(t, "")
	grant = createStremioAccess(t, local, model.ShareInput{Name: "Local network", Scope: "all", Enabled: true, StreamPreferences: &model.StreamPreferences{}})
	if grant.Share.StreamPreferences.Mode != "all" {
		t.Fatal("default saved filters do not use an explicit language mode")
	}
	if grant.Share.URL != "/stremio/s1."+grant.Share.ID {
		t.Fatal("local Stremio installation requires a public address")
	}
}
