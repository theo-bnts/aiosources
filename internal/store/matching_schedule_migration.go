package store

// The clock is independent of history retention and survives a worker restart.
// Interrupted work remains due; successful, stopped and failed attempts respect
// the administrator's interval before another automatic pass starts.
const matchingScheduleSchema = `
ALTER TABLE ingest.aio_state ADD COLUMN last_match TIMESTAMPTZ;
UPDATE ingest.aio_state SET last_match=(
 SELECT max(finished_at) FROM ingest.operation_runs
 WHERE kind='matching' AND status IN ('succeeded','cancelled','failed')
);
`
