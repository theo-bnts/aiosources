package store

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestBookChangeUpgradePreservesAnImportedCatalogAndItsCheckpoint(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 78); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL100W','The Amber Orchard','{}')`); err != nil {
		t.Fatal(err)
	}
	_, err = db.pool.Exec(ctx, `UPDATE ingest.ol_import_state SET complete=TRUE,snapshot='https://archive.org/download/ol_dump_2026-01-01/all.gz',position=1234,works=1,last_sync=now();
INSERT INTO ingest.settings(key,value) VALUES('aiosource.config.v2','{"revision":4,"openlibrary_days":30,"openlibrary_enabled":true}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := db.OpenLibraryStatus(ctx)
	if err != nil || !status.Complete || !status.DataComplete || status.Position != 1234 || status.Works != 1 {
		t.Fatal("upgrade reset the imported catalog", status, err)
	}
	config, err := db.AIOConfig(ctx)
	if err != nil || config.OpenLibraryMinutes != 15 || !config.OpenLibraryEnabled {
		t.Fatal("upgrade did not preserve enabled state and migrate the cadence", config.OpenLibraryMinutes, err)
	}
	var title string
	if err = db.pool.QueryRow(ctx, `SELECT display_title FROM ingest.aio_works WHERE openlibrary_id='OL100W'`).Scan(&title); err != nil || title != "The Amber Orchard" {
		t.Fatal("existing book was lost", title, err)
	}
}

func TestBookResetClearsOnlyBookChangesAndKeepsVideoAndSources(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "123", Titles: []string{"Synthetic Movie"}}, []string{"synthetic movie"}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueueBookChanges(ctx, []books.ChangeKey{{ID: "OL100W", Revision: 2, At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.ol_deleted(id,ol_revision) VALUES('OL200W',3);
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','one','{"title":"The Amber Orchard EPUB"}');
UPDATE ingest.ol_changes_state SET anchor_id='100',anchor_at=now(),needs_dump_after=now(),error='Pending recovery';`); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetBookInformation(ctx); err != nil {
		t.Fatal(err)
	}
	var jobs, deleted, videos, torrents int
	err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.ol_change_jobs),(SELECT count(*) FROM ingest.ol_deleted),(SELECT count(*) FROM ingest.aio_works WHERE media_type='movie'),(SELECT count(*) FROM ingest.torrents)`).Scan(&jobs, &deleted, &videos, &torrents)
	state, stateErr := db.BookChangesState(ctx)
	if err != nil || stateErr != nil || jobs != 0 || deleted != 0 || videos != 1 || torrents != 1 || state.AnchorID != "" || state.NeedsDumpAfter != nil || state.Error != "" {
		t.Fatal("book reset retained stale jobs or touched other data", jobs, deleted, videos, torrents, state, err, stateErr)
	}
}

func TestBookIdentifierLookupUsesItsIndexAndIgnoresMalformedObjects(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
 ('fixture','one','{"title":"Opaque Filename EPUB","attributes":{"isbn10":["1861972717"]}}'),
 ('fixture','bad','{"title":"Unrelated","attributes":"not JSON"}');`); err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err := db.pool.QueryRow(ctx, `SELECT ingest.aio_native_book_candidates(fields) FROM ingest.torrents WHERE source_id='one'`).Scan(&keys); err != nil || len(keys) != 1 || keys[0] != "isbn:978186197271" {
		t.Fatal("native book identifier normalization failed", keys, err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SET LOCAL enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err = tx.QueryRow(ctx, `EXPLAIN(FORMAT JSON) SELECT provider_id,source_id FROM ingest.torrents WHERE (`+nativeBookAssertionPredicate+`) AND ingest.aio_native_book_candidates(fields) && ARRAY['isbn:978186197271']`).Scan(&plan); err != nil || !strings.Contains(plan, "aio_torrents_native_books") {
		t.Fatal("incremental book discovery would scan every source row", plan, err)
	}
}
