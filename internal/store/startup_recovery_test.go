package store

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestRecoveryDoesNotWaitForLiveCollectionQueue(t *testing.T) {
	ctx, endpoint, owner := activityDatabase(t)
	run := mirrorClaim(t, ctx, owner, model.Provider{ID: "live-source", Adapter: "http_json"}, model.ModeFull)
	var err error
	run, err = owner.SavePage(ctx, run, model.Page{Next: json.RawMessage(`{"page":2}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := owner.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(queue)
	if err := lockProviderQueue(ctx, queue, run.ProviderID); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	// A second process must recognize the worker's live session lease before
	// waiting for its queue transaction, which may be publishing a large import.
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := peer.RecoverInterrupted(bounded, map[string]bool{run.ProviderID: true}); err != nil {
		t.Fatalf("recovery waited for a live worker's queue: %v", err)
	}
	live, err := peer.GetRun(ctx, run.ID)
	if err != nil || live.Status != model.StatusRunning || !bytes.Equal(live.Cursor, run.Cursor) {
		t.Fatalf("live checkpoint changed: %+v %v", live, err)
	}
	if err := queue.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	owner.Close()
	if err := peer.RecoverInterrupted(ctx, map[string]bool{run.ProviderID: true}); err != nil {
		t.Fatal(err)
	}
	recovered, err := peer.GetRun(ctx, run.ID)
	if err != nil || recovered.Status != model.StatusQueued || !bytes.Equal(recovered.Cursor, run.Cursor) {
		t.Fatalf("orphaned checkpoint was not recovered: %+v %v", recovered, err)
	}
}
