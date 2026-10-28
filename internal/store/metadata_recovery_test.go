package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestInformationRecoveryUpgradeKeepsExistingCatalogsAndHonestFreshness(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			ctx, dsn := testutil.NewDatabase(t)
			db, err := Open(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = db.migrateThrough(ctx, 79); err != nil {
				t.Fatal(err)
			}
			if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years) VALUES('movie','101','{}');
 INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL100W','Synthetic Almanac','{}');
 UPDATE ingest.aio_state SET last_sync=now()-interval '10 minutes',changes_through=now()-interval '10 minutes';
 UPDATE ingest.ol_import_state SET position=1234,complete=TRUE,works=1;`); err != nil {
				t.Fatal(err)
			}
			if pending {
				if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) VALUES('movie','101')`); err != nil {
					t.Fatal(err)
				}
			}
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			var index string
			if err = db.pool.QueryRow(ctx, `SELECT pg_get_indexdef('ingest.aio_works_refresh'::regclass)`).Scan(&index); err != nil || !strings.Contains(index, "WHERE") {
				t.Fatal("video freshness checks would index the entire book catalog", index, err)
			}
			status, err := db.AIOAutomationStatus(ctx)
			if err != nil || status.LastSync == nil || status.ChangesThrough == nil || (status.AppliedThrough == nil) != pending {
				t.Fatal("upgrade lost progress or acknowledged pending details", status, err)
			}
			var videos, books int
			err = db.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE media_type='movie'),count(*) FILTER(WHERE media_type='book') FROM ingest.aio_works`).Scan(&videos, &books)
			bookState, bookErr := db.OpenLibraryStatus(ctx)
			if err != nil || bookErr != nil || videos != 1 || books != 1 || !bookState.Complete || bookState.Position != 1234 {
				t.Fatal("upgrade erased an existing catalog", videos, books, bookState, err, bookErr)
			}
		})
	}
}

func TestInformationRecoveryRefillsBoundedJobsAndKeepsAcknowledgedProgress(t *testing.T) {
	ctx, db := sharingDatabase(t)
	limit, err := db.RuntimeValue(ctx, "metadata", "queue_batch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years,updated_at)
	 SELECT 'movie',n::text,'{}',now()-interval '20 days' FROM generate_series(1,$1::int) n`, limit+1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.aio_state SET changes_through=now()-interval '20 days',applied_through=now()-interval '20 days'`); err != nil {
		t.Fatal(err)
	}
	if err = db.BeginAIOMetadataRecovery(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = db.QueueAIOMetadataRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_metadata_jobs`).Scan(&count); err != nil || count != limit {
		t.Fatal("recovery ignored the saved batch limit", count, limit, err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.aio_metadata_jobs SET version=7,attempts=3,next_attempt=now()+interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueueAIOMetadataRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	var preserved int
	err = db.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE version=7 AND attempts=3 AND next_attempt>now()+interval '50 minutes'),count(*) FROM ingest.aio_metadata_jobs`).Scan(&preserved, &count)
	if err != nil || preserved != limit || count != limit+1 {
		t.Fatal("refilling erased retries or duplicated jobs", preserved, count, err)
	}
	if err = db.AIOCompletedSync(ctx, time.Now().Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	before, err := db.AIOAutomationStatus(ctx)
	if err != nil || before.RecoveryStartedAt == nil || before.LastSync != nil || before.SyncRetryAt == nil || before.AppliedThrough == nil || time.Since(*before.AppliedThrough) < 19*24*time.Hour {
		t.Fatal("unapplied information was acknowledged", before, err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.aio_works SET updated_at=now() WHERE tmdb_id='1';
 DELETE FROM ingest.aio_metadata_jobs WHERE tmdb_id='1';`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueueAIOMetadataRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_metadata_jobs WHERE tmdb_id='1'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("completed information was downloaded again", count, err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.aio_works SET updated_at=now(); DELETE FROM ingest.aio_metadata_jobs;`); err != nil {
		t.Fatal(err)
	}
	if err = db.AIOCompletedSync(ctx, time.Now().Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	after, err := db.AIOAutomationStatus(ctx)
	if err != nil || after.RecoveryStartedAt != nil || after.SyncRetryAt != nil || after.LastSync == nil || after.AppliedThrough == nil || !after.AppliedThrough.Equal(*after.ChangesThrough) {
		t.Fatal("completed recovery was not acknowledged", after, err)
	}
	if err = db.BeginAIOMetadataRecovery(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = db.ResetAIOMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	after, err = db.AIOAutomationStatus(ctx)
	if err != nil || after.RecoveryStartedAt != nil || after.AppliedThrough != nil {
		t.Fatal("explicit reset retained an obsolete recovery", after, err)
	}
}

func TestBookScheduleAndAdmissionUseTheSameDurableDeadlines(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.ol_import_state SET complete=TRUE;
 UPDATE ingest.ol_changes_state SET bootstrap_complete=TRUE,checked_at=now();`); err != nil {
		t.Fatal(err)
	}
	next, err := db.NextOpenLibraryUpdate(ctx, 15)
	if err != nil || time.Until(next) < 14*time.Minute || time.Until(next) > 16*time.Minute {
		t.Fatal("saved interval is not displayed", next, err)
	}
	if due, err := db.OpenLibraryDue(ctx, 15); err != nil || due {
		t.Fatal("admission disagrees with the next update", due, err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.ol_change_jobs(id,revision,changed_at,next_attempt) VALUES('OL100W',1,now(),now()+interval '5 minutes')`); err != nil {
		t.Fatal(err)
	}
	next, err = db.NextOpenLibraryUpdate(ctx, 15)
	if err != nil || time.Until(next) < 4*time.Minute || time.Until(next) > 6*time.Minute {
		t.Fatal("individual retry does not wake the worker", next, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET retry_at=now()+interval '30 minutes'`); err != nil {
		t.Fatal(err)
	}
	next, err = db.NextOpenLibraryUpdate(ctx, 15)
	if err != nil || time.Until(next) < 29*time.Minute || time.Until(next) > 31*time.Minute {
		t.Fatal("provider cooldown was hidden by an earlier document retry", next, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET retry_at=NULL; UPDATE ingest.ol_change_jobs SET next_attempt=now()-interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if due, err := db.OpenLibraryDue(ctx, 15); err != nil || !due {
		t.Fatal("a ready document is not admitted", due, err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM ingest.ol_change_jobs;
 UPDATE ingest.ol_changes_state SET bootstrap_complete=FALSE,needs_dump_after=now(),dump_checked_at=now(),checked_at=now()+interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	next, err = db.NextOpenLibraryUpdate(ctx, 15)
	if err != nil || time.Until(next) < 23*time.Hour || time.Until(next) > 25*time.Hour {
		t.Fatal("recovery export polling is not part of the shared schedule", next, err)
	}
}
