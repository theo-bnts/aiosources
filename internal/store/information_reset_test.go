package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/theo-bnts/aiosources/internal/model"
)

func informationResetFixture(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,display_title,years) VALUES
 ('book',NULL,'OL100W','Synthetic Book','{2024}'),('movie','101','','Synthetic Film','{2024}'),('tv','202','','Synthetic Series','{2025}');
INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,display_title,lower(display_title) FROM ingest.aio_works;
INSERT INTO ingest.ol_authors(id,name) VALUES('OL200A','Synthetic Author');
INSERT INTO ingest.ol_editions(id,work_keys,title,normalized,isbn) VALUES('OL300M','{OL100W}','Synthetic Edition','synthetic edition','{9780306406157}');
INSERT INTO ingest.ol_redirects(source,target) VALUES('OL400W','OL100W');
UPDATE ingest.ol_import_state SET works=1,authors=1,editions=1,position=1234,complete=TRUE,data_complete=TRUE;
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
 ('fixture','book',jsonb_build_object('title','Synthetic Book EPUB','info_hash',repeat('a',40))),
 ('mirror','book-copy',jsonb_build_object('title','Synthetic Book EPUB','info_hash',repeat('a',40))),
 ('fixture','movie',jsonb_build_object('title','Synthetic Film 1080p','info_hash',repeat('b',40))),
 ('fixture','tv',jsonb_build_object('title','Synthetic Series S01E01','info_hash',repeat('c',40))),
 ('fixture','denied-book',jsonb_build_object('title','Excluded Book EPUB','info_hash',repeat('d',40))),
 ('fixture','denied-video',jsonb_build_object('title','Excluded Film','imdb_id','tt1234567','info_hash',repeat('e',40))),
 ('fixture','detached-book',jsonb_build_object('title','Opaque Filename','info_hash',repeat('f',40))),
 ('fixture','ambiguous-book',jsonb_build_object('title','Undecided Book EPUB','info_hash',repeat('9',40))),
 ('fixture','unmatched-video',jsonb_build_object('title','Unmatched Film 1080p','info_hash',repeat('0',40))),
 ('fixture','other',jsonb_build_object('title','Other Data','info_hash',repeat('6',40)));
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture'
 FROM ingest.torrents t JOIN ingest.aio_works w ON w.media_type=t.source_id;
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,status,reason)
 SELECT provider_id,source_id,fields_fingerprint,CASE source_id WHEN 'ambiguous-book' THEN 'ambiguous' ELSE 'unmatched' END,'Synthetic undecided record'
 FROM ingest.torrents WHERE source_id IN ('ambiguous-book','unmatched-video');
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,openlibrary_id)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,md5(ingest.aio_review_evidence(t.fields)::text),w.media_type,COALESCE(w.tmdb_id,''),w.openlibrary_id
 FROM ingest.torrents t JOIN ingest.aio_works w ON w.media_type=t.source_id;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,openlibrary_id)
 SELECT provider_id,source_id,fields_fingerprint,md5(ingest.aio_review_evidence(fields)::text),
 CASE source_id WHEN 'detached-book' THEN 'book' ELSE '' END,'',CASE source_id WHEN 'detached-book' THEN 'OL999W' ELSE '' END
 FROM ingest.torrents WHERE source_id IN ('denied-book','denied-video','detached-book');
DELETE FROM ingest.aio_match_queue;
INSERT INTO ingest.aio_availability(service,account,hash,cached,checked_at)
 SELECT 'torbox',account,repeat(hash,40),TRUE,now() FROM unnest(ARRAY['one','two']) account CROSS JOIN unnest(ARRAY['a','b','c','d','e','f','6','7','8']) hash;
INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files)
 SELECT 'torbox',repeat(hash,40),TRUE,now(),'[]' FROM unnest(ARRAY['a','b','c','d','e','f','6','7','8']) hash;
INSERT INTO ingest.aio_resources(service,account,resource,hashes,checked_at,last_attempt) VALUES
 ('torbox','one','book:openlibrary:OL100W',ARRAY[repeat('a',40),repeat('7',40)],now(),now()),
 ('torbox','one','movie:tmdb:101',ARRAY[repeat('b',40),repeat('8',40)],now(),now()),
 ('torbox','one','other:unknown',ARRAY[repeat('6',40)],now(),now());
INSERT INTO ingest.aio_cleanup(account,hash,magnet_id) VALUES('one',repeat('a',40),123);
INSERT INTO ingest.aio_preparations(service,account,hash,provider_id,expires_at) VALUES('realdebrid','one',repeat('a',40),'123',now()+interval '1 day');
INSERT INTO ingest.operation_runs(id,kind,service,trigger,resource,lock_key,status,finished_at) VALUES
 ('book-meta','metadata','openlibrary','manual','',101,'succeeded',now()),
 ('video-meta','metadata','tmdb','manual','',102,'succeeded',now()),
 ('book-cache','cache','torbox','playback','book:openlibrary:OL100W',103,'succeeded',now()),
 ('video-cache','cache','torbox','playback','movie:imdb:tt1234567',104,'succeeded',now()),
 ('book-download','download','torbox','manual','book:openlibrary:OL100W',105,'succeeded',now()),
 ('video-download','download','torbox','manual','movie:tmdb:101',106,'succeeded',now()),
 ('book-public','download','torbox','public','book:openlibrary:OL999W',107,'succeeded',now()),
 ('shared-matching','matching','local','manual','',108,'succeeded',now());
INSERT INTO ingest.operation_events(run_id,phase,processed,errors) SELECT id,'succeeded',1,0 FROM ingest.operation_runs;
INSERT INTO ingest.operation_downloads(run_id,work_id,input,account)
 SELECT CASE t.source_id WHEN 'movie' THEN 'video-download' ELSE 'book-download' END,w.id,jsonb_build_object('occurrence_id',t.occurrence_id),'one'
 FROM ingest.torrents t JOIN ingest.aio_works w ON w.media_type=t.source_id WHERE t.source_id IN ('book','movie');
INSERT INTO ingest.activity_logs(event_id,level,kind,message,run_id,service)
 SELECT 'test-'||id,'info','test.operation','Synthetic operation',id,CASE WHEN kind='metadata' THEN service ELSE '' END FROM ingest.operation_runs;
INSERT INTO ingest.activity_logs(event_id,level,kind,message,service) VALUES
 ('book-summary','info','openlibrary.sync_completed','Synthetic book update','openlibrary'),
 ('video-summary','info','tmdb.sync_completed','Synthetic video update','tmdb'),
 ('security','info','security.session_revoked','Synthetic security event','');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []model.StremioRequest{
		{MediaType: "movie", Resource: "imdb:tt1234567", Outcome: "empty"},
		{MediaType: "invalid", Resource: "Manifest", Outcome: "results", Kind: "manifest"},
	} {
		if err := db.RecordStremioRequest(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, db
}

func TestInformationResetForeignKeyLookupIsIndexedWithoutAStatusFilter(t *testing.T) {
	ctx, db := informationResetFixture(t)
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SET LOCAL enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err = tx.QueryRow(ctx, `EXPLAIN(FORMAT JSON) UPDATE ingest.aio_matches SET work_id=NULL WHERE work_id=1`).Scan(&plan); err != nil || !strings.Contains(plan, "aio_matches_work") {
		t.Fatal("foreign-key cleanup would scan every identification per deleted work", plan, err)
	}
}

func TestInformationResetRemovesOnlyItsDependentData(t *testing.T) {
	for _, scope := range []string{"books", "videos", "metadata"} {
		t.Run(scope, func(t *testing.T) {
			ctx, db := informationResetFixture(t)
			if err := db.resetInformation(ctx, scope); err != nil {
				t.Fatal(err)
			}
			var works, reviews, queue, accounts, shared, operations, events, continuations, logs, requests, torrents, ownership, preparations int
			err := db.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_match_reviews),(SELECT count(*) FROM ingest.aio_match_queue),
 (SELECT count(*) FROM ingest.aio_availability),(SELECT count(*) FROM ingest.aio_shared_availability),
 (SELECT count(*) FROM ingest.operation_runs),(SELECT count(*) FROM ingest.operation_events),(SELECT count(*) FROM ingest.operation_downloads),
 (SELECT count(*) FROM ingest.activity_logs),(SELECT count(*) FROM ingest.stremio_requests),(SELECT count(*) FROM ingest.torrents),
 (SELECT count(*) FROM ingest.aio_cleanup),(SELECT count(*) FROM ingest.aio_preparations)`).Scan(
				&works, &reviews, &queue, &accounts, &shared, &operations, &events, &continuations, &logs, &requests, &torrents, &ownership, &preparations)
			if err != nil {
				t.Fatal(err)
			}
			got := []int{works, reviews, queue, accounts, shared, operations, events, continuations, logs, requests, torrents, ownership, preparations}
			want := map[string][]int{
				"books":    {2, 3, 5, 10, 5, 4, 4, 1, 6, 2, 10, 1, 1},
				"videos":   {1, 3, 4, 10, 5, 5, 5, 1, 7, 1, 10, 1, 1},
				"metadata": {0, 0, 10, 2, 1, 0, 0, 0, 1, 1, 10, 1, 1},
			}[scope]
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("reset %s did not preserve independent data: got %v, want %v", scope, got, want)
			}
			var decisions []string
			if err := db.pool.QueryRow(ctx, `SELECT ARRAY(SELECT source_id FROM ingest.aio_matches ORDER BY source_id)`).Scan(&decisions); err != nil {
				t.Fatal(err)
			}
			wantDecisions := map[string][]string{
				"books": {"movie", "tv", "unmatched-video"}, "videos": {"ambiguous-book", "book"}, "metadata": {},
			}[scope]
			if !reflect.DeepEqual(decisions, wantDecisions) {
				t.Fatal("reset retained automatic decisions from the removed category", decisions, wantDecisions)
			}
			var otherCached bool
			if err := db.pool.QueryRow(ctx, `SELECT cached FROM ingest.aio_shared_availability WHERE hash=repeat('6',40)`).Scan(&otherCached); err != nil || !otherCached {
				t.Fatal("unrelated availability was removed", err)
			}
			// The two copies and two accounts must not retain a book observation.
			if scope != "videos" {
				var remaining int
				if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_availability WHERE hash=repeat('a',40)`).Scan(&remaining); err != nil || remaining != 0 {
					t.Fatal("a source/account copy retained reset availability", remaining, err)
				}
			}
		})
	}
}

func TestBookResetDoesNotClassifyAVideoFromAnInactiveReview(t *testing.T) {
	ctx, db := informationResetFixture(t)
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.aio_match_reviews SET media_type='book',tmdb_id='',openlibrary_id='OL100W',active=FALSE WHERE source_id='movie'`); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetBookInformation(ctx); err != nil {
		t.Fatal(err)
	}
	var cached, matched bool
	if err := db.pool.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM ingest.aio_shared_availability WHERE hash=repeat('b',40) AND cached),
 EXISTS(SELECT 1 FROM ingest.aio_matches m JOIN ingest.aio_works w ON w.id=m.work_id WHERE m.source_id='movie' AND w.media_type='movie')`).Scan(&cached, &matched); err != nil || !cached || !matched {
		t.Fatal("inactive book review removed current video data", cached, matched, err)
	}
}

func TestInformationResetRefusesALiveDownloadAndRollsBack(t *testing.T) {
	ctx, db := informationResetFixture(t)
	const name = "catalog-download:torbox:synthetic-book"
	release, locked, err := db.AIOLock(ctx, name)
	if err != nil || !locked {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if _, err := db.BeginOperation(ctx, model.OperationRun{Kind: "download", Service: "torbox", Trigger: "manual", Resource: "book:openlibrary:OL100W"}, name); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetBookInformation(ctx); !errors.Is(err, model.ErrBusy) {
		t.Fatal("reset raced a live download", err)
	}
	var works, reviews, availability int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_match_reviews),(SELECT count(*) FROM ingest.aio_availability)`).Scan(&works, &reviews, &availability); err != nil || works != 3 || reviews != 6 || availability != 18 {
		t.Fatal("busy reset partially removed data", works, reviews, availability, err)
	}
	release()
	if err := db.ResetBookInformation(ctx); err != nil {
		t.Fatal("completed download kept reset blocked", err)
	}
}

func TestInformationResetRollsBackEveryDependentDeletion(t *testing.T) {
	ctx, db := informationResetFixture(t)
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.aio_state ADD CONSTRAINT reset_failure CHECK(cache_generation=1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetBookInformation(ctx); err == nil {
		t.Fatal("injected reset failure was ignored")
	}
	var works, reviews, operations, availability int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_match_reviews),(SELECT count(*) FROM ingest.operation_runs),(SELECT count(*) FROM ingest.aio_availability)`).Scan(&works, &reviews, &operations, &availability); err != nil || works != 3 || reviews != 6 || operations != 8 || availability != 18 {
		t.Fatal("failed reset left partial deletions", works, reviews, operations, availability, err)
	}
}

func TestCacheGenerationFenceRejectsConcurrentPreResetWriters(t *testing.T) {
	ctx, db := sharingDatabase(t)
	config := db.pool.Config()
	db.pool.Close()
	config.MaxConns, config.MinConns, config.MinIdleConns = 4, 0, 0
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	db.pool = pool
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	generation, err := db.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The reset transaction and three cache writers occupy the smallest
	// supported pool. Inspect their waits without needing a fifth pooled slot.
	control, err := pgx.ConnectConfig(ctx, config.ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer closeConnection(control)
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `SELECT cache_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	item := model.AIOAvailability{Generation: generation, Hash: strings.Repeat("a", 40), Cached: true, CheckedAt: time.Now()}
	results := make(chan error, 3)
	go func() { results <- db.SaveAIOAvailability(ctx, "torbox", "one", item) }()
	go func() { results <- db.SaveSharedAvailability(ctx, "torbox", item) }()
	go func() {
		results <- db.SaveAIOResource(ctx, model.AIOResource{Generation: generation, Service: "torbox", Account: "one", Key: "book:openlibrary:OL100W", Hashes: []string{item.Hash}, CheckedAt: item.CheckedAt, LastAttempt: item.CheckedAt})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := control.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'WITH generation AS MATERIALIZED%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pre-reset cache writes were not fenced", waiting)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := tx.Exec(ctx, `UPDATE ingest.aio_state SET cache_generation=cache_generation+1`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_availability)+(SELECT count(*) FROM ingest.aio_shared_availability)+(SELECT count(*) FROM ingest.aio_resources)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("pre-reset writer resurrected an observation", count, err)
	}
}
