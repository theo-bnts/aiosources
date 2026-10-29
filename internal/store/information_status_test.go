package store

import (
	"context"
	"testing"
	"time"
)

func TestInformationStatisticsDoNotWaitForTorrentDecisions(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,years)
VALUES('movie','101','',ARRAY[2024]),('tv','202','',ARRAY[2025]),('book',NULL,'OL300W',ARRAY[2023]);
UPDATE ingest.ol_import_state SET works=1;
UPDATE ingest.aio_state SET last_sync='2026-01-02',applied_through='2026-01-01',inventory_date='2026-01-01';`); err != nil {
		t.Fatal(err)
	}
	full, err := db.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(lock)
	if _, err = lock.Exec(ctx, `LOCK TABLE ingest.torrents,ingest.aio_matches,ingest.aio_match_queue IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	// A decision scan blocks behind this lock; information-only statistics must
	// remain available during an unrelated source or identification operation.
	read, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	info, err := db.AIOInformationStatus(read)
	if err != nil || info.Works != 3 || info.VideoWorks != 2 || info.Pending != full.Pending || info.MetadataDue != full.MetadataDue ||
		info.InventoryDate != full.InventoryDate || info.LastSync == nil || info.AppliedThrough == nil ||
		!info.LastSync.Equal(*full.LastSync) || !info.AppliedThrough.Equal(*full.AppliedThrough) {
		t.Fatalf("information statistics depend on torrent decisions or lost a checkpoint: %+v, %v", info, err)
	}
}
