package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSharedAvailabilityRetainsOnlyAccountIndependentDataAndFreshEvidence(t *testing.T) {
	ctx, db := sharingDatabase(t)
	generation, _ := db.AIOCacheGeneration(ctx)
	hash := strings.Repeat("a", 40)
	positive := model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation,
		Files: []model.AIOFile{{ID: 42, Path: "Example.mp4", Size: 100, Link: "https://private.example/account-link"}}}
	if err := db.SaveSharedAvailability(ctx, "torbox", positive); err != nil {
		t.Fatal(err)
	}
	older := positive
	older.Cached, older.CheckedAt = false, positive.CheckedAt.Add(-time.Minute)
	if err := db.SaveSharedAvailability(ctx, "torbox", older); err != nil {
		t.Fatal(err)
	}
	other := older
	other.CheckedAt = time.Now().Add(-20 * time.Minute)
	if err := db.SaveSharedAvailability(ctx, "alldebrid", other); err != nil {
		t.Fatal(err)
	}
	read := func(service string) []model.AIOAvailability {
		t.Helper()
		items, err := db.SharedAvailability(ctx, service, []string{hash}, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	items := read("torbox")
	if len(items) != 1 || !items[0].Cached || len(items[0].Files) != 1 || items[0].Files[0].ID != 0 || items[0].Files[0].Link != "" || positive.Files[0].ID != 42 {
		t.Fatal("shared projection is account-bound, mutated its input or accepted older evidence")
	}
	if len(read("alldebrid")) != 0 {
		t.Fatal("old negative suppressed a later cache check")
	}
	if err := db.ForgetSharedAvailability(ctx, "torbox", hash, older.CheckedAt); err != nil {
		t.Fatal(err)
	}
	if len(read("torbox")) != 1 {
		t.Fatal("old invalidation removed a newer result")
	}
	positive.CheckedAt = time.Now().Add(time.Hour)
	if err := db.SaveSharedAvailability(ctx, "torbox", positive); err != nil {
		t.Fatal(err)
	}
	if len(read("torbox")) != 0 {
		t.Fatal("future evidence was served")
	}
	if err := db.ResetAIOCache(ctx); err != nil {
		t.Fatal(err)
	}
	positive.CheckedAt = time.Now()
	if err := db.SaveSharedAvailability(ctx, "torbox", positive); err != nil {
		t.Fatal(err)
	}
	if len(read("torbox")) != 0 {
		t.Fatal("reset was repopulated by an old writer")
	}
	access, err := db.PublicAccess(ctx)
	if err != nil || access.Enabled {
		t.Fatal("public access enabled by default", err)
	}
	access.Enabled = true
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if err = db.SetPublicAccess(ctx, access); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale access change accepted")
	}
}

func TestProviderDiagnosticsUseAllowlistedValuesAndConcurrentFixedCaps(t *testing.T) {
	ctx, db := sharingDatabase(t)
	run, err := db.BeginOperation(ctx, model.OperationRun{Kind: "cache", Service: "torbox", Trigger: "playback"}, "synthetic-cache-operation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.operation_events(run_id,phase,processed,errors) SELECT $1,'availability',0,0 FROM generate_series(1,998)`, run.ID); err != nil {
		t.Fatal(err)
	}
	var checks sync.WaitGroup
	for range 8 {
		checks.Add(1)
		go func() {
			defer checks.Done()
			if e := db.RecordProviderCall(ctx, run.ID, "provider_availability", 200, 5, ""); e != nil {
				t.Error(e)
			}
		}()
	}
	checks.Wait()
	events, err := db.OperationEvents(ctx, run.ID, model.ListOptions{})
	if err != nil || events.Total != 1000 {
		t.Fatal("concurrent diagnostics bypassed fixed cap", events.Total, err)
	}
	if err = db.RecordProviderCall(ctx, run.ID, "https://private.example/key", 200, 5, ""); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("arbitrary phase persisted")
	}
	request := model.StremioRequest{Origin: "public", Kind: "playback", Service: "torbox", MediaType: "movie", Resource: "imdb:tt1234567", Outcome: "results"}
	for range 40 {
		request.Calls = append(request.Calls, model.ProviderCall{Phase: "provider_playback", HTTPStatus: 200, DurationMS: 10})
	}
	if err = db.RecordStremioRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	list, _ := db.ListStremioRequests(ctx, model.ListOptions{})
	if len(list.Items) != 1 || list.Items[0].Calls != nil {
		t.Fatal("summary loads detailed diagnostics")
	}
	detail, err := db.StremioRequest(ctx, list.Items[0].ID)
	if err != nil || len(detail.Calls) != 32 {
		t.Fatal("playback diagnostics not bounded", err)
	}
	request.Calls = []model.ProviderCall{{Phase: "provider_playback", ErrorCode: "private-value"}}
	if err = db.RecordStremioRequest(ctx, request); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("arbitrary provider error persisted")
	}
}
