package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMatchingUpgradeRequeuesAffectedRecordsOnceAndPreservesReviews(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "1", Titles: []string{"Example Film"}}, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
('example','working','{"title":"Example.Film.2021"}'),
('example','miss','{"title":"Example.Film.2021"}'),
('example','ambiguous','{"title":"Example.Film.2021"}'),
('example','embedded-marker','{"title":"Zone51.2021"}'),
('example','reversed','{"title":"Example.Serial.S03-S01.2021"}'),
('example','oversized','{"title":"Example.Serial.S01E001-E500.2021"}'),
('example','valid-pack','{"title":"Example.Serial.S01-S03.2021"}'),
('example','reviewed','{"title":"Example.Film.2021"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,md5(fields::text),1,1,CASE source_id WHEN 'miss' THEN 'unmatched' WHEN 'ambiguous' THEN 'ambiguous' ELSE 'matched' END,'previous decision' FROM ingest.torrents;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id)
SELECT provider_id,source_id,md5(fields::text),md5(ingest.aio_review_evidence(fields)::text),'movie','1' FROM ingest.torrents WHERE source_id='reviewed';
UPDATE ingest.aio_matches SET status='unmatched' WHERE source_id='reviewed';
DELETE FROM ingest.aio_match_queue WHERE source_id<>'miss';
UPDATE ingest.aio_match_queue SET version=7;`)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if _, err = db.pool.Exec(ctx, matchingRecheckSchema); err != nil {
			t.Fatal(err)
		}
		var ids []string
		if err = db.pool.QueryRow(ctx, `SELECT array_agg(source_id ORDER BY source_id) FROM ingest.aio_match_queue`).Scan(&ids); err != nil {
			t.Fatal(err)
		}
		if len(ids) != 5 || ids[0] != "ambiguous" || ids[1] != "embedded-marker" || ids[2] != "miss" || ids[3] != "oversized" || ids[4] != "reversed" {
			t.Fatalf("wrong upgrade scope: %v", ids)
		}
		var version int
		if err = db.pool.QueryRow(ctx, `SELECT version FROM ingest.aio_match_queue WHERE source_id='miss'`).Scan(&version); err != nil || version != 7 {
			t.Fatal("upgrade reset pending work", version, err)
		}
		var published int
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_matches WHERE status='matched'`).Scan(&published); err != nil || published != 5 {
			t.Fatal("upgrade erased working matches", published, err)
		}
	}
}
