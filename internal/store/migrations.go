package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Migration identifiers use the two-int advisory namespace, separate from the
// one-bigint namespace used by provider workers.
const migrationLockNamespace int32 = 1768843109
const migrationLockID int32 = 1

const initialSchema = `
CREATE TABLE IF NOT EXISTS ingest.runs (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('preview','incremental','full')),
    status TEXT NOT NULL CHECK (status IN ('queued','running','paused','succeeded','failed','cancelled')),
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    max_pages INTEGER NOT NULL DEFAULT 0 CHECK (max_pages >= 0),
    pages INTEGER NOT NULL DEFAULT 0 CHECK (pages >= 0),
    records INTEGER NOT NULL DEFAULT 0 CHECK (records >= 0),
    errors INTEGER NOT NULL DEFAULT 0 CHECK (errors >= 0),
    revision TEXT NOT NULL,
    config JSONB NOT NULL CHECK (jsonb_typeof(config) = 'object'),
    cursor JSONB,
    traversal_done BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS runs_one_active_provider
    ON ingest.runs(provider_id) WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS runs_recent ON ingest.runs(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS runs_provider_recent ON ingest.runs(provider_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS runs_queue ON ingest.runs(created_at, id) WHERE status = 'queued';
CREATE TABLE IF NOT EXISTS ingest.pages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES ingest.runs(id),
    provider_id TEXT NOT NULL,
    page_index INTEGER NOT NULL,
    body BYTEA NOT NULL,
    content_type TEXT NOT NULL,
    fingerprint TEXT,
    position TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    error TEXT NOT NULL DEFAULT '',
    done BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS pages_success_fingerprint
    ON ingest.pages(run_id, fingerprint) WHERE fingerprint IS NOT NULL;
CREATE INDEX IF NOT EXISTS pages_run ON ingest.pages(run_id, id);
CREATE TABLE IF NOT EXISTS ingest.raw_records (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES ingest.runs(id),
    provider_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    page_id BIGINT NOT NULL REFERENCES ingest.pages(id),
    page_index INTEGER NOT NULL,
    raw BYTEA NOT NULL,
    content_type TEXT NOT NULL,
    fields JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(fields) = 'object'),
    error TEXT NOT NULL DEFAULT '',
    ignored BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS raw_records_provider ON ingest.raw_records(provider_id, id DESC);
CREATE INDEX IF NOT EXISTS raw_records_run ON ingest.raw_records(run_id, id DESC);
CREATE INDEX IF NOT EXISTS raw_records_source ON ingest.raw_records(provider_id, source_id);
CREATE TABLE IF NOT EXISTS ingest.torrents (
    provider_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    fields JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(fields) = 'object'),
    raw_id BIGINT REFERENCES ingest.raw_records(id),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    historical BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (provider_id, source_id),
    CHECK (NOT historical OR raw_id IS NULL)
);
CREATE INDEX IF NOT EXISTS torrents_recent ON ingest.torrents(last_seen_at DESC, provider_id, source_id);
CREATE TABLE IF NOT EXISTS ingest.staged_torrents (
    run_id TEXT NOT NULL REFERENCES ingest.runs(id),
    provider_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    fields JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(fields) = 'object'),
    raw_id BIGINT NOT NULL REFERENCES ingest.raw_records(id),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (run_id, source_id)
);
CREATE TABLE IF NOT EXISTS ingest.events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES ingest.runs(id),
    kind TEXT NOT NULL,
    message TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS events_run ON ingest.events(run_id, id);
CREATE TABLE IF NOT EXISTS ingest.secrets (
    name TEXT PRIMARY KEY,
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS ingest.settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ingest.legacy_imports (
    provider_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider_id, source_id)
);
`

const observationSchema = `
ALTER TABLE ingest.raw_records ADD COLUMN IF NOT EXISTS auxiliary BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest.pages ADD COLUMN IF NOT EXISTS reset_staging BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest.pages ADD COLUMN IF NOT EXISTS require_unique_ids BOOLEAN NOT NULL DEFAULT FALSE;
`

const scheduleSchema = `
ALTER TABLE ingest.runs ADD COLUMN trigger TEXT NOT NULL DEFAULT 'manual'
    CHECK (trigger IN ('manual','scheduled'));
CREATE TABLE ingest.schedule_clocks (
    provider_id TEXT PRIMARY KEY,
    revision TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    next_run_at TIMESTAMPTZ,
    last_attempt_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    CHECK (active = (next_run_at IS NOT NULL))
);
CREATE INDEX runs_scheduled_paused ON ingest.runs(provider_id,created_at,id)
    WHERE status='paused' AND trigger='scheduled';
DELETE FROM ingest.settings WHERE key LIKE 'jobs.schedule.initial.%';
`

const knownPageStreakSchema = `
ALTER TABLE ingest.runs ADD COLUMN known_page_streak INTEGER NOT NULL DEFAULT 0 CHECK (known_page_streak>=0);
`

const fullScheduleSchema = `
ALTER TABLE ingest.schedule_clocks ADD COLUMN full_due_at TIMESTAMPTZ;
`

const metadataFollowupSchema = `
ALTER TABLE ingest.runs ADD COLUMN metadata_parent_run_id TEXT REFERENCES ingest.runs(id);
ALTER TABLE ingest.runs ADD CONSTRAINT runs_metadata_parent_mode
    CHECK (metadata_parent_run_id IS NULL OR mode='metadata');
CREATE UNIQUE INDEX runs_one_metadata_followup ON ingest.runs(metadata_parent_run_id);
`

const initialFullSchema = `
ALTER TABLE ingest.runs ADD COLUMN initial_full BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest.runs ADD CONSTRAINT runs_initial_full_mode CHECK (NOT initial_full OR mode='full');
CREATE INDEX runs_successful_collection ON ingest.runs(provider_id)
    WHERE status='succeeded' AND mode IN ('full','incremental');
`

var migrations = []struct {
	version int
	sql     string
}{
	{1, initialSchema}, {2, observationSchema}, {3, scheduleSchema},
	{4, activitySchema}, {5, sharingSchema}, {6, catalogIdentityPrivacySchema},
	{7, operationsEventsSchema}, {8, securitySchema}, {9, shareControlsSchema},
	{10, searchHistorySchema}, {11, healthSchema}, {12, backupSchema},
	{13, coverageSchema}, {14, runRecordsSchema},
	{15, pauseSchema}, {16, coverageLatestSchema},
	{17, filterCoverageSchema},
	{18, collectionSettingsSchema},
	{19, collectionPolicySchema},
	{20, metadataRetentionSchema},
	{21, archiveReferenceSchema},
	{22, knownPageStreakSchema},
	{23, fullScheduleSchema},
	{24, requestLimitsSchema},
	{25, metadataFollowupSchema},
	{26, aiosourceSchema},
	{27, initialFullSchema},
	{28, automaticRecoveryDefaultSchema},
	{29, healthRevisionSchema},
	{30, secretRevisionSchema},
	{31, aioAutomationSchema},
	{32, serviceActivitySchema},
	{33, operationsSchema},
	{34, aioPerformanceSchema},
	{35, aioNativeIDSchema},
	{36, searchFragmentsSchema},
	{37, catalogSyncSchema},
	{38, activityConsolidationSchema},
	{39, rawSearchSchema},
	{40, consoleSimplificationSchema},
	{41, seederRetirementSchema},
	{42, fixedImportDefaultsSchema},
	{43, aioExperienceSchema},
	{44, matchingRecheckSchema},
	{45, wordEpisodeRecheckSchema},
	{46, historyRetentionSchema},
	{47, peersRetirementSchema},
	{48, statisticsFingerprintSchema},
	{49, publicAccessSchema},
	{50, publicAccessActivitySchema},
	{51, storageSchema},
	{52, publicDownloadsSchema},
	{53, catalogSearchSchema},
	{54, localizedTitlesSchema},
	{55, torznabSchema},
	{56, publicConnectionsSchema},
	{57, stremioConnectionsSchema},
	{58, connectionDefaultsSchema},
	{59, operationDownloadsSchema},
	{60, torznabPreferencesSchema},
	{61, leecherRetirementSchema},
	{62, catalogHistorySchema},
	{63, networkSchema},
}

// LatestSchemaVersion is the newest schema this binary can migrate and read.
func LatestSchemaVersion() int {
	return migrations[len(migrations)-1].version
}

func (s *Store) Migrate(ctx context.Context) error {
	return s.migrateThrough(ctx, LatestSchemaVersion())
}

func (s *Store) migrateThrough(ctx context.Context, version int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin schema migration", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", migrationLockNamespace, migrationLockID); err != nil {
		return databaseError("lock schema migration", err)
	}
	if _, err := tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS ingest;
CREATE TABLE IF NOT EXISTS ingest.schema_migrations (
    version INTEGER PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return databaseError("initialize schema migration", err)
	}
	for _, migration := range migrations {
		if migration.version > version {
			break
		}
		sum := sha256.Sum256([]byte(migration.sql))
		checksum := hex.EncodeToString(sum[:])
		var previous string
		err := tx.QueryRow(ctx, "SELECT checksum FROM ingest.schema_migrations WHERE version=$1", migration.version).Scan(&previous)
		if err == nil {
			if previous != checksum {
				return errors.New("ingestion schema migration checksum mismatch")
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return databaseError("read schema migration", err)
		}
		if _, err := tx.Exec(ctx, migration.sql); err != nil {
			return databaseError("apply schema migration", err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO ingest.schema_migrations(version,checksum) VALUES($1,$2)", migration.version, checksum); err != nil {
			return databaseError("record schema migration", err)
		}
	}
	return databaseError("commit schema migration", tx.Commit(ctx))
}
