package store

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

func TestCatalogClockUpgradePreservesNativeDeadlinesAndPublishedCopy(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	native := routineSource(t, "3m")
	p := model.Provider{ID: "remote-copy", Name: "Remote copy", Adapter: "http_json", Enabled: true, HTTP: model.HTTPConfig{Catalog: true}, Schedule: model.Schedule{Cron: "0 6 * * *", Mode: model.ModeFull, MaxPages: 4, KnownPages: 8}, CatalogSync: &model.CatalogSync{Enabled: true, IntervalMinutes: 15}}
	remote := scheduling.Sources(map[string]model.ProviderDocument{p.ID: {Provider: p, Revision: "unchanged"}})[0]
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sources := []scheduling.Source{native, remote}
	before, err := db.ReconcileSchedules(ctx, sources, start)
	if err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, p.ID, "retained", `{"title":"Retained remote copy","size":9007199254740993}`, nil)
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.schedule_clocks SET profile='source',next_run_at=$2,full_due_at=$2 WHERE provider_id=$1`, p.ID, start.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	now := start.Add(30 * time.Second)
	after, err := db.ReconcileSchedules(ctx, sources, now)
	if err != nil || !after[native.ID].NextRunAt.Equal(*before[native.ID].NextRunAt) || !after[native.ID].FullDueAt.Equal(*before[native.ID].FullDueAt) {
		t.Fatalf("native clock changed during catalog upgrade: %+v %v", after, err)
	}
	clock := after[p.ID]
	if clock.Profile != "catalog" || clock.FullDueAt != nil || clock.NextRunAt == nil || !clock.NextRunAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("legacy catalog clock was not converted: %+v", clock)
	}
	stable, err := db.ReconcileSchedules(ctx, sources, now.Add(time.Minute))
	if err != nil || !stable[p.ID].NextRunAt.Equal(*clock.NextRunAt) {
		t.Fatalf("catalog clock reset on a read or restart: %+v %v", stable, err)
	}
	_, run, err := db.AttemptSchedule(ctx, remote, *clock.NextRunAt, nil)
	if err != nil || run == nil || run.Mode != model.ModeFull || !run.InitialFull || run.MaxPages != 0 || run.Config.Schedule.Mode != model.ModeIncremental || run.Config.Schedule.KnownPages != 0 || run.Config.Schedule.Cron != "" || run.Config.Schedule.FullEvery != "" {
		t.Fatalf("catalog job inherited advanced source settings: %+v %v", run, err)
	}
	rows, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || rows.Total != 1 || rows.Items[0].Fields["title"] != "Retained remote copy" {
		t.Fatalf("clock migration changed the published copy: %+v %v", rows, err)
	}
}

func TestCatalogIntervalUpgradeResumesLegacyFullWithOriginalCheckpoint(t *testing.T) {
	ctx, endpoint, db := scheduleDatabase(t)
	p := mirrorSource("legacy-remote")
	p.Enabled = true
	p.Schedule = model.Schedule{Every: "1m", Mode: model.ModeFull, MaxPages: 2}
	source := scheduling.Sources(map[string]model.ProviderDocument{p.ID: {Provider: p, Revision: "saved"}})[0]
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateRun(ctx, model.Run{ProviderID: p.ID, Config: p, Mode: model.ModeFull, MaxPages: 2, Revision: source.Revision})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an immutable run created by the old scheduled Full dispatcher.
	// CreateRun deliberately always uses a manual trigger.
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.runs SET trigger='scheduled' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != created.ID {
		t.Fatalf("claim legacy Full: %+v %v", run, err)
	}
	saved := mirrorPage(t, ctx, db, *run, "full", "next-page", false, mirrorItem("first", map[string]any{"title": "First page"}, false))
	paused, err := db.FinishRun(ctx, saved.ID, model.StatusPaused, "", model.PauseBudget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.schedule_clocks SET profile='source' WHERE provider_id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	restarted, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	clocks, err := restarted.ReconcileSchedules(ctx, []scheduling.Source{source}, start.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, resumed, err := restarted.AttemptSchedule(ctx, source, *clocks[p.ID].NextRunAt, nil)
	if err != nil || resumed == nil || resumed.ID != paused.ID || resumed.Mode != model.ModeFull || resumed.InitialFull || resumed.MaxPages != paused.MaxPages || !bytes.Equal(resumed.Cursor, paused.Cursor) || !reflect.DeepEqual(resumed.Config, paused.Config) {
		t.Fatalf("interval upgrade lost the immutable legacy continuation: %+v %v", resumed, err)
	}
	run, err = restarted.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != resumed.ID {
		t.Fatalf("claim resumed Full: %+v %v", run, err)
	}
	saved = mirrorPage(t, ctx, restarted, *run, "full", "completed", true, mirrorItem("second", map[string]any{"title": "Second page"}, false))
	mirrorFinish(t, ctx, restarted, saved, model.StatusSucceeded)
	if rows := mirrorRows(t, ctx, restarted, p.ID); len(rows) != 2 {
		t.Fatalf("resumed snapshot lost a committed page: %+v", rows)
	}
	mirrorCheckpoint(t, ctx, restarted, p, "completed")
}
