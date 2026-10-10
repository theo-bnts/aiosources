package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestPeersRetirementPreservesFreshAndStaleIdentificationAndReviews(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 46); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"fresh", "stale", "queued"} {
		publishSharingRow(t, ctx, db, "fixture", id, `{"title":"Example Film 2026","size":9007199254740993,"peers":12,"attributes":{"peers":["12"],"imdbid":["tt123"]}}`, nil)
	}
	if _, err = db.pool.Exec(ctx, `
INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','123','tt123',ARRAY[2026]);
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT provider_id,source_id,CASE WHEN source_id='stale' THEN repeat('0',32) ELSE md5(fields::text) END,1,1,'matched','verified' FROM ingest.torrents;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id)
 SELECT provider_id,source_id,md5(fields::text),md5(ingest.aio_review_evidence(fields)::text),'movie','123' FROM ingest.torrents;
DELETE FROM ingest.aio_match_queue;
INSERT INTO ingest.aio_match_queue(provider_id,source_id) VALUES('fixture','queued');`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"torrents", "catalog_journal", "torrent_history_baselines"} {
		var count int
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table+` WHERE fields @? '$.**.peers'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retired peers remain in %s: %d %v", table, count, err)
		}
	}
	items, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{})
	if err != nil || items.Total != 3 {
		t.Fatalf("diagnostics: %+v %v", items, err)
	}
	for _, item := range items.Items {
		want := "pending"
		if item.SourceID == "fresh" {
			want = "matched"
		}
		if item.Status != want {
			t.Fatalf("retirement changed decision freshness: %+v", item)
		}
		detail, e := db.AIOReviewDetail(ctx, item.OccurrenceID)
		if e != nil || detail.Review == nil || !detail.Review.Active || detail.Review.SourceChanged || detail.Review.Fingerprint != detail.Record.Fingerprint {
			t.Fatalf("retirement lost manual review: %+v %v", detail, e)
		}
	}
	publishSharingRow(t, ctx, db, "fixture", "new", `{"title":"New","peers":99,"nested":[{"peers":4,"kept":"value"}]}`, nil)
	var exists bool
	if err = db.pool.QueryRow(ctx, `SELECT fields @? '$.**.peers' FROM ingest.torrents WHERE source_id='new'`).Scan(&exists); err != nil || exists {
		t.Fatalf("storage boundary retained peers: %v %v", exists, err)
	}
}
