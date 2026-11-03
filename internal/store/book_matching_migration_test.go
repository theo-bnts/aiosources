package store

import (
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestBookMatchingUpgradeResumesDecisionsAndKeepsManualChoices(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 86); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,book_normalized,years)
VALUES('book','OL100W','The Amber Orchard','the amber orchard','{}');
INSERT INTO ingest.aio_works(media_type,tmdb_id,display_title,years) VALUES('movie','1','Example movie','{}');
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
('fixture','pending','{"title":"Example pending EPUB"}'),
('fixture','ambiguous','{"title":"Example ambiguous EPUB"}'),
('fixture','reviewed','{"title":"Opaque archive","media_type":"book"}'),
('fixture','published-ebook','{"title":"The Amber Orchard EPUB"}'),
('fixture','published-audio','{"title":"The Amber Orchard M4B"}'),
('fixture','new-label','{"title":"Morgan.Vale.The.Amber.Orchard.retail.eBOOK"}'),
('fixture','video','{"title":"Example movie"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT t.provider_id,t.source_id,t.fields_fingerprint,
 CASE WHEN source_id IN('reviewed','published-ebook','published-audio') THEN (SELECT id FROM ingest.aio_works WHERE openlibrary_id='OL100W')
 WHEN source_id='video' THEN (SELECT id FROM ingest.aio_works WHERE tmdb_id='1') END,1,
 CASE WHEN source_id IN('reviewed','published-ebook','published-audio','video') THEN 'matched' WHEN source_id='ambiguous' THEN 'ambiguous' ELSE 'unmatched' END,
 CASE source_id WHEN 'pending' THEN 'book information not ready' WHEN 'ambiguous' THEN 'multiple or incomplete book candidates' WHEN 'reviewed' THEN 'manually reviewed' ELSE 'exact alias with compatible evidence' END FROM ingest.torrents t;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,openlibrary_id)
SELECT provider_id,source_id,fields_fingerprint,md5(ingest.aio_review_evidence(fields)::text),'book','','OL100W' FROM ingest.torrents WHERE source_id='reviewed';
DELETE FROM ingest.aio_match_queue WHERE source_id<>'pending';
UPDATE ingest.aio_match_queue SET version=9;
UPDATE ingest.ol_import_state SET position=12345,complete=TRUE,data_complete=TRUE,works=1;`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		var queued []string
		var version int
		if err = db.pool.QueryRow(ctx, `SELECT array_agg(source_id ORDER BY source_id) FROM ingest.aio_match_queue`).Scan(&queued); err != nil || !reflect.DeepEqual(queued, []string{"ambiguous", "new-label", "pending", "published-audio", "published-ebook", "reviewed"}) {
			t.Fatal("upgrade changed unrelated work or missed books", queued, err)
		}
		if err = db.pool.QueryRow(ctx, `SELECT version FROM ingest.aio_match_queue WHERE source_id='pending'`).Scan(&version); err != nil || version != 9 {
			t.Fatal("upgrade reset existing work", version, err)
		}
		var formats []string
		if err = db.pool.QueryRow(ctx, `SELECT array_agg(book_kind ORDER BY source_id) FROM ingest.aio_matches WHERE source_id IN('published-audio','published-ebook')`).Scan(&formats); err != nil || !reflect.DeepEqual(formats, []string{"audiobook", "ebook"}) {
			t.Fatal("upgrade hid existing book formats while their recheck was pending", formats, err)
		}
	}
	results, err := db.MatchAIOBatch(ctx, []model.AIORecord{{Provider: "fixture", SourceID: "reviewed"}}, func(model.AIORecord, AIOCandidateSearch) (model.AIOMatchResult, error) {
		t.Fatal("automatic matching replaced the saved manual choice")
		return model.AIOMatchResult{}, nil
	})
	if err != nil || len(results) != 1 || results[0].Status != "matched" || results[0].Work.OpenLibrary != "OL100W" {
		t.Fatal("manual book choice was lost", err)
	}
	var active bool
	var kind string
	var position int64
	if err = db.pool.QueryRow(ctx, `SELECT r.active,m.book_kind,(SELECT position FROM ingest.ol_import_state) FROM ingest.aio_match_reviews r JOIN ingest.aio_matches m USING(provider_id,source_id) WHERE r.source_id='reviewed'`).Scan(&active, &kind, &position); err != nil || !active || kind != "book" || position != 12345 {
		t.Fatal("classification repair reset reviews or imported information", active, kind, position, err)
	}
}
