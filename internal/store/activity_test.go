package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func activityDatabase(t *testing.T) (context.Context, string, *Store) {
	t.Helper()
	ctx, url := testutil.NewDatabase(t)
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, url, db
}

func TestActivityAtomicLogsAndFiltering(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	run, err := db.CreateRun(ctx, model.Run{ProviderID: "source", Config: model.Provider{ID: "source"}, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Second)
	if _, err := db.AddEvent(ctx, run.ID, "page_saved", "https://private.example/?token=never-publish", map[string]any{"page": 2, "records": 3, "headers": map[string]any{"Authorization": "never-publish"}, "mode": "never-publish", "errors": "never-publish", "done": true}); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycleEvent(ctx, tx, run.ID, "succeeded", "never-publish"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM ingest.activity_logs WHERE kind='run.succeeded' AND run_id=$1", run.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("log absent inside event transaction: %d %v", count, err)
	}
	rollback(tx)
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.activity_logs WHERE kind='run.succeeded' AND run_id=$1", run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back event leaked a log: %d %v", count, err)
	}
	if _, err := db.AddEvent(ctx, run.ID, "succeeded", "never-publish", map[string]any{"records": 3, "url": "never-publish"}); err != nil {
		t.Fatal(err)
	}
	debug, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{ProviderID: "source", RunID: run.ID, Limit: 1}, Levels: []string{"debug"}, From: &before})
	if err != nil || debug.Total != 1 || len(debug.Items) != 1 || debug.Items[0].Kind != "run.page_saved" {
		t.Fatalf("combined log filters: %+v %v", debug, err)
	}
	encoded, _ := json.Marshal(debug)
	if strings.Contains(string(encoded), "never-publish") || strings.Contains(string(encoded), "Authorization") {
		t.Fatalf("private event metadata escaped: %s", encoded)
	}
	if debug.Items[0].Data["records"] != json.Number("3") {
		t.Fatalf("safe numeric metadata lost: %+v", debug.Items[0].Data)
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{RunID: run.ID, Query: "run.succeeded", Limit: 20}})
	if err != nil || logs.Total != 1 || logs.Items[0].Message == "" {
		t.Fatalf("terminal outcome missing from logs: %+v %v", logs, err)
	}
}

func TestScheduleFailureDedupAndRecovery(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	for range 2 {
		if err := db.RecordActivity(ctx, "error", "schedule.failed", "token=never-publish", "source", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	first, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Query: "schedule.failed", Limit: 20}})
	if err != nil || first.Total != 1 {
		t.Fatalf("repeated failure alert storm: %+v %v", first, err)
	}
	if err := db.RecordActivity(ctx, "info", "schedule.queued", "", "source", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordActivity(ctx, "error", "schedule.failed", "", "source", "", nil); err != nil {
		t.Fatal(err)
	}
	after, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Query: "schedule.failed", Limit: 20}})
	if err != nil || after.Total != 2 {
		t.Fatalf("new failure after recovery lost: %+v %v", after, err)
	}
}

func TestActivityMigrationBackfillsWithoutChangingEvents(t *testing.T) {
	ctx, url := testutil.NewDatabase(t)
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.pool.Exec(ctx, "CREATE SCHEMA ingest;"+initialSchema+observationSchema+scheduleSchema+`
INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config)
VALUES('old-source-run','old-source','Old source','preview','queued','','{"id":"old-source"}');
INSERT INTO ingest.events(run_id,kind,message,data)
VALUES('old-source-run','queued','Collection queued','{}');`); err != nil {
		t.Fatal(err)
	}
	event, err := db.AddEvent(ctx, "old-source-run", "succeeded", "original private history", map[string]any{"url": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, activitySchema); err != nil {
		t.Fatal(err)
	}
	// Current readers also require the service context added to activity.
	if _, err := db.pool.Exec(ctx, serviceActivitySchema+activityConsolidationSchema); err != nil {
		t.Fatal(err)
	}
	events, err := db.Events(ctx, "old-source-run", model.ListOptions{Limit: 20})
	if err != nil || len(events.Items) != 2 || events.Items[0].Message != event.Message {
		t.Fatalf("migration mutated original history: %+v %v", events, err)
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 20}})
	if err != nil || logs.Total != 2 {
		t.Fatalf("history not backfilled: %+v %v", logs, err)
	}
	var retained int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.activity_logs WHERE kind='run.succeeded'").Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("notable history missing from logs: %d %v", retained, err)
	}
}
