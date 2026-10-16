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
const aioDiagnosticJoins = ` LEFT JOIN ingest.aio_matches m USING(provider_id,source_id)
LEFT JOIN ingest.aio_match_queue q USING(provider_id,source_id)
LEFT JOIN ingest.aio_works w ON w.id=m.work_id`
const aioDiagnosticSource = ` FROM ingest.torrents t` + aioDiagnosticJoins

func (s *Store) ListAIOMatchDiagnostics(ctx context.Context, opts model.ListOptions) (model.List[model.AIOMatchDiagnostic], error) {
	status := opts.Status
	opts, err := torrentSearchOptions(opts)
	opts.Status = status
	result := model.List[model.AIOMatchDiagnostic]{Items: []model.AIOMatchDiagnostic{}, Limit: opts.Limit, Offset: opts.Offset}
	if err != nil {
		return result, err
	}
	if opts.Status != "" && opts.Status != "pending" && opts.Status != "matched" && opts.Status != "ambiguous" && opts.Status != "unmatched" {
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
	filter := fmt.Sprintf(` WHERE status=$%d`, len(args)+1)
	countSQL := projection + ` SELECT count(*) FROM diagnostics` + filter
	countArgs := append(append([]any{}, args...), opts.Status)
	listSQL := projection + ` SELECT occurrence_id,provider_id,source_id,title,status,
CASE WHEN status='pending' THEN 'awaiting identification' ELSE COALESCE(reason,'') END,
CASE WHEN status='matched' THEN COALESCE(imdb_id,'') ELSE '' END,
CASE WHEN status='pending' THEN NULL ELSE updated_at END FROM diagnostics` + filter + `
ORDER BY provider_id,source_id` + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(countArgs)+1, len(countArgs)+2)
	listArgs := append(append([]any{}, countArgs...), opts.Limit, opts.Offset)
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
