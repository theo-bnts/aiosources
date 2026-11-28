package aiosource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

const syntheticGame = `{"id":101,"name":"Synthetic Odyssey","updated_at":1700000000,"first_release_date":1700000000,"cover":{"image_id":"synthetic_cover"},"alternative_names":[{"name":"Odyssée synthétique"}],"platforms":[{"id":6,"name":"PC (Microsoft Windows)","slug":"win"}],"game_type":{"type":"Main Game"}}`

func TestGameUpdateCheckpointAndRetryTime(t *testing.T) {
	now := time.Now()
	lastSync := now.Add(-time.Minute)
	state := model.GameStatus{LastSync: &lastSync}
	if next := NextGameUpdate(state, 15, now); !next.Equal(now.Add(14 * time.Minute)) {
		t.Fatalf("update interval returned %v", next)
	}
	state.StartedAt = &lastSync
	if next := NextGameUpdate(state, 15, now); !next.Equal(now) {
		t.Fatalf("an interrupted checkpoint should be due immediately: %v", next)
	}
	retry := now.Add(5 * time.Minute)
	state.RetryAt = &retry
	if next := NextGameUpdate(state, 15, now); !next.Equal(retry) {
		t.Fatalf("an explicitly paused checkpoint should respect its retry time: %v", next)
	}
}

func TestGamesPreserveSymbolOnlyNamesAndNestedReviewEvidence(t *testing.T) {
	ctx, s, conn := testService(t)
	w, err := decodeGame(igdbGame{ID: 1, Name: "!!!", Updated: 1700000000})
	if err != nil || len(w.Titles) != 1 {
		t.Fatal("a valid symbol-only title stopped catalog decoding", err)
	}
	for pass := 0; pass < 2; pass++ {
		state, err := s.DB.BeginGameUpdate(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.DB.PublishGames(ctx, state, []model.AIOWork{w}, 1); err != nil {
			t.Fatal("symbol-only game could not be published", err)
		}
		state, _ = s.DB.GameStatus(ctx)
		if err = s.DB.CompleteGameUpdate(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	var revision int64
	if err = conn.QueryRow(ctx, `SELECT revision FROM aiosources.aio_works WHERE igdb_id='1'`).Scan(&revision); err != nil || revision != 1 {
		t.Fatal("unchanged symbol-only title invalidated existing matches", revision, err)
	}
	var updated, unchanged bool
	err = conn.QueryRow(ctx, `SELECT aiosources.aio_review_evidence('{"attributes":{"igdb_id":"1"}}')<>aiosources.aio_review_evidence('{"attributes":{"igdb_id":"2"}}'),aiosources.aio_review_evidence('{"title":"Example Film","tmdb_id":"1"}')=aiosources.aio_bibliographic_review_evidence('{"title":"Example Film","tmdb_id":"1"}')`).Scan(&updated, &unchanged)
	if err != nil || !updated || !unchanged {
		t.Fatal("game evidence failed to change or unrelated evidence changed", err)
	}
	if err = s.Reset(ctx, "games"); err != nil {
		t.Fatal(err)
	}
	var message, service string
	if err = conn.QueryRow(ctx, `SELECT message,service FROM aiosources.activity_logs WHERE kind='maintenance.games_completed'`).Scan(&message, &service); err != nil || message != "Game information reset completed" || service != "igdb" {
		t.Fatal("game maintenance did not use normal activity history", message, service, err)
	}
}

func TestGamesResumeCommittedBatchesAfterAServiceRestart(t *testing.T) {
	ctx, s, conn := testService(t)
	var broken atomic.Bool
	broken.Store(true)
	var firstPage atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			fmt.Fprint(w, `{"access_token":"synthetic-bearer","token_type":"bearer","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/games/count" {
			fmt.Fprint(w, `{"count":60}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		start, end := 0, 0
		if strings.Contains(q, "id > 0") {
			firstPage.Add(1)
			start, end = 1, 50
		}
		if strings.Contains(q, "id > 50") {
			if broken.Load() {
				w.WriteHeader(503)
				return
			}
			start, end = 51, 60
		}
		fmt.Fprint(w, "[")
		for i := start; i > 0 && i <= end; i++ {
			if i > start {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%d,"name":"Synthetic Game %d","updated_at":1700000000}`, i, i)
		}
		fmt.Fprint(w, "]")
	}))
	defer upstream.Close()
	s.IGDBBase, s.IGDBTokenURL, s.GameHTTP = upstream.URL, upstream.URL+"/token", upstream.Client()
	if err := s.Vault.Put(ctx, "igdb_client_fixture", "synthetic-client"); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "igdb_fixture", "synthetic-secret"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.IGDBClientIDSecret, c.IGDBSecret = "igdb_client_fixture", "igdb_fixture"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.DeleteSecret(ctx, "igdb_client_fixture"); !errors.Is(err, model.ErrConflict) {
		t.Fatal("a configured IGDB client ID secret could be deleted", err)
	}
	if err := s.DB.DeleteSecret(ctx, "igdb_fixture"); !errors.Is(err, model.ErrConflict) {
		t.Fatal("a configured IGDB credential could be deleted", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE aiosources.runtime_settings SET values=jsonb_set(values,'{request_attempts}','1') WHERE section='metadata_retries'`); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncGames(ctx, "manual"); err == nil {
		t.Fatal("provider failure ignored")
	}
	before, err := s.DB.GameStatus(ctx)
	if err != nil || before.Works != 50 || before.After != 50 || before.Complete || before.StartedAt == nil {
		t.Fatal("partial progress lost", err)
	}
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, s.DB, s.Vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	restarted.IGDBBase, restarted.IGDBTokenURL, restarted.GameHTTP = upstream.URL, upstream.URL+"/token", upstream.Client()
	broken.Store(false)
	if err = restarted.SyncGames(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	after, err := s.DB.GameStatus(ctx)
	if err != nil || after.Works != 60 || !after.Complete || after.Processed != 60 || firstPage.Load() != 1 {
		t.Fatal("restart skipped or repeated committed data", before, after, err)
	}
}

func TestGamesImportLocalSearchReviewAndIsolatedReset(t *testing.T) {
	ctx, s, conn := testService(t)
	var auth atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			auth.Add(1)
			if r.Method != "POST" || r.URL.RawQuery != "" || r.FormValue("client_secret") != "synthetic-client-secret" || r.FormValue("grant_type") != "client_credentials" {
				t.Error("unsafe or invalid OAuth request")
			}
			fmt.Fprint(w, `{"access_token":"synthetic-bearer","token_type":"bearer","expires_in":3600}`)
			return
		}
		if r.Header.Get("Client-ID") != "synthetic-client" || r.Header.Get("Authorization") != "Bearer synthetic-bearer" {
			t.Error("incorrect IGDB authorization")
		}
		if r.URL.Path == "/games/count" {
			fmt.Fprint(w, `{"count":1}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		query := string(body)
		if strings.Contains(query, "offset") || !strings.Contains(query, "sort id asc") {
			t.Error("unstable pagination")
		}
		if strings.Contains(query, "id > 0") {
			fmt.Fprintf(w, "[%s]", syntheticGame)
		} else {
			fmt.Fprint(w, "[]")
		}
	}))
	defer upstream.Close()
	s.IGDBBase, s.IGDBTokenURL, s.GameHTTP = upstream.URL, upstream.URL+"/token", upstream.Client()
	if err := s.Vault.Put(ctx, "igdb_client_fixture", "synthetic-client"); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "igdb_fixture", "synthetic-client-secret"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.IGDBClientIDSecret, c.IGDBSecret = "igdb_client_fixture", "igdb_fixture"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','game','{"title":"Synthetic.Odyssey.PC.v1.2.3","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","category":"Jeux PC","size":1024}');
INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,status,reason,normalized) SELECT provider_id,source_id,fields_fingerprint,'unmatched','insufficient evidence','synthetic odyssey pc' FROM aiosources.torrents;
DELETE FROM aiosources.aio_match_queue;`); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncGames(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	state, err := s.DB.GameStatus(ctx)
	if err != nil || !state.Complete || state.Works != 1 || state.After != 0 || state.StartedAt != nil || auth.Load() != 1 {
		t.Fatal("import checkpoint incorrect", state, err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "odyssee synthetique"}, model.CatalogSearchFilters{Type: "game", Language: "fr"})
	if err != nil || page.Total != 1 || page.Items[0].IGDB != "101" || page.Items[0].TMDB != "" || !strings.Contains(page.Items[0].Cover, "images.igdb.com/") {
		t.Fatal("local game search or identifier namespace", page, err)
	}
	rows, err := s.DB.CatalogRecords(ctx, page.Items[0].ID)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	detail, err := s.DB.AIOReviewDetail(ctx, rows[0].OccurrenceID)
	if err != nil || detail.Work == nil || detail.Work.IGDB != "101" {
		t.Fatal("game review unavailable", err)
	}
	if err = s.DB.SaveAIOReview(ctx, rows[0].OccurrenceID, model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, WorkID: detail.Work.ID, WorkRevision: detail.Work.Revision}); err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncGames(ctx, "manual"); err != nil {
		t.Fatal("incremental update", err)
	}
	var revision int64
	if err = conn.QueryRow(ctx, `SELECT revision FROM aiosources.aio_works WHERE igdb_id='101'`).Scan(&revision); err != nil || revision != detail.Work.Revision {
		t.Fatal("unchanged game invalidated matches", err, revision)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,display_title,years) VALUES('movie','101','Unrelated Film','{2023}'); INSERT INTO aiosources.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL101W','Unrelated Book','{2023}')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Reset(ctx, "games"); err != nil {
		t.Fatal(err)
	}
	var games, other, history, reviews, files int
	err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.aio_works WHERE media_type='game'),(SELECT count(*) FROM aiosources.aio_works WHERE media_type<>'game'),(SELECT count(*) FROM aiosources.operation_runs WHERE service='igdb'),(SELECT count(*) FROM aiosources.aio_match_reviews WHERE media_type='game'),(SELECT count(*) FROM aiosources.torrents)`).Scan(&games, &other, &history, &reviews, &files)
	if err != nil || games != 0 || other != 2 || history != 0 || reviews != 0 || files != 1 {
		t.Fatal("game reset changed unrelated data or retained dependents", games, other, history, reviews, files, err)
	}
}

func TestGamesRejectMalformedPagesAndRenewRevokedTokens(t *testing.T) {
	ctx, s, conn := testService(t)
	var auth, requests atomic.Int32
	var valid atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			auth.Add(1)
			fmt.Fprintf(w, `{"access_token":"synthetic-%d","token_type":"bearer","expires_in":3600}`, auth.Load())
			return
		}
		if r.URL.Path == "/games/count" {
			fmt.Fprint(w, `{"count":1}`)
			return
		}
		requests.Add(1)
		if requests.Load() == 1 {
			w.WriteHeader(401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "id > 0") {
			if valid.Load() {
				fmt.Fprintf(w, "[%s]", syntheticGame)
			} else {
				fmt.Fprintf(w, "[%s,%s]", syntheticGame, syntheticGame)
			}
		} else {
			fmt.Fprint(w, "[]")
		}
	}))
	defer upstream.Close()
	s.IGDBBase, s.IGDBTokenURL, s.GameHTTP = upstream.URL, upstream.URL+"/token", upstream.Client()
	if err := s.Vault.Put(ctx, "igdb_client_fixture", "synthetic-client"); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "igdb_fixture", "synthetic-secret"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.IGDBClientIDSecret, c.IGDBSecret = "igdb_client_fixture", "igdb_fixture"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncGames(ctx, "manual"); err == nil {
		t.Fatal("unordered duplicate provider IDs accepted")
	}
	state, err := s.DB.GameStatus(ctx)
	if err != nil || state.After != 0 || state.Works != 0 || auth.Load() != 2 {
		t.Fatal("bad response advanced checkpoint or token renewal failed", err)
	}
	valid.Store(true)
	if err = s.SyncGames(ctx, "manual"); err != nil {
		t.Fatal("could not resume rejected page", err)
	}
	var leaked int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM aiosources.operation_runs WHERE error LIKE '%synthetic-secret%' OR error LIKE '%synthetic-1%'`).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatal("credentials entered operation history")
	}
}

func TestLegacyIGDBClientIDMigratesToEncryptedSecret(t *testing.T) {
	ctx, s, _ := testService(t)
	if err := s.Vault.Put(ctx, "igdb_secret_ref", "synthetic-secret"); err != nil {
		t.Fatal(err)
	}
	// Seed a legacy v2 setting containing plaintext igdb_client_id directly in the database.
	if err := s.DB.SetSetting(ctx, "aiosource.config.v2", `{"revision":2,"igdb_client_id":"legacy-twitch-client-id","igdb_secret":"igdb_secret_ref","igdb_enabled":true,"igdb_minutes":15,"igdb_requests_per_second":3,"igdb_reconcile_days":7,"software_minutes":360,"openlibrary_minutes":15,"musicbrainz_minutes":60,"sync_minutes":15,"match_seconds":30,"tmdb_requests_per_second":35,"cache_minutes":4320,"response_seconds":5,"trackers":{}}`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, s.DB, s.Vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := restarted.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	current, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(current.IGDBClientIDSecret, "igdb_client_id_") || current.IGDBClientID != "" {
		t.Fatalf("legacy igdb_client_id was not migrated: %+v", current)
	}
	resolved, err := restarted.igdbClientID(ctx, current)
	if err != nil || resolved != "legacy-twitch-client-id" {
		t.Fatalf("migrated igdb client ID mismatch: %q %v", resolved, err)
	}
	rawSetting, err := s.DB.Setting(ctx, "aiosource.config.v2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawSetting, "legacy-twitch-client-id") || strings.Contains(rawSetting, `"igdb_client_id":`) {
		t.Fatalf("plaintext igdb_client_id remained in settings: %s", rawSetting)
	}
	if err := s.DB.DeleteSecret(ctx, current.IGDBClientIDSecret); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("configured igdb_client_id_secret could be deleted: %v", err)
	}
	// Conflicting plaintext and secret reference must be rejected.
	conflicting := current
	conflicting.IGDBClientID = "another-client"
	if err := restarted.SaveConfig(ctx, conflicting); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("conflicting igdb_client_id and igdb_client_id_secret accepted: %v", err)
	}
	// Secret containing spaces must be rejected.
	if err := s.Vault.Put(ctx, "bad_client_id", "invalid client id"); err != nil {
		t.Fatal(err)
	}
	invalid := current
	invalid.IGDBClientIDSecret = "bad_client_id"
	if err := restarted.SaveConfig(ctx, invalid); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("invalid client ID secret value accepted: %v", err)
	}
}

// Provider pacing is part of this complete update. Requests use only the
// loopback fixture; no developer credentials or live catalog enter this gate.
func BenchmarkGameInformation(b *testing.B) {
	ctx, s, _ := testService(b)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			fmt.Fprint(w, `{"access_token":"synthetic-benchmark","token_type":"bearer","expires_in":3600}`)
		case "/games/count":
			fmt.Fprint(w, `{"count":100}`)
		case "/games":
			body, _ := io.ReadAll(r.Body)
			fmt.Fprint(w, "[")
			if strings.Contains(string(body), "id > 0") {
				for i := 1; i <= 100; i++ {
					if i > 1 {
						fmt.Fprint(w, ",")
					}
					fmt.Fprintf(w, `{"id":%d,"name":"Synthetic Game %d","updated_at":1700000000}`, i, i)
				}
			}
			fmt.Fprint(w, "]")
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	s.IGDBBase, s.IGDBTokenURL, s.GameHTTP = server.URL, server.URL+"/token", server.Client()
	if err := s.Vault.Put(ctx, "game_benchmark_client", "synthetic-client"); err != nil {
		b.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "game_benchmark", "synthetic-credential"); err != nil {
		b.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.IGDBClientIDSecret, c.IGDBSecret = "game_benchmark_client", "game_benchmark"
	if err := s.SaveConfig(ctx, c); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := s.DB.ResetGameInformation(ctx); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.SyncGames(ctx, "manual"); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	state, err := s.DB.GameStatus(ctx)
	if err != nil || !state.Complete || state.Works != 100 {
		b.Fatal("game update lost records", err)
	}
	b.ReportMetric(100, "games/op")
}

func BenchmarkGameMatching(b *testing.B) {
	ctx, s, conn := testService(b)
	if _, err := conn.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,years,game_data)
 SELECT 'game',n::text,'Synthetic Game '||n,'{2024}','{"platforms":[{"id":6,"name":"PC (Microsoft Windows)","slug":"win"}]}' FROM generate_series(1,100) n;
 INSERT INTO aiosources.aio_titles(work_id,title,normalized) SELECT id,display_title,lower(display_title) FROM aiosources.aio_works WHERE media_type='game';
 INSERT INTO aiosources.torrents(provider_id,source_id,fields) SELECT 'synthetic',n::text,jsonb_build_object('title','Synthetic.Game.'||n||'.PC','categories',ARRAY[4050],'info_hash',md5(n::text)||'12345678') FROM generate_series(1,100) n;
 ANALYZE aiosources.aio_works;ANALYZE aiosources.aio_titles;ANALYZE aiosources.torrents;`); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := s.DB.ResetAIOMapping(ctx); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.Match(ctx); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	var matched int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM aiosources.aio_matches WHERE status='matched'`).Scan(&matched); err != nil || matched != 100 {
		b.Fatal("game matching lost reliable results", err)
	}
	b.ReportMetric(100, "matched/100")
}

func TestGamesBackfillOlderCatalogsOnceWithoutRepeatingIdentification(t *testing.T) {
	ctx, s, conn := testService(t)
	state, err := s.DB.BeginGameUpdate(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.CompleteGameUpdate(ctx, state); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, `DELETE FROM aiosources.settings WHERE key='igdb.matching.v2';
 INSERT INTO aiosources.settings(key,value) VALUES('igdb.matching.v1','1') ON CONFLICT DO NOTHING;
 INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','legacy-game','{"title":"Synthetic Odyssey PC","category":"Jeux PC"}');
 INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,status,reason,normalized) SELECT provider_id,source_id,fields_fingerprint,'unmatched','insufficient evidence','synthetic odyssey pc' FROM aiosources.torrents;
 DELETE FROM aiosources.aio_match_queue;`)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		state, err = s.DB.BeginGameUpdate(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}
		if state.Phase != "changes" {
			t.Fatal("fixture did not use the incremental path")
		}
		if err = s.DB.CompleteGameUpdate(ctx, state); err != nil {
			t.Fatal(err)
		}
		var version int64
		err = conn.QueryRow(ctx, `SELECT version FROM aiosources.aio_match_queue WHERE source_id='legacy-game'`).Scan(&version)
		if err != nil || version != 1 {
			t.Fatal("older catalog was skipped or repeatedly queued", pass, version, err)
		}
	}
}
