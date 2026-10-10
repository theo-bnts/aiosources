package store

import (
	"context"
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestOperationHistoryRecoveryUsesActualWorkerLocks(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	release, locked, err := db.AIOLock(ctx, "matcher")
	if err != nil || !locked {
		t.Fatal(err)
	}
	defer release()
	run, err := db.BeginOperation(ctx, model.OperationRun{Kind: "matching", Service: "local", Trigger: "automatic"}, "matcher")
	if err != nil {
		t.Fatal(err)
	}
	run.Phase = "identification"
	run.Processed = 7
	run.Results = map[string]int64{"matched": 5, "unmatched": 2}
	if err = db.SaveOperation(ctx, run); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err = other.RecoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := other.Operation(ctx, run.ID)
	if err != nil || active.Status != "running" || active.Processed != 7 {
		t.Fatalf("live worker changed: %+v %v", active, err)
	}
	release()
	if err = other.RecoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	interrupted, err := other.Operation(ctx, run.ID)
	if err != nil || interrupted.Status != "interrupted" || interrupted.Processed != 7 || interrupted.Results["matched"] != 5 || interrupted.FinishedAt == nil {
		t.Fatalf("lost checkpoint: %+v %v", interrupted, err)
	}
	if err = other.RecoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := other.OperationEvents(ctx, run.ID, model.ListOptions{Limit: 1})
	if err != nil || events.Total != 3 || len(events.Items) != 1 || events.Items[0].Phase != "interrupted" {
		t.Fatalf("recovery events %+v %v", events, err)
	}
	if err = db.SaveOperation(ctx, run); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("terminal checkpoint overwritten: %v", err)
	}
}

func TestOperationHistoryFiltersPaginationAndPreservesPreviousAttempt(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	release, locked, err := db.AIOLock(ctx, "metadata")
	if err != nil || !locked {
		t.Fatal(err)
	}
	defer release()
	first, err := db.BeginOperation(ctx, model.OperationRun{Kind: "metadata", Service: "tmdb", Trigger: "manual"}, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	first.Processed = 20
	first.Errors = 2
	first.Results = map[string]int64{}
	if err = db.SaveOperation(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := db.BeginOperation(ctx, model.OperationRun{Kind: "metadata", Service: "tmdb", Trigger: "automatic"}, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	second.Status = "succeeded"
	second.Processed = 10
	if err = db.SaveOperation(ctx, second); err != nil {
		t.Fatal(err)
	}
	previous, err := db.Operation(ctx, first.ID)
	if err != nil || previous.Status != "interrupted" || previous.Processed != 20 {
		t.Fatalf("previous attempt %+v %v", previous, err)
	}
	cache, err := db.BeginOperation(ctx, model.OperationRun{Kind: "cache", Service: "torbox", Trigger: "playback", Resource: "movie:tt1234567"}, "test-cache")
	if err != nil {
		t.Fatal(err)
	}
	cache.Status = "failed"
	cache.Errors = 1
	if err = db.SaveOperation(ctx, cache); err != nil {
		t.Fatal(err)
	}
	page, err := db.ListOperations(ctx, model.OperationFilter{Group: "metadata", ListOptions: model.ListOptions{Limit: 1}})
	if err != nil || page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("metadata page %+v %v", page, err)
	}
	next, err := db.ListOperations(ctx, model.OperationFilter{Group: "metadata", ListOptions: model.ListOptions{Limit: 1, Offset: 1}})
	if err != nil || next.Total != 2 || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatalf("next page %+v %v", next, err)
	}
	debrid, err := db.ListOperations(ctx, model.OperationFilter{Group: "debrid", Service: "torbox", ListOptions: model.ListOptions{Status: "failed"}})
	if err != nil || debrid.Total != 1 || debrid.Items[0].ID != cache.ID {
		t.Fatalf("debrid filter %+v %v", debrid, err)
	}
	empty, err := db.ListOperations(ctx, model.OperationFilter{Group: "metadata", Service: "torbox"})
	if err != nil || empty.Total != 0 || empty.Items == nil {
		t.Fatalf("empty filter %+v %v", empty, err)
	}
	_, err = db.OperationEvents(context.Background(), "missing", model.ListOptions{})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown run %v", err)
	}
}
