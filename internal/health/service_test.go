package health

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestDatabaseFailureRetainsLastMeasurementAndFreshLocalSpace(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service, err := New(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.measure(ctx)
	before := service.Report()
	if before == nil || !before.Database.Available || before.Database.MeasuredAt == nil || before.Database.TotalBytes <= 0 || !before.Disk.Available || len(before.Database.Categories) != 8 {
		t.Fatal("initial storage measurement is incomplete")
	}
	db.Close()
	service.measure(ctx)
	after := service.Report()
	if after.Status != "unavailable" || after.Database.Available || after.Database.TotalBytes != before.Database.TotalBytes || !after.Database.MeasuredAt.Equal(*before.Database.MeasuredAt) || !after.Disk.Available || after.Disk.MeasuredAt.Before(before.Disk.MeasuredAt) {
		t.Fatal("database failure fabricated storage values or discarded the last successful measurement")
	}
	if !before.Database.Available || before.Status != "healthy" {
		t.Fatal("failed measurement mutated a previously published snapshot")
	}
}
