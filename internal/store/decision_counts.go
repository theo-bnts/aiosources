package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	decisionCountsCacheThreshold = 100 * time.Millisecond
	decisionCountsCacheMinRows   = 50_000
	decisionCountsCacheTTL       = 30 * time.Second

	decisionCountsQuery = `WITH decisions AS (
 SELECT t.provider_id,` + aioDiagnosticStatus + ` AS result` + aioDiagnosticSource + `
)
SELECT provider_id,
 count(*)::bigint AS total,
 count(*) FILTER(WHERE result='matched')::bigint AS matched,
 count(*) FILTER(WHERE result='ambiguous')::bigint AS ambiguous,
 count(*) FILTER(WHERE result='unmatched')::bigint AS unmatched,
 count(*) FILTER(WHERE result='pending')::bigint AS pending
FROM decisions
GROUP BY provider_id`

	aioStatusQuery = decisionCountsQuery
)

type decisionCountRow struct {
	total     int64
	matched   int64
	ambiguous int64
	unmatched int64
	pending   int64
}

type decisionCountsSnapshot struct {
	byProvider      map[string]decisionCountRow
	totals          decisionCountRow
	computedAt      time.Time
	computeDuration time.Duration
}

type decisionCountFlight struct {
	done chan struct{}
	snap decisionCountsSnapshot
	err  error
}

func (s *Store) invalidateDecisionCounts() {
	s.decisionMu.Lock()
	s.decisionGen++
	s.decisionCache = nil
	s.decisionFlight = nil
	s.decisionMu.Unlock()
}

func (snap decisionCountsSnapshot) cacheable() bool {
	return snap.totals.total >= decisionCountsCacheMinRows || snap.computeDuration >= decisionCountsCacheThreshold
}

// decisionCounts returns per-provider and global identification decision counts
// shared by SourceTorrentStats and AIOStatus. Concurrent callers coalesce onto
// a single serial (non-/dev/shm) database scan, and large or slow catalog scans
// are served stale-while-revalidate for 30s to keep rapid admin tab switching
// instantaneous and stable.
func (s *Store) decisionCounts(ctx context.Context) (decisionCountsSnapshot, error) {
	s.decisionMu.Lock()
	cached := s.decisionCache
	s.decisionMu.Unlock()
	if cached != nil && cached.cacheable() {
		if time.Since(cached.computedAt) > decisionCountsCacheTTL {
			s.refreshDecisionCountsAsync()
		}
		return *cached, nil
	}
	return s.refreshDecisionCounts(ctx)
}

// WarmDecisionCounts populates the shared decision-count cache in the
// background whenever identified matches exist.
func (s *Store) WarmDecisionCounts(ctx context.Context) error {
	var hasMatches bool
	if err := s.workerPool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.aio_matches WHERE status='matched' AND work_id IS NOT NULL)`).Scan(&hasMatches); err != nil {
		return databaseError("check decision count matches", err)
	}
	if !hasMatches {
		return nil
	}
	s.decisionMu.Lock()
	cached := s.decisionCache
	s.decisionMu.Unlock()
	if cached != nil && cached.cacheable() && time.Since(cached.computedAt) <= decisionCountsCacheTTL {
		return nil
	}
	_, err := s.refreshDecisionCounts(ctx)
	return err
}

func (s *Store) refreshDecisionCounts(ctx context.Context) (decisionCountsSnapshot, error) {
	flight, err := s.startDecisionCountsRefresh()
	if err != nil {
		return decisionCountsSnapshot{}, err
	}
	select {
	case <-flight.done:
		return flight.snap, flight.err
	case <-ctx.Done():
		return decisionCountsSnapshot{}, ctx.Err()
	}
}

func (s *Store) refreshDecisionCountsAsync() {
	_, _ = s.startDecisionCountsRefresh()
}

func (s *Store) startDecisionCountsRefresh() (*decisionCountFlight, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, context.Canceled
	}
	s.decisionMu.Lock()
	if s.decisionFlight != nil {
		existing := s.decisionFlight
		s.decisionMu.Unlock()
		s.mu.Unlock()
		return existing, nil
	}
	flight := &decisionCountFlight{done: make(chan struct{})}
	gen := s.decisionGen
	s.decisionFlight = flight
	s.listeners.Add(1)
	s.decisionMu.Unlock()
	s.mu.Unlock()

	go func() {
		defer s.listeners.Done()
		defer func() {
			s.decisionMu.Lock()
			if s.decisionFlight == flight {
				s.decisionFlight = nil
			}
			s.decisionMu.Unlock()
			close(flight.done)
		}()
		parent := s.background
		if parent == nil {
			parent = context.Background()
		}
		refreshCtx, cancel := context.WithTimeout(parent, 3*time.Minute)
		defer cancel()
		snap, err := s.computeDecisionCounts(refreshCtx)
		flight.snap = snap
		flight.err = err
		if err == nil {
			s.decisionMu.Lock()
			if s.decisionGen == gen {
				s.decisionCache = &snap
			}
			s.decisionMu.Unlock()
		}
	}()
	return flight, nil
}

func (s *Store) computeDecisionCounts(ctx context.Context) (decisionCountsSnapshot, error) {
	started := time.Now()
	tx, err := s.workerPool().BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return decisionCountsSnapshot{}, databaseError("begin decision counts", err)
	}
	defer rollback(tx)
	// Avoid parallel gather hash tables exhausting small container /dev/shm.
	if _, err := tx.Exec(ctx, `SET LOCAL max_parallel_workers_per_gather = 0`); err != nil {
		return decisionCountsSnapshot{}, databaseError("configure decision counts parallelism", err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '180s'`); err != nil {
		return decisionCountsSnapshot{}, databaseError("configure decision counts timeout", err)
	}

	rows, err := tx.Query(ctx, decisionCountsQuery)
	if err != nil {
		return decisionCountsSnapshot{}, databaseError("count identification decisions", err)
	}
	defer rows.Close()

	snap := decisionCountsSnapshot{
		byProvider: map[string]decisionCountRow{},
	}
	for rows.Next() {
		var providerID string
		var row decisionCountRow
		if err := rows.Scan(&providerID, &row.total, &row.matched, &row.ambiguous, &row.unmatched, &row.pending); err != nil {
			return decisionCountsSnapshot{}, databaseError("read identification decision counts", err)
		}
		snap.byProvider[providerID] = row
		snap.totals.total += row.total
		snap.totals.matched += row.matched
		snap.totals.ambiguous += row.ambiguous
		snap.totals.unmatched += row.unmatched
		snap.totals.pending += row.pending
	}
	if err := rows.Err(); err != nil {
		return decisionCountsSnapshot{}, databaseError("count identification decisions", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return decisionCountsSnapshot{}, databaseError("finish decision counts", err)
	}
	snap.computedAt = time.Now()
	snap.computeDuration = time.Since(started)
	return snap, nil
}
