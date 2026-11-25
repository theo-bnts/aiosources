package store

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDiagnosticsFastPathUsesPartialIndexAndMatchesSemantics(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "777", Titles: []string{"Indexed Film"}}, []string{"indexed film"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `
INSERT INTO aiosources.torrents(provider_id,source_id,fields)
SELECT CASE WHEN n%2=0 THEN 'alpha' ELSE 'beta' END, 'm-'||lpad(n::text,4,'0'), jsonb_build_object('title','Matched Release '||n)
FROM generate_series(1,2000) n;
INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES
 ('alpha','u-1','{"title":"Unmatched Alpha 1"}'),
 ('alpha','u-2','{"title":"Unmatched Alpha 2"}'),
 ('beta','u-1','{"title":"Unmatched Beta 1"}'),
 ('alpha','a-1','{"title":"Ambiguous Alpha 1"}'),
 ('beta','a-1','{"title":"Ambiguous Beta 1"}'),
 ('alpha','u-stale','{"title":"Stale Fingerprint"}'),
 ('alpha','u-queued','{"title":"Queued Record"}');
INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,
 CASE WHEN source_id='u-stale' THEN 'outdated' ELSE fields_fingerprint END,
 CASE WHEN source_id LIKE 'm-%' THEN (SELECT id FROM aiosources.aio_works LIMIT 1) ELSE NULL END,
 CASE WHEN source_id LIKE 'm-%' THEN 1 ELSE 0 END,
 CASE
  WHEN source_id LIKE 'm-%' THEN 'matched'
  WHEN source_id LIKE 'a-%' THEN 'ambiguous'
  ELSE 'unmatched'
 END,
 'fixture reason'
FROM aiosources.torrents;
DELETE FROM aiosources.aio_match_queue WHERE source_id<>'u-queued';
ANALYZE aiosources.torrents; ANALYZE aiosources.aio_matches; ANALYZE aiosources.aio_match_queue;`); err != nil {
		t.Fatal(err)
	}

	unmatched, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: "unmatched", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if unmatched.Total != 3 || len(unmatched.Items) != 3 {
		t.Fatalf("expected 3 current unmatched rows (excluding stale/queued), got total=%d items=%+v", unmatched.Total, unmatched.Items)
	}
	wantKeys := []string{"alpha/u-1", "alpha/u-2", "beta/u-1"}
	for i, item := range unmatched.Items {
		if got := item.ProviderID + "/" + item.SourceID; got != wantKeys[i] {
			t.Fatalf("unmatched order[%d] = %s, want %s", i, got, wantKeys[i])
		}
		if item.Status != "unmatched" || item.Reason != "fixture reason" || item.IMDb != "" || item.UpdatedAt == nil {
			t.Fatalf("unexpected unmatched item: %+v", item)
		}
	}

	alphaUnmatched, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: "unmatched", ProviderID: "alpha", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if alphaUnmatched.Total != 2 || len(alphaUnmatched.Items) != 1 || alphaUnmatched.Items[0].SourceID != "u-2" {
		t.Fatalf("provider-filtered unmatched page: %+v", alphaUnmatched)
	}

	multiProvider, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: "ambiguous", ProviderID: "alpha,beta", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if multiProvider.Total != 2 || len(multiProvider.Items) != 2 {
		t.Fatalf("multi-provider ambiguous: %+v", multiProvider)
	}

	queryFiltered, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: "unmatched", Query: "Beta", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if queryFiltered.Total != 1 || len(queryFiltered.Items) != 1 || queryFiltered.Items[0].ProviderID != "beta" {
		t.Fatalf("query fallback unmatched: %+v", queryFiltered)
	}

	// Verify PostgreSQL chooses the partial covering index aio_matches_diagnostic_status
	// and never touches aio_works on the direct fast path.
	var planJSON []byte
	if err := db.pool.QueryRow(ctx, `EXPLAIN (FORMAT JSON)
SELECT t.occurrence_id,m.provider_id,m.source_id,COALESCE(t.fields->>'title',''),m.status,COALESCE(m.reason,''),'',m.updated_at
FROM aiosources.aio_matches m
JOIN aiosources.torrents t USING(provider_id,source_id)
LEFT JOIN aiosources.aio_match_queue q USING(provider_id,source_id)
WHERE m.status='unmatched' AND COALESCE(m.fingerprint=t.fields_fingerprint,false) AND q.provider_id IS NULL
ORDER BY m.provider_id,m.source_id LIMIT 50 OFFSET 0`).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	planText := string(planJSON)
	if !strings.Contains(planText, "aio_matches_diagnostic_status") {
		t.Fatalf("expected direct diagnostics query to use aio_matches_diagnostic_status, plan:\n%s", planText)
	}
	if strings.Contains(planText, `"aio_works"`) {
		t.Fatalf("direct diagnostics query must not scan aio_works, plan:\n%s", planText)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(planJSON, &parsed); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionCountsSingleflightSWRAndInvalidation(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "888", Titles: []string{"Shared Stats Film"}}, []string{"shared stats film"}); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "alpha", "m1", `{"title":"Shared Stats Film"}`, nil)
	publishSharingRow(t, ctx, db, "alpha", "u1", `{"title":"Unknown Film"}`, nil)
	publishSharingRow(t, ctx, db, "beta", "p1", `{"title":"Pending Film"}`, nil)
	if _, err := db.pool.Exec(ctx, `
INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,fields_fingerprint,
 CASE WHEN source_id='m1' THEN (SELECT id FROM aiosources.aio_works LIMIT 1) END,
 CASE WHEN source_id='m1' THEN 1 ELSE 0 END,
 CASE WHEN source_id='m1' THEN 'matched' ELSE 'unmatched' END,
 'fixture'
FROM aiosources.torrents WHERE provider_id='alpha';
DELETE FROM aiosources.aio_match_queue WHERE provider_id='alpha';`); err != nil {
		t.Fatal(err)
	}

	// Concurrent callers across AIOStatus and SourceTorrentStats coalesce safely under -race.
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 6; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			st, err := db.AIOStatus(ctx)
			if err != nil {
				errs <- err
				return
			}
			if st.Matched != 1 || st.Unmatched != 1 || st.MatchingPending != 1 {
				t.Errorf("concurrent AIOStatus mismatch: %+v", st)
			}
		}()
		go func() {
			defer wg.Done()
			stats, err := db.SourceTorrentStats(ctx, []string{"alpha", "beta"})
			if err != nil {
				errs <- err
				return
			}
			if stats.Totals.Imported != 3 || len(stats.Items) != 2 || stats.Items[0].Matched != 1 || stats.Items[0].Unmatched != 1 || stats.Items[1].Pending != 1 {
				t.Errorf("concurrent SourceTorrentStats mismatch: %+v", stats)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// WarmDecisionCounts succeeds when matches exist.
	if err := db.WarmDecisionCounts(ctx); err != nil {
		t.Fatalf("WarmDecisionCounts: %v", err)
	}

	// Simulate a slow large-catalog snapshot (above decisionCountsCacheThreshold)
	// and verify fresh cache hit, stale-while-revalidate refresh, and immediate invalidation.
	db.decisionMu.Lock()
	db.decisionCache = &decisionCountsSnapshot{
		byProvider: map[string]decisionCountRow{
			"alpha": {total: 99, matched: 90, unmatched: 9},
		},
		totals:          decisionCountRow{total: 99, matched: 90, unmatched: 9},
		computedAt:      time.Now(),
		computeDuration: decisionCountsCacheThreshold + 50*time.Millisecond,
	}
	db.decisionMu.Unlock()

	cachedStatus, err := db.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cachedStatus.Matched != 90 || cachedStatus.Unmatched != 9 {
		t.Fatalf("expected fresh large-catalog cache hit, got %+v", cachedStatus)
	}

	// Expire the snapshot: first call serves stale immediately and kicks off async refresh.
	db.decisionMu.Lock()
	db.decisionCache.computedAt = time.Now().Add(-decisionCountsCacheTTL - time.Second)
	db.decisionMu.Unlock()

	swrStatus, err := db.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if swrStatus.Matched != 90 {
		t.Fatalf("expected stale-while-revalidate response before refresh finishes, got %+v", swrStatus)
	}

	// Wait briefly for the async refresh goroutine to update the cache with real DB counts.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		db.decisionMu.Lock()
		snap := db.decisionCache
		flight := db.decisionFlight
		db.decisionMu.Unlock()
		if flight == nil && snap != nil && snap.totals.matched == 1 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	refreshedStatus, err := db.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedStatus.Matched != 1 || refreshedStatus.Unmatched != 1 || refreshedStatus.MatchingPending != 1 {
		t.Fatalf("expected refreshed live counts after SWR, got %+v", refreshedStatus)
	}

	// Invalidation clears cache and bumps generation so ResetSourceData immediately reflects zero rows.
	db.decisionMu.Lock()
	db.decisionCache = &decisionCountsSnapshot{
		byProvider:      map[string]decisionCountRow{"alpha": {total: 50, matched: 50}},
		totals:          decisionCountRow{total: 50, matched: 50},
		computedAt:      time.Now(),
		computeDuration: decisionCountsCacheThreshold + 50*time.Millisecond,
	}
	genBefore := db.decisionGen
	db.decisionMu.Unlock()

	if err := db.ResetSourceData(ctx, 1); err != nil {
		t.Fatalf("ResetSourceData: %v", err)
	}
	db.decisionMu.Lock()
	genAfter := db.decisionGen
	cacheAfter := db.decisionCache
	db.decisionMu.Unlock()
	if genAfter <= genBefore || cacheAfter != nil {
		t.Fatalf("ResetSourceData did not invalidate decision cache: gen %d->%d cache=%v", genBefore, genAfter, cacheAfter)
	}
	afterReset, err := db.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterReset.Matched != 0 || afterReset.Unmatched != 0 || afterReset.MatchingPending != 0 {
		t.Fatalf("expected zero counts after ResetSourceData, got %+v", afterReset)
	}
}
