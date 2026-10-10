package store

import (
	"context"

	"github.com/theo-bnts/aiosources/internal/model"
)

// RecordInformationActivity records each attempted update independently from
// incident transitions. Repeated failures remain diagnosable without creating
// duplicate incident transitions. All persisted text is derived from fixed categories.
func (s *Store) RecordInformationActivity(ctx context.Context, d model.ServiceDiagnostic, deferred int) error {
	if !d.Valid() || deferred < 0 {
		return model.ErrInvalid
	}
	level, kind := "info", "tmdb.sync_completed"
	message := d.Message()
	if d.FailureCode == "interrupted" {
		kind = "tmdb.sync_interrupted"
	} else if d.FailureCode != "" {
		level, kind = "error", "tmdb.sync_failed"
	}
	if deferred > 0 {
		level, kind = "warn", "tmdb.records_deferred"
		message = d.DeferredMessage(deferred)
	}
	id, err := newRunID()
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message,service,data) VALUES($1,$2,$3,$4,'tmdb',jsonb_build_object('operation',$5::text,'failure_code',$6::text,'status_code',$7::int,'deferred_records',$8::int))`, "activity-"+id, level, kind, message, d.Operation, d.FailureCode, d.StatusCode, deferred)
	return databaseError("record information update activity", err)
}
