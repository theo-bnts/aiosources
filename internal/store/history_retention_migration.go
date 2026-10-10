package store

// Keep recovery and publication evidence independently of diagnostic archives.
// Existing migrations are immutable; only this upgrade retires backup tables.
const catalogRetentionStateSchema = `
CREATE TABLE ingest.catalog_retention (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 floor BIGINT NOT NULL DEFAULT 0 CHECK(floor>=0)
);
INSERT INTO ingest.catalog_retention DEFAULT VALUES;
CREATE TABLE ingest.catalog_baselines (
 provider_id TEXT NOT NULL,source_id TEXT NOT NULL,origin JSONB NOT NULL CHECK(jsonb_typeof(origin)='object'),
 fields JSONB NOT NULL CHECK(jsonb_typeof(fields)='object'),deleted BOOLEAN NOT NULL,
 sequence BIGINT NOT NULL,recorded_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(provider_id,source_id,origin)
);
`

const historyRetentionSchema = `
DROP TABLE ingest.backup_jobs;
DROP TABLE ingest.backup_settings;

CREATE TABLE ingest.collection_state (
 provider_id TEXT PRIMARY KEY,
 initialized_at TIMESTAMPTZ NOT NULL
);
INSERT INTO ingest.collection_state(provider_id,initialized_at)
SELECT provider_id,min(finished_at) FROM ingest.runs
WHERE status='succeeded' AND mode IN ('full','incremental')
 AND NOT COALESCE(config->>'adapter'='http_json' AND config->'http'->>'catalog'='true',FALSE)
GROUP BY provider_id;

ALTER TABLE ingest.runs ADD COLUMN rejected_primary BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest.runs ADD COLUMN coverage_attempt TEXT NOT NULL DEFAULT '';
CREATE INDEX raw_records_rejected_primary ON ingest.raw_records(run_id,page_id)
 WHERE error<>'' AND NOT auxiliary;
UPDATE ingest.runs r SET rejected_primary=TRUE WHERE errors>0 AND EXISTS (
 SELECT 1 FROM ingest.raw_records raw JOIN ingest.pages page ON page.id=raw.page_id
 WHERE raw.run_id=r.id AND NOT raw.auxiliary AND raw.error<>'' AND page.error='');
UPDATE ingest.runs r SET coverage_attempt=COALESCE((SELECT id::text FROM ingest.events
 WHERE run_id=r.id AND kind='resumed' AND data->>'from_status'='failed' ORDER BY id DESC LIMIT 1),'');

ALTER TABLE ingest.torrents ADD COLUMN collection_verified_at TIMESTAMPTZ;
WITH verified AS (
 SELECT r.provider_id,accepted.source_id,min(r.finished_at) AS verified_at
 FROM ingest.runs r JOIN ingest.run_record_identities accepted ON accepted.run_id=r.id
 JOIN ingest.torrents t ON t.provider_id=r.provider_id AND t.source_id=accepted.source_id
 WHERE r.status='succeeded' AND r.mode IN ('full','incremental')
  AND r.finished_at>=t.first_seen_at AND (r.mode='full' OR NOT r.rejected_primary)
 GROUP BY r.provider_id,accepted.source_id
)
UPDATE ingest.torrents t SET collection_verified_at=v.verified_at FROM verified v
 WHERE t.provider_id=v.provider_id AND t.source_id=v.source_id;

-- Only this compact identity evidence participates in option-filter coverage.
-- It remains usable when the corresponding processed records have expired.
CREATE TABLE ingest.run_filter_records (
 page_id BIGINT NOT NULL REFERENCES ingest.pages(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL,
 PRIMARY KEY(page_id,source_id)
);
INSERT INTO ingest.run_filter_records(page_id,source_id)
SELECT DISTINCT raw.page_id,raw.source_id FROM ingest.raw_records raw
 JOIN ingest.pages page ON page.id=raw.page_id
 WHERE page.error='' AND page.metadata->>'traversal_phase'='options_list'
  AND raw.error='' AND NOT raw.ignored AND NOT raw.auxiliary;

ALTER TABLE ingest.torrents DROP CONSTRAINT torrents_raw_id_fkey;
ALTER TABLE ingest.torrents ADD FOREIGN KEY(raw_id) REFERENCES ingest.raw_records(id) ON DELETE SET NULL;
ALTER TABLE ingest.staged_torrents DROP CONSTRAINT staged_torrents_raw_id_fkey;
ALTER TABLE ingest.staged_torrents ALTER COLUMN raw_id DROP NOT NULL;
ALTER TABLE ingest.staged_torrents ADD FOREIGN KEY(raw_id) REFERENCES ingest.raw_records(id) ON DELETE SET NULL;
ALTER TABLE ingest.activity_logs DROP CONSTRAINT activity_logs_source_event_id_fkey;
ALTER TABLE ingest.activity_logs ADD FOREIGN KEY(source_event_id) REFERENCES ingest.events(id) ON DELETE SET NULL;
CREATE INDEX torrents_raw_record ON ingest.torrents(raw_id) WHERE raw_id IS NOT NULL;
CREATE INDEX staged_torrents_raw_record ON ingest.staged_torrents(raw_id) WHERE raw_id IS NOT NULL;

-- Old journal rows had no clock. Treat them as pre-retention history, retaining
-- one authoritative state per copy before expiring any older checkpoint.
ALTER TABLE ingest.catalog_journal ADD COLUMN recorded_at TIMESTAMPTZ NOT NULL DEFAULT '-infinity';
ALTER TABLE ingest.catalog_journal ALTER COLUMN recorded_at SET DEFAULT statement_timestamp();
` + catalogRetentionStateSchema + `
CREATE INDEX catalog_journal_retention ON ingest.catalog_journal(recorded_at,sequence);
CREATE INDEX torrent_history_retention ON ingest.torrent_history(occurred_at,id);
CREATE INDEX raw_records_retention ON ingest.raw_records(created_at,id);
CREATE INDEX raw_records_page ON ingest.raw_records(page_id);
CREATE INDEX pages_retention ON ingest.pages(created_at,id);
CREATE INDEX events_retention ON ingest.events(created_at,id);
CREATE INDEX operation_runs_retention ON ingest.operation_runs(finished_at,id) WHERE status<>'running';
CREATE INDEX operation_events_retention ON ingest.operation_events(created_at,id);
CREATE INDEX runs_retention ON ingest.runs(finished_at,id) WHERE status IN ('succeeded','failed','cancelled');

CREATE TRIGGER catalog_baselines_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON ingest.catalog_baselines
 FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_journal_immutable();

CREATE OR REPLACE FUNCTION ingest.catalog_journal_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP IN ('DELETE','UPDATE') AND current_setting('ingest.history_retention',TRUE)='on' THEN RETURN NULL; END IF;
 RAISE EXCEPTION 'Published catalogue history is immutable';
END;
$$;
ALTER FUNCTION ingest.activity_message(TEXT) RENAME TO pre_retention_activity_message;
CREATE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'history.retention_failed' THEN 'History cleanup could not finish; it will be retried automatically'
 WHEN 'history.retention_recovered' THEN 'History cleanup recovered'
 ELSE ingest.pre_retention_activity_message(kind) END;
$$;
`
