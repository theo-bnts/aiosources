package integration

import (
	"context"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

// Complete-history regressions explicitly opt into full storage; the default
// compact path is exercised by the other lifecycle and storage contract tests.
func useFullStorage(t *testing.T, ctx context.Context, db *store.Store) {
	t.Helper()
	current, err := db.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.Settings.Mode = model.StorageFull
	if err := db.UpdateStorageSettings(ctx, current.Settings); err != nil {
		t.Fatal(err)
	}
}
