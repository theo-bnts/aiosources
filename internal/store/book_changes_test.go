package store

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestBookResetClearsOnlyBookChangesAndKeepsVideoAndSources(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "123", Titles: []string{"Synthetic Movie"}}, []string{"synthetic movie"}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueueBookChanges(ctx, []books.ChangeKey{{ID: "OL100W", Revision: 2, At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.ol_deleted(id,ol_revision) VALUES('OL200W',3);
INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','one','{"title":"The Amber Orchard EPUB"}');
UPDATE aiosources.ol_changes_state SET anchor_id='100',anchor_at=now(),needs_dump_after=now(),error='Pending recovery';`); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetBookInformation(ctx); err != nil {
		t.Fatal(err)
	}
	var jobs, deleted, videos, torrents int
	err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.ol_change_jobs),(SELECT count(*) FROM aiosources.ol_deleted),(SELECT count(*) FROM aiosources.aio_works WHERE media_type='movie'),(SELECT count(*) FROM aiosources.torrents)`).Scan(&jobs, &deleted, &videos, &torrents)
	state, stateErr := db.BookChangesState(ctx)
	if err != nil || stateErr != nil || jobs != 0 || deleted != 0 || videos != 1 || torrents != 1 || state.AnchorID != "" || state.NeedsDumpAfter != nil || state.Error != "" {
		t.Fatal("book reset retained stale jobs or touched other data", jobs, deleted, videos, torrents, state, err, stateErr)
	}
}

func TestBookIdentifierLookupUsesItsIndexAndIgnoresMalformedObjects(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES
 ('fixture','one','{"title":"Opaque Filename EPUB","attributes":{"isbn10":["1861972717"]}}'),
 ('fixture','bad','{"title":"Unrelated","attributes":"not JSON"}');`); err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err := db.pool.QueryRow(ctx, `SELECT aiosources.aio_native_book_candidates(fields) FROM aiosources.torrents WHERE source_id='one'`).Scan(&keys); err != nil || len(keys) != 1 || keys[0] != "isbn:978186197271" {
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
	if err = tx.QueryRow(ctx, `EXPLAIN(FORMAT JSON) SELECT provider_id,source_id FROM aiosources.torrents WHERE (`+nativeBookAssertionPredicate+`) AND aiosources.aio_native_book_candidates(fields) && ARRAY['isbn:978186197271']`).Scan(&plan); err != nil || !strings.Contains(plan, "aio_torrents_native_books") {
		t.Fatal("incremental book discovery would scan every source row", plan, err)
	}
}
