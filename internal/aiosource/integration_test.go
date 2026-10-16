package aiosource

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"github.com/theo-bnts/aiosources/internal/vault"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitForMatching(t *testing.T, ctx context.Context, s *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := s.DB.AIOStatus(ctx)
		_, matching, _ := s.State()
		if err != nil {
			t.Fatal(err)
		}
		if status.MatchingPending == 0 && !matching {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("identification did not finish: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func testService(t testing.TB) (context.Context, *Service, *pgx.Conn) {
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
	v, err := vault.New(ctx, db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	c := store.DefaultAIOConfig()
	c.AutoSync, c.AutoMatch = false, false
	if err := db.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, db, v, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return ctx, s, conn
}
func seedMovie(t *testing.T, ctx context.Context, s *Service, conn *pgx.Conn, provider, id, hash string) {
	t.Helper()
	w := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie"}, Years: []int{2021}}
	if err := s.DB.SaveAIOWork(ctx, w, []string{"example movie"}); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{"title": "Example.Movie.2021.FRENCH.1080p.WEB-DL", "info_hash": hash, "size": int64(5000000000), "imdb_id": "1234567", "tmdb_id": "101"}
	b, _ := json.Marshal(fields)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES($1,$2,$3)`, provider, id, b); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestLocalPublicationProvenanceAndStaleRevision(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	seedMovie(t, ctx, s, conn, "one", "1", hash)
	seedMovie(t, ctx, s, conn, "two", "1", hash)
	token, _ := s.Token(ctx)
	out, err := s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(out) != 2 {
		t.Fatalf("streams %+v %v", out, err)
	}
	if out[0].InfoHash != hash || !strings.Contains(out[0].Name, "🇫🇷") || out[0].Hints.Size != 5000000000 {
		t.Fatal("missing stream metadata")
	}
	if _, err = conn.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{title}','"Different.Movie.2021"') WHERE provider_id='one'`); err != nil {
		t.Fatal(err)
	}
	out, err = s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(out) != 1 {
		t.Fatalf("stale mapping was served: %+v %v", out, err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	w := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt7654321", Titles: []string{"Example Movie"}, Years: []int{2021}}
	if err = s.DB.SaveAIOWork(ctx, w, []string{"example movie"}); err != nil {
		t.Fatal(err)
	}
	out, err = s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(out) != 0 {
		t.Fatal("metadata revision served stale IDs")
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := s.DB.AIOStatus(ctx)
	if err != nil || status.Matched != 0 {
		t.Fatalf("conflicting source IDs published %+v %v", status, err)
	}
	old := token
	token, err = s.RotateToken(ctx)
	if err != nil || token == old || s.Authorized(ctx, old) || !s.Authorized(ctx, token) {
		t.Fatal("token revocation failed")
	}
}

func TestNewHomonymInvalidatesPublicationAndDeletionRematches(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	// Remove supplied IDs so this decision depends on the complete title set.
	if _, err := conn.Exec(ctx, `UPDATE ingest.torrents SET fields=fields-'imdb_id'-'tmdb_id'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	assertStreams := func(want int) {
		t.Helper()
		out, err := s.DB.AIOStreams(ctx, "imdb", "tt1234567", "movie")
		if err != nil || len(out) != want {
			t.Fatalf("published %d, want %d: %v", len(out), want, err)
		}
	}
	assertStreams(1)
	homonym := model.AIOWork{Type: "movie", TMDB: "102", IMDb: "tt7654321", Titles: []string{"Example Movie"}, Years: []int{2021}}
	if err := s.DB.SaveAIOWork(ctx, homonym, []string{"example movie"}); err != nil {
		t.Fatal(err)
	}
	assertStreams(0) // Old work revision is unchanged; the queue must invalidate it.
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := conn.QueryRow(ctx, `SELECT status FROM ingest.aio_matches`).Scan(&status); err != nil || status != "ambiguous" {
		t.Fatalf("homonym decision %q: %v", status, err)
	}
	if err := s.DB.DeleteAIOWork(ctx, "movie", "102"); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	assertStreams(1)
}

func TestPrivateTorrentUsesStremioTrackerSourceSyntax(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	if err := s.Vault.Put(ctx, "private_passkey", "synthetic/key"); err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Trackers["source"] = model.AIOTracker{Private: true, Announce: "https://tracker.example/announce", PasskeySecret: "private_passkey"}
	if err = s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	token, err := s.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	streams, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
	if err != nil || len(streams) != 1 {
		t.Fatalf("private stream: %v", err)
	}
	if len(streams[0].Sources) != 1 || streams[0].Sources[0] != "tracker:https://tracker.example/announce/synthetic%2Fkey" {
		t.Fatalf("invalid Stremio tracker source: %v", streams[0].Sources)
	}
	if !strings.HasPrefix(streams[0].URL, "magnet:?") || streams[0].InfoHash == "" {
		t.Fatal("private torrent identity or magnet absent")
	}
}

func TestMatchingQueueFollowsSourceTransactionsAndResets(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	assertQueue := func(want int) {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_match_queue`).Scan(&n); err != nil || n != want {
			t.Fatalf("matching queue %d, want %d: %v", n, want, err)
		}
	}
	assertQueue(0)
	if _, err := conn.Exec(ctx, `UPDATE ingest.torrents SET fields=fields`); err != nil {
		t.Fatal(err)
	}
	assertQueue(0)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{title}','"Wrong.Movie.2022"')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertQueue(0)
	if _, err = conn.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{title}','"Wrong.Movie.2022"')`); err != nil {
		t.Fatal(err)
	}
	assertQueue(1)
	streams, err := s.DB.AIOStreams(ctx, "imdb", "tt1234567", "movie")
	if err != nil || len(streams) != 0 {
		t.Fatal("source mutation retained publication", err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	assertQueue(0)
	if err = s.Reset(ctx, "mapping"); err != nil {
		t.Fatal(err)
	}
	assertQueue(1)
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	assertQueue(0)
	if err = s.Reset(ctx, "metadata"); err != nil {
		t.Fatal(err)
	}
	assertQueue(1)
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	assertQueue(0)
	var records int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM ingest.torrents`).Scan(&records); err != nil || records != 1 {
		t.Fatal("extension reset removed source provenance", err)
	}
}

func TestTemporaryCleanupHasBoundedWorkersAndDurableOverflow(t *testing.T) {
	ctx, s, _ := testService(t)
	entered, release := make(chan struct{}, 12), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/magnet/delete" {
			t.Error("unexpected provider call")
			w.WriteHeader(500)
			return
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{}}`)
	}))
	defer server.Close()
	defer close(release)
	s.Providers.ADBase = server.URL
	s.Providers.Client = server.Client()
	for n := 1; n <= 12; n++ {
		hash := fmt.Sprintf("%040x", n)
		if err := s.DB.AddAIOCleanup(ctx, model.AIOCleanup{Account: fingerprint("synthetic-key"), Hash: hash, ID: int64(n)}); err != nil {
			t.Fatal(err)
		}
		s.enqueueCleanup(ctx, "synthetic-key", hash)
	}
	for n := 0; n < 4; n++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("cleanup workers did not start")
		}
	}
	select {
	case <-entered:
		t.Fatal("cleanup exceeded its concurrency limit")
	case <-time.After(250 * time.Millisecond):
	}
	jobs, err := s.DB.AIOCleanup(ctx, fingerprint("synthetic-key"))
	if err != nil || len(jobs) != 12 {
		t.Fatal("overflow ownership was lost", err)
	}
}

func TestCacheResetRemainsIndependentDuringMetadataSync(t *testing.T) {
	ctx, s, _ := testService(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"results":[],"total_pages":1}`)
	}))
	defer server.Close()
	defer close(release)
	defer s.Stop(true, true)
	s.TMDBBase = server.URL
	s.MetadataHTTP = server.Client()
	if err := s.Vault.Put(ctx, "tmdb_key", "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.TMDBSecret = "tmdb_key"
	if err = s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err = s.StartSync(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("metadata sync did not start")
	}
	if err = s.Reset(ctx, "cache"); err != nil {
		t.Fatal("metadata sync blocked cache reset", err)
	}
	syncing, _, _ := s.State()
	if !syncing {
		t.Fatal("cache reset stopped metadata sync")
	}
}
func TestCacheAccountsRestartFreshnessAndDeferredPlayback(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	seedMovie(t, ctx, s, conn, "source", "1", hash)
	var mu sync.Mutex
	checks, plays := 0, 0
	cached := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/torrents/checkcached":
			checks++
			if cached {
				fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example Movie","size":123,"files":[{"id":0,"name":"Example.Movie.mkv","size":123}]}}}`, hash, hash)
			} else {
				fmt.Fprint(w, `{"success":true,"data":{}}`)
			}
		default:
			plays++
			t.Error("stream invoked playback")
		}
	}))
	defer server.Close()
	s.Providers.TBBase = server.URL
	s.Providers.Client = server.Client()
	if err := s.Vault.Put(ctx, "tb_key", "account-one"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TorBoxSecret = "tb_key"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	token, _ := s.Token(ctx)
	for i := 0; i < 2; i++ {
		out, err := s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
		if err != nil || len(out) != 1 || !strings.HasPrefix(out[0].Name, "[TB+]") || out[0].InfoHash != hash || out[0].Hints.Size != 123 || out[0].Hints.Service != "torbox" || out[0].Hints.Cached == nil || !*out[0].Hints.Cached {
			t.Fatalf("invalid cache stream %+v %v", out, err)
		}
	}
	mu.Lock()
	if checks != 1 || plays != 0 {
		t.Errorf("checks=%d playback=%d", checks, plays)
	}
	cached = false
	mu.Unlock()
	if err := s.Vault.Put(ctx, "tb_key", "account-two"); err != nil {
		t.Fatal(err)
	}
	out, err := s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(out) != 1 || !strings.HasPrefix(out[0].Name, "[TB+]") {
		t.Fatal("account change lost fresh shared availability", err)
	}
	mu.Lock()
	if checks != 1 {
		t.Error("new account repeated a fresh read-only cache check")
	}
	mu.Unlock()
	// Availability is now shared; simulate expiry before expecting the changed
	// provider response to be discovered. Playback IDs remain account-specific.
	if _, err = conn.Exec(ctx, `UPDATE ingest.aio_shared_availability SET checked_at=now()-interval '4 days'`); err != nil {
		t.Fatal(err)
	}
	out, err = s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(out) != 0 {
		t.Fatalf("uncached private source served: %+v %v", out, err)
	}
	c, _ = s.DB.AIOConfig(ctx)
	c.Trackers["source"] = model.AIOTracker{Private: false}
	if err = s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	out, err = s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(out) != 1 || !strings.HasPrefix(out[0].Name, "[TB download]") {
		t.Fatalf("public uncached source: %+v %v", out, err)
	}
	// Warm reads start a background retry. Finish that pass before artificially
	// moving stored timestamps four days backwards; the warm-concurrency tests
	// cover real expiry during an active pass with explicit synchronization.
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		idle := len(s.flights) == 0
		s.mu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background availability pass did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	cached = true
	mu.Unlock()
	if _, err = conn.Exec(ctx, `UPDATE ingest.aio_resources SET checked_at=now()-interval '4 days'; UPDATE ingest.aio_availability SET checked_at=now()-interval '4 days'; UPDATE ingest.aio_shared_availability SET checked_at=now()-interval '4 days'`); err != nil {
		t.Fatal(err)
	}
	out, err = s.Streams(ctx, "http://localhost", token, "movie", "tt1234567")
	if err != nil || len(out) != 1 || !strings.HasPrefix(out[0].Name, "[TB+]") {
		t.Fatalf("expired resource not refreshed %+v %v", out, err)
	}
}
func TestAllDebridOnlyDeletesConfirmedTemporaryMagnets(t *testing.T) {
	ctx, s, _ := testService(t)
	hash := strings.Repeat("a", 40)
	var mu sync.Mutex
	present := false
	deleted := []string{}
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			if present {
				fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":10,"hash":"%s","statusCode":4}]}}`, hash)
			} else {
				fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
			}
		case "/v4/magnet/upload":
			uploads++
			if r.Form.Get("magnets[]") != hash {
				t.Error("cache probe sent tracker credentials")
			}
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":11,"hash":"%s","ready":true}]}}`, hash)
		case "/v4/magnet/files":
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":%s,"files":[{"n":"movie.mkv","s":100,"l":"https://example.com/file"}]}]}}`, r.Form.Get("id[]"))
		case "/v4/magnet/delete":
			deleted = append(deleted, r.Form.Get("id"))
			fmt.Fprint(w, `{"status":"success","data":{"message":"Deleted"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer server.Close()
	s.Providers.ADBase = server.URL
	s.Providers.Client = server.Client()
	a, err := s.adCheck(ctx, "account", hash)
	if err != nil || !a.Cached {
		t.Fatalf("check %+v %v", a, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		jobs, err := s.DB.AIOCleanup(ctx, fingerprint("account"))
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("temporary cleanup did not settle")
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	present = true
	mu.Unlock()
	if _, err = s.adCheck(ctx, "account", hash); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if uploads != 1 || len(deleted) != 1 || deleted[0] != "11" {
		t.Fatalf("uploads=%d deleted=%+v", uploads, deleted)
	}
}
func TestCacheSingleFlightSurvivesRequestTimeout(t *testing.T) {
	ctx, s, _ := testService(t)
	hash := strings.Repeat("a", 40)
	started, finish := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	checks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		checks++
		mu.Unlock()
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-finish:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	}))
	defer server.Close()
	s.Providers.TBBase = server.URL
	s.Providers.Client = server.Client()
	c := store.DefaultAIOConfig()
	hashes := []string{hash}
	f1, err := s.cacheFlight("torbox", "key", "resource", hashes, c)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	f2, err := s.cacheFlight("torbox", "key", "resource", hashes, c)
	if err != nil || f1 != f2 {
		t.Fatal("duplicate flight")
	}
	close(finish)
	select {
	case <-f1.done:
	case <-time.After(5 * time.Second):
		t.Fatal("flight hung")
	}
	if f1.err != nil {
		t.Fatal(f1.err)
	}
	mu.Lock()
	defer mu.Unlock()
	if checks != 1 {
		t.Fatal("duplicate probe")
	}
	results, err := s.DB.AIOAvailability(ctx, "torbox", fingerprint("key"), hashes, time.Now().Add(-time.Minute))
	if err != nil || len(results) != 1 || results[0].Cached {
		t.Fatalf("persisted result %+v %v", results, err)
	}
}
