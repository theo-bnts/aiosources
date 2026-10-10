package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestWordEpisodeUpgradeRequeuesOnceWithoutChangingPriorMigrationOrReviews(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "tv", TMDB: "1", Titles: []string{"Example Serial"}}, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
('example','ep','{"title":"Example.Serial.S02.Ep03"}'),
('example','episode','{"title":"Example.Serial.S02.Episode 03"}'),
('example','working','{"title":"Example.Serial.S02E03"}'),
('example','reviewed','{"title":"Example.Serial.S02.Ep03"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,md5(fields::text),1,1,'matched','previous decision' FROM ingest.torrents;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id)
SELECT provider_id,source_id,md5(fields::text),md5(ingest.aio_review_evidence(fields)::text),'tv','1' FROM ingest.torrents WHERE source_id='reviewed';
DELETE FROM ingest.aio_match_queue WHERE source_id<>'ep';
UPDATE ingest.aio_match_queue SET version=7;`)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if _, err = db.pool.Exec(ctx, wordEpisodeRecheckSchema); err != nil {
			t.Fatal(err)
		}
		var ids []string
		if err = db.pool.QueryRow(ctx, `SELECT array_agg(source_id ORDER BY source_id) FROM ingest.aio_match_queue`).Scan(&ids); err != nil || len(ids) != 2 || ids[0] != "ep" || ids[1] != "episode" {
			t.Fatalf("wrong word-numbering recheck: %v (%v)", ids, err)
		}
		var version, published int
		if err = db.pool.QueryRow(ctx, `SELECT version FROM ingest.aio_match_queue WHERE source_id='ep'`).Scan(&version); err != nil || version != 7 {
			t.Fatal("upgrade reset existing queued work", version, err)
		}
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_matches WHERE status='matched'`).Scan(&published); err != nil || published != 4 {
			t.Fatal("upgrade erased existing decisions", published, err)
		}
	}
}
