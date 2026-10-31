package store

import (
	"github.com/theo-bnts/aiosources/internal/books"
	"testing"
	"time"
)

func TestBookSelectionPreservesAnExplicitManualChoiceForAnOpaqueSourceRecord(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','opaque','{"title":"Opaque Synthetic Record"}');
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,openlibrary_id)
 SELECT provider_id,source_id,fields_fingerprint,md5(ingest.aio_review_evidence(fields)::text),'book','','OL501W' FROM ingest.torrents;
 INSERT INTO ingest.ol_redirects(source,target) VALUES('OL501W','OL500W');`); err != nil {
		t.Fatal(err)
	}
	e := books.Edition{ID: "OL600M", Works: []string{"OL500W"}, Title: "Synthetic Printed Edition", Normalized: "synthetic printed edition"}
	for _, active := range []bool{true, false} {
		if _, err := db.pool.Exec(ctx, `UPDATE ingest.aio_match_reviews SET active=$1`, active); err != nil {
			t.Fatal(err)
		}
		selection, err := db.BookImportSelection(ctx, DefaultAIOConfig())
		if err != nil || selection.Allows(e) != active {
			t.Fatal("bulk selection lost explicit book evidence", active, err)
		}
		relevant, err := db.BookChangeRelevant(ctx, books.Record{Edition: &e})
		if err != nil || relevant != active {
			t.Fatal("incremental selection disagreed with the bulk import", active, relevant, err)
		}
	}
}

func TestBookSelectionChangePreservesQueuedChangesAndDeletionEvidence(t *testing.T) {
	ctx, db := sharingDatabase(t)
	c := DefaultAIOConfig()
	if err := db.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := db.PrepareBookImport(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := db.QueueBookDependencies(ctx, []books.ChangeKey{{ID: "OL500W", Revision: 2, At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.ol_deleted(id,ol_revision,ol_modified_at)VALUES('OL600M',3,now());
UPDATE ingest.ol_changes_state SET anchor_id='42',anchor_at=now(),tail_start_id='40',tail_since=now()-interval '1 day',needs_dump_after=now()-interval '1 hour' WHERE singleton;`); err != nil {
		t.Fatal(err)
	}
	c, err := db.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.OpenLibraryLanguages = []string{"fr"}
	if err = db.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	state, err := db.BookChangesState(ctx)
	if err != nil || state.AnchorID != "42" || state.TailID != "40" || state.NeedsDumpAfter == nil {
		t.Fatal("selection erased change continuity", state, err)
	}
	var jobs, deletions int
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.ol_change_jobs),(SELECT count(*) FROM ingest.ol_deleted)`).Scan(&jobs, &deletions); err != nil || jobs != 1 || deletions != 1 {
		t.Fatal("selection erased pending corrections or deletions", jobs, deletions, err)
	}
}
