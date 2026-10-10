package store

import (
	"context"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStorageMeasurementDoesNotTakeCollectionLeases(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	run := mirrorClaim(t, ctx, db, metadataSource("system-health"), model.ModeFull)
	defer func() { _, _ = db.FinishRun(ctx, run.ID, model.StatusCancelled, "", "") }()
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	report, err := db.MeasureHealth(deadline, model.HealthDisk{Available: true, AvailableBytes: 123})
	if err != nil || !report.Database.Available || report.Database.TotalBytes <= 0 || report.Disk.AvailableBytes != 123 || len(report.Database.Categories) != 8 {
		t.Fatalf("storage measurement traversed collection data or lost its scope: %v", err)
	}
}
