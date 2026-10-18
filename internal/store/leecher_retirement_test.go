package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestLeecherRetirementPreservesFreshAndStaleIdentificationAndReviews(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 60); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"fresh", "stale", "queued"} {
		publishSharingRow(t, ctx, db, "fixture", id, `{"title":"Example Film 2026","size":9007199254740993,"leechers":12,"attributes":{"leechers":["12"],"imdbid":["tt123"]}}`, nil)
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
	shares := []model.ShareCredential{}
	for _, name := range []string{"Mixed", "Counter only"} {
		share, err := db.CreateShare(ctx, model.ShareInput{Name: name, Scope: "all", Fields: []string{"title"}})
		if err != nil {
			t.Fatal(err)
		}
		shares = append(shares, share)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.shares SET enabled=TRUE,fields=CASE WHEN id=$1 THEN ARRAY['title','leechers'] ELSE ARRAY['leechers'] END WHERE id IN ($1,$2)`, shares[0].Share.ID, shares[1].Share.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for index, original := range shares {
		share, err := db.Share(ctx, original.Share.ID)
		if err != nil || (index == 0 && (len(share.Fields) != 1 || share.Fields[0] != "title" || !share.Enabled)) || (index == 1 && (len(share.Fields) != 0 || share.Enabled)) {
			t.Fatalf("retired sharing permissions were widened or lost: %+v %v", share, err)
		}
	}
	for _, table := range []string{"torrents", "staged_torrents", "raw_records", "catalog_journal", "catalog_baselines", "torrent_history_baselines"} {
		var count int
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table+` WHERE fields @? '$.**.leechers'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retired leechers remain in %s: %d %v", table, count, err)
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
	publishSharingRow(t, ctx, db, "fixture", "new", `{"title":"New","leechers":99,"nested":[{"leechers":4,"kept":"value"}]}`, nil)
	var exists bool
	if err = db.pool.QueryRow(ctx, `SELECT fields @? '$.**.leechers' FROM ingest.torrents WHERE source_id='new'`).Scan(&exists); err != nil || exists {
		t.Fatalf("storage boundary retained leechers: %v %v", exists, err)
	}
}
