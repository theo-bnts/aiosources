package store

import (
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestUnavailableAccountItemInvalidatesOnlyItsOlderEvidence(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	generation, err := db.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hash, observed := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now().Add(-time.Minute)
	for _, account := range []string{"older", "newer", "other"} {
		at := observed.Add(-time.Second)
		if account == "newer" {
			at = observed.Add(time.Second)
		}
		if err := db.SaveAIOAvailability(ctx, "realdebrid", account, model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: at, Generation: generation}); err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []string{"older", "newer"} {
		if err := db.ForgetAIOAvailability(ctx, "realdebrid", account, hash, observed); err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []string{"older", "newer", "other"} {
		items, err := db.AIOAvailability(ctx, "realdebrid", account, []string{hash}, observed.Add(-time.Hour))
		if err != nil || (len(items) == 1) != (account != "older") {
			t.Fatalf("account isolation or observation ordering failed: %s %v", account, err)
		}
	}
}
