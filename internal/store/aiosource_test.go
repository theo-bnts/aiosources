package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"strings"
	"testing"
	"time"
)

type aioFakeRow struct{ fields string }

func (r aioFakeRow) Scan(dest ...any) error {
	*dest[0].(*string) = "source"
	*dest[1].(*string) = "id"
	*dest[2].(*[]byte) = []byte(r.fields)
	*dest[3].(*string) = "fingerprint"
	return nil
}
func TestAIOFieldExtractionAcceptsNativeAndTorznabIDs(t *testing.T) {
	for _, fields := range []string{
		`{"title":"Title","info_hash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","size":5000000000,"imdb_id":1234567,"tmdb_id":55,"tvdb_id":77,"season":1,"episode":2}`,
		`{"title":"Title","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":"5000000000","attributes":{"imdbid":["1234567"],"tmdbid":["55"],"tvdbid":["77"],"season":["1"],"episode":["2"]}}`,
		`{"title":"Title","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":5000000000,"external_ids":{"imdb":"tt1234567","tmdb":"55","tvdb":"77"},"season":1,"episode":2}`,
	} {
		r, err := scanAIORecord(aioFakeRow{fields})
		if err != nil || r.IMDb != "tt1234567" || r.TMDB != "55" || r.TVDB != "77" || r.Size != 5000000000 || r.Season == nil || *r.Season != 1 || r.Episode == nil || *r.Episode != 2 {
			t.Fatalf("record %+v %v", r, err)
		}
	}
	for _, fields := range []string{`{"imdb_id":"tt1","attributes":{"imdbid":["2"]}}`, `{"attributes":{"imdbid":["1","2"]}}`} {
		r, err := scanAIORecord(aioFakeRow{fields})
		if err != nil || r.IMDb != "invalid conflicting values" {
			t.Fatalf("conflicting IDs not retained as veto: %+v %v", r, err)
		}
	}
	r, err := scanAIORecord(aioFakeRow{`{"season":1.5,"episode":-2}`})
	if err != nil || r.Season == nil || *r.Season != -1 || r.Episode == nil || *r.Episode != -1 {
		t.Fatal("invalid observations were treated as absent")
	}
	r, err = scanAIORecord(aioFakeRow{`{"imdb_id":0,"tmdb_id":"0"}`})
	if err != nil || r.IMDb != "" || r.TMDB != "" {
		t.Fatal("absent zero IDs were treated as assertions")
	}
}
func TestAIOIncidentPersistence(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, active := range []bool{true, true, false, false, true} {
		if err = s.AIOIncident(ctx, "torbox", active); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var count int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind='aiosource.incident'`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("incident history %d %v", count, err)
	}
	var raw []byte
	if err = conn.QueryRow(ctx, `SELECT data FROM ingest.activity_logs WHERE kind='aiosource.incident' LIMIT 1`).Scan(&raw); err != nil || !json.Valid(raw) {
		t.Fatal("invalid incident metadata")
	}
}
func TestAIOCacheResetRejectsPreResetWrites(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	generation, err := s.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResetAIOCache(ctx); err != nil {
		t.Fatal(err)
	}
	v := model.AIOAvailability{Generation: generation, Hash: strings.Repeat("a", 40), Cached: true, CheckedAt: time.Now()}
	if err = s.SaveAIOAvailability(ctx, "torbox", "account", v); err != nil {
		t.Fatal(err)
	}
	out, err := s.AIOAvailability(ctx, "torbox", "account", []string{v.Hash}, time.Now().Add(-time.Minute))
	if err != nil || len(out) != 0 {
		t.Fatal("pre-reset task repopulated the cache")
	}
	v.Generation, err = s.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAIOAvailability(ctx, "torbox", "account", v); err != nil {
		t.Fatal(err)
	}
	out, err = s.AIOAvailability(ctx, "torbox", "account", []string{v.Hash}, time.Now().Add(-time.Minute))
	if err != nil || len(out) != 1 {
		t.Fatal("new generation did not publish")
	}
}

func TestAIOInventoryDoesNotErasePostSnapshotMetadata(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	for _, id := range []string{"1", "2", "3"} {
		if err = s.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: id, Titles: []string{"Title"}}, []string{"title"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = conn.Exec(ctx, `UPDATE ingest.aio_works SET updated_at=now()-interval '3 days' WHERE tmdb_id<>'3'`); err != nil {
		t.Fatal(err)
	}
	date := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if err = s.QueueAIOInventory(ctx, "movie", []string{"2", "2"}, date); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileAIOInventory(ctx, date); err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err = conn.QueryRow(ctx, `SELECT ARRAY(SELECT tmdb_id FROM ingest.aio_works ORDER BY tmdb_id)`).Scan(&ids); err != nil || strings.Join(ids, ",") != "2,3" {
		t.Fatalf("inventory retained %v: %v", ids, err)
	}
}

func TestAIOCandidateGenerationRejectsStaleDecision(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err = conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','1','{"title":"Title"}')`); err != nil {
		t.Fatal(err)
	}
	var r model.AIORecord
	r.Provider, r.SourceID, r.Normalized = "source", "1", "title"
	if err = conn.QueryRow(ctx, `SELECT md5(fields::text) FROM ingest.torrents`).Scan(&r.Fingerprint); err != nil {
		t.Fatal(err)
	}
	r.CandidateGeneration, err = s.AIOCandidateGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "1", Titles: []string{"Title"}}, []string{"title"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAIOMatch(ctx, r, "unmatched", "no candidates", nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_matches`).Scan(&n); err != nil || n != 0 {
		t.Fatal("stale candidate set was published", err)
	}
	r.CandidateGeneration, err = s.AIOCandidateGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAIOMatch(ctx, r, "unmatched", "current decision", nil); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_matches`).Scan(&n); err != nil || n != 1 {
		t.Fatal("current generation rejected", err)
	}
}

func TestAIOCandidateSearchUsesIndexesAtCatalogScale(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err = conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years) SELECT 'movie',n::text,ARRAY[2021] FROM generate_series(1,50000) n;
INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,md5(tmdb_id),md5(tmdb_id) FROM ingest.aio_works;
ANALYZE ingest.aio_works;ANALYZE ingest.aio_titles;`); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "50001", IMDb: "tt8888888", Titles: []string{"Unique Target Film"}, Years: []int{2021}}, []string{"unique target film"}); err != nil {
		t.Fatal(err)
	}
	works, overflow, err := s.AIOCandidates(ctx, "unique target film", "", "tt8888888", "")
	if err != nil || overflow || len(works) != 1 || works[0].TMDB != "50001" {
		t.Fatalf("catalog-scale candidates: %d %v %v", len(works), overflow, err)
	}
	var raw []byte
	if err = conn.QueryRow(ctx, `EXPLAIN (ANALYZE,FORMAT JSON) `+aioCandidatesQuery, "unique target film", "", "tt8888888", "", "", nil, []int{}, nil, false).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type planNode struct {
		NodeType string  `json:"Node Type"`
		Relation string  `json:"Relation Name"`
		Loops    float64 `json:"Actual Loops"`
		Plans    []json.RawMessage
	}
	var roots []struct{ Plan json.RawMessage }
	if err = json.Unmarshal(raw, &roots); err != nil {
		t.Fatal(err)
	}
	var inspect func(json.RawMessage)
	inspect = func(raw json.RawMessage) {
		var p planNode
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		if p.Loops > 0 && strings.Contains(p.NodeType, "Seq Scan") && (p.Relation == "aio_works" || p.Relation == "aio_titles") {
			t.Fatalf("catalog search traversed all %s rows", p.Relation)
		}
		for _, child := range p.Plans {
			inspect(child)
		}
	}
	for _, root := range roots {
		inspect(root.Plan)
	}
	// Long-lived pooled connections can use a generic prepared plan. A single
	// fresh custom-plan check does not cover the worker's steady-state queries.
	if _, err = conn.Exec(ctx, `SET plan_cache_mode=force_generic_plan`); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(ctx, `EXPLAIN (ANALYZE,FORMAT JSON) `+aioCandidatesQuery, "unique target film", "", "tt8888888", "", "", nil, []int{}, nil, false).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &roots); err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		inspect(root.Plan)
	}
}
