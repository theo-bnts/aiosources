package main

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestStartupMigrationCanOutlastConnectionTimeout(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	t.Setenv("DATABASE_URL", endpoint)
	conn, err := pgx.Connect(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	gate, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Rollback(ctx) }()
	// An earlier upgrade or a large index build can hold this shared migration
	// lock longer than the connection budget. Exercise the real command path.
	if _, err := gate.Exec(ctx, "SELECT pg_advisory_xact_lock(1768843109,1)"); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command(ctx, []string{"pause", "--run", "missing-run"}) }()
	select {
	case err := <-finished:
		t.Fatalf("startup stopped before the schema upgrade could finish: %v", err)
	case <-time.After(31 * time.Second):
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("command did not reach the missing-run check after migration: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("startup did not finish after the migration lock was released")
	}
	var version int
	if err := conn.QueryRow(ctx, "SELECT max(version) FROM ingest.schema_migrations").Scan(&version); err != nil || version != store.LatestSchemaVersion() {
		t.Fatalf("startup did not complete the schema upgrade: %d %v", version, err)
	}
}
