package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/vault"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicAllDebridPlaybackUsesGuestAccountAndHandlesCacheRace(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("c", 40)
	nativeID := "https://source.example/record?private=never-share"
	seedMovie(t, ctx, s, conn, "one", nativeID, hash)
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	cache := func() {
		t.Helper()
		if err := s.DB.SaveSharedAvailability(ctx, "alldebrid", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Example.Movie.2021.mp4", Size: 100, ID: 999, Link: "https://provider.example/admin-link"}}}); err != nil {
			t.Fatal(err)
		}
	}
	cache()
	var ready, existing, incomplete atomic.Bool
	ready.Store(true)
	var adds, deletes atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer visitor-ad-secret" {
			t.Error("administrator credential used for visitor playback")
			w.WriteHeader(401)
			return
		}
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			if existing.Load() {
				fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":41,"hash":"%s","statusCode":1}]}}`, hash)
			} else {
				fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
			}
		case "/v4/magnet/upload":
			adds.Add(1)
			if r.Form.Get("magnets[]") != publicMagnet(model.AIORecord{Hash: hash}) {
				t.Error("source credentials were sent to the visitor account")
			}
			if incomplete.Load() {
				fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s"}]}}`, hash)
			} else {
				fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s","ready":%t}]}}`, hash, ready.Load())
			}
		case "/v4/magnet/files":
			if r.Form.Get("id[]") != "77" {
				t.Error("another account's magnet ID reused")
			}
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":77,"files":[{"n":"Example.Movie.2021.mp4","s":100,"l":"https://provider.example/visitor-file"}]}]}}`)
		case "/v4/link/unlock":
			if r.Form.Get("link") != "https://provider.example/visitor-file" {
				t.Error("another account's file link reused")
			}
			fmt.Fprint(w, `{"status":"success","data":{"link":"https://video.example/visitor-download"}}`)
		case "/v4/magnet/delete":
			deletes.Add(1)
			if r.Form.Get("id") != "77" {
				t.Error("deleted an existing user entry")
			}
			fmt.Fprint(w, `{"status":"success","data":{"message":"deleted"}}`)
		default:
			t.Error("unexpected provider operation")
		}
	}))
	defer upstream.Close()
	s.Providers.ADBase = upstream.URL
	visitor := &PublicInstallation{Services: map[string]string{"alldebrid": "visitor-ad-secret"}}
	streams, err := s.PublicStreams(ctx, "https://public.example", "opaque", "movie", "tt1234567", visitor)
	if err != nil || len(streams) != 1 || strings.Contains(streams[0].URL, "never-share") || streams[0].InfoHash != "" {
		t.Fatal("private source identity exposed or cached stream lost", err)
	}
	u, _ := url.Parse(streams[0].URL)
	locator := strings.Split(u.Path, "/")[6]
	resolve := func(life context.Context) (string, error) {
		return s.PublicPlaybackSource(life, locator, "alldebrid", "movie", "tt1234567", visitor)
	}
	link, err := s.TracePlayback(ctx, "public", "alldebrid", "movie", "tt1234567", resolve)
	if err != nil || link != "https://video.example/visitor-download" || adds.Load() != 1 || deletes.Load() != 0 {
		t.Fatal("cached visitor playback failed", err)
	}
	history, err := s.DB.ListStremioRequestsFiltered(ctx, model.ListOptions{}, "public", "playback")
	if err != nil || len(history.Items) != 1 {
		t.Fatal("playback history missing", err)
	}
	detail, err := s.DB.StremioRequest(ctx, history.Items[0].ID)
	if err != nil || len(detail.Calls) != 4 || detail.Service != "alldebrid" {
		t.Fatal("provider calls missing from playback history", err)
	}
	encoded, _ := json.Marshal(detail)
	for _, private := range []string{"visitor-ad-secret", "visitor-file", "visitor-download", "never-share", "admin-link"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private data entered playback history")
		}
	}
	incomplete.Store(true)
	if _, err = s.TracePlayback(ctx, "public", "alldebrid", "movie", "tt1234567", resolve); !errors.Is(err, ErrInvalidResponse) || deletes.Load() != 1 {
		t.Fatal("incomplete response abandoned a confirmed new entry", err)
	}
	incomplete.Store(false)
	ready.Store(false)
	if _, err = s.TracePlayback(ctx, "public", "alldebrid", "movie", "tt1234567", resolve); !errors.Is(err, ErrPending) || deletes.Load() != 2 {
		t.Fatal("expired cache did not clean its newly created entry", err)
	}
	states, _ := s.DB.SharedAvailability(ctx, "alldebrid", []string{hash}, time.Now().Add(-time.Hour))
	if len(states) != 0 {
		t.Fatal("disappeared cache remained positive")
	}
	cache()
	existing.Store(true)
	if _, err = s.TracePlayback(ctx, "public", "alldebrid", "movie", "tt1234567", resolve); !errors.Is(err, ErrPending) || adds.Load() != 3 || deletes.Load() != 2 {
		t.Fatal("existing user download was changed", err)
	}
	if _, err = s.PublicStreams(ctx, "https://public.example", "opaque", "movie", "tt1234567", &PublicInstallation{Services: map[string]string{"unknown": "visitor-key"}}); !errors.Is(err, ErrAuth) {
		t.Fatal("unsupported installation fell back to private direct playback")
	}
}

func TestPublicAllDebridAdministratorProbesRetainActualCleanupCalls(t *testing.T) {
	ctx, s, _ := testService(t)
	hash := strings.Repeat("f", 40)
	c, _ := s.DB.AIOConfig(ctx)
	c.AllDebridSecret, c.ResponseSeconds = "adminAD", 2
	if err := s.Vault.Put(ctx, "adminAD", "administrator-probe-key"); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer administrator-probe-key" {
			t.Error("guest key used in recoverable background work")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
		case "/v4/magnet/upload":
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s","ready":true}]}}`, hash)
		case "/v4/magnet/files":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":77,"files":[{"n":"Example.Movie.mp4","s":100,"l":"https://provider.example/admin-file"}]}]}}`)
		case "/v4/magnet/delete":
			fmt.Fprint(w, `{"status":"success","data":{"message":"deleted"}}`)
		default:
			t.Error("unexpected provider request")
		}
	}))
	defer upstream.Close()
	s.Providers.ADBase = upstream.URL
	states, err := s.publicAvailability(ctx, c, "movie:imdb:1234567", []string{hash}, &PublicInstallation{Services: map[string]string{"alldebrid": "visitor-unused-key"}})
	if err != nil || !states["alldebrid"][hash].Cached {
		t.Fatal("administrator probe did not fill common availability", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(s.cleanupSlots) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("owned cleanup did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	runs, _ := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "cache"})
	if len(runs.Items) != 1 {
		t.Fatal("cache history missing")
	}
	events, err := s.DB.OperationEvents(ctx, runs.Items[0].ID, model.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events.Items {
		if event.Phase == "provider_cleanup" && event.HTTPStatus == 200 {
			return
		}
	}
	t.Fatal("actual background deletion absent from history")
}

func TestPublicProvidersCheckIndependentlyWithoutBackgroundVisitorKeys(t *testing.T) {
	ctx, s, _ := testService(t)
	hash := strings.Repeat("d", 40)
	c, _ := s.DB.AIOConfig(ctx)
	c.ResponseSeconds = 2
	c.AllDebridSecret = "adminAD"
	if err := s.Vault.Put(ctx, "adminAD", "admin-key-for-probe"); err != nil {
		t.Fatal(err)
	}
	checked := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			if r.Header.Get("Authorization") != "Bearer admin-key-for-probe" {
				t.Error("visitor key entered restartable AllDebrid probe")
			}
			select {
			case <-checked:
			case <-r.Context().Done():
				return
			case <-time.After(3 * time.Second):
				t.Error("AllDebrid prevented TorBox from starting")
			}
			w.WriteHeader(503)
		case "/torrents/checkcached":
			if r.Header.Get("Authorization") != "Bearer visitor-tb-key" {
				t.Error("TorBox used administrator key")
			}
			close(checked)
			fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example","size":100,"files":[{"id":99,"name":"Example.Movie.mp4","size":100}]}}}`, hash, hash)
		default:
			t.Error("unexpected operation")
		}
	}))
	defer upstream.Close()
	s.Providers.ADBase, s.Providers.TBBase = upstream.URL, upstream.URL
	states, err := s.publicAvailability(ctx, c, "movie:imdb:1234567", []string{hash}, &PublicInstallation{Services: map[string]string{"alldebrid": "visitor-ad-key", "torbox": "visitor-tb-key"}})
	if err != nil || !states["torbox"][hash].Cached || len(states["alldebrid"]) != 0 {
		t.Fatal("one provider failure hid the other provider", err)
	}
}

func TestPublicInstallationIsStatelessScopedAndRevocable(t *testing.T) {
	ctx, s, conn := testService(t)
	key := "visitor-key-never-store"
	if _, err := s.CreatePublicInstallation(ctx, "https://public.example", map[string]string{"torbox": key}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("disabled mode accepted installation")
	}
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled = true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"id":42,"email":"visitor-private@example.invalid"}}`)
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	for _, keys := range []map[string]string{nil, {"unsupported": key}, {"torbox": "bad\nkey"}} {
		if _, err := s.CreatePublicInstallation(ctx, "https://public.example", keys); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("invalid keys accepted")
		}
	}
	token, err := s.CreatePublicInstallation(ctx, "https://public.example", map[string]string{"torbox": key})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, key) {
		t.Fatal("URL exposes key")
	}
	installation, err := s.OpenPublicInstallation(ctx, "https://public.example", token)
	if err != nil || installation.Services["torbox"] != key {
		t.Fatal("installation cannot be opened")
	}
	if _, err = s.OpenPublicInstallation(ctx, "https://another.example", token); !errors.Is(err, ErrAuth) {
		t.Fatal("wrong audience accepted")
	}
	tampered := "A" + token[1:]
	if token[0] == 'A' {
		tampered = "B" + token[1:]
	}
	if _, err = s.OpenPublicInstallation(ctx, "https://public.example", tampered); !errors.Is(err, ErrAuth) {
		t.Fatal("tampering accepted")
	}
	otherVault, err := vault.New(ctx, s.DB, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Service{DB: s.DB, Vault: otherVault}
	if _, err = restarted.OpenPublicInstallation(ctx, "https://public.example", token); err != nil {
		t.Fatal("restart invalidates durable encryption")
	}
	for _, table := range []string{"settings", "secrets", "aio_public_access", "stremio_requests", "operation_runs", "operation_events", "aio_availability", "aio_shared_availability", "aio_resources", "aio_cleanup", "activity_logs"} {
		rows, e := conn.Query(ctx, `SELECT row_to_json(t)::text FROM ingest.`+table+` t`)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var data string
			if e = rows.Scan(&data); e != nil {
				t.Fatal(e)
			}
			if strings.Contains(data, key) || strings.Contains(data, token) || strings.Contains(data, "visitor-private@") {
				t.Fatalf("private data saved in %s", table)
			}
		}
		rows.Close()
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.Enabled = false
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.Enabled = true
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OpenPublicInstallation(ctx, "https://public.example", token); !errors.Is(err, ErrAuth) {
		t.Fatal("old URL reactivated")
	}
}

func TestPublicSharedCachePrivatePlaybackAndIndependentProviders(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	seedMovie(t, ctx, s, conn, "one", "1", hash)
	guestKey := "guest-torbox-key"
	var checks atomic.Int64
	var created atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+guestKey {
			t.Error("used another account's key")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/torrents/checkcached":
			checks.Add(1)
			fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example","size":100,"files":[{"id":99,"name":"Example.Movie.2021.mp4","size":100}]}}}`, hash, hash)
		case "/torrents/createtorrent":
			created.Add(1)
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			if r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") {
				t.Error("private source may download or exposes announce")
			}
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":"%s"}}`, hash)
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":"%s","download_finished":true,"download_present":true,"files":[{"id":3,"name":"Example.Movie.2021.mp4","size":100}]}}`, hash)
		case "/torrents/requestdl":
			if r.URL.Query().Get("file_id") != "3" {
				t.Error("reused another account's file ID")
			}
			fmt.Fprint(w, `{"success":true,"data":"https://video.example/visitor-link"}`)
		default:
			t.Error("unexpected provider operation")
		}
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	visitor := &PublicInstallation{Services: map[string]string{"torbox": guestKey}}
	streams, err := s.PublicStreams(ctx, "https://public.example", "opaque-configuration", "movie", "tt1234567", visitor)
	if err != nil || len(streams) != 1 || streams[0].InfoHash != "" || len(streams[0].Sources) != 0 || streams[0].Hints.Cached == nil || !*streams[0].Hints.Cached {
		t.Fatal("private cached streams unavailable or expose download data", err)
	}
	u, _ := url.Parse(streams[0].URL)
	parts := strings.Split(u.Path, "/")
	if len(parts) != 9 {
		t.Fatalf("invalid public playback path: %d", len(parts))
	}
	link, err := s.PublicPlaybackSource(ctx, parts[6], "torbox", "movie", "tt1234567", visitor)
	if err != nil || link != "https://video.example/visitor-link" || created.Load() != 1 {
		t.Fatal("visitor playback failed", err)
	}
	shared, err := s.DB.SharedAvailability(ctx, "torbox", []string{hash}, time.Now().Add(-time.Hour))
	if err != nil || len(shared) != 1 || len(shared[0].Files) != 1 || shared[0].Files[0].ID != 0 || shared[0].Files[0].Link != "" {
		t.Fatal("account-bound files leaked into shared cache", err)
	}
	before := checks.Load()
	c, _ := s.DB.AIOConfig(ctx)
	if err = s.Vault.Put(ctx, "adminTB", "admin-account-key"); err != nil {
		t.Fatal(err)
	}
	c.TorBoxSecret = "adminTB"
	if err = s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Availability(ctx, c, "movie:imdb:1234567", []string{hash}); err != nil || checks.Load() != before {
		t.Fatal("administrator did not reuse visitor cache", err)
	}
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	if err = s.DB.SaveSharedAvailability(ctx, "alldebrid", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Files: shared[0].Files, Generation: generation}); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `UPDATE ingest.aio_shared_availability SET checked_at=now()-interval '90 days' WHERE service='torbox'`); err != nil {
		t.Fatal(err)
	}
	visitor.Services["alldebrid"] = "visitor-ad-key"
	streams, err = s.PublicStreams(ctx, "https://public.example", "opaque-configuration", "movie", "tt1234567", visitor)
	if err != nil || len(streams) != 2 || checks.Load() != before+1 {
		t.Fatal("independent service cache failed", err)
	}
	history, err := s.DB.ListStremioRequestsFiltered(ctx, model.ListOptions{}, "public", "stream")
	if err != nil || history.Total != 2 {
		t.Fatal("public requests absent from existing history", err)
	}
	if err = s.DB.ResetAIOCache(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicPlayback(ctx, "torbox", "one", "1", "movie", "tt1234567", visitor); !errors.Is(err, ErrPending) {
		t.Fatal("uncached private playback accepted", err)
	}
	if created.Load() != 1 {
		t.Fatal("uncached private source was added")
	}
	if err = s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation}); err != nil {
		t.Fatal(err)
	}
	shared, _ = s.DB.SharedAvailability(ctx, "torbox", []string{hash}, time.Now().Add(-time.Hour))
	if len(shared) != 0 {
		t.Fatal("late writer bypassed cache reset")
	}
}

func TestPublicCacheFailureStaysUnknownAndDoesNotSaveCredentials(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("b", 40)
	seedMovie(t, ctx, s, conn, "one", "1", hash)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "visitor-key-in-error-must-not-be-saved")
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	out, err := s.PublicStreams(ctx, "https://public.example", "sealed-private-configuration", "movie", "tt1234567", &PublicInstallation{Services: map[string]string{"torbox": "visitor-key-in-error-must-not-be-saved"}})
	if err != nil || len(out) != 0 {
		t.Fatal("failed cache check exposed private stream", err)
	}
	shared, _ := s.DB.SharedAvailability(ctx, "torbox", []string{hash}, time.Now().Add(-time.Hour))
	if len(shared) != 0 {
		t.Fatal("authentication failure became global negative")
	}
	runs, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "cache"})
	if err != nil || len(runs.Items) != 1 || runs.Items[0].Status != "failed" || runs.Items[0].Phase != "availability" {
		t.Fatal("provider failure absent from history", err)
	}
	events, err := s.DB.OperationEvents(ctx, runs.Items[0].ID, model.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events.Items {
		if e.HTTPStatus == 401 && e.ErrorCode == "authentication" {
			found = true
		}
	}
	if !found {
		t.Fatal("provider diagnostics missing")
	}
	for _, table := range []string{"operation_runs", "operation_events", "stremio_requests", "activity_logs", "aio_resources", "aio_cleanup"} {
		rows, e := conn.Query(ctx, `SELECT row_to_json(t)::text FROM ingest.`+table+` t`)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var data string
			_ = rows.Scan(&data)
			if strings.Contains(data, "visitor-key-in-error") || strings.Contains(data, "sealed-private-configuration") {
				t.Fatal("private configuration entered history")
			}
		}
		rows.Close()
	}
}

func TestPublicSeasonPackRequiresTheRequestedEpisode(t *testing.T) {
	files := []model.AIOFile{{Path: "Example.S02E03.mp4", Size: 100}, {Path: "Example.S02E04.mp4", Size: 200}}
	requested, err := ParseID("tt1234567:2:4", "series")
	if err != nil {
		t.Fatal(err)
	}
	file, ok := SelectFile(files, requested)
	if !ok || file.Path != "Example.S02E04.mp4" {
		t.Fatal("wrong episode selected")
	}
	missing, _ := ParseID("tt1234567:2:5", "series")
	if _, ok = SelectFile(files, missing); ok {
		t.Fatal("missing episode declared cached")
	}
	if _, ok = SelectFile(nil, requested); ok {
		t.Fatal("missing file list declared cached")
	}
}

func TestPublicSeasonPackSelectionAndHistoryUseThePublishedEpisode(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("e", 40)
	work := model.AIOWork{Type: "tv", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Serial"}, Years: []int{2020}, SeasonYears: map[int]int{2: 2020}}
	if err := s.DB.SaveAIOWork(ctx, work, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example','pack',jsonb_build_object('title','Example.Serial.S02.COMPLETE.2020','imdb_id','tt1234567','info_hash',$1::text,'season',2))`, hash); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	a := model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Example.Serial.S02E04.mp4", Size: 200}}}
	if err := s.DB.SaveSharedAvailability(ctx, "alldebrid", a); err != nil {
		t.Fatal(err)
	}
	visitor := &PublicInstallation{Services: map[string]string{"alldebrid": "visitor-key"}}
	streams, err := s.PublicStreams(ctx, "https://public.example", "opaque", "series", "tt1234567:2:4", visitor)
	if err != nil || len(streams) != 1 || streams[0].Hints.Filename != a.Files[0].Path || streams[0].Hints.Size != 200 {
		t.Fatal("cached pack did not select the requested episode", err)
	}
	streams, err = s.PublicStreams(ctx, "https://public.example", "opaque", "series", "tt1234567:2:5", visitor)
	if err != nil || len(streams) != 0 {
		t.Fatal("missing episode returned as cached", err)
	}
	history, _ := s.DB.ListStremioRequestsFiltered(ctx, model.ListOptions{}, "public", "stream")
	if len(history.Items) != 2 || history.Items[0].FileUnavailable != 1 || history.Items[0].Matched != 1 {
		t.Fatal("episode exclusion absent from history")
	}
	a.Files, a.CheckedAt = nil, time.Now()
	if err := s.DB.SaveSharedAvailability(ctx, "alldebrid", a); err != nil {
		t.Fatal(err)
	}
	streams, err = s.PublicStreams(ctx, "https://public.example", "opaque", "series", "tt1234567:2:4", visitor)
	if err != nil || len(streams) != 0 {
		t.Fatal("missing file list certified an episode", err)
	}
}
