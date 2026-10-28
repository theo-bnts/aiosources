package store

import (
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestBookRetryUpgradePreservesQuotaAndSavedProgress(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 80); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET retry_at=now()+interval '1 hour';
 UPDATE ingest.ol_import_state SET position=1234,complete=TRUE,data_complete=TRUE,works=1;`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := db.BookChangesState(ctx)
	progress, progressErr := db.OpenLibraryStatus(ctx)
	if err != nil || progressErr != nil || state.ProviderRetryAt == nil || state.RetryAt == nil || !state.ProviderRetryAt.Equal(*state.RetryAt) || !progress.Complete || progress.Position != 1234 {
		t.Fatal("upgrade discarded a saved wait or import", state, progress, err, progressErr)
	}
	if err = db.ResetBookInformation(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := db.BookChangesState(ctx)
	next, nextErr := db.NextOpenLibraryUpdate(ctx, 15)
	if err != nil || nextErr != nil || after.ProviderRetryAt == nil || !after.ProviderRetryAt.Equal(*state.ProviderRetryAt) || time.Until(next) < 59*time.Minute {
		t.Fatal("reset bypassed a provider-requested wait", after, next, err, nextErr)
	}
}
