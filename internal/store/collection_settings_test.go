package store

import (
	"fmt"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDefaultCollectionConcurrencyPreservesAnExistingSetting(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	initial, err := db.CollectionSettings(ctx)
	if err != nil || initial.Workers != 5 {
		t.Fatalf("unset concurrency should default to five: %+v %v", initial, err)
	}
	if err := db.InitializeCollectionSettings(ctx, model.DefaultCollectionWorkers); err != nil {
		t.Fatal(err)
	}
	initial.Workers = 3
	if err := db.UpdateCollectionSettings(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if err := db.InitializeCollectionSettings(ctx, model.DefaultCollectionWorkers); err != nil {
		t.Fatal(err)
	}
	saved, err := db.CollectionSettings(ctx)
	if err != nil || saved.Workers != 3 {
		t.Fatalf("restart overwrote saved concurrency: %+v %v", saved, err)
	}
}

func TestCollectionCapacityIsSharedAndDecreasesDrainActiveRuns(t *testing.T) {
	ctx, databaseURL, first := activityDatabase(t)
	if err := first.InitializeCollectionSettings(ctx, 1); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	for index := range 3 {
		id := fmt.Sprintf("capacity-%d", index)
		if _, err := first.CreateRun(ctx, model.Run{ProviderID: id, Config: model.Provider{ID: id}, Mode: model.ModePreview}); err != nil {
			t.Fatal(err)
		}
	}
	type claim struct {
		owner *Store
		run   *model.Run
		err   error
	}
	start, results := make(chan struct{}), make(chan claim, 2)
	for _, owner := range []*Store{first, second} {
		go func() {
			<-start
			run, err := owner.ClaimNext(ctx)
			results <- claim{owner, run, err}
		}()
	}
	close(start)
	var active []claim
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.run != nil {
			active = append(active, result)
		}
	}
	if len(active) != 1 {
		t.Fatalf("shared limit admitted %d simultaneous collections, want 1", len(active))
	}
	update := func(workers int) {
		t.Helper()
		current, err := second.CollectionOverview(ctx)
		if err != nil {
			t.Fatal(err)
		}
		current.Settings.Workers = workers
		if err := second.UpdateCollectionSettings(ctx, current.Settings); err != nil {
			t.Fatal(err)
		}
	}
	update(2)
	run, err := second.ClaimNext(ctx)
	if err != nil || run == nil {
		t.Fatalf("increased capacity did not admit queued work: %+v %v", run, err)
	}
	active = append(active, claim{owner: second, run: run})
	update(1)
	for _, held := range active {
		current, err := first.GetRun(ctx, held.run.ID)
		if err != nil || current.Status != model.StatusRunning || current.PauseRequested || current.CancelRequested {
			t.Fatalf("decrease interrupted an active collection: %+v %v", current, err)
		}
	}
	for _, held := range active {
		if run, err := first.ClaimNext(ctx); err != nil || run != nil {
			t.Fatalf("decrease admitted work before active runs drained: %+v %v", run, err)
		}
		if _, err := held.owner.FinishRun(ctx, held.run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
			t.Fatal(err)
		}
	}
	// A subsequent process's bootstrap value must not overwrite the saved limit.
	if err := second.InitializeCollectionSettings(ctx, 32); err != nil {
		t.Fatal(err)
	}
	current, err := second.CollectionOverview(ctx)
	if err != nil || current.Settings.Workers != 1 || current.Running != 0 || current.Queued != 1 {
		t.Fatalf("saved limit or queue state did not survive another initialization: %+v %v", current, err)
	}
	run, err = first.ClaimNext(ctx)
	if err != nil || run == nil {
		t.Fatalf("drained capacity did not admit remaining work: %+v %v", run, err)
	}
	if _, err := first.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
}
