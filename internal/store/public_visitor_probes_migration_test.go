package store

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestVisitorProbeUpgradeRetiresMixedPlansWithoutResettingCacheOrCleanup(t *testing.T) {
	ctx, db := sharingDatabase(t)
	generation, _ := db.AIOCacheGeneration(ctx)
	hash := strings.Repeat("a", 40)
	for _, service := range []string{"alldebrid", "torbox"} {
		if err := db.SaveAIOResource(ctx, model.AIOResource{Service: service, Account: "synthetic", Key: "example", Hashes: []string{hash}, CheckedAt: time.Now(), LastAttempt: time.Now(), Generation: generation}); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveSharedAvailability(ctx, service, model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AddAIOCleanup(ctx, model.AIOCleanup{Account: "synthetic", Hash: hash, ID: 77}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM ingest.schema_migrations WHERE version=85`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var plans, cache, cleanup int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_resources),(SELECT count(*) FROM ingest.aio_shared_availability),(SELECT count(*) FROM ingest.aio_cleanup)`).Scan(&plans, &cache, &cleanup); err != nil || plans != 1 || cache != 2 || cleanup != 1 {
		t.Fatal("migration removed availability or pending owned cleanup", plans, cache, cleanup, err)
	}
	if err := db.SaveAIOResource(ctx, model.AIOResource{Service: "alldebrid", Account: "synthetic", Key: "new-administrator-request", Hashes: []string{hash}, CheckedAt: time.Now(), LastAttempt: time.Now(), Generation: generation}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, found, err := db.AIOResource(ctx, "alldebrid", "synthetic", "new-administrator-request"); err != nil || !found {
		t.Fatal("migration was applied again", err)
	}
}
