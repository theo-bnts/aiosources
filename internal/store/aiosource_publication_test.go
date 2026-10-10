package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAIOMetadataBatchNoOpAndVersionedPublication(t *testing.T) {
	ctx, db := sharingDatabase(t)
	w := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie", "Other Title"}, Years: []int{2021, 2022}}
	aliases := []string{"example movie", "other title"}
	if err := db.SaveAIOWork(ctx, w, aliases); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "source", "1", `{"title":"Example.Movie.2021","imdb_id":"tt1234567"}`, nil)
	rows, err := db.AIOMatchBatch(ctx, 100)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	works, _, err := db.AIOCandidates(ctx, "example movie", "", "", "")
	if err != nil || len(works) != 1 {
		t.Fatal(works, err)
	}
	rows[0].CandidateGeneration, err = db.AIOCandidateGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAIOMatch(ctx, rows[0], "matched", "verified", &works[0]); err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err := db.pool.QueryRow(ctx, `SELECT metadata_generation FROM ingest.aio_state`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if err := db.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	jobs, _ := db.AIOMetadataJobs(ctx)
	if err := db.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	w.Titles, w.Years = []string{"Other Title", "Example Movie"}, []int{2022, 2021, 2021}
	if err := db.SaveAIOMetadataBatch(ctx, []AIOMetadataPublication{{Work: w, Normalized: []string{"other title", "example movie"}, Job: &jobs[0]}}); err != nil {
		t.Fatal(err)
	}
	var revision, current int64
	var pending, queued int
	if err := db.pool.QueryRow(ctx, `SELECT revision,(SELECT metadata_generation FROM ingest.aio_state),(SELECT count(*) FROM ingest.aio_metadata_jobs),(SELECT count(*) FROM ingest.aio_match_queue) FROM ingest.aio_works`).Scan(&revision, &current, &pending, &queued); err != nil || revision != works[0].Revision || current != generation || pending != 1 || queued != 0 {
		t.Fatalf("no-op withdrew streams or acknowledged a newer job: revision=%d generation=%d pending=%d queued=%d err=%v", revision, current, pending, queued, err)
	}
	streams, err := db.AIOStreams(ctx, "imdb", "tt1234567", "movie")
	if err != nil || len(streams) != 1 {
		t.Fatalf("routine refresh withdrew a working stream: %+v %v", streams, err)
	}
	w.Adult = true
	if err := db.SaveAIOWork(ctx, w, []string{"other title", "example movie"}); err != nil {
		t.Fatal(err)
	}
	streams, err = db.AIOStreams(ctx, "imdb", "tt1234567", "movie")
	if err != nil || len(streams) != 0 {
		t.Fatal("adult metadata remains published", err)
	}
	works, _, err = db.AIOCandidates(ctx, "example movie", "101", "tt1234567", "")
	if err != nil || len(works) != 0 {
		t.Fatal("adult metadata remains eligible", err)
	}
}

func TestAIOMatchingAndMetadataPublicationShareSmallPoolSafely(t *testing.T) {
	ctx, db := sharingDatabase(t)
	config := db.pool.Config()
	db.pool.Close()
	config.MaxConns, config.MinConns = 4, 0
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	db.pool = pool
	if db.AIOMatchWorkers() != 1 {
		t.Fatal("small pool did not reserve publication capacity")
	}
	w := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Film"}, Years: []int{2021}}
	if err := db.SaveAIOWork(ctx, w, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
SELECT 'source',n::text,jsonb_build_object('title','Example.Film.2021') FROM generate_series(1,30) n`); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.AIOMatchBatch(ctx, 100)
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var group sync.WaitGroup
	for start := 0; start < len(rows); start += 10 {
		picks := rows[start : start+10]
		group.Go(func() {
			_, err := db.MatchAIOBatch(work, picks, func(r model.AIORecord, find AIOCandidateSearch) (model.AIOMatchResult, error) {
				works, _, err := find(model.AIOCandidateFilter{Title: "example film", Type: "movie"})
				if err != nil || len(works) != 1 {
					return model.AIOMatchResult{}, model.ErrConflict
				}
				return model.AIOMatchResult{Record: r, Status: "matched", Work: &works[0]}, nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	group.Go(func() {
		for range 10 {
			if err := db.SaveAIOWork(work, w, []string{"example film"}); err != nil {
				t.Error(err)
				return
			}
		}
	})
	group.Wait()
	var matches, queue int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_matches),(SELECT count(*) FROM ingest.aio_match_queue)`).Scan(&matches, &queue); err != nil || matches != 30 || queue != 0 {
		t.Fatal("pool contention lost matching work", matches, queue, err)
	}
}

func TestAIOPerformanceUpgradeRequeuesOldMissesWithoutWithdrawingMatches(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years) VALUES('movie','101',ARRAY[2021]);
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','matched','{}'),('source','unmatched','{}'),('source','ambiguous','{}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,md5(fields::text),(SELECT id FROM ingest.aio_works),1,source_id,'previous decision' FROM ingest.torrents;
DELETE FROM ingest.aio_match_queue;
INSERT INTO ingest.aio_match_queue(provider_id,source_id,version) VALUES('source','ambiguous',9);
ALTER TABLE ingest.aio_works DROP COLUMN adult;
DROP INDEX ingest.aio_metadata_jobs_due,ingest.aio_works_refresh,ingest.aio_torrents_tmdb,ingest.aio_torrents_imdb,ingest.aio_torrents_tvdb;
DELETE FROM ingest.schema_migrations WHERE version=34;`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var queue, version int
	if err := db.pool.QueryRow(ctx, `SELECT count(*),max(version) FROM ingest.aio_match_queue`).Scan(&queue, &version); err != nil || queue != 2 || version != 9 {
		t.Fatal("upgrade lost an existing queue version or withdrew working streams", queue, version, err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal("upgrade was not idempotent", err)
	}
}

func TestAIOMetadataBatchRollsBackDetailsAndAcknowledgements(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.QueueAIOMetadata(ctx, "movie", []string{"1", "2"}, true); err != nil {
		t.Fatal(err)
	}
	jobs, _ := db.AIOMetadataJobs(ctx)
	inputs := []AIOMetadataPublication{}
	for _, job := range jobs {
		j := job
		inputs = append(inputs, AIOMetadataPublication{Work: model.AIOWork{Type: j.Type, TMDB: j.ID, Titles: []string{"A Film"}, Years: []int{2021}}, Normalized: []string{"a film"}, Job: &j})
	}
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.aio_titles ADD CONSTRAINT synthetic_publication_failure CHECK(work_id<0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAIOMetadataBatch(ctx, inputs); err == nil {
		t.Fatal("storage failure was accepted")
	}
	var works, pending int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_metadata_jobs)`).Scan(&works, &pending); err != nil || works != 0 || pending != 2 {
		t.Fatalf("partially published metadata: %d %d %v", works, pending, err)
	}
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.aio_titles DROP CONSTRAINT synthetic_publication_failure`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAIOMetadataBatch(ctx, inputs); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_metadata_jobs)`).Scan(&works, &pending); err != nil || works != 2 || pending != 0 {
		t.Fatalf("publication did not recover: %d %d %v", works, pending, err)
	}
}

func TestAIOMatchBatchSeasonRequestsAreAtomicAndRejectStaleWork(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "source", "1", `{"title":"Example.Series.S01E02.2021"}`, nil)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "tv", TMDB: "202", Titles: []string{"Example Series"}, Years: []int{2018}}, []string{"example series"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.AIOMatchBatch(ctx, 100)
	decide := func(r model.AIORecord, find AIOCandidateSearch) (model.AIOMatchResult, error) {
		return model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "pending episode year", Seasons: []model.AIOSeasonRequest{{TMDB: "202", Season: 1}}}, nil
	}
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.aio_metadata_jobs ADD CONSTRAINT synthetic_request_failure CHECK(false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MatchAIOBatch(ctx, rows, decide); err == nil {
		t.Fatal("season queue failure was accepted")
	}
	var matches, requests, queue int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_matches),(SELECT count(*) FROM ingest.aio_season_requests),(SELECT count(*) FROM ingest.aio_match_queue)`).Scan(&matches, &requests, &queue); err != nil || matches != 0 || requests != 0 || queue != 1 {
		t.Fatalf("partial matching publication: %d %d %d %v", matches, requests, queue, err)
	}
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.aio_metadata_jobs DROP CONSTRAINT synthetic_request_failure`); err != nil {
		t.Fatal(err)
	}
	works, _, _ := db.AIOCandidates(ctx, "example series", "", "", "")
	stale := works[0]
	stale.Revision++
	if _, err := db.MatchAIOBatch(ctx, rows, func(r model.AIORecord, _ AIOCandidateSearch) (model.AIOMatchResult, error) {
		return model.AIOMatchResult{Record: r, Status: "matched", Work: &stale}, nil
	}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale metadata was published", err)
	}
	if _, err := db.MatchAIOBatch(ctx, rows, decide); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "202" {
		t.Fatal("required episode fetch was lost", jobs, err)
	}
}

func TestAIOMatchCandidateEligibilityPrecedesOverflowLimit(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years)
SELECT 'movie',n::text,ARRAY[2021] FROM generate_series(1,150) n;
INSERT INTO ingest.aio_works(media_type,tmdb_id,years)
SELECT 'tv',n::text,ARRAY[1990] FROM generate_series(151,300) n;
INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,'Example Serial','example serial' FROM ingest.aio_works;`); err != nil {
		t.Fatal(err)
	}
	w := model.AIOWork{Type: "tv", TMDB: "301", IMDb: "tt1234567", Titles: []string{"Example Serial"}, Years: []int{2018}, SeasonYears: map[int]int{1: 2019}, EpisodeYears: map[string]int{"1:2": 2021}}
	if err := db.SaveAIOWork(ctx, w, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "source", "1", `{"title":"Example.Serial.S01E02.2021"}`, nil)
	rows, _ := db.AIOMatchBatch(ctx, 100)
	year, episode := 2021, 2
	filter := model.AIOCandidateFilter{Title: "example serial", Type: "tv", Year: &year, Seasons: []int{1}, Episode: &episode}
	results, err := db.MatchAIOBatch(ctx, rows, func(r model.AIORecord, find AIOCandidateSearch) (model.AIOMatchResult, error) {
		works, overflow, err := find(filter)
		if err != nil || overflow || len(works) != 1 || works[0].TMDB != "301" {
			t.Fatalf("impossible homonyms hid the valid episode candidate: %+v overflow=%v %v", works, overflow, err)
		}
		return model.AIOMatchResult{Record: r, Status: "matched", Work: &works[0]}, nil
	})
	if err != nil || len(results) != 1 {
		t.Fatal(results, err)
	}
	// The safety cap still rejects a genuinely crowded eligible candidate set.
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years)
SELECT 'tv',n::text,ARRAY[2021] FROM generate_series(302,411) n;
INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,'Example Serial','example serial' FROM ingest.aio_works WHERE tmdb_id::int>=302;
INSERT INTO ingest.aio_match_queue(provider_id,source_id) VALUES('source','1')`); err != nil {
		t.Fatal(err)
	}
	_, err = db.MatchAIOBatch(ctx, rows, func(r model.AIORecord, find AIOCandidateSearch) (model.AIOMatchResult, error) {
		works, overflow, err := find(filter)
		if err != nil || !overflow || len(works) != 101 {
			t.Fatalf("real candidate overflow was suppressed: count=%d %v %v", len(works), overflow, err)
		}
		return model.AIOMatchResult{Record: r, Status: "ambiguous", Reason: "candidate safety cap"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAIOMetadataDeletionKeepsNewerJobsAndNoOpRefresh(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	jobs, _ := db.AIOMetadataJobs(ctx)
	if err := db.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteAIOMetadataJob(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	jobs, _ = db.AIOMetadataJobs(ctx)
	if len(jobs) != 1 || jobs[0].Version != 2 {
		t.Fatal("older 404 acknowledged a later notification", jobs)
	}
	if err := db.DeleteAIOMetadataJob(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"101", "102"} {
		if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: id, Titles: []string{"A Title"}, Years: []int{2021}}, []string{"a title"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.aio_works SET updated_at=now()-interval '31 days' WHERE tmdb_id='101'`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.QueueAIORefresh(ctx); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ = db.AIOMetadataJobs(ctx)
	if len(jobs) != 1 || jobs[0].ID != "101" || jobs[0].Version != 1 {
		t.Fatal("freshness audit requeued recent data or reset a durable job", jobs)
	}
}
