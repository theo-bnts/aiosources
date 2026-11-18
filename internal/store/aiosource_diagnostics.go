package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// A queued or stale result is never presented as a current identification.
// This predicate follows the stream publication rules, including work revisions.
const aioDiagnosticStatus = `CASE WHEN m.provider_id IS NULL OR q.provider_id IS NOT NULL
 OR m.fingerprint IS DISTINCT FROM t.fields_fingerprint
 OR (m.status='matched' AND (w.id IS NULL OR m.revision IS DISTINCT FROM w.revision))
 THEN 'pending' ELSE m.status END`
const aioDiagnosticJoins = ` LEFT JOIN aiosources.aio_matches m USING(provider_id,source_id)
LEFT JOIN aiosources.aio_match_queue q USING(provider_id,source_id)
LEFT JOIN aiosources.aio_works w ON w.id=m.work_id`
const aioDiagnosticSource = ` FROM aiosources.torrents t` + aioDiagnosticJoins

func (s *Store) ListAIOMatchDiagnostics(ctx context.Context, opts model.ListOptions) (model.List[model.AIOMatchDiagnostic], error) {
	status := opts.Status
	opts, err := torrentSearchOptions(opts)
	opts.Status = status
	result := model.List[model.AIOMatchDiagnostic]{Items: []model.AIOMatchDiagnostic{}, Limit: opts.Limit, Offset: opts.Offset}
	if err != nil {
		return result, err
	}
	if !model.ValidSelection(opts.Status, func(value string) bool {
		return value == "pending" || value == "matched" || value == "ambiguous" || value == "unmatched"
	}) {
		return result, model.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin identification diagnostics", err)
	}
	defer rollback(tx)
	sourceFilter, _, args, _ := torrentSearchSQL(opts)
	selected := `SELECT occurrence_id,provider_id,source_id,fields,fields_fingerprint` + sourceFilter
	projection := `WITH selected AS (` + selected + `), diagnostics AS (SELECT t.occurrence_id,t.provider_id,t.source_id,
COALESCE(t.fields->>'title','') AS title,` + aioDiagnosticStatus + ` AS status,
m.reason, w.imdb_id, m.updated_at FROM selected t` + aioDiagnosticJoins + `)`
	filter := fmt.Sprintf(` WHERE status=ANY(string_to_array($%d,','))`, len(args)+1)
	countSQL := projection + ` SELECT count(*) FROM diagnostics` + filter
	countArgs := append(append([]any{}, args...), opts.Status)
	listSQL := projection + ` SELECT occurrence_id,provider_id,source_id,title,status,
CASE WHEN status='pending' THEN 'awaiting identification' ELSE COALESCE(reason,'') END,
CASE WHEN status='matched' THEN COALESCE(imdb_id,'') ELSE '' END,
CASE WHEN status='pending' THEN NULL ELSE updated_at END FROM diagnostics` + filter + `
ORDER BY provider_id,source_id` + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(countArgs)+1, len(countArgs)+2)
	listArgs := append(append([]any{}, countArgs...), opts.Limit, opts.Offset)
	selectedKeys := `SELECT provider_id,source_id,fields_fingerprint` + sourceFilter
	if opts.Status == "ambiguous" || opts.Status == "unmatched" {
		// Count current ambiguous/unmatched decisions without deriving every other status.
		// Their validity depends on the source fingerprint and queue, not on a
		// work revision. Keep the ordered listing's original plan: restricting
		// its joins here can make PostgreSQL sort all rows before LIMIT.
		current := fmt.Sprintf(` FROM selected t
JOIN aiosources.aio_matches m USING(provider_id,source_id)
LEFT JOIN aiosources.aio_match_queue q USING(provider_id,source_id)
WHERE m.status=$%d AND COALESCE(m.fingerprint=t.fields_fingerprint,false) AND q.provider_id IS NULL`, len(args)+1)
		prefix := `WITH selected AS (` + selectedKeys + `)`
		countSQL = prefix + ` SELECT count(*)` + current
		countArgs = append(append([]any{}, args...), opts.Status)
	}
	if opts.Status == "matched" {
		countSQL = `WITH selected AS (` + selectedKeys + `) SELECT count(*) FROM selected t
JOIN aiosources.aio_matches m USING(provider_id,source_id)
LEFT JOIN aiosources.aio_works w ON w.id=m.work_id
LEFT JOIN aiosources.aio_match_queue q USING(provider_id,source_id)
WHERE m.status='matched' AND m.work_id IS NOT NULL AND q.provider_id IS NULL
AND CASE WHEN w.id IS NULL OR m.fingerprint IS DISTINCT FROM t.fields_fingerprint OR m.revision IS DISTINCT FROM w.revision THEN false ELSE true END`
		countArgs = args
	}
	if opts.Status == "pending" {
		pendingSQL := `WITH selected AS (` + selectedKeys + `), page AS (
SELECT count(*) OVER() AS total,t.provider_id,t.source_id
FROM selected t` + aioDiagnosticJoins + `
WHERE (` + aioDiagnosticStatus + `)='pending'
ORDER BY t.provider_id||'',t.source_id||''` + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2) + `)
SELECT page.total,t.occurrence_id,t.provider_id,t.source_id,COALESCE(t.fields->>'title',''),
'pending','awaiting identification','',NULL::timestamptz
FROM page JOIN aiosources.torrents t USING(provider_id,source_id)
ORDER BY page.provider_id,page.source_id`
		pendingArgs := append(append([]any{}, args...), opts.Limit, opts.Offset)
		rows, err := tx.Query(ctx, pendingSQL, pendingArgs...)
		if err != nil {
			return result, databaseError("list identification diagnostics", err)
		}
		defer rows.Close()
		for rows.Next() {
			var item model.AIOMatchDiagnostic
			if err = rows.Scan(&result.Total, &item.OccurrenceID, &item.ProviderID, &item.SourceID, &item.Title, &item.Status, &item.Reason, &item.IMDb, &item.UpdatedAt); err != nil {
				return result, databaseError("read identification diagnostic", err)
			}
			result.Items = append(result.Items, item)
		}
		if err = rows.Err(); err != nil {
			return result, databaseError("list identification diagnostics", err)
		}
		rows.Close()
		if len(result.Items) == 0 && opts.Offset > 0 {
			countKeysSQL := `WITH selected AS (` + selectedKeys + `) SELECT count(*) FROM selected t` + aioDiagnosticJoins + ` WHERE (` + aioDiagnosticStatus + `)='pending'`
			if err = tx.QueryRow(ctx, countKeysSQL, args...).Scan(&result.Total); err != nil {
				return result, databaseError("count identification diagnostics", err)
			}
		}
		return result, databaseError("finish identification diagnostics", tx.Commit(ctx))
	}
	if opts.Status == "" {
		// Without a decision filter, count only the catalog and join decisions
		// after selecting the visible page. Otherwise even an ordinary list
		// computes fingerprints and joins metadata for the entire catalog twice.
		countSQL = `SELECT count(*)` + sourceFilter
		countArgs = args
		listSQL = `WITH selected AS MATERIALIZED (` + selected + `
ORDER BY provider_id,source_id` + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2) + `), diagnostics AS (
SELECT t.occurrence_id,t.provider_id,t.source_id,COALESCE(t.fields->>'title','') AS title,` + aioDiagnosticStatus + ` AS status,
m.reason,w.imdb_id,m.updated_at FROM selected t` + aioDiagnosticJoins + `)
SELECT occurrence_id,provider_id,source_id,title,status,
CASE WHEN status='pending' THEN 'awaiting identification' ELSE COALESCE(reason,'') END,
CASE WHEN status='matched' THEN COALESCE(imdb_id,'') ELSE '' END,
CASE WHEN status='pending' THEN NULL ELSE updated_at END FROM diagnostics ORDER BY provider_id,source_id`
		listArgs = append(append([]any{}, args...), opts.Limit, opts.Offset)
	}
	if err = tx.QueryRow(ctx, countSQL, countArgs...).Scan(&result.Total); err != nil {
		return result, databaseError("count identification diagnostics", err)
	}
	if result.Total <= int64(opts.Offset) {
		return result, databaseError("finish identification diagnostics", tx.Commit(ctx))
	}
	rows, err := tx.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return result, databaseError("list identification diagnostics", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item model.AIOMatchDiagnostic
		if err = rows.Scan(&item.OccurrenceID, &item.ProviderID, &item.SourceID, &item.Title, &item.Status, &item.Reason, &item.IMDb, &item.UpdatedAt); err != nil {
			return result, databaseError("read identification diagnostic", err)
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, databaseError("list identification diagnostics", err)
	}
	return result, databaseError("finish identification diagnostics", tx.Commit(ctx))
}
