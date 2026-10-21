package store

import (
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMatchingClockIsDurableAndInterruptedWorkDoesNotPostponeIt(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, status := range []string{"running", "interrupted", "succeeded", "cancelled", "failed"} {
		if _, err := db.pool.Exec(ctx, `UPDATE ingest.aio_state SET last_match=NULL`); err != nil {
			t.Fatal(err)
		}
		run, err := db.BeginOperation(ctx, model.OperationRun{Kind: "matching", Service: "local", Trigger: "manual"}, "matcher")
		if err != nil {
			t.Fatal(err)
		}
		run.Status, run.Phase, run.Results = status, "identification", map[string]int64{}
		if err = db.SaveOperation(ctx, run); err != nil {
			t.Fatal(err)
		}
		var clock *time.Time
		if err = db.pool.QueryRow(ctx, `SELECT last_match FROM ingest.aio_state`).Scan(&clock); err != nil {
			t.Fatal(err)
		}
		if (clock != nil) != (status != "running" && status != "interrupted") {
			t.Fatalf("wrong clock for %s: %v", status, clock)
		}
		if clock != nil {
			// History pruning is independent of the saved automatic schedule.
			if _, err = db.pool.Exec(ctx, `DELETE FROM ingest.operation_events WHERE run_id=$1`, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = db.pool.Exec(ctx, `DELETE FROM ingest.operation_runs WHERE id=$1`, run.ID); err != nil {
				t.Fatal(err)
			}
			current, err := db.AIOAutomationStatus(ctx)
			if err != nil || current.LastMatch == nil || !current.LastMatch.Equal(*clock) {
				t.Fatal("history pruning changed cadence", err)
			}
		}
	}
}

func TestIdentificationIntervalDefaultsForExistingSavedConfiguration(t *testing.T) {
	ctx, db := sharingDatabase(t)
	c := DefaultAIOConfig()
	if err := db.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.settings SET value=(value::jsonb-'match_seconds')::text WHERE key='aiosource.config.v2'`); err != nil {
		t.Fatal(err)
	}
	c, err := db.AIOConfig(ctx)
	if err != nil || c.MatchSeconds != 30 {
		t.Fatal("old configuration lost interval default", c.MatchSeconds, err)
	}
}

func TestNumberingUpgradePreservesWorkingDataAndQueuedVersions(t *testing.T) {
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
('example','spaced','{"title":"Example Serial Season 3 - 09"}'),
('example','ordinal','{"title":"Example Serial 2nd Season - 01"}'),
('example','range','{"title":"Example Serial - 01-12"}'),
('example','absolute','{"title":"Example.Serial.S01E02.EP1158"}'),
('example','year','{"title":"Example Film - 2021"}'),
('example','reviewed','{"title":"Example Serial Season 3 - 09"}'),
('example','working','{"title":"Example.Serial.S01E02.1080p"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,status,reason)
SELECT provider_id,source_id,md5(fields::text),'unmatched','old decision' FROM ingest.torrents;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id)
SELECT provider_id,source_id,md5(fields::text),md5(ingest.aio_review_evidence(fields)::text),'tv','1' FROM ingest.torrents WHERE source_id='reviewed';
DELETE FROM ingest.aio_match_queue WHERE source_id<>'spaced'; UPDATE ingest.aio_match_queue SET version=7;`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = db.pool.Exec(ctx, releaseNumberingRecheckSchema); err != nil {
			t.Fatal(err)
		}
		var ids []string
		var version, matches int
		if err = db.pool.QueryRow(ctx, `SELECT array_agg(source_id ORDER BY source_id) FROM ingest.aio_match_queue`).Scan(&ids); err != nil {
			t.Fatal(err)
		}
		if len(ids) != 5 {
			t.Fatal("wrong recheck scope", ids)
		}
		if err = db.pool.QueryRow(ctx, `SELECT (SELECT version FROM ingest.aio_match_queue WHERE source_id='spaced'),(SELECT count(*) FROM ingest.aio_matches)`).Scan(&version, &matches); err != nil || version != 7 || matches != 7 {
			t.Fatal("upgrade changed saved decisions", version, matches, err)
		}
	}
}
