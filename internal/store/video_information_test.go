package store

import (
	"reflect"
	"testing"
)

func TestVideoInformationResetPreservesBooksReviewsAndProviderWaits(t *testing.T) {
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,display_title,years) VALUES
 ('movie','101','','Synthetic Film','{2024}'),('tv','202','','Synthetic Series','{2025}'),('book',NULL,'OL100W','Synthetic Book','{2023}');
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
 ('fixture','movie','{"title":"Synthetic Film"}'),('fixture','tv','{"title":"Synthetic Series"}'),('fixture','book','{"title":"Synthetic Book EPUB"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture'
 FROM ingest.torrents t JOIN ingest.aio_works w ON w.media_type=t.source_id;
DELETE FROM ingest.aio_match_queue;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,openlibrary_id)
 SELECT provider_id,source_id,fields_fingerprint,md5(ingest.aio_review_evidence(fields)::text),source_id,
 CASE source_id WHEN 'movie' THEN '101' WHEN 'tv' THEN '202' ELSE '' END,
 CASE source_id WHEN 'book' THEN 'OL100W' ELSE '' END FROM ingest.torrents;
INSERT INTO ingest.ol_authors(id,name) VALUES('OL200A','Synthetic Author');
INSERT INTO ingest.ol_editions(id,work_keys,title,normalized) VALUES('OL300M',ARRAY['OL100W'],'Synthetic Edition','synthetic edition');
UPDATE ingest.ol_import_state SET works=1,authors=1,editions=1,complete=TRUE,data_complete=TRUE,position=1234;
UPDATE ingest.ol_changes_state SET anchor_id='100',anchor_at=now(),changes_through=now(),bootstrap_complete=TRUE;
INSERT INTO ingest.ol_change_jobs(id,revision,changed_at) VALUES('OL400W',2,now());
INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) VALUES('movie','101');
UPDATE ingest.aio_state SET provider_retry_at=now()+interval '1 hour',last_sync=now(),changes_through=now(),inventory_date='2026-01-01',sync_error='Synthetic failure';`)
	if err != nil {
		t.Fatal(err)
	}
	before, err := db.OpenLibraryStatus(ctx)
	changes, changesErr := db.BookChangesState(ctx)
	status, statusErr := db.AIOStatus(ctx)
	if err != nil || changesErr != nil || statusErr != nil || status.VideoWorks != 2 || status.Works != 3 {
		t.Fatal("video counts include books", before, status, err, changesErr, statusErr)
	}
	if err = db.ResetVideoInformation(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := db.OpenLibraryStatus(ctx)
	afterChanges, changesErr := db.BookChangesState(ctx)
	afterStatus, statusErr := db.AIOStatus(ctx)
	if err != nil || changesErr != nil || statusErr != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(changes, afterChanges) || afterStatus.VideoWorks != 0 || afterStatus.Works != 1 || afterStatus.LastSync != nil || afterStatus.ChangesThrough != nil || afterStatus.SyncError != "" || afterStatus.ProviderRetryAt == nil || !afterStatus.ProviderRetryAt.Equal(*status.ProviderRetryAt) {
		t.Fatal("video reset changed book progress or lost its provider wait", before, after, changes, afterChanges, afterStatus, err, changesErr, statusErr)
	}
	var reviews, bookMatches, queued, sources, bookJobs int
	err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_match_reviews WHERE active),
 (SELECT count(*) FROM ingest.aio_matches m JOIN ingest.aio_works w ON w.id=m.work_id WHERE w.media_type='book' AND m.status='matched'),
 (SELECT count(*) FROM ingest.aio_match_queue),(SELECT count(*) FROM ingest.torrents),(SELECT count(*) FROM ingest.ol_change_jobs)`).Scan(&reviews, &bookMatches, &queued, &sources, &bookJobs)
	if err != nil || reviews != 1 || bookMatches != 1 || queued != 2 || sources != 3 || bookJobs != 1 {
		t.Fatal("video reset changed books, manual choices or source data", reviews, bookMatches, queued, sources, bookJobs, err)
	}
}
