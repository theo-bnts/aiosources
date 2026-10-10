package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// ScopeCounts returns only committed observations in this run, never live rows
// from a previous collection or the number of distinct content hashes.
func (s *Store) ScopeCounts(ctx context.Context, runID string) (map[string]int64, error) {
	counts := make(map[string]int64)
	rows, err := s.pool.Query(ctx, "SELECT scope_id,observed_count FROM ingest.run_scope_counts WHERE run_id=$1", runID)
	if err != nil {
		return nil, databaseError("read scope coverage", err)
	}
	defer rows.Close()
	for rows.Next() {
		var scope string
		var count int64
		if err := rows.Scan(&scope, &count); err != nil {
			return nil, databaseError("read scope count", err)
		}
		counts[scope] = count
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read scope coverage", err)
	}
	return counts, nil
}

// ObservedIDs checks a bounded caller-supplied batch against this run's committed
// scope memberships. An identity observed in multiple scopes is returned once.
func (s *Store) ObservedIDs(ctx context.Context, runID string, ids []string) (map[string]bool, error) {
	observed := make(map[string]bool)
	if len(ids) == 0 {
		return observed, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT m.source_id FROM ingest.run_scope_memberships m
WHERE m.run_id=$1 AND m.source_id=ANY($2::text[])
AND NOT EXISTS (SELECT 1 FROM ingest.run_scope_refresh_candidates c WHERE c.run_id=m.run_id AND c.source_id=m.source_id)`, runID, ids)
	if err != nil {
		return nil, databaseError("read observed identities", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, databaseError("read observed identity", err)
		}
		observed[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read observed identities", err)
	}
	return observed, nil
}

// FilterComplete uses only successful archived queries and currently accepted
// memberships. A changing total, duplicate identity, rejected record or retired
// observation cannot turn a truncated option filter into proof of completeness.
func (s *Store) FilterComplete(ctx context.Context, runID, scopeID string, fingerprints []string) (bool, error) {
	if len(fingerprints) == 0 {
		return false, nil
	}
	var complete bool
	err := s.pool.QueryRow(ctx, `WITH selected_pages AS MATERIALIZED (
    SELECT id,CASE WHEN jsonb_typeof(metadata->'total')='number'
        AND metadata->>'total' ~ '^(0|[1-9][0-9]*)$'
        THEN (metadata->>'total')::numeric END AS total
    FROM ingest.pages
    WHERE run_id=$1 AND metadata->>'fingerprint_scope'=ANY($3::text[])
      AND metadata->>'traversal_phase'='options_list' AND error=''
)
SELECT COALESCE(count(*)>0 AND count(total)=count(*) AND min(total)=max(total)
    AND min(total)=(
        SELECT count(DISTINCT r.source_id) FROM selected_pages p
        JOIN ingest.run_filter_records r ON r.page_id=p.id
        JOIN ingest.run_scope_memberships m ON m.run_id=$1 AND m.scope_id=$2 AND m.source_id=r.source_id
        WHERE NOT EXISTS (SELECT 1 FROM ingest.run_scope_refresh_candidates c WHERE c.run_id=$1 AND c.source_id=r.source_id)
    ),false)
FROM selected_pages`, runID, scopeID, fingerprints).Scan(&complete)
	return complete, databaseError("read filter coverage", err)
}

// NextRefreshID selects only invalidated canonical numeric IDs in the scanned
// prefix. The caller checkpoints its position, including holes and aliases,
// independently of the forward scan; no unbounded candidate list is retained.
func (s *Store) NextRefreshID(ctx context.Context, runID string, after, through int64) (int64, error) {
	if through <= after {
		return 0, nil
	}
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(min(id),0)::bigint FROM (
SELECT CASE WHEN source_id ~ '^[1-9][0-9]{0,18}$' THEN source_id::numeric END AS id
FROM ingest.run_scope_refresh_candidates WHERE run_id=$1
) candidates WHERE id>$2::numeric AND id<=$3::numeric`, runID, after, through).Scan(&id)
	if err != nil {
		return 0, databaseError("read scope refresh identity", err)
	}
	return id, nil
}

// refreshCoverage retires only derived evidence. Candidate identities prevent
// missing-ID recovery from skipping an older observation in an overlapping,
// unaffected scope. A later valid observation clears its candidate marker.
func refreshCoverage(ctx context.Context, tx pgx.Tx, runID string, scopes []string, reset bool) error {
	if reset {
		for _, table := range []string{"run_scope_memberships", "run_scope_counts", "run_scope_refresh_candidates"} {
			if _, err := tx.Exec(ctx, "DELETE FROM ingest."+table+" WHERE run_id=$1", runID); err != nil {
				return databaseError("reset scope coverage", err)
			}
		}
		return nil
	}
	if len(scopes) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ingest.run_scope_refresh_candidates(run_id,source_id)
SELECT DISTINCT run_id,source_id FROM ingest.run_scope_memberships WHERE run_id=$1 AND scope_id=ANY($2::text[])
ON CONFLICT DO NOTHING`, runID, scopes); err != nil {
		return databaseError("retain scope refresh candidates", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM ingest.run_scope_memberships WHERE run_id=$1 AND scope_id=ANY($2::text[])", runID, scopes); err != nil {
		return databaseError("refresh scope memberships", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM ingest.run_scope_counts WHERE run_id=$1 AND scope_id=ANY($2::text[])", runID, scopes); err != nil {
		return databaseError("refresh scope counts", err)
	}
	// Keep a simultaneous match in another scope, but never publish a row
	// supported only by the retired generation. Original raw rows are untouched.
	for _, table := range []string{"staged_torrents", "run_record_identities"} {
		if _, err := tx.Exec(ctx, `DELETE FROM ingest.`+table+` r
WHERE r.run_id=$1 AND EXISTS (SELECT 1 FROM ingest.run_scope_refresh_candidates c WHERE c.run_id=r.run_id AND c.source_id=r.source_id)
AND NOT EXISTS (SELECT 1 FROM ingest.run_scope_memberships m WHERE m.run_id=r.run_id AND m.source_id=r.source_id)`, runID); err != nil {
			return databaseError("retire scoped derived identities", err)
		}
	}
	return nil
}

func saveCoverage(ctx context.Context, tx pgx.Tx, runID string, latest map[string][]string) error {
	if len(latest) == 0 {
		return nil
	}
	identities := make([]string, 0, len(latest))
	var scopes, ids []string
	for id, matches := range latest {
		identities = append(identities, id)
		for _, scope := range matches {
			scopes, ids = append(scopes, scope), append(ids, id)
		}
	}
	// Replace all memberships for each source identity with its last valid
	// observation on this page. Content hashes never participate in identity.
	if _, err := tx.Exec(ctx, `WITH removed AS (
DELETE FROM ingest.run_scope_memberships WHERE run_id=$1 AND source_id=ANY($2::text[]) RETURNING scope_id
), deltas AS (SELECT scope_id,count(*) AS n FROM removed GROUP BY scope_id)
UPDATE ingest.run_scope_counts c SET observed_count=c.observed_count-d.n
FROM deltas d WHERE c.run_id=$1 AND c.scope_id=d.scope_id`, runID, identities); err != nil {
		return databaseError("replace latest scope memberships", err)
	}
	if _, err := tx.Exec(ctx, `WITH inserted AS (
INSERT INTO ingest.run_scope_memberships(run_id,scope_id,source_id)
SELECT DISTINCT $1,scope_id,source_id FROM unnest($2::text[],$3::text[]) AS observed(scope_id,source_id)
RETURNING scope_id
)
INSERT INTO ingest.run_scope_counts(run_id,scope_id,observed_count)
SELECT $1,scope_id,count(*) FROM inserted GROUP BY scope_id
ON CONFLICT (run_id,scope_id) DO UPDATE SET
observed_count=ingest.run_scope_counts.observed_count+EXCLUDED.observed_count`, runID, scopes, ids); err != nil {
		return databaseError("retain latest scope coverage", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM ingest.run_scope_counts WHERE run_id=$1 AND observed_count=0", runID); err != nil {
		return databaseError("remove empty scope counts", err)
	}
	_, err := tx.Exec(ctx, "DELETE FROM ingest.run_scope_refresh_candidates WHERE run_id=$1 AND source_id=ANY($2::text[])", runID, identities)
	return databaseError("complete refreshed observations", err)
}
