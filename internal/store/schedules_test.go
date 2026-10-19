package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/scheduling"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestScheduledBudgetContinuationPreservesCheckpointAndExplainsItsCause(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := scheduledSource(t)
	start := time.Now().UTC()
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	due := start.Add(time.Minute)
	_, queued, err := db.AttemptSchedule(ctx, source, due, nil)
	if err != nil || queued == nil {
		t.Fatal("queue scheduled collection", err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatal("claim scheduled collection", err)
	}
	checkpoint := json.RawMessage(`{"page":2}`)
	saved, err := db.SavePage(ctx, *claimed, model.Page{Body: []byte("response"), Next: checkpoint}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, saved.ID, model.StatusPaused, "page budget reached", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	_, continued, err := db.AttemptSchedule(ctx, source, due.Add(time.Minute), nil)
	if err != nil || continued == nil || continued.ID != saved.ID || continued.Status != model.StatusQueued || continued.Pages != saved.Pages || string(continued.Cursor) != string(saved.Cursor) {
		t.Fatalf("budget continuation lost its saved progress: %+v %v", continued, err)
	}
	events, err := db.Events(ctx, saved.ID, model.ListOptions{Limit: 1})
	if err != nil || len(events.Items) != 1 || events.Items[0].Message != "Scheduled collection queued for continuation" {
		t.Fatalf("budget continuation was presented as an unexpected interruption: %+v %v", events, err)
	}
}

func TestScheduledAdapterRenameContinuesTheOriginalCheckpoint(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	registry, err := providers.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	document, err := registry.Save("scheduled-source", `{"version":1,"id":"scheduled-source","name":"Example archive","adapter":"http_json","url":"https://archive.example/items","enabled":true,"http":{"headers":{},"query":{"id":9007199254740993,"ratio":1.2300e+2}},"schedule":{"every":"1m","mode":"full","max_pages":1}}`, "")
	if err != nil {
		t.Fatal(err)
	}
	source := scheduling.Sources(map[string]model.ProviderDocument{document.Provider.ID: document})[0]
	start := time.Now().UTC()
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	_, queued, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
	if err != nil || queued == nil {
		t.Fatal("initial scheduled work was not queued", err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	saved, err := db.SavePage(ctx, *claimed, model.Page{Next: json.RawMessage(`{"page":23}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(ctx, saved.ID, model.StatusPaused, "Page budget reached", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	if count, err := registry.UpgradeDefinitions(); err != nil || count != 1 {
		t.Fatal("source file was not upgraded", count, err)
	}
	document, err = registry.Get(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	source = scheduling.Sources(map[string]model.ProviderDocument{document.Provider.ID: document})[0]
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, resumed, err := db.AttemptSchedule(ctx, source, start.Add(2*time.Minute), nil)
	if err != nil || resumed == nil || resumed.ID != saved.ID || string(resumed.Cursor) != string(saved.Cursor) || resumed.Pages != saved.Pages || resumed.Revision != saved.Revision || resumed.Config.Adapter != "http_json" {
		t.Fatal("adapter rename restarted or blocked the saved traversal", err)
	}
}

func scheduleDatabase(t *testing.T) (context.Context, string, *Store) {
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

func scheduledSource(t *testing.T) scheduling.Source {
	t.Helper()
	p := model.Provider{ID: "scheduled-source", Name: "Scheduled source", Enabled: true,
		Schedule: model.Schedule{Every: "1m", Mode: model.ModeFull, MaxPages: 1}}
	plan, err := scheduling.Parse(p.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	return scheduling.Source{ID: p.ID, Revision: "original", Provider: p, Valid: true, Plan: plan}
}

func routineSource(t *testing.T, fullEvery string) scheduling.Source {
	t.Helper()
	source := scheduledSource(t)
	source.Provider.Schedule.Mode = model.ModeIncremental
	source.Provider.Schedule.FullEvery = fullEvery
	plan, err := scheduling.Parse(source.Provider.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	source.Plan = plan
	return source
}

func TestScheduledSlotIsUniqueAcrossInstancesAndRestart(t *testing.T) {
	ctx, url, db := scheduleDatabase(t)
	source := routineSource(t, "3m")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clocks, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start)
	if err != nil || clocks[source.ID].NextRunAt == nil || !clocks[source.ID].NextRunAt.Equal(start.Add(time.Minute)) || clocks[source.ID].FullDueAt == nil || !clocks[source.ID].FullDueAt.Equal(start.Add(3*time.Minute)) {
		t.Fatalf("new schedule did not wait its first interval: %+v %v", clocks, err)
	}
	second, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	// A long outage consumes only one due slot, not one slot per missed minute.
	due := start.Add(24 * time.Hour)
	var group sync.WaitGroup
	errors := make(chan error, 12)
	for index := range 12 {
		instance := db
		if index%2 != 0 {
			instance = second
		}
		group.Go(func() {
			_, _, err := instance.AttemptSchedule(ctx, source, due, nil)
			errors <- err
		})
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	runs, err := second.ListRuns(ctx, model.ListOptions{ProviderID: source.ID})
	if err != nil || runs.Total != 1 {
		t.Fatalf("concurrent due slot duplicated work: total=%d err=%v", runs.Total, err)
	}
	first := runs.Items[0]
	if first.Trigger != model.TriggerScheduled || first.Mode != model.ModeFull || first.MaxPages != 1 {
		t.Fatalf("scheduled execution policy lost: %+v", first)
	}
	if _, err := second.RequestCancel(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	second.Close()
	restarted, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	clocks, err = restarted.ReconcileSchedules(ctx, []scheduling.Source{source}, due.Add(30*time.Second))
	if err != nil || !clocks[source.ID].NextRunAt.Equal(due.Add(time.Minute)) || clocks[source.ID].FullDueAt == nil || !clocks[source.ID].FullDueAt.Equal(due.Add(3*time.Minute)) {
		t.Fatalf("restart reset the durable clock: %+v %v", clocks, err)
	}
	if _, run, err := restarted.AttemptSchedule(ctx, source, due.Add(59*time.Second), nil); err != nil || run != nil {
		t.Fatalf("restart dispatched early: %+v %v", run, err)
	}
	_, next, err := restarted.AttemptSchedule(ctx, source, due.Add(time.Minute), nil)
	if err != nil || next == nil || next.ID == first.ID || next.Mode != model.ModeFull || !next.InitialFull {
		t.Fatalf("cancelled scheduled run was resumed instead of a fresh traversal: %+v %v", next, err)
	}
}

func TestScheduleClockAndRunRollbackTogether(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := routineSource(t, "1m")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	// Reject the clock update after the run insert. A split transaction would
	// leave a queued run behind and lose atomic ownership of this due slot.
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_schedule_advance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'synthetic schedule persistence failure'; END; $$;
CREATE TRIGGER reject_schedule_advance BEFORE UPDATE ON ingest.schedule_clocks
FOR EACH ROW EXECUTE FUNCTION ingest.reject_schedule_advance()`); err != nil {
		t.Fatal(err)
	}
	due := start.Add(time.Minute)
	if _, _, err := db.AttemptSchedule(ctx, source, due, nil); err == nil {
		t.Fatal("injected schedule persistence failure was ignored")
	}
	runs, err := db.ListRuns(ctx, model.ListOptions{ProviderID: source.ID})
	if err != nil || runs.Total != 0 {
		t.Fatalf("failed clock transaction left a queued run: total=%d err=%v", runs.Total, err)
	}
	if _, err := db.pool.Exec(ctx, "DROP TRIGGER reject_schedule_advance ON ingest.schedule_clocks"); err != nil {
		t.Fatal(err)
	}
	_, queued, err := db.AttemptSchedule(ctx, source, due, nil)
	if err != nil || queued == nil || queued.Mode != model.ModeFull {
		t.Fatalf("rolled-back due slot was lost: %+v %v", queued, err)
	}
}

func TestSchedulePreparationFailuresAreDurableAndRateLimited(t *testing.T) {
	ctx, url, db := scheduleDatabase(t)
	source := routineSource(t, "1m")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	due := start.Add(time.Minute)
	clock, run, err := db.AttemptSchedule(ctx, source, due, errors.New("private-secret-value"))
	if err != nil || run != nil || clock.LastError == "" || clock.LastError == "private-secret-value" {
		t.Fatalf("failed enqueue was not safely recorded: %+v %+v %v", clock, run, err)
	}
	db.Close()
	restarted, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	clocks, err := restarted.ReconcileSchedules(ctx, []scheduling.Source{source}, due.Add(time.Second))
	if err != nil || clocks[source.ID].LastError != clock.LastError || clocks[source.ID].LastAttemptAt == nil || !clocks[source.ID].LastAttemptAt.Equal(due) || clocks[source.ID].FullDueAt == nil || !clocks[source.ID].FullDueAt.Equal(due) {
		t.Fatalf("restart lost failed attempt: %+v %v", clocks, err)
	}
	if _, run, err := restarted.AttemptSchedule(ctx, source, due.Add(time.Second), nil); err != nil || run != nil {
		t.Fatalf("failed attempt retried before its deadline: run=%+v err=%v", run, err)
	}
	clock, run, err = restarted.AttemptSchedule(ctx, source, due.Add(time.Minute), nil)
	if err != nil || run == nil || run.Mode != model.ModeFull || clock.LastError != "" || clock.FullDueAt == nil || !clock.FullDueAt.Equal(due.Add(2*time.Minute)) {
		t.Fatalf("repaired source did not retry on the next tick: %+v %+v %v", clock, run, err)
	}
}

func TestPausedScheduledRunCancellationDoesNotAutoResume(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := scheduledSource(t)
	start := time.Now().UTC()
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	due := start.Add(time.Minute)
	_, queued, err := db.AttemptSchedule(ctx, source, due, nil)
	if err != nil || queued == nil {
		t.Fatalf("queue scheduled collection: %+v %v", queued, err)
	}
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != queued.ID {
		t.Fatalf("claim scheduled collection: %+v %v", claimed, err)
	}
	if _, err := db.FinishRun(ctx, queued.ID, model.StatusPaused, "page budget reached", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	cancelled, err := db.RequestCancel(ctx, queued.ID)
	if err != nil || cancelled.Status != model.StatusCancelled {
		t.Fatalf("paused cancellation did not terminate the run: %+v %v", cancelled, err)
	}
	_, next, err := db.AttemptSchedule(ctx, source, due.Add(time.Minute), nil)
	if err != nil || next == nil || next.ID == queued.ID {
		t.Fatalf("next tick resurrected a cancelled traversal: %+v %v", next, err)
	}
}

func TestDueScheduleRecoversOnlyAnOrphanedPeerRun(t *testing.T) {
	ctx, url, owner := scheduleDatabase(t)
	peer, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	source := scheduledSource(t)
	start := time.Now().UTC()
	if _, err := owner.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	due := start.Add(time.Minute)
	_, queued, err := owner.AttemptSchedule(ctx, source, due, nil)
	if err != nil || queued == nil {
		t.Fatalf("queue peer collection: %+v %v", queued, err)
	}
	if claimed, err := owner.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != queued.ID {
		t.Fatalf("claim peer collection: %+v %v", claimed, err)
	}
	if _, stolen, err := peer.AttemptSchedule(ctx, source, due.Add(time.Minute), nil); err != nil || stolen != nil {
		t.Fatalf("live peer lease was stolen: %+v %v", stolen, err)
	}
	live, err := peer.GetRun(ctx, queued.ID)
	if err != nil || live.Status != model.StatusRunning || live.PauseReason != "" {
		t.Fatalf("a live worker's durable status changed during scheduled recovery: %+v %v", live, err)
	}
	// Session loss releases the lease without completing the durable run.
	owner.Close()
	_, continuation, err := peer.AttemptSchedule(ctx, source, due.Add(2*time.Minute), nil)
	if err != nil || continuation != nil {
		t.Fatalf("schedule duplicated the recovered attempt: %+v %v", continuation, err)
	}
	recovered, err := peer.GetRun(ctx, queued.ID)
	if err != nil || recovered.Status != model.StatusQueued || recovered.PauseReason != "" {
		t.Fatalf("surviving scheduler did not retain the orphan checkpoint: %+v %v", recovered, err)
	}
}

func TestFullRoutineClockResetsOnlyWithRevisionOrActivation(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := routineSource(t, "3m")
	seedCollectionBaseline(t, ctx, db, source.Provider)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	// Re-reading the source must not postpone either deadline.
	clocks, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start.Add(2*time.Minute))
	if err != nil || clocks[source.ID].FullDueAt == nil || !clocks[source.ID].FullDueAt.Equal(start.Add(3*time.Minute)) || clocks[source.ID].NextRunAt == nil || !clocks[source.ID].NextRunAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("unchanged reconciliation moved clocks: %+v %v", clocks, err)
	}
	source.Revision = "edited"
	clocks, err = db.ReconcileSchedules(ctx, []scheduling.Source{source}, start.Add(2*time.Minute))
	if err != nil || clocks[source.ID].FullDueAt == nil || !clocks[source.ID].FullDueAt.Equal(start.Add(5*time.Minute)) || clocks[source.ID].NextRunAt == nil || !clocks[source.ID].NextRunAt.Equal(start.Add(3*time.Minute)) {
		t.Fatalf("revision did not wait fresh intervals: %+v %v", clocks, err)
	}
	source.Provider.Enabled = false
	source.Revision = "disabled"
	clocks, err = db.ReconcileSchedules(ctx, []scheduling.Source{source}, start.Add(3*time.Minute))
	if err != nil || clocks[source.ID].FullDueAt != nil || clocks[source.ID].NextRunAt != nil {
		t.Fatalf("disabled source retained active deadlines: %+v %v", clocks, err)
	}
	source.Provider.Enabled = true
	source.Revision = "reactivated"
	clocks, err = db.ReconcileSchedules(ctx, []scheduling.Source{source}, start.Add(4*time.Minute))
	if err != nil || clocks[source.ID].FullDueAt == nil || !clocks[source.ID].FullDueAt.Equal(start.Add(7*time.Minute)) {
		t.Fatalf("reactivation retained an overdue Full deadline: %+v %v", clocks, err)
	}
	source.Provider.Schedule.FullEvery = ""
	source.Plan, err = scheduling.Parse(source.Provider.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	source.Revision = "routine-removed"
	clocks, err = db.ReconcileSchedules(ctx, []scheduling.Source{source}, start.Add(5*time.Minute))
	if err != nil || clocks[source.ID].FullDueAt != nil {
		t.Fatalf("removing Full routine left an executable deadline: %+v %v", clocks, err)
	}
	_, run, err := db.AttemptSchedule(ctx, source, start.Add(24*time.Hour), nil)
	if err != nil || run == nil || run.Mode != model.ModeIncremental {
		t.Fatalf("ordinary Incremental schedule inherited Full work: %+v %v", run, err)
	}
}

func TestFullRoutineBlockedDeadlineSurvivesUntilEnqueue(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := routineSource(t, "1m")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	manual, err := db.CreateRun(ctx, model.Run{ProviderID: source.ID, Config: source.Provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	checkBlocked := func(tick int, preparationErr error) {
		t.Helper()
		due := start.Add(time.Duration(tick) * time.Minute)
		clock, run, err := db.AttemptSchedule(ctx, source, due, preparationErr)
		if err != nil || run != nil || clock.FullDueAt == nil || !clock.FullDueAt.Equal(start.Add(time.Minute)) || clock.NextRunAt == nil || !clock.NextRunAt.Equal(due.Add(time.Minute)) {
			t.Fatalf("blocked tick consumed Full deadline or did not skip base slot: %+v %+v %v", clock, run, err)
		}
	}
	checkBlocked(1, nil)
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != manual.ID {
		t.Fatalf("claim manual work: %+v %v", claimed, err)
	}
	if _, err := db.RequestPause(ctx, manual.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, manual.ID, model.StatusFailed, "synthetic failure while paused", ""); err != nil {
		t.Fatal(err)
	}
	checkBlocked(2, nil)
	if _, err := db.RequestCancel(ctx, manual.ID); err != nil {
		t.Fatal(err)
	}
	checkBlocked(3, errors.New("synthetic unavailable source"))
	due := start.Add(4 * time.Minute)
	clock, run, err := db.AttemptSchedule(ctx, source, due, nil)
	if err != nil || run == nil || run.Mode != model.ModeFull || clock.FullDueAt == nil || !clock.FullDueAt.Equal(due.Add(time.Minute)) {
		t.Fatalf("repaired source did not consume retained Full deadline: %+v %+v %v", clock, run, err)
	}
}

func TestFullRoutineIncrementalContinuationTakesPrecedence(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := routineSource(t, "2m")
	seedCollectionBaseline(t, ctx, db, source.Provider)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	_, incremental, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
	if err != nil || incremental == nil || incremental.Mode != model.ModeIncremental {
		t.Fatalf("first regular tick did not begin Incremental: %+v %v", incremental, err)
	}
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != incremental.ID {
		t.Fatalf("claim Incremental work: %+v %v", claimed, err)
	}
	if _, err := db.FinishRun(ctx, incremental.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	fullDue := start.Add(2 * time.Minute)
	clock, resumed, err := db.AttemptSchedule(ctx, source, fullDue, nil)
	if err != nil || resumed == nil || resumed.ID != incremental.ID || resumed.Mode != model.ModeIncremental || clock.FullDueAt == nil || !clock.FullDueAt.Equal(fullDue) {
		t.Fatalf("due Full superseded an immutable Incremental continuation: %+v %+v %v", clock, resumed, err)
	}
	if _, err := db.RequestCancel(ctx, incremental.ID); err != nil {
		t.Fatal(err)
	}
	clock, full, err := db.AttemptSchedule(ctx, source, start.Add(3*time.Minute), nil)
	if err != nil || full == nil || full.Mode != model.ModeFull || full.ID == incremental.ID || clock.FullDueAt == nil || !clock.FullDueAt.Equal(start.Add(5*time.Minute)) {
		t.Fatalf("eligible tick lost the overdue Full: %+v %+v %v", clock, full, err)
	}
}

func TestFullRoutineWaitsForRegularCronTick(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := routineSource(t, "1m")
	source.Provider.Schedule.Every = ""
	source.Provider.Schedule.Cron = "*/10 * * * *"
	var err error
	source.Plan, err = scheduling.Parse(source.Provider.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	clock, run, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
	if err != nil || run != nil || clock.FullDueAt == nil || !clock.FullDueAt.Equal(start.Add(time.Minute)) || clock.NextRunAt == nil || !clock.NextRunAt.Equal(start.Add(10*time.Minute)) {
		t.Fatalf("Full deadline dispatched outside the base cadence: %+v %+v %v", clock, run, err)
	}
	clock, run, err = db.AttemptSchedule(ctx, source, start.Add(10*time.Minute), nil)
	if err != nil || run == nil || run.Mode != model.ModeFull || clock.FullDueAt == nil || !clock.FullDueAt.Equal(start.Add(11*time.Minute)) {
		t.Fatalf("eligible cron tick did not select overdue Full: %+v %+v %v", clock, run, err)
	}
}
