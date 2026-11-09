package store

import (
	"github.com/theo-bnts/aiosources/internal/model"
	"testing"
)

func TestFilteredBookReconciliationKeepsEditionEvidenceAliasesAndItsResumeBoundary(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Example film"}}, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,openlibrary_id,display_title,years,ol_seen,ol_revision,ol_modified_at)
 SELECT 'book','OL'||n||'W','Example book','{}',CASE WHEN n IN(5,6) THEN 0 ELSE 1 END,1,CASE WHEN n=5 THEN now() ELSE '2020-01-01'::timestamptz END FROM generate_series(1,1100) n;
 INSERT INTO aiosources.ol_redirects(source,target) VALUES('OL9999W','OL4W');
 INSERT INTO aiosources.ol_editions(id,work_keys,title,normalized,ol_seen)VALUES
 ('OL1M',ARRAY['OL1W'],'Example edition','example edition',1),
 ('OL2M',ARRAY['OL2W','OL3W'],'Example edition','example edition',1),
 ('OL3M',ARRAY['OL9999W'],'Example edition','example edition',1),
 ('OL5M',ARRAY['OL5W'],'Example edition','example edition',1),
 ('OL6M',ARRAY['OL6W'],'Example edition','example edition',1);
 UPDATE aiosources.ol_import_state SET generation=1,snapshot_at='2026-01-01',data_complete=TRUE,export_phase=5,prune_table=3,works=1100,position=12345;`); err != nil {
		t.Fatal(err)
	}
	// Compare the batched membership proof with the original predicate on a
	// small fixture, including aliases, shared editions and missing editions.
	rows, err := db.pool.Query(ctx, `SELECT b.openlibrary_id,b.excluded,NOT EXISTS(SELECT 1 FROM aiosources.ol_editions e WHERE e.work_keys && aiosources.ol_aliases(b.openlibrary_id)) AS expected FROM (`+bookSnapshotWorkBatchSQL+`) b(openlibrary_id,revision,seen,modified,excluded)`, "")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var key string
		var excluded, expected bool
		if err = rows.Scan(&key, &excluded, &expected); err != nil {
			t.Fatal(err)
		}
		if excluded != expected {
			t.Fatal("batched edition evidence changed", key)
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count != 1000 {
		t.Fatal("candidate batch was not bounded", count, err)
	}
	done, inspected, err := db.PruneBookSnapshot(ctx)
	if err != nil || done || inspected != 1000 {
		t.Fatal("first cleanup batch", done, inspected, err)
	}
	var after string
	if err = db.pool.QueryRow(ctx, `SELECT prune_after FROM aiosources.ol_import_state`).Scan(&after); err != nil || after == "" {
		t.Fatal("cleanup did not save its resume boundary", err)
	}
	total := inspected
	for range 5 {
		done, inspected, err = db.PruneBookSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		total += inspected
		if done {
			break
		}
	}
	var works, videos int
	var position int64
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.aio_works WHERE media_type='book'),(SELECT count(*) FROM aiosources.aio_works WHERE media_type='movie'),position FROM aiosources.ol_import_state`).Scan(&works, &videos, &position); err != nil || !done || total != 1100 || works != 5 || videos != 1 || position != 12345 {
		t.Fatal("cleanup lost selected data, resume progress or video data", works, videos, total, position, err)
	}
}
