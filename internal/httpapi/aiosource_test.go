package httpapi

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"github.com/theo-bnts/aiosources/internal/vault"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func addonAPI(t *testing.T) (sharingAPIFixture, *aiosource.Service, *pgx.Conn) {
	t.Helper()
	ctx, dsn := testutil.NewDatabase(t)
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	registry, err := providers.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	v, err := vault.New(ctx, db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	config := store.DefaultAIOConfig()
	config.AutoSync, config.AutoMatch = false, false
	if err := db.SetAIOConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	addon, err := aiosource.New(ctx, db, v, registry)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = addon.Close(ctx)
	})
	handler, err := New(Options{Store: db, Providers: registry, Jobs: jobs.New(db, registry, v.Resolve, 1), Vault: v, AIOSource: addon, AdminPassword: "synthetic-admin-password", PublicURL: "https://addon.example", Frontend: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<title>AIOSources</title>")}}})
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"password":"synthetic-admin-password"}`)).WithContext(ctx)
	login.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &session) != nil || len(w.Result().Cookies()) != 1 {
		t.Fatal("login failed")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return sharingAPIFixture{ctx: ctx, db: db, handler: handler, cookie: w.Result().Cookies()[0], csrf: session.CSRF}, addon, conn
}

func TestInstallationAuthenticationIsBoundedBeforeDatabaseAccess(t *testing.T) {
	f, addon, control := addonAPI(t)
	token, err := testPrivateInstallation(t, f.ctx, addon)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := control.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(f.ctx, `LOCK TABLE ingest.shares IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	s := &server{options: Options{AIOSource: addon, Store: f.db}, streams: make(chan struct{}, 64), streamAuth: make(chan struct{}, 64)}
	var protected atomic.Int32
	handler := s.aioProtect(func(w http.ResponseWriter, _ *http.Request) {
		protected.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	request := func(token string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/stremio/"+token+"/manifest.json", nil).WithContext(f.ctx)
		r.SetPathValue("token", token)
		return r
	}
	results := make(chan int, 64)
	defer func() {
		_ = tx.Rollback(context.Background())
		for range 64 {
			if status := <-results; status != http.StatusUnauthorized {
				t.Errorf("invalid installation was not rejected: %d", status)
			}
		}
	}()
	for range 64 {
		go func() {
			w := httptest.NewRecorder()
			handler(w, request(token[:strings.LastIndex(token, ".")+1]+strings.Repeat("A", 43)))
			results <- w.Code
		}()
	}
	deadline, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	for len(s.streamAuth) != 64 {
		select {
		case <-time.After(time.Millisecond):
		case <-deadline.Done():
			t.Fatal("installation checks did not reach the blocked database boundary")
		}
	}
	excess := httptest.NewRecorder()
	handler(excess, request(token[:strings.LastIndex(token, ".")+1]+strings.Repeat("A", 43)))
	if excess.Code != http.StatusTooManyRequests || excess.Header().Get("Retry-After") == "" || excess.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("excess installation checks entered the database queue: %d", excess.Code)
	}
	if protected.Load() != 0 {
		t.Fatal("an unauthorized installation reached protected addon work")
	}
	if err := tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	for len(s.streamAuth) != 0 {
		select {
		case <-time.After(time.Millisecond):
		case <-deadline.Done():
			t.Fatal("installation admission was not released after the database recovered")
		}
	}
	valid := httptest.NewRecorder()
	handler(valid, request(token))
	if valid.Code != http.StatusNoContent || protected.Load() != 1 {
		t.Fatal("admission protection changed valid installation behavior")
	}
}

func TestInstallationStorageTimeoutIsRetryableWithoutAuthorizingPlayback(t *testing.T) {
	f, addon, control := addonAPI(t)
	token, err := testPrivateInstallation(t, f.ctx, addon)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := control.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(f.ctx, `LOCK TABLE ingest.shares IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	s := &server{options: Options{AIOSource: addon, Store: f.db}, streams: make(chan struct{}, 64), streamAuth: make(chan struct{}, 64)}
	protected := false
	handler := s.aioProtect(func(w http.ResponseWriter, _ *http.Request) {
		protected = true
		w.WriteHeader(http.StatusNoContent)
	})
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/stremio/"+token+"/manifest.json", nil).WithContext(f.ctx)
		r.SetPathValue("token", token)
		return r
	}
	started := time.Now()
	w := httptest.NewRecorder()
	handler(w, request())
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" || protected || len(s.streamAuth) != 0 || time.Since(started) > 7*time.Second {
		t.Fatalf("unavailable storage invalidated a valid token, leaked admission, or allowed playback: %d", w.Code)
	}
	if err := tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	handler(w, request())
	if w.Code != http.StatusNoContent || !protected {
		t.Fatal("valid installation did not recover with the database")
	}
}
func TestStremioAuthenticationCORSAndInstallation(t *testing.T) {
	f, addon, _ := addonAPI(t)
	if response := f.request("GET", "/api/aiosource", "", false, ""); response.Code != 401 {
		t.Fatal("admin API is public")
	}
	status := f.request("GET", "/api/aiosource", "", true, "")
	var payload struct {
		ManifestPath string `json:"manifest_path"`
	}
	if status.Code != 200 || json.Unmarshal(status.Body.Bytes(), &payload) != nil || payload.ManifestPath != "" {
		t.Fatal("status failed")
	}
	token, _ := testPrivateInstallation(t, f.ctx, addon)
	manifest := f.request("GET", "/stremio/"+token+"/manifest.json", "", false, "")
	if manifest.Code != 200 || manifest.Header().Get("Access-Control-Allow-Origin") != "*" || manifest.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("manifest contract %d %+v", manifest.Code, manifest.Header())
	}
	var m struct {
		ID        string
		Logo      string
		Version   string
		Resources []any
	}
	if json.Unmarshal(manifest.Body.Bytes(), &m) != nil || !strings.HasPrefix(m.ID, "org.aiosource.v2.connection.") || len(m.Resources) != 1 || m.Logo != aiosource.LogoURL || m.Version != aiosource.Version {
		t.Fatal("invalid manifest")
	}
	if response := f.request("GET", "/stremio/invalid/manifest.json", "", false, ""); response.Code != 401 {
		t.Fatal("invalid token accepted")
	}
	empty := f.request("GET", "/stremio/"+token+"/stream/movie/tt1234567.json", "", false, "")
	if empty.Code != 200 || strings.TrimSpace(empty.Body.String()) != `{"streams":[]}` {
		t.Fatalf("empty stream %d %s", empty.Code, empty.Body.String())
	}
	for _, path := range []string{"/addon", "/metadata", "/debrid"} {
		if response := f.request("GET", path, "", false, ""); response.Code != 200 {
			t.Errorf("panel route %s: %d", path, response.Code)
		}
	}
	if response := f.request("OPTIONS", "/stremio/"+token+"/stream/movie/tt1.json", "", false, ""); response.Code != 204 {
		t.Fatal("preflight failed")
	}
	request := httptest.NewRequest("POST", "/api/aiosource/token", nil).WithContext(f.ctx)
	request.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("token rotation bypassed CSRF")
	}
	if response := f.request("POST", "/api/aiosource/token", "", true, ""); response.Code != 404 {
		t.Fatal("retired token creation endpoint remains available")
	}
}

func TestIdentificationDiagnosticsRequireAdministratorSessionAndValidateFilters(t *testing.T) {
	f, _, _ := addonAPI(t)
	if response := f.request("GET", "/api/aiosource/matches", "", false, ""); response.Code != 401 {
		t.Fatal("identification diagnostics are public")
	}
	for _, query := range []string{"status=unknown", "limit=0", "offset=-1", "provider=" + strings.Repeat("x", 129)} {
		if response := f.request("GET", "/api/aiosource/matches?"+query, "", true, ""); response.Code != 400 {
			t.Fatalf("invalid filter %s: %d", query, response.Code)
		}
	}
	response := f.request("GET", "/api/aiosource/matches?status=pending", "", true, "")
	var list model.List[model.AIOMatchDiagnostic]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || list.Total != 0 || list.Items == nil {
		t.Fatalf("empty diagnostics: %d %s", response.Code, response.Body.String())
	}
}

func TestPrivateAnnounceConfigurationContainsOnlyProtectedSecretReferences(t *testing.T) {
	f, addon, _ := addonAPI(t)
	const value = "https://tracker.example/synthetic%2Fkey/announce?passkey=synthetic%2Bkey&source=one"
	if err := addon.Vault.Put(f.ctx, "tracker_url", value); err != nil {
		t.Fatal(err)
	}
	c, _ := f.db.AIOConfig(f.ctx)
	c.Trackers["source"] = model.AIOTracker{Private: true, AnnounceSecret: "tracker_url"}
	body, _ := json.Marshal(c)
	if response := f.request("PUT", "/api/aiosource/config", string(body), true, ""); response.Code != 204 {
		t.Fatalf("configuration save: %d", response.Code)
	}
	response := f.request("GET", "/api/aiosource", "", true, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"announce_secret":"tracker_url"`) || strings.Contains(response.Body.String(), "tracker.example") || strings.Contains(response.Body.String(), "synthetic%") {
		t.Fatal("admin status exposed announce credentials")
	}
	if response := f.request("DELETE", "/api/secrets/tracker_url", `{"revision":1}`, true, ""); response.Code != 409 {
		t.Fatal("referenced tracker URL could be deleted")
	}
}
func TestStremioLocalStreamAndDeferredProviderPlayback(t *testing.T) {
	for _, scenario := range []struct {
		name, service string
		freshUpload   bool
	}{
		{"alldebrid_existing", "alldebrid", false},
		{"alldebrid_upload", "alldebrid", true},
		{"torbox", "torbox", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service := scenario.service
			f, addon, conn := addonAPI(t)
			hash := strings.Repeat("a", 40)
			work := model.AIOWork{Type: "tv", TMDB: "202", IMDb: "tt7654321", Titles: []string{"Example Serial"}, Years: []int{2020}}
			if err := f.db.SaveAIOWork(f.ctx, work, []string{"example serial"}); err != nil {
				t.Fatal(err)
			}
			fields := `{"title":"Example.Serial.S01.COMPLETE.2020.1080p","info_hash":"` + hash + `","size":100}`
			if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','https://source.invalid/torrent/1',$1)`, fields); err != nil {
				t.Fatal(err)
			}
			if err := addon.Match(f.ctx); err != nil {
				t.Fatal(err)
			}
			const key = "synthetic-provider-key"
			if err := addon.Vault.Put(f.ctx, "debrid", key); err != nil {
				t.Fatal(err)
			}
			c, _ := f.db.AIOConfig(f.ctx)
			c.AutoMatch = false
			if service == "alldebrid" {
				c.AllDebridSecret = "debrid"
			} else {
				c.TorBoxSecret = "debrid"
			}
			if err := addon.SaveConfig(f.ctx, c); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			var playback atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					if scenario.freshUpload && playback.Load() {
						w.Write([]byte(`{"status":"success","data":{"magnets":[]}}`))
					} else {
						w.Write([]byte(`{"status":"success","data":{"magnets":[{"id":42,"hash":"` + hash + `","statusCode":4}]}}`))
					}
				case "/v4/magnet/upload":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if !strings.HasPrefix(r.Form.Get("magnets[]"), "magnet:?xt=urn:btih:"+hash+"&") {
						w.Write([]byte(`{"status":"success","data":{"magnets":[{"error":{"code":"MAGNET_INVALID_URI"}}]}}`))
						return
					}
					w.Write([]byte(`{"status":"success","data":{"magnets":[{"id":42,"hash":"` + hash + `","ready":true}]}}`))
				case "/v4/magnet/files":
					w.Write([]byte(`{"status":"success","data":{"magnets":[{"id":"42","files":[{"n":"Example.S01E02.mkv","s":100,"l":"https://files.example/episode2"},{"n":"Example.S01E01.mkv","s":999,"l":"https://files.example/episode1"}]}]}}`))
				case "/v4/link/unlock":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("link") != "https://files.example/episode2" {
						t.Error("wrong episode played")
					}
					w.Write([]byte(`{"status":"success","data":{"link":"https://video.example/episode2.mkv"}}`))
				case "/torrents/checkcached":
					w.Write([]byte(`{"success":true,"data":{"` + hash + `":{"hash":"` + hash + `","name":"Example","size":100,"files":[{"id":0,"name":"Example.S01E02.mkv","size":100}]}}}`))
				case "/torrents/createtorrent":
					w.Write([]byte(`{"success":true,"data":{"hash":"` + hash + `","torrent_id":42}}`))
				case "/torrents/mylist":
					w.Write([]byte(`{"success":true,"data":{"hash":"` + hash + `","download_finished":true,"download_present":true,"files":[{"id":0,"name":"Example.S01E02.mkv","size":100},{"id":1,"name":"Example.S01E01.mkv","size":999}]}}`))
				case "/torrents/requestdl":
					if r.URL.Query().Get("file_id") != "0" {
						t.Error("wrong episode played")
					}
					w.Write([]byte(`{"success":true,"data":"https://video.example/episode2.mkv"}`))
				default:
					t.Error("unexpected provider request")
				}
			}))
			defer server.Close()
			addon.Providers.ADBase, addon.Providers.TBBase = server.URL, server.URL
			addon.Providers.Client = server.Client()
			token, _ := testPrivateInstallation(t, f.ctx, addon)
			response := f.request("GET", "/stremio/"+token+"/stream/series/tt7654321:1:2.json", "", false, "")
			var streams struct{ Streams []aiosource.Stream }
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &streams) != nil || len(streams.Streams) != 1 {
				t.Fatalf("streams %d %s", response.Code, response.Body.String())
			}
			if scenario.freshUpload && !strings.HasPrefix(streams.Streams[0].Name, "[AD+] ") {
				t.Fatal("cached AllDebrid stream was not exposed before playback")
			}
			cacheCalls := int32(1)
			if service == "alldebrid" {
				cacheCalls = 2
			}
			if calls.Load() != cacheCalls || strings.Contains(streams.Streams[0].URL, key) {
				t.Fatal("stream invoked playback or leaked key")
			}
			if !strings.HasPrefix(streams.Streams[0].URL, "https://addon.example/") {
				t.Fatal("public origin ignored")
			}
			play := strings.TrimPrefix(streams.Streams[0].URL, "https://addon.example")
			playback.Store(true)
			response = f.request("GET", play, "", false, "")
			if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "https://video.example/episode2.mkv" {
				t.Fatalf("playback %d %s", response.Code, response.Body.String())
			}
			playbackCalls := int32(3)
			if scenario.freshUpload {
				playbackCalls++
			}
			if calls.Load() != cacheCalls+playbackCalls {
				t.Fatalf("playback calls %d", calls.Load())
			}
		})
	}
}

func TestTMDBRateConfigurationIsBoundedAndLegacyRequestsStillWork(t *testing.T) {
	f, _, _ := addonAPI(t)
	c, err := f.db.AIOConfig(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(c)
	var legacy map[string]any
	if err := json.Unmarshal(body, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "tmdb_requests_per_second")
	delete(legacy, "match_seconds")
	body, _ = json.Marshal(legacy)
	if response := f.request("PUT", "/api/aiosource/config", string(body), true, ""); response.Code != 204 {
		t.Fatalf("previous client could not save its configuration: %d", response.Code)
	}
	c, _ = f.db.AIOConfig(f.ctx)
	if c.TMDBRequestsPerSecond != 35 {
		t.Fatal("previous client did not receive the v1 request limit", c.TMDBRequestsPerSecond)
	}
	if c.MatchSeconds != 30 {
		t.Fatal("previous client did not receive the identification interval", c.MatchSeconds)
	}
	for _, rate := range []int{0, 4, 41} {
		c.TMDBRequestsPerSecond = rate
		body, _ = json.Marshal(c)
		if response := f.request("PUT", "/api/aiosource/config", string(body), true, ""); response.Code != 422 {
			t.Fatalf("out-of-range request limit %d accepted: %d", rate, response.Code)
		}
	}
	if response := f.request("PUT", "/api/aiosource/config", `{}`, true, ""); response.Code != 422 {
		t.Fatal("defaulting the new field made an empty configuration valid", response.Code)
	}
}

func TestIdentificationIntervalValidationAndLightweightControls(t *testing.T) {
	f, _, conn := addonAPI(t)
	if response := f.request("GET", "/api/aiosource/controls", "", false, ""); response.Code != 401 {
		t.Fatal("controls lack authentication", response.Code)
	}
	c, _ := f.db.AIOConfig(f.ctx)
	for _, seconds := range []int{0, 4, 86401} {
		c.MatchSeconds = seconds
		body, _ := json.Marshal(c)
		if response := f.request("PUT", "/api/aiosource/config", string(body), true, ""); response.Code != 422 {
			t.Fatalf("invalid identification interval %d: %d", seconds, response.Code)
		}
	}
	for _, seconds := range []int{5, 86400} {
		c.MatchSeconds = seconds
		body, _ := json.Marshal(c)
		if response := f.request("PUT", "/api/aiosource/config", string(body), true, ""); response.Code != 204 {
			t.Fatalf("valid identification interval %d: %d", seconds, response.Code)
		}
		c, _ = f.db.AIOConfig(f.ctx)
		if c.MatchSeconds != seconds {
			t.Fatal("interval was not saved", c.MatchSeconds)
		}
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years) VALUES('movie','1',ARRAY[2021]);
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one','{"title":"Example Film"}');`); err != nil {
		t.Fatal(err)
	}
	full := f.request("GET", "/api/aiosource", "", true, "")
	light := f.request("GET", "/api/aiosource/controls", "", true, "")
	var a, b map[string]json.RawMessage
	if full.Code != 200 || light.Code != 200 || json.Unmarshal(full.Body.Bytes(), &a) != nil || json.Unmarshal(light.Body.Bytes(), &b) != nil {
		t.Fatalf("status projections %d / %d", full.Code, light.Code)
	}
	for _, key := range []string{"config", "automation", "syncing", "matching", "error", "recovery_required"} {
		if string(a[key]) != string(b[key]) {
			t.Fatal("controls diverge from status", key)
		}
	}
	var flags map[string]any
	if json.Unmarshal(b["status"], &flags) != nil || flags["has_information"] != true || flags["works"] != nil || flags["matching_pending"] != nil {
		t.Fatal("controls exposed substitute catalog counts", string(b["status"]))
	}
}
