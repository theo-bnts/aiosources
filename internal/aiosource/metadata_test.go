package aiosource

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/theo-bnts/aiosources/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const movieDetail = `{"id":101,"adult":false,"title":"Example Movie","original_title":"Example Movie","release_date":"2021-01-01","external_ids":{"imdb_id":"tt1234567"},"alternative_titles":{"titles":[{"title":"Film Exemple"}]},"translations":{"translations":[{"data":{"title":"Exemple Film"}}]},"release_dates":{"results":[{"release_dates":[{"release_date":"2022-01-01T00:00:00Z"}]}]}}`
const tvDetail = `{"id":202,"adult":false,"name":"Example Serial","original_name":"Example Serial","first_air_date":"2018-01-01","external_ids":{"imdb_id":"tt7654321","tvdb_id":501},"alternative_titles":{"results":[]},"translations":{"translations":[]},"seasons":[{"season_number":1,"air_date":"2019-01-01"}]}`

func TestTMDBTypedValidation(t *testing.T) {
	w, err := decodeWork("movie", "101", []byte(movieDetail))
	if err != nil || w.IMDb != "tt1234567" || len(w.Titles) != 3 || !contains(w.Years, 2022) {
		t.Fatalf("%+v %v", w, err)
	}
	for _, bad := range []string{`{}`, strings.Replace(movieDetail, `"id":101`, `"id":999`, 1), strings.Replace(movieDetail, `"external_ids":{"imdb_id":"tt1234567"}`, `"external_ids":null`, 1), strings.Replace(movieDetail, `"title":"Example Movie"`, `"title":""`, 1)} {
		if _, err = decodeWork("movie", "101", []byte(bad)); err == nil {
			t.Errorf("accepted incomplete details")
		}
	}
	for _, part := range []string{`"adult":false,`, `"titles":[{"title":"Film Exemple"}]`, `"translations":[{"data":{"title":"Exemple Film"}}]`, `"results":[{"release_dates":[{"release_date":"2022-01-01T00:00:00Z"}]}]`} {
		if _, err := decodeWork("movie", "101", []byte(strings.Replace(movieDetail, part, "", 1))); err == nil {
			t.Errorf("accepted omitted decision data: %s", part)
		}
	}
}

func TestTMDBSymbolOnlyTitleDoesNotBlockInformationUpdate(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	if err := s.Vault.Put(ctx, "tmdb", "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret, c.AutoMatch = "tmdb", true
	// The official 2026-10-03 movie export contains ID 366238 with title ".".
	// Its punctuation disappears during normalization. Other detail fields here
	// are synthetic; this valid record must not prevent the following jobs.
	const symbolMovie = `{"id":366238,"adult":false,"title":".","original_title":".","release_date":"2020-01-01","external_ids":{"imdb_id":"tt9876543"},"alternative_titles":{"titles":[]},"translations":{"translations":[]},"release_dates":{"results":[]}}`
	followingMovie := strings.NewReplacer(`"id":101`, `"id":366239`, "Example Movie", "Following Movie", "tt1234567", "tt8765432").Replace(movieDetail)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".json.gz"):
			g := gzip.NewWriter(w)
			if strings.Contains(r.URL.Path, "movie_ids") {
				fmt.Fprintln(g, `{"id":101}`)
				fmt.Fprintln(g, `{"id":366238}`)
				fmt.Fprintln(g, `{"id":366239}`)
			} else {
				fmt.Fprintln(g, `{"id":202}`)
			}
			_ = g.Close()
		case strings.HasSuffix(r.URL.Path, "/changes"):
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
		case r.URL.Path == "/movie/366238":
			fmt.Fprint(w, symbolMovie)
		case r.URL.Path == "/movie/366239":
			fmt.Fprint(w, followingMovie)
		case r.URL.Path == "/tv/202":
			fmt.Fprint(w, tvDetail)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	s.TMDBBase, s.ExportBase, s.MetadataHTTP = server.URL, server.URL, server.Client()
	if err := s.Sync(ctx, c); err != nil {
		t.Fatalf("a punctuation-only title blocked the entire update: %v", err)
	}
	runs, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "metadata"})
	if err != nil || runs.Total != 1 || runs.Items[0].Status != "succeeded" || runs.Items[0].Processed != 3 || runs.Items[0].Errors != 0 {
		t.Fatalf("completed update history %+v %v", runs, err)
	}
	waitForMatching(t, ctx, s)
	status, err := s.DB.AIOStatus(ctx)
	if err != nil || status.Works != 4 || status.Pending != 0 || status.Matched != 1 || status.LastSync == nil || status.SyncError != "" {
		t.Fatalf("the update did not complete or changed existing matches: %+v %v", status, err)
	}
	var imdb string
	var years []int
	var titles int
	if err := conn.QueryRow(ctx, `SELECT imdb_id,years,(SELECT count(*) FROM ingest.aio_titles WHERE work_id=w.id) FROM ingest.aio_works w WHERE media_type='movie' AND tmdb_id='366238'`).Scan(&imdb, &years, &titles); err != nil || imdb != "tt9876543" || len(years) != 1 || years[0] != 2020 || titles != 0 {
		t.Fatalf("valid identifiers were lost or an empty searchable alias was created: %q %v %d %v", imdb, years, titles, err)
	}
	out, err := s.Streams(ctx, "http://local", testPrivateToken(t, ctx, s), "movie", "tt1234567")
	if err != nil || len(out) != 1 {
		t.Fatalf("existing IMDb/Stremio mapping was affected: %+v %v", out, err)
	}
	for _, title := range []string{".", "…", "✦", "Following Movie"} {
		candidates, overflow, err := s.DB.AIOCandidates(ctx, Normalize(title), "", "", "")
		if err != nil || overflow {
			t.Fatalf("candidate search failed: %v %v", overflow, err)
		}
		for _, candidate := range candidates {
			if candidate.TMDB == "366238" {
				t.Fatal("a symbol-only title introduced a false title candidate")
			}
		}
	}
	candidates, overflow, err := s.DB.AIOCandidates(ctx, "", "366238", "tt9876543", "")
	if err != nil || overflow || len(candidates) != 1 || candidates[0].TMDB != "366238" {
		t.Fatalf("the symbol-only work lost its identifier lookup: %+v %v %v", candidates, overflow, err)
	}
	// A later inventory must recognize the stored work instead of queuing the
	// same unsearchable title again on every daily update.
	if err := s.DB.AIOInventoryDate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx, c); err != nil {
		t.Fatalf("a later inventory reintroduced the blocked record: %v", err)
	}
	waitForMatching(t, ctx, s)
	status, err = s.DB.AIOStatus(ctx)
	if err != nil || status.Works != 4 || status.Pending != 0 || status.Matched != 1 || status.SyncError != "" {
		t.Fatalf("the completed catalog was not preserved: %+v %v", status, err)
	}
}

func TestTMDBInventorySyncAndEpisodeYearPublication(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	fields := fmt.Sprintf(`{"title":"Example.Serial.S01E02.2020.1080p","info_hash":"%s","imdb_id":"tt7654321","tmdb_id":"202","size":100}`, hash)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('series','1',$1)`, fields); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".json.gz"):
			w.Header().Set("Content-Type", "application/gzip")
			g := gzip.NewWriter(w)
			if strings.Contains(r.URL.Path, "movie_ids") {
				fmt.Fprintln(g, `{"id":101}`)
			} else {
				fmt.Fprintln(g, `{"id":202}`)
			}
			_ = g.Close()
		case r.URL.Path == "/movie/changes" || r.URL.Path == "/tv/changes":
			fmt.Fprint(w, `{"page":1,"total_pages":1,"results":[]}`)
		case r.URL.Path == "/movie/101":
			fmt.Fprint(w, movieDetail)
		case r.URL.Path == "/tv/202":
			fmt.Fprint(w, tvDetail)
		case r.URL.Path == "/tv/202/season/1":
			fmt.Fprint(w, `{"season_number":1,"episodes":[{"season_number":1,"episode_number":2,"air_date":"2020-01-01"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	s.TMDBBase = server.URL
	s.ExportBase = server.URL
	s.MetadataHTTP = server.Client()
	if err := s.Vault.Put(ctx, "tmdb", "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret = "tmdb"
	c.AutoMatch = true
	// Automatic identification honors the saved opt-in, including when a
	// metadata publication wakes the dispatcher during a manual sync.
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx, c); err != nil {
		t.Fatal(err)
	}
	waitForMatching(t, ctx, s)
	status, err := s.DB.AIOStatus(ctx)
	if err != nil || status.Works != 2 || status.Matched != 1 || status.Pending != 0 || status.LastSync == nil {
		t.Fatalf("status %+v %v", status, err)
	}
	out, err := s.Streams(ctx, "http://local", testPrivateToken(t, ctx, s), "series", "tt7654321:1:2")
	if err != nil || len(out) != 1 {
		t.Fatalf("episode year mapping %+v %v", out, err)
	}
	out, err = s.Streams(ctx, "http://local", testPrivateToken(t, ctx, s), "series", "tt7654321:1:1")
	if err != nil || len(out) != 0 {
		t.Fatal("different episode returned")
	}
}
func TestMetadataFailuresRetainQueueAndPublishedData(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"id":101,"title":"Partial"}`) }))
	defer server.Close()
	s.TMDBBase = server.URL
	s.MetadataHTTP = server.Client()
	jobs, err := s.DB.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatal("no durable job")
	}
	ok, err := s.details(ctx, "key", jobs[0])
	if ok || err == nil {
		t.Fatal("incomplete response accepted")
	}
	if err = s.DB.FinishAIOMetadata(ctx, jobs[0], false); err != nil {
		t.Fatal(err)
	}
	out, err := s.Streams(ctx, "http://local", testPrivateToken(t, ctx, s), "movie", "tt1234567")
	if err != nil || len(out) != 1 {
		t.Fatal("failed update erased published data")
	}
	var count int
	if err = conn.QueryRow(ctx, `SELECT attempts FROM ingest.aio_metadata_jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry was lost")
	}
	stale := jobs[0]
	if err = s.DB.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.FinishAIOMetadata(ctx, stale, true); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_metadata_jobs`).Scan(&pending); err != nil || pending != 1 {
		t.Fatal("old job erased newer change")
	}
}
func TestFourteenDayGapRequiresExplicitReset(t *testing.T) {
	ctx, s, _ := testService(t)
	if err := s.Vault.Put(ctx, "tmdb", "synthetic"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret = "tmdb"
	if err := s.DB.AIOChangesThrough(ctx, time.Now().Add(-15*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { t.Error("blocked sync contacted TMDB") }))
	defer server.Close()
	s.TMDBBase = server.URL
	s.MetadataHTTP = server.Client()
	if err := s.Sync(ctx, c); err == nil {
		t.Fatal("accepted change gap")
	}
	if err := s.Reset(ctx, "metadata"); err != nil {
		t.Fatal(err)
	}
	status, err := s.DB.AIOStatus(ctx)
	if err != nil || status.ChangesThrough != nil {
		t.Fatal("reset did not clear checkpoint")
	}
}
func TestConfigurationAtomicRevisionAndSecretProtection(t *testing.T) {
	ctx, s, _ := testService(t)
	c, _ := s.DB.AIOConfig(ctx)
	before := c
	if err := s.Vault.Put(ctx, "tb", "secret"); err != nil {
		t.Fatal(err)
	}
	c.TorBoxSecret = "tb"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConfig(ctx, before); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale configuration overwritten: %v", err)
	}
	if err := s.DB.DeleteSecret(ctx, "tb"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("configured secret deleted: %v", err)
	}
	if _, err := s.Vault.Resolve(ctx, "aiosource_installation"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("obsolete installation credential was generated", err)
	}
	c, _ = s.DB.AIOConfig(ctx)
	c.CacheMinutes = 0
	if err := s.SaveConfig(ctx, c); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("accepted invalid settings")
	}
	actual, _ := s.DB.AIOConfig(ctx)
	if actual.CacheMinutes == 0 {
		t.Fatal("partial configuration persisted")
	}
	b, _ := json.Marshal(actual)
	if strings.Contains(string(b), `"secret"`) {
		t.Fatal("configuration exposed credential value")
	}
}
