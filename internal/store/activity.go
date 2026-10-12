package store

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

var activityIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func validateActivityPage(options model.ListOptions) error {
	if options.Limit < 1 || options.Limit > 200 || options.Offset < 0 || options.Offset > 1000000 || len(options.Query) > 500 || len(options.ProviderID) > 128 || len(options.RunID) > 128 {
		return model.ErrInvalid
	}
	return nil
}

func (s *Store) Logs(ctx context.Context, options model.LogOptions) (model.List[model.LogEntry], error) {
	result := model.List[model.LogEntry]{Items: []model.LogEntry{}, Limit: options.Limit, Offset: options.Offset}
	if err := validateActivityPage(options.ListOptions); err != nil {
		return result, err
	}
	if options.Service != "" && options.Service != "tmdb" && options.Service != "alldebrid" && options.Service != "torbox" {
		return result, model.ErrInvalid
	}
	for _, level := range options.Levels {
		if !model.IsActivityLevel(level) {
			return result, model.ErrInvalid
		}
	}
	if options.From != nil && options.To != nil && options.From.After(*options.To) {
		return result, model.ErrInvalid
	}
	levels := options.Levels
	if len(levels) == 0 {
		levels = []string{"debug", "info", "warn", "error"}
	}
	args := []any{levels, options.ProviderID, options.RunID, options.Query, options.From, options.To, options.Service}
	where := ` WHERE level=ANY($1::text[]) AND ($2='' OR provider_id=$2) AND ($3='' OR run_id=$3)
 AND ($4='' OR strpos(lower(message||' '||title||' '||kind||' '||provider_id||' '||run_id||' '||service),lower($4))>0)
 AND ($5::timestamptz IS NULL OR created_at >= $5) AND ($6::timestamptz IS NULL OR created_at <= $6)
 AND ($7='' OR service=$7)`
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin activity query", err)
	}
	defer rollback(tx)
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM ingest.activity_logs"+where, args...).Scan(&result.Total); err != nil {
		return result, databaseError("count activity logs", err)
	}
	args = append(args, options.Limit, options.Offset)
	rows, err := tx.Query(ctx, "SELECT id,level,kind,message,provider_id,run_id,created_at,data,service,title FROM ingest.activity_logs"+where+" ORDER BY created_at DESC,id DESC LIMIT $8 OFFSET $9", args...)
	if err != nil {
		return result, databaseError("list activity logs", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entry model.LogEntry
		var data []byte
		if err := rows.Scan(&entry.ID, &entry.Level, &entry.Kind, &entry.Message, &entry.ProviderID, &entry.RunID, &entry.CreatedAt, &data, &entry.Service, &entry.Title); err != nil {
			return result, databaseError("read activity log", err)
		}
		entry.Data, err = decodeObject(data)
		if err != nil {
			return result, databaseError("decode activity metadata", err)
		}
		result.Items = append(result.Items, entry)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list activity logs", err)
	}
	return result, databaseError("finish activity query", tx.Commit(ctx))
}

// RecordActivity records independent service/scheduler events. Never pass secret
// values, URLs, request headers, source documents or payloads. Messages are
// canonicalized by kind rather than trusting arbitrary error strings; the message
// argument is intentionally not persisted. Metadata uses the same SQL typed
// allowlist as collection events; unknown keys and nonmatching types are dropped.
// Each call creates an event except consecutive schedule.failed for one provider,
// which is deduplicated until another schedule.* activity records recovery. Calls
// are serialized per provider so multiple schedulers cannot create alert storms.
func (s *Store) RecordActivity(ctx context.Context, level, kind, message, providerID, runID string, data map[string]any) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin service activity", err)
	}
	defer rollback(tx)
	if err := recordActivityTx(ctx, tx, level, kind, message, providerID, runID, data); err != nil {
		return err
	}
	return databaseError("commit service activity", tx.Commit(ctx))
}

func recordActivityTx(ctx context.Context, tx pgx.Tx, level, kind, message, providerID, runID string, data map[string]any) error {
	if !model.IsActivityLevel(level) || (providerID != "" && !activityIdentity.MatchString(providerID)) || (runID != "" && !activityIdentity.MatchString(runID)) {
		return model.ErrInvalid
	}
	switch kind {
	case "service.started", "service.stopped", "service.failed", "scheduler.started", "scheduler.stopped", "scheduler.failed", "schedule.failed", "schedule.queued", "schedule.skipped",
		"health.warning", "health.recovered", "history.retention_failed", "history.retention_recovered", "storage.sources_reset":
	case "security.mfa_enabled", "security.mfa_disabled", "security.recovery_rotated", "security.session_revoked", "security.sessions_revoked", "security.public_access_enabled", "security.public_access_disabled", "share.created", "share.permissions_changed", "share.rotated", "share.revoked":
	default:
		return model.ErrInvalid
	}
	encoded, err := jsonObject(data)
	if err != nil {
		return err
	}
	id, err := newRunID()
	if err != nil {
		return err
	}
	if strings.HasPrefix(kind, "schedule.") {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", providerLockKey("activity:"+providerID)); err != nil {
			return databaseError("lock schedule activity", err)
		}
		if kind == "schedule.failed" {
			var lastKind string
			err := tx.QueryRow(ctx, `SELECT kind FROM ingest.activity_logs WHERE provider_id=$1 AND kind LIKE 'schedule.%' ORDER BY id DESC LIMIT 1`, providerID).Scan(&lastKind)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return databaseError("read previous schedule activity", err)
			}
			if lastKind == kind {
				return nil
			}
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message,provider_id,run_id,data) VALUES($1,$2,$3,ingest.activity_message($3),$4,$5,ingest.safe_activity_data($6))`, "activity-"+id, level, kind, providerID, runID, encoded)
	if err != nil {
		return databaseError("record service activity", err)
	}
	return nil
}
