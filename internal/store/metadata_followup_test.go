package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

func metadataFollowups(t *testing.T, ctx context.Context, db *Store, parent model.Run) []model.Run {
	t.Helper()
	runs, err := db.ListRuns(ctx, model.ListOptions{ProviderID: parent.ProviderID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var children []model.Run
	for _, run := range runs.Items {
		if run.MetadataParentRunID == parent.ID {
			children = append(children, run)
		}
	}
	return children
}

func TestIncrementalMetadataFollowupScopesNewAcceptedNativeIdentities(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	provider := metadataSource("metadata-followup-scope")
	seed := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	seed, err := db.SavePage(ctx, seed, model.Page{Items: []model.Record{coverageRecord("10-old"), coverageRecord("11-replaced-hash")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, seed, model.StatusSucceeded)
	provider.Traversal.MetadataAfterIncremental = true
	created, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, ProviderName: "Frozen source name", Config: provider, Revision: "frozen", Mode: model.ModeIncremental, MaxPages: 7})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != created.ID {
		t.Fatalf("claim parent: %+v %v", claimed, err)
	}
	parent := *claimed
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET first_seen_at=$2 WHERE provider_id=$1", provider.ID, parent.CreatedAt.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	replaced, complete, partial := coverageRecord("11-replaced-hash"), coverageRecord("50-complete"), coverageRecord("30-partial")
	replaced.Fields["info_hash"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	complete.Fields["external_ids"], complete.Fields["metadata"] = []any{}, false
	partial.Fields["external_ids"] = []any{}
	ignored, auxiliary := coverageRecord("60-ignored"), coverageRecord("70-auxiliary")
	ignored.Ignored, auxiliary.Auxiliary = true, true
	parent, err = db.SavePage(ctx, parent, model.Page{Items: []model.Record{
		coverageRecord("10-old"), replaced, coverageRecord("20-boundary"), partial,
		coverageRecord("40-hash-sibling"), complete, ignored, auxiliary, coverageRecord("90-future"),
	}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET first_seen_at=$2 WHERE provider_id=$1 AND source_id='20-boundary'", provider.ID, parent.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET first_seen_at=$2 WHERE provider_id=$1 AND source_id='90-future'", provider.ID, parent.CreatedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"60-ignored", "70-auxiliary", "80-unaccepted"} {
		publishSharingRow(t, ctx, db, provider.ID, id, `{"info_hash":"0123456789012345678901234567890123456789"}`, nil)
	}
	parent = mirrorFinish(t, ctx, db, parent, model.StatusSucceeded)
	children := metadataFollowups(t, ctx, db, parent)
	if len(children) != 1 {
		t.Fatalf("successful Incremental did not queue exactly one follow-up: %+v", children)
	}
	child := children[0]
	if child.Status != model.StatusQueued || child.Mode != model.ModeMetadata || child.Trigger != parent.Trigger || child.Revision != parent.Revision ||
		child.ProviderName != parent.ProviderName || child.MaxPages != parent.MaxPages || !reflect.DeepEqual(child.Config, parent.Config) || !reflect.DeepEqual(child.Policy, parent.Policy) ||
		child.StartedAt != nil || child.FinishedAt != nil || child.Pages != 0 || child.Records != 0 || child.DistinctRecords != 0 || len(child.Cursor) != 0 || child.TraversalDone {
		t.Fatalf("follow-up lost its frozen policy or inherited traversal progress: %+v", child)
	}
	want := []model.MetadataCandidate{
		{SourceID: "20-boundary", InfoHash: "0123456789012345678901234567890123456789"},
		{SourceID: "30-partial", InfoHash: "0123456789012345678901234567890123456789"},
		{SourceID: "40-hash-sibling", InfoHash: "0123456789012345678901234567890123456789"},
	}
	candidates, err := db.MetadataCandidates(ctx, child, "", 100)
	if err != nil || !reflect.DeepEqual(candidates, want) {
		t.Fatalf("follow-up escaped new accepted identity scope or collapsed hash siblings: %+v %v", candidates, err)
	}
	next, err := db.MetadataCandidates(ctx, child, "20-boundary", 1)
	if err != nil || !reflect.DeepEqual(next, want[1:2]) {
		t.Fatalf("linked keyset continuation changed scope: %+v %v", next, err)
	}
	var eventData []byte
	if err := db.pool.QueryRow(ctx, "SELECT data FROM ingest.events WHERE run_id=$1 AND kind='metadata_queued'", parent.ID).Scan(&eventData); err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(eventData, &data); err != nil || !reflect.DeepEqual(data, map[string]any{"metadata_run_id": child.ID}) {
		t.Fatalf("parent event lacks a safe child relationship: %s %v", eventData, err)
	}
	db.Close()
	reopened, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	persisted, err := reopened.GetRun(ctx, child.ID)
	if err != nil || persisted.MetadataParentRunID != parent.ID || persisted.Status != model.StatusQueued {
		t.Fatalf("reopening lost the queued relationship: %+v %v", persisted, err)
	}
	candidates, err = reopened.MetadataCandidates(ctx, persisted, "", 100)
	if err != nil || !reflect.DeepEqual(candidates, want) {
		t.Fatalf("reopening expanded the candidate scope: %+v %v", candidates, err)
	}
	claimed, err = reopened.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != child.ID {
		t.Fatalf("claim manual follow-up after reopening: %+v %v", claimed, err)
	}
	if _, err := reopened.FinishRun(ctx, child.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	if active, err := reopened.ClaimNext(ctx); err != nil || active != nil {
		t.Fatalf("manual follow-up bypassed its budget pause: %+v %v", active, err)
	}
	resumed, err := reopened.ResumeRun(ctx, child.ID)
	if err != nil || resumed.MetadataParentRunID != parent.ID {
		t.Fatalf("explicit resume lost the follow-up origin: %+v %v", resumed, err)
	}
	if _, err := reopened.RequestCancel(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	standalone := mirrorClaim(t, ctx, reopened, provider, model.ModeMetadata)
	candidates, err = reopened.MetadataCandidates(ctx, standalone, "", 100)
	ids := make([]string, len(candidates))
	for index, candidate := range candidates {
		ids[index] = candidate.SourceID
	}
	if err != nil || standalone.MetadataParentRunID != "" || !reflect.DeepEqual(ids, []string{"10-old", "11-replaced-hash", "20-boundary", "30-partial", "40-hash-sibling", "60-ignored", "70-auxiliary", "80-unaccepted"}) {
		t.Fatalf("standalone Metadata inherited the automatic restriction: %+v %v", candidates, err)
	}
	if _, err := reopened.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeMetadata, MetadataParentRunID: parent.ID}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("manual creation accepted an automatic parent link: %v", err)
	}
}

func TestIncrementalMetadataFollowupRequiresSuccessfulEligibleNewWork(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	cases := []struct {
		name    string
		mode    model.RunMode
		status  model.RunStatus
		control string
	}{
		{name: "opt-out"}, {name: "unsupported"}, {name: "no-records"}, {name: "old-only"}, {name: "complete-only"}, {name: "ignored-only"}, {name: "future-only"},
		{name: "preview", mode: model.ModePreview}, {name: "full", mode: model.ModeFull}, {name: "metadata", mode: model.ModeMetadata},
		{name: "budget-pause", status: model.StatusPaused}, {name: "failed", status: model.StatusFailed}, {name: "cancelled", status: model.StatusCancelled},
		{name: "pause-request", control: "pause"}, {name: "cancel-request", control: "cancel"}, {name: "unfinished"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := metadataSource("followup-" + tc.name)
			provider.Traversal.MetadataAfterIncremental = tc.name != "opt-out"
			if tc.name == "unsupported" {
				provider.Traversal.IDRecovery = nil
			}
			mode := tc.mode
			if mode == "" {
				mode = model.ModeIncremental
			}
			parent := mirrorClaim(t, ctx, db, provider, mode)
			record := coverageRecord("record")
			if tc.name == "complete-only" {
				record.Fields["external_ids"], record.Fields["metadata"] = []any{}, map[string]any{}
			}
			if tc.name == "ignored-only" {
				record.Ignored = true
			}
			items := []model.Record{record}
			if tc.name == "no-records" {
				items = nil
			}
			var err error
			parent, err = db.SavePage(ctx, parent, model.Page{Items: items, Done: tc.name != "unfinished"}, "")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "old-only" || tc.name == "future-only" {
				firstSeen := parent.CreatedAt.Add(-time.Second)
				if tc.name == "future-only" {
					firstSeen = parent.CreatedAt.Add(time.Hour)
				}
				if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET first_seen_at=$2 WHERE provider_id=$1", provider.ID, firstSeen); err != nil {
					t.Fatal(err)
				}
			}
			if tc.control == "pause" {
				if _, err := db.RequestPause(ctx, parent.ID); err != nil {
					t.Fatal(err)
				}
			} else if tc.control == "cancel" {
				if _, err := db.RequestCancel(ctx, parent.ID); err != nil {
					t.Fatal(err)
				}
			}
			status, reason := tc.status, model.PauseReason("")
			if status == "" {
				status = model.StatusSucceeded
			}
			if status == model.StatusPaused {
				reason = model.PauseBudget
			}
			finished, err := db.FinishRun(ctx, parent.ID, status, "", reason)
			if tc.name == "unfinished" {
				if !errors.Is(err, model.ErrConflict) {
					t.Fatalf("unfinished Incremental accepted success: %+v %v", finished, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if children := metadataFollowups(t, ctx, db, parent); len(children) != 0 {
				t.Fatalf("ineligible completion queued Metadata: %+v", children)
			}
			if tc.control == "pause" {
				if finished.Status != model.StatusPaused || finished.PauseReason != model.PauseManual {
					t.Fatalf("completion bypassed a pause request: %+v", finished)
				}
				if _, err := db.ResumeRun(ctx, parent.ID); err != nil {
					t.Fatal(err)
				}
				resumed, err := db.ClaimNext(ctx)
				if err != nil || resumed == nil || resumed.ID != parent.ID {
					t.Fatalf("resume completed traversal: %+v %v", resumed, err)
				}
				mirrorFinish(t, ctx, db, *resumed, model.StatusSucceeded)
				children := metadataFollowups(t, ctx, db, parent)
				if len(children) != 1 {
					t.Fatalf("explicit resume lost the follow-up: %+v", children)
				}
				if _, err := db.RequestCancel(ctx, children[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			if tc.control == "cancel" && finished.Status != model.StatusCancelled {
				t.Fatalf("completion bypassed a cancellation: %+v", finished)
			}
		})
	}
}

func TestIncrementalMetadataFollowupRollsBackAndRecoversWithoutDuplicates(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	provider := metadataSource("metadata-followup-recovery")
	provider.Traversal.MetadataAfterIncremental = true
	parent := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	parent, err := db.SavePage(ctx, parent, model.Page{Items: []model.Record{coverageRecord("new")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Fail after the child insert. Neither success nor the queued child may
	// survive independently of the lifecycle event that links them.
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_metadata_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.kind='metadata_queued' THEN RAISE EXCEPTION 'synthetic follow-up persistence failure'; END IF; RETURN NEW; END; $$;
CREATE TRIGGER reject_metadata_event BEFORE INSERT ON ingest.events
FOR EACH ROW EXECUTE FUNCTION ingest.reject_metadata_event()`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, parent.ID, model.StatusSucceeded, "", ""); err == nil {
		t.Fatal("follow-up event failure did not roll back completion")
	}
	stored, err := db.GetRun(ctx, parent.ID)
	if err != nil || stored.Status != model.StatusRunning || !stored.TraversalDone || len(metadataFollowups(t, ctx, db, parent)) != 0 {
		t.Fatalf("failed completion left a partial parent/child transition: %+v %v", stored, err)
	}
	if _, err := db.pool.Exec(ctx, "DROP TRIGGER reject_metadata_event ON ingest.events"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if err := reopened.RecoverInterrupted(ctx, map[string]bool{provider.ID: true}); err != nil {
		t.Fatal(err)
	}
	stored, err = reopened.GetRun(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == model.StatusPaused {
		if _, err := reopened.ResumeRun(ctx, parent.ID); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := reopened.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != parent.ID || !claimed.TraversalDone {
		t.Fatalf("recovery lost the completed checkpoint: %+v %v", claimed, err)
	}
	mirrorFinish(t, ctx, reopened, *claimed, model.StatusSucceeded)
	children := metadataFollowups(t, ctx, reopened, parent)
	if len(children) != 1 {
		t.Fatalf("recovery lost or duplicated the follow-up: %+v", children)
	}
	childID := children[0].ID
	if _, err := reopened.FinishRun(ctx, parent.ID, model.StatusSucceeded, "", ""); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("completed parent was finalized again: %v", err)
	}
	reopened.Close()
	restarted, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	if err := restarted.RecoverInterrupted(ctx, map[string]bool{provider.ID: true}); err != nil {
		t.Fatal(err)
	}
	claimed, err = restarted.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != childID || claimed.MetadataParentRunID != parent.ID {
		t.Fatalf("restart lost the committed queue item: %+v %v", claimed, err)
	}
	child, err := restarted.SavePage(ctx, *claimed, model.Page{Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, restarted, child, model.StatusSucceeded)
	children = metadataFollowups(t, ctx, restarted, parent)
	if len(children) != 1 || children[0].ID != childID || len(metadataFollowups(t, ctx, restarted, child)) != 0 {
		t.Fatalf("recovery or Metadata completion duplicated automatic work: %+v", children)
	}
	var events int
	if err := restarted.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.events WHERE run_id=$1 AND kind='metadata_queued'", parent.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("successful completion did not have exactly one durable parent event: %d %v", events, err)
	}
}

func TestScheduledMetadataFollowupContinuesBudgetWithoutChangingSnapshot(t *testing.T) {
	ctx, endpoint, db := scheduleDatabase(t)
	source := routineSource(t, "2m")
	provider := metadataSource(source.ID)
	provider.Enabled, provider.Schedule = true, source.Provider.Schedule
	provider.Traversal.MetadataAfterIncremental = true
	source.Provider = provider
	seedCollectionBaseline(t, ctx, db, provider)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	due := start.Add(time.Minute)
	_, queued, err := db.AttemptSchedule(ctx, source, due, nil)
	if err != nil || queued == nil || queued.Mode != model.ModeIncremental {
		t.Fatalf("queue scheduled Incremental: %+v %v", queued, err)
	}
	parent, err := db.ClaimNext(ctx)
	if err != nil || parent == nil || parent.ID != queued.ID {
		t.Fatalf("claim scheduled Incremental: %+v %v", parent, err)
	}
	saved, err := db.SavePage(ctx, *parent, model.Page{Items: []model.Record{coverageRecord("10"), coverageRecord("20")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, saved, model.StatusSucceeded)
	child, err := db.ClaimNext(ctx)
	if err != nil || child == nil || child.MetadataParentRunID != parent.ID || child.Trigger != model.TriggerScheduled || child.MaxPages != parent.MaxPages || !reflect.DeepEqual(child.Policy, parent.Policy) {
		t.Fatalf("scheduled follow-up lost its execution policy: %+v %v", child, err)
	}
	cursor := json.RawMessage(`{"metadata_version":1,"after":"10"}`)
	saved, err = db.SavePage(ctx, *child, model.Page{Next: cursor}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, child.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	// Discovery gets a turn between metadata budgets, including a due Full.
	clock, discovery, err := reopened.AttemptSchedule(ctx, source, due.Add(time.Minute), nil)
	if err != nil || discovery == nil || discovery.Mode != model.ModeFull || discovery.ID == child.ID {
		t.Fatalf("metadata monopolized discovery: %+v %v", discovery, err)
	}
	claimedDiscovery, err := reopened.ClaimNext(ctx)
	if err != nil || claimedDiscovery == nil || claimedDiscovery.ID != discovery.ID {
		t.Fatalf("claim interleaved discovery: %+v %v", claimedDiscovery, err)
	}
	if _, err := reopened.FinishRun(ctx, discovery.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	_, discovery, err = reopened.AttemptSchedule(ctx, source, due.Add(2*time.Minute), nil)
	if err != nil || discovery == nil || discovery.ID != claimedDiscovery.ID {
		t.Fatalf("older metadata stranded budgeted discovery: %+v %v", discovery, err)
	}
	claimedDiscovery, err = reopened.ClaimNext(ctx)
	if err != nil || claimedDiscovery == nil || claimedDiscovery.ID != discovery.ID {
		t.Fatal("claim continued discovery", err)
	}
	collected, err := reopened.SavePage(ctx, *claimedDiscovery, model.Page{Items: []model.Record{coverageRecord("10"), coverageRecord("20")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, reopened, collected, model.StatusSucceeded)
	// Enrichment now resumes its original immutable snapshot and cursor.
	clock, continued, err := reopened.AttemptSchedule(ctx, source, due.Add(3*time.Minute), nil)
	if err != nil || continued == nil || continued.ID != child.ID || continued.Mode != model.ModeMetadata || continued.MetadataParentRunID != parent.ID ||
		!reflect.DeepEqual(continued.Config, saved.Config) || !reflect.DeepEqual(continued.Policy, saved.Policy) || clock.FullDueAt == nil || !clock.FullDueAt.Equal(due.Add(3*time.Minute)) {
		t.Fatalf("scheduled budget continuation replaced the follow-up: %+v %+v %v", clock, continued, err)
	}
	var state struct {
		After string `json:"after"`
	}
	if err := json.Unmarshal(continued.Cursor, &state); err != nil || state.After != "10" {
		t.Fatalf("continuation lost the committed metadata checkpoint: %s %v", continued.Cursor, err)
	}
	candidates, err := reopened.MetadataCandidates(ctx, *continued, state.After, 100)
	if err != nil || !reflect.DeepEqual(candidates, []model.MetadataCandidate{{SourceID: "20", InfoHash: "0123456789012345678901234567890123456789"}}) {
		t.Fatalf("continuation replayed or expanded its parent scope: %+v %v", candidates, err)
	}
	claimed, err := reopened.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != child.ID {
		t.Fatalf("claim continued Metadata: %+v %v", claimed, err)
	}
	if _, err := reopened.FinishRun(ctx, child.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	source.Revision = "changed"
	if _, err := reopened.ReconcileSchedules(ctx, []scheduling.Source{source}, due.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	clock, continued, err = reopened.AttemptSchedule(ctx, source, due.Add(4*time.Minute), nil)
	if err != nil || continued != nil || clock.LastError == "" {
		t.Fatalf("changed revision automatically resumed or replaced Metadata: %+v %+v %v", clock, continued, err)
	}
	persisted, err := reopened.GetRun(ctx, child.ID)
	if err != nil || persisted.Status != model.StatusPaused || persisted.Revision != parent.Revision || len(metadataFollowups(t, ctx, reopened, *parent)) != 1 {
		t.Fatalf("incompatible schedule mutated the immutable follow-up: %+v %v", persisted, err)
	}
}
