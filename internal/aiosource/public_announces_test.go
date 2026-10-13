package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/vault"
)

func visitorSourceRegistry(t *testing.T, s *Service) {
	t.Helper()
	registry, err := providers.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	s.sources = registry
	c, err := s.DB.AIOConfig(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Trackers = map[string]model.AIOTracker{}
	for id, name := range map[string]string{"private-one": "Example Library", "private-two": "Another Library", "public-one": "Public Library", "unspecified": "Unspecified Library"} {
		raw, _ := json.Marshal(map[string]any{"version": 1, "id": id, "name": name, "adapter": "http_json", "url": "https://source.example.invalid/administrator-source-endpoint", "enabled": false, "auth": map[string]string{"type": "bearer", "secret_ref": "administrator_source_secret"}})
		if doc, err := registry.Save(id, string(raw), ""); err != nil {
			t.Fatal(err, doc.Issues)
		}
		if id != "unspecified" {
			c.Trackers[id] = model.AIOTracker{Private: id != "public-one", AnnounceSecret: "admin_announce"}
		}
	}
	// An invalid rotated admin value would make any accidental vault resolution
	// fail. Public setup and playback must not attempt to read it at all.
	if err := s.Vault.Put(s.ctx, "admin_announce", "administrator-announce-never-resolve"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.SetAIOConfig(s.ctx, c); err != nil {
		t.Fatal(err)
	}
}

func assertNoVisitorData(t *testing.T, ctx context.Context, conn *pgx.Conn, private ...string) {
	t.Helper()
	for _, table := range []string{"settings", "secrets", "aio_public_access", "stremio_requests", "operation_runs", "operation_events", "aio_availability", "aio_shared_availability", "aio_resources", "aio_cleanup", "activity_logs"} {
		rows, err := conn.Query(ctx, `SELECT row_to_json(t)::text FROM ingest.`+table+` t`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			for _, value := range private {
				if strings.Contains(row, value) {
					t.Fatalf("visitor data persisted in %s", table)
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
}

func TestVisitorAnnouncesAreOptInScopedEncryptedAndNeverStored(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	const key = "visitor-announce-service-key"
	const announce = "https://announce.example.invalid/visitor-passkey/announce?token=own&mode=private"
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled = true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if list, err := s.PublicPrivateSources(ctx); err != nil || len(list) != 0 {
		t.Fatal("private source list exposed without consent", err)
	}
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	keys := map[string]string{"torbox": key}
	if _, err := s.CreatePublicInstallation(ctx, "https://public.example", keys, map[string]string{"private-one": announce}); !errors.Is(err, model.ErrInvalid) || calls.Load() != 0 {
		t.Fatal("disabled optional policy contacted a provider or accepted an announce", err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.AllowPrivateDownloads = true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	list, err := s.PublicPrivateSources(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "Another Library" || list[1].Name != "Example Library" {
		t.Fatal("source consent list lost names, ordering or privacy classification", list, err)
	}
	encoded, _ := json.Marshal(list)
	if strings.Contains(string(encoded), "admin") || strings.Contains(string(encoded), "https") || strings.Contains(string(encoded), "secret") {
		t.Fatal("public source list contains definition or credential data")
	}
	for _, input := range []map[string]string{{"unknown": announce}, {"public-one": announce}, {"unspecified": announce}, {"private-one": ""}, {"private-one": "https://user:secret@example.invalid/announce"}, {"private-one": "https://example.invalid/announce#private"}, {"private-one": "file:///private"}, {"private-one": "https://example.invalid/bad\nvalue"}} {
		if _, err := s.CreatePublicInstallation(ctx, "https://public.example", keys, input); !errors.Is(err, model.ErrInvalid) || calls.Load() != 0 || strings.Contains(err.Error(), "private-passkey") {
			t.Fatal("invalid or unauthorized announcement accepted or contacted a provider", err)
		}
	}
	configuration, err := s.CreatePublicInstallation(ctx, "https://public.example", keys, map[string]string{"private-one": announce})
	if err != nil || strings.Contains(configuration, "visitor") || strings.Contains(configuration, announce) {
		t.Fatal("installation failed or exposed visitor credentials", err)
	}
	visitor, err := s.OpenPublicInstallation(ctx, "https://public.example", configuration)
	if err != nil || visitor.Announces["private-one"] != announce {
		t.Fatal("encrypted announcement did not round-trip", err)
	}
	restartedVault, err := vault.New(ctx, s.DB, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Service{DB: s.DB, Vault: restartedVault}
	if _, err := restarted.OpenPublicInstallation(ctx, "https://public.example", configuration); err != nil {
		t.Fatal("restart discarded stateless visitor configuration", err)
	}
	if _, err := s.OpenPublicInstallation(ctx, "https://other.example", configuration); !errors.Is(err, ErrAuth) {
		t.Fatal("announcement link accepted on a different origin")
	}
	longAnnounce := "https://announce.example.invalid/announce?key=" + strings.Repeat("p", 2450)
	longLink, err := s.CreatePublicInstallation(ctx, "https://public.example", map[string]string{"torbox": strings.Repeat("k", 512)}, map[string]string{"private-one": longAnnounce})
	if err != nil || len(longLink) <= 4096 || len(longLink) > maxPublicInstallationSize {
		t.Fatal("bounded multi-credential links still hit the old key-only size limit", err)
	}
	if decoded, err := s.OpenPublicInstallation(ctx, "https://public.example", longLink); err != nil || decoded.Announces["private-one"] != longAnnounce {
		t.Fatal("valid larger encrypted link cannot be used", err)
	}
	if _, err := s.OpenPublicInstallation(ctx, "https://public.example", strings.Repeat("a", maxPublicInstallationSize+1)); !errors.Is(err, ErrAuth) {
		t.Fatal("oversized encrypted configuration was accepted")
	}
	assertNoVisitorData(t, ctx, conn, key, announce, "visitor-passkey", configuration)
}

func TestVisitorUncachedPlaybackUsesOnlyOwnAnnounceAndLivePermission(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			visitorSourceRegistry(t, s)
			hash, otherHash := strings.Repeat("a", 40), strings.Repeat("b", 40)
			seedMovie(t, ctx, s, conn, "private-one", "1", hash)
			seedMovie(t, ctx, s, conn, "private-two", "2", otherHash)
			const key = "visitor-service-secret"
			const own = "https://announce.example.invalid/visitor-own-passkey/announce?token=own&mode=private"
			var ready, expectAnnounce atomic.Bool
			expectAnnounce.Store(true)
			var adds, deletes atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("administrator service credential used for visitor")
					w.WriteHeader(401)
					return
				}
				checkMagnet := func(value string) {
					q, err := url.ParseQuery(strings.TrimPrefix(value, "magnet:?"))
					want := ""
					if expectAnnounce.Load() {
						want = own
					}
					if err != nil || q.Get("xt") != "urn:btih:"+hash || q.Get("tr") != want || strings.Contains(value, "administrator") {
						t.Error("wrong announce or hash sent to visitor account")
					}
				}
				switch r.URL.Path {
				case "/user/me":
					fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
				case "/v4/user":
					fmt.Fprint(w, `{"status":"success","data":{"user":{"isPremium":true}}}`)
				case "/v4.1/magnet/status":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
				case "/v4/magnet/upload":
					_ = r.ParseForm()
					checkMagnet(r.Form.Get("magnets[]"))
					adds.Add(1)
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s","ready":%t}]}}`, hash, ready.Load())
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":77,"files":[{"n":"Example.Movie.2021.mp4","s":100,"l":"https://provider.example/visitor-file"}]}]}}`)
				case "/v4/link/unlock":
					fmt.Fprint(w, `{"status":"success","data":{"link":"https://video.example/visitor-download"}}`)
				case "/v4/magnet/delete":
					deletes.Add(1)
					fmt.Fprint(w, `{"status":"success","data":{"message":"deleted"}}`)
				case "/torrents/createtorrent":
					_ = r.ParseMultipartForm(1 << 20)
					checkMagnet(r.FormValue("magnet"))
					if (r.FormValue("add_only_if_cached") == "true") == expectAnnounce.Load() {
						t.Error("TorBox cached-only policy does not match the selected path")
					}
					adds.Add(1)
					fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":"%s"}}`, hash)
				case "/torrents/mylist":
					fmt.Fprintf(w, `{"success":true,"data":{"hash":"%s","download_finished":%t,"download_present":%t,"files":[{"id":3,"name":"Example.Movie.2021.mp4","size":100}]}}`, hash, ready.Load(), ready.Load())
				case "/torrents/requestdl":
					fmt.Fprint(w, `{"success":true,"data":"https://video.example/visitor-download"}`)
				case "/torrents/checkcached":
					if ready.Load() {
						fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example Movie","size":100,"files":[{"id":93,"name":"Example.Movie.2021.mp4","size":100}]}}}`, hash, hash)
					} else {
						fmt.Fprint(w, `{"success":true,"data":{}}`)
					}
				default:
					t.Errorf("unexpected provider operation: %s", r.URL.Path)
				}
			}))
			defer upstream.Close()
			s.Providers.ADBase, s.Providers.TBBase = upstream.URL, upstream.URL
			access, _ := s.DB.PublicAccess(ctx)
			access.Enabled, access.AllowPrivateDownloads = true, true
			if err := s.DB.SetPublicAccess(ctx, access); err != nil {
				t.Fatal(err)
			}
			configuration, err := s.CreatePublicInstallation(ctx, "https://public.example", map[string]string{service: key}, map[string]string{"private-one": own})
			if err != nil {
				t.Fatal(err)
			}
			visitor, err := s.OpenPublicInstallation(ctx, "https://public.example", configuration)
			if err != nil {
				t.Fatal(err)
			}
			streams, err := s.PublicStreams(ctx, "https://public.example", configuration, "movie", "tt1234567", visitor)
			if err != nil || len(streams) != 1 || streams[0].Hints.Indexer != "Example Library" || streams[0].InfoHash != "" || len(streams[0].Sources) != 0 || (streams[0].Hints.Cached != nil && *streams[0].Hints.Cached) {
				t.Fatal("own announce did not unlock only its uncached source safely", streams, err)
			}
			encoded, _ := json.Marshal(streams)
			if strings.Contains(string(encoded), own) || strings.Contains(string(encoded), "visitor-own-passkey") || strings.Contains(string(encoded), "administrator") {
				t.Fatal("stream response exposed private announcement data")
			}
			locator := strings.Split(strings.TrimPrefix(streams[0].URL, "https://public.example"), "/")[6]
			play := func() (string, error) {
				return s.TracePlayback(ctx, "public", service, "movie", "tt1234567", func(ctx context.Context) (string, error) {
					return s.PublicPlaybackSource(ctx, locator, service, "movie", "tt1234567", visitor)
				})
			}
			if _, err := play(); !errors.Is(err, ErrPending) || adds.Load() != 1 || deletes.Load() != 0 {
				t.Fatal("intentional visitor download was not started or was deleted", err)
			}
			access, _ = s.DB.PublicAccess(ctx)
			access.AllowPrivateDownloads = false
			if err := s.DB.SetPublicAccess(ctx, access); err != nil {
				t.Fatal(err)
			}
			if _, err := s.OpenPublicInstallation(ctx, "https://public.example", configuration); err != nil {
				t.Fatal("optional permission change revoked all playback", err)
			}
			if _, err := play(); !errors.Is(err, ErrPending) || adds.Load() != 1 {
				t.Fatal("previous playback link bypassed live optional permission", err)
			}
			if rows, err := s.PublicStreams(ctx, "https://public.example", configuration, "movie", "tt1234567", visitor); err != nil || len(rows) != 0 {
				t.Fatal("disabled uncached permission still advertised private results", err)
			}
			access, _ = s.DB.PublicAccess(ctx)
			access.AllowPrivateDownloads = true
			if err := s.DB.SetPublicAccess(ctx, access); err != nil {
				t.Fatal(err)
			}
			ready.Store(true)
			if link, err := play(); err != nil || link != "https://video.example/visitor-download" {
				t.Fatal("visitor download did not become playable", err)
			}
			cache, err := s.DB.SharedAvailability(ctx, service, []string{hash}, time.Now().Add(-time.Hour))
			if err != nil || len(cache) != 1 || !cache[0].Cached || len(cache[0].Files) != 1 || cache[0].Files[0].ID != 0 || cache[0].Files[0].Link != "" {
				t.Fatal("verified playback failed to populate the safe shared cache", cache, err)
			}
			access, _ = s.DB.PublicAccess(ctx)
			access.AllowPrivateDownloads = false
			if err := s.DB.SetPublicAccess(ctx, access); err != nil {
				t.Fatal(err)
			}
			expectAnnounce.Store(false)
			if _, err := play(); err != nil {
				t.Fatal("cached playback required or sent the visitor announce", err)
			}
			assertNoVisitorData(t, ctx, conn, key, own, "visitor-own-passkey", configuration, "visitor-download", "visitor-file")
		})
	}
}

func TestVisitorInstallationRejectsPermissionWithdrawnDuringValidation(t *testing.T) {
	ctx, s, _ := testService(t)
	visitorSourceRegistry(t, s)
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.AllowPrivateDownloads = true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-resume
		fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	result := make(chan error, 1)
	go func() {
		_, err := s.CreatePublicInstallation(ctx, "https://public.example", map[string]string{"torbox": "visitor-validation-key"}, map[string]string{"private-one": "https://announce.example.invalid/visitor/announce"})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider validation did not start")
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.AllowPrivateDownloads = false
	err := s.DB.SetPublicAccess(ctx, access)
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, model.ErrInvalid) {
		t.Fatal("late installation bypassed withdrawn permission", err)
	}
}

func TestVisitorAnnounceSeasonPacksNeverInventCachedEpisodeAvailability(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	hash := strings.Repeat("e", 40)
	work := model.AIOWork{Type: "tv", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Serial"}, Years: []int{2020}, SeasonYears: map[int]int{2: 2020}}
	if err := s.DB.SaveAIOWork(ctx, work, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('private-one','pack',jsonb_build_object('title','Example.Serial.S02.COMPLETE.2020','imdb_id','tt1234567','info_hash',$1::text,'season',2))`, hash); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.AllowPrivateDownloads = true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	visitor := &PublicInstallation{Generation: access.Generation, Services: map[string]string{"alldebrid": "visitor-service-key"}, Announces: map[string]string{"private-one": "udp://announce.example.invalid:6969/visitor/announce"}}
	streams, err := s.PublicStreams(ctx, "https://public.example", "encrypted", "series", "tt1234567:2:4", visitor)
	if err != nil || len(streams) != 1 || streams[0].Hints.Cached != nil || streams[0].InfoHash != "" {
		t.Fatal("uncached season pack was lost or presented as verified cached", err)
	}
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	a := model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Example.Serial.S02E04.mp4", Size: 200}}}
	if err := s.DB.SaveSharedAvailability(ctx, "alldebrid", a); err != nil {
		t.Fatal(err)
	}
	streams, err = s.PublicStreams(ctx, "https://public.example", "encrypted", "series", "tt1234567:2:5", visitor)
	if err != nil || len(streams) != 0 {
		t.Fatal("own announce bypassed an explicit missing-episode file list", err)
	}
	if _, err := s.PublicPlayback(ctx, "alldebrid", "private-one", "pack", "series", "tt1234567:2:5", visitor); !errors.Is(err, ErrPending) {
		t.Fatal("old playback link bypassed the current missing-episode file list", err)
	}
	streams, err = s.PublicStreams(ctx, "https://public.example", "encrypted", "series", "tt1234567:2:4", visitor)
	if err != nil || len(streams) != 1 || streams[0].Hints.Filename != a.Files[0].Path || streams[0].Hints.Cached == nil || !*streams[0].Hints.Cached {
		t.Fatal("known episode lost cache or filename hints", err)
	}
	a.Files, a.CheckedAt = nil, time.Now()
	if err := s.DB.SaveSharedAvailability(ctx, "alldebrid", a); err != nil {
		t.Fatal(err)
	}
	streams, err = s.PublicStreams(ctx, "https://public.example", "encrypted", "series", "tt1234567:2:4", visitor)
	if err != nil || len(streams) != 1 || streams[0].Hints.Cached != nil {
		t.Fatal("missing file evidence was advertised as verified cached", err)
	}
	a.Files, a.CheckedAt = []model.AIOFile{{Path: "Example.Serial.S02E04.mp4", Size: 200}}, time.Now()
	if err := s.DB.SaveSharedAvailability(ctx, "alldebrid", a); err != nil {
		t.Fatal(err)
	}
	if err := s.sources.Close(); err != nil {
		t.Fatal(err)
	}
	streams, err = s.PublicStreams(ctx, "https://public.example", "encrypted", "series", "tt1234567:2:4", visitor)
	if err != nil || len(streams) != 1 {
		t.Fatal("unreadable optional source list prevented cached playback", err)
	}
	if err := s.DB.ForgetSharedAvailability(ctx, "alldebrid", hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	streams, err = s.PublicStreams(ctx, "https://public.example", "encrypted", "series", "tt1234567:2:4", visitor)
	if err != nil || len(streams) != 0 {
		t.Fatal("unreadable source definition authorized uncached playback", err)
	}
}
