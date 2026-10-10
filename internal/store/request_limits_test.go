package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func requestLimitDatabase(t *testing.T) (context.Context, string, *Store) {
	t.Helper()
	ctx, url := testutil.NewDatabase(t)
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, url, db
}

func requestLimitClock(t *testing.T, ctx context.Context, db *Store) time.Time {
	t.Helper()
	var now time.Time
	if err := db.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now
}

func seedProviderRequest(t *testing.T, ctx context.Context, db *Store, providerID string, admittedAt time.Time) {
	t.Helper()
	if _, err := db.pool.Exec(ctx, "INSERT INTO ingest.provider_requests(provider_id,admitted_at) VALUES($1,$2)", providerID, admittedAt); err != nil {
		t.Fatal(err)
	}
}

func requireProviderRequestDeadline(t *testing.T, ctx context.Context, db *Store, providerID string, limits model.RequestLimits, want time.Time) {
	t.Helper()
	got, err := db.ReserveProviderRequest(ctx, providerID, limits)
	if err != nil || !got.Equal(want) {
		t.Fatalf("reservation deadline = %s, err = %v; want %s", got, err, want)
	}
}

func TestProviderRequestLimitsRollingWindows(t *testing.T) {
	ctx, _, db := requestLimitDatabase(t)
	for _, test := range []struct {
		name   string
		window time.Duration
		limits model.RequestLimits
	}{
		{"minute", time.Minute, model.RequestLimits{PerMinute: 2}},
		{"hour", time.Hour, model.RequestLimits{PerHour: 2}},
		{"day", 24 * time.Hour, model.RequestLimits{PerDay: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := requestLimitClock(t, ctx, db)
			older, newer := now.Add(-10*time.Second), now.Add(-5*time.Second)
			seedProviderRequest(t, ctx, db, test.name, older)
			seedProviderRequest(t, ctx, db, test.name, newer)
			want := older.Add(test.window)
			requireProviderRequestDeadline(t, ctx, db, test.name, test.limits, want)
			requireProviderRequestDeadline(t, ctx, db, test.name, test.limits, want)

			// Expire only the older admission without sleeping. Denied attempts
			// must not consume budget or prevent the next real reservation.
			if _, err := db.pool.Exec(ctx, `UPDATE ingest.provider_requests SET admitted_at=$3
WHERE provider_id=$1 AND admitted_at=$2`, test.name, older, now.Add(-test.window-time.Second)); err != nil {
				t.Fatal(err)
			}
			requireProviderRequestDeadline(t, ctx, db, test.name, test.limits, time.Time{})
			requireProviderRequestDeadline(t, ctx, db, test.name, test.limits, newer.Add(test.window))
		})
	}
}

func TestProviderRequestLimitsUseNthAdmissionAndLatestWindow(t *testing.T) {
	ctx, _, db := requestLimitDatabase(t)
	now := requestLimitClock(t, ctx, db)
	for _, age := range []time.Duration{2 * time.Hour, 50 * time.Minute, 30 * time.Minute, 30 * time.Second, 10 * time.Second} {
		seedProviderRequest(t, ctx, db, "source", now.Add(-age))
	}
	for _, test := range []struct {
		name   string
		limits model.RequestLimits
		wait   time.Duration
	}{
		{"minute", model.RequestLimits{PerMinute: 1, PerHour: 5, PerDay: 6}, 50 * time.Second},
		{"hour", model.RequestLimits{PerMinute: 1, PerHour: 4, PerDay: 6}, 10 * time.Minute},
		{"day", model.RequestLimits{PerMinute: 1, PerHour: 4, PerDay: 5}, 22 * time.Hour},
		// Lowering a limit cannot use the oldest admission's expiry: the
		// second most recent of five admissions determines this deadline.
		{"lowered-day", model.RequestLimits{PerDay: 2}, 24*time.Hour - 30*time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireProviderRequestDeadline(t, ctx, db, "source", test.limits, now.Add(test.wait))
		})
	}
}

func TestProviderRequestLimitsRetainDayAcrossConfigurationChanges(t *testing.T) {
	ctx, _, db := requestLimitDatabase(t)
	now := requestLimitClock(t, ctx, db)
	seedProviderRequest(t, ctx, db, "source", now.Add(-25*time.Hour))
	seedProviderRequest(t, ctx, db, "source", now.Add(-2*time.Hour))
	// A minute-only configuration must retain the two-hour-old admission.
	requireProviderRequestDeadline(t, ctx, db, "source", model.RequestLimits{PerMinute: 1}, time.Time{})
	requireProviderRequestDeadline(t, ctx, db, "source", model.RequestLimits{PerDay: 2}, now.Add(22*time.Hour))
	// The admission older than 24 hours cannot block a newly enabled window.
	requireProviderRequestDeadline(t, ctx, db, "source", model.RequestLimits{PerDay: 3}, time.Time{})
}

func TestProviderRequestLimitsConcurrentAdmissionCap(t *testing.T) {
	ctx, url, db := requestLimitDatabase(t)
	second, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	for _, test := range []struct {
		name   string
		limits model.RequestLimits
	}{
		{"minute", model.RequestLimits{PerMinute: 3}},
		{"hour", model.RequestLimits{PerHour: 3}},
		{"day", model.RequestLimits{PerDay: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			type result struct {
				deadline time.Time
				err      error
			}
			start := make(chan struct{})
			results := make(chan result, 16)
			var group sync.WaitGroup
			for index := range 16 {
				instance := db
				if index%2 != 0 {
					instance = second
				}
				group.Go(func() {
					<-start
					deadline, err := instance.ReserveProviderRequest(ctx, test.name, test.limits)
					results <- result{deadline, err}
				})
			}
			close(start)
			group.Wait()
			close(results)
			admitted := 0
			for result := range results {
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.deadline.IsZero() {
					admitted++
				}
			}
			if admitted != 3 {
				t.Fatalf("concurrent reservations admitted %d requests; want 3", admitted)
			}
		})
	}
}

func TestProviderRequestLimitsSourceIsolationAndReopen(t *testing.T) {
	ctx, url, db := requestLimitDatabase(t)
	limits := model.RequestLimits{PerDay: 1}
	requireProviderRequestDeadline(t, ctx, db, "first", limits, time.Time{})
	requireProviderRequestDeadline(t, ctx, db, "second", limits, time.Time{})
	deadline, err := db.ReserveProviderRequest(ctx, "first", limits)
	if err != nil || deadline.IsZero() {
		t.Fatalf("second request was not denied: deadline=%s err=%v", deadline, err)
	}
	db.Close()
	reopened, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	requireProviderRequestDeadline(t, ctx, reopened, "first", limits, deadline)
	requireProviderRequestDeadline(t, ctx, reopened, "third", limits, time.Time{})
}
