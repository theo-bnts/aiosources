package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestPublicConnectionsUpgradePreservesDisclosureAndKeepsTorznabOptIn(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.migrateThrough(ctx, 55); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.aio_public_access SET enabled=true,allow_private_downloads=true,revision=7,generation=9`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	access, err := db.PublicAccess(ctx)
	if err != nil || !access.Enabled || !access.ExposeSources || access.TorznabEnabled || access.Generation != 9 || access.Revision != 7 || access.TorznabGeneration != 1 {
		t.Fatal("upgrade changed consent or existing installations", access, err)
	}
	access.TorznabEnabled = true
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	next, err := db.PublicAccess(ctx)
	if err != nil || next.TorznabGeneration != 2 || next.Generation != 9 {
		t.Fatal("Torznab policy used the Stremio generation", next, err)
	}
}
