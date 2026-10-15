package store

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSharedCacheStatisticsUsePlaybackFreshnessWithoutCountingPrivateAccounts(t *testing.T) {
	ctx, db := sharingDatabase(t)
	g, err := db.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, item := range []struct {
		cached bool
		at     time.Time
	}{
		{true, now.Add(-time.Minute)}, {false, now.Add(-time.Minute)},
		{true, now.Add(-2 * time.Hour)}, {false, now.Add(-16 * time.Minute)}, {true, now.Add(time.Hour)},
	} {
		a := model.AIOAvailability{Generation: g, Hash: strings.Repeat(string(rune('a'+i)), 40), Cached: item.cached, CheckedAt: item.at}
		if err := db.SaveSharedAvailability(ctx, "torbox", a); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SaveAIOAvailability(ctx, "torbox", "synthetic-private-account", model.AIOAvailability{Generation: g, Hash: strings.Repeat("f", 40), Cached: true, CheckedAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	items, err := db.SharedAvailabilityStats(ctx, now.Add(-time.Hour))
	if err != nil || len(items) != 1 || items[0].Service != "torbox" || items[0].Cached != 1 || items[0].Uncached != 1 {
		t.Fatalf("expired, future or private observations leaked into shared counts: %+v, %v", items, err)
	}
	available, err := db.SharedAvailability(ctx, "torbox", []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40), strings.Repeat("e", 40)}, now.Add(-time.Hour))
	if err != nil || len(available) != int(items[0].Cached+items[0].Uncached) {
		t.Fatalf("statistics differ from playback: %d, %v", len(available), err)
	}
}
