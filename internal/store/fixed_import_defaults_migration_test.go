package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestFixedImportDefaultsMigrationPreservesRunSnapshotsAndSavedPolicies(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "previous opt-out", true: "previous automatic recovery"}[enabled], func(t *testing.T) {
			ctx, endpoint, db := activityDatabase(t)
			if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.collection_settings
 ADD COLUMN default_request_timeout_seconds INTEGER NOT NULL DEFAULT 30,
 ADD COLUMN default_preview_pages INTEGER NOT NULL DEFAULT 3,
 ADD COLUMN default_max_pages INTEGER NOT NULL DEFAULT 0,
 ADD COLUMN default_max_duration_seconds INTEGER NOT NULL DEFAULT 0;
 DELETE FROM ingest.schema_migrations WHERE version IN (42,73,74); DROP TABLE ingest.runtime_settings`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.pool.Exec(ctx, `UPDATE ingest.collection_settings SET workers=5,
 auto_resume_interrupted=$1,default_request_timeout_seconds=71,default_preview_pages=11,
 default_max_pages=9,default_max_duration_seconds=123,revision=7`, enabled); err != nil {
				t.Fatal(err)
			}
			before, err := db.CollectionSettings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			policy := before.Policy()
			policy.MaxDurationSeconds = 123
			created, err := db.CreateRun(ctx, model.Run{ProviderID: "fixed-defaults", Mode: model.ModeFull,
				Config: model.Provider{ID: "fixed-defaults", RequestTimeout: "71s"}, MaxPages: 9, Policy: &policy})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := db.ClaimNext(ctx)
			if err != nil || claimed == nil || claimed.ID != created.ID {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			saved, err := db.SavePage(ctx, *claimed, model.Page{Body: []byte("saved checkpoint"), Next: json.RawMessage(`{"page":2}`)}, "")
			if err != nil {
				t.Fatal(err)
			}
			want := before
			want.Revision++
			want.AutoResumeInterrupted = true
			for range 2 {
				if err := db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				got, err := db.CollectionSettings(ctx)
				if err != nil || got != want {
					t.Fatalf("saved policy or migration idempotency changed: %+v %v", got, err)
				}
				run, err := db.GetRun(ctx, saved.ID)
				if err != nil || run.MaxPages != 9 || run.Config.RequestTimeout != "71s" || run.Policy == nil || *run.Policy != policy || !bytes.Equal(run.Cursor, saved.Cursor) {
					t.Fatalf("upgrade changed existing run snapshot: %+v %v", run, err)
				}
			}
			if err := db.UpdateCollectionSettings(ctx, before); !errors.Is(err, model.ErrConflict) {
				t.Fatalf("stale draft accepted: %v", err)
			}
			var retired int
			if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='ingest' AND table_name='collection_settings' AND column_name IN ('default_request_timeout_seconds','default_preview_pages','default_max_pages','default_max_duration_seconds')`).Scan(&retired); err != nil || retired != 0 {
				t.Fatalf("retired storage: %d %v", retired, err)
			}
			db.Close()
			restarted, err := Open(ctx, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.Close)
			for range 2 {
				if err := restarted.RecoverInterrupted(ctx, map[string]bool{saved.ProviderID: true}); err != nil {
					t.Fatal(err)
				}
			}
			recovered, err := restarted.GetRun(ctx, saved.ID)
			if err != nil || recovered.Status != model.StatusQueued || recovered.Policy == nil || *recovered.Policy != policy || recovered.MaxPages != 9 || recovered.Config.RequestTimeout != "71s" || !bytes.Equal(recovered.Cursor, saved.Cursor) {
				t.Fatalf("unconditional recovery lost existing work: %+v %v", recovered, err)
			}
		})
	}
}
