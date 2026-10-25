package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSourceDeletionDefersUntilRemoteMetadataPublicationFinishes(t *testing.T) {
	parent, db := sharingDatabase(t)
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	publishSharingRow(t, ctx, db, "removed", "one", `{"title":"Synthetic local release"}`, nil)
	provider := mirrorSource("retained_mirror")
	provider.CatalogEntities = model.CatalogEntities{Metadata: true}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run = mirrorPage(t, ctx, db, run, "full", "synthetic-checkpoint", true,
		mirrorItem("one", map[string]any{"title": "Synthetic remote release"}, false))

	// Hold the publisher at its metadata boundary. Deletion must yield before
	// taking the journal lock that the admitted publisher still needs.
	gate, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(gate)
	var gatePID int
	if err = gate.QueryRow(ctx, `SELECT pg_backend_pid() FROM ingest.aio_state FOR UPDATE`).Scan(&gatePID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := db.FinishRun(ctx, run.ID, model.StatusSucceeded, "", "")
		finished <- err
	}()
	waitForStorageLock(t, ctx, db, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, gatePID)
	deleted := make(chan error, 1)
	go func() { deleted <- db.DeleteSourceData(ctx, "removed", nil) }()
	select {
	case err := <-deleted:
		if !errors.Is(err, model.ErrBusy) {
			t.Fatalf("source deletion did not yield to the publisher: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("source deletion blocked the admitted publisher")
	}
	if rows := mirrorRows(t, ctx, db, "removed"); len(rows) != 1 {
		t.Fatal("deferred deletion removed source data")
	}
	if err = gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal("remote publication failed after deferred deletion:", err)
	}
	if err := db.DeleteSourceData(ctx, "removed", nil); err != nil {
		t.Fatal("source deletion failed after publication:", err)
	}
	if rows := mirrorRows(t, ctx, db, provider.ID); len(rows) != 1 {
		t.Fatal("the other source's publication was lost")
	}
	if rows := mirrorRows(t, ctx, db, "removed"); len(rows) != 0 {
		t.Fatal("deleted source data remains")
	}
}
