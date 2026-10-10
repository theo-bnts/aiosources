package store

const healthSchema = `
CREATE TABLE ingest.health_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
    stale_hours INTEGER NOT NULL DEFAULT 24 CHECK(stale_hours BETWEEN 1 AND 2160),
    stuck_minutes INTEGER NOT NULL DEFAULT 30 CHECK(stuck_minutes BETWEEN 5 AND 10080),
    min_free_bytes BIGINT NOT NULL DEFAULT 1073741824 CHECK(min_free_bytes BETWEEN 0 AND 1125899906842624),
    min_free_percent INTEGER NOT NULL DEFAULT 10 CHECK(min_free_percent BETWEEN 0 AND 95),
    journal_growth_bytes_per_day BIGINT NOT NULL DEFAULT 1073741824 CHECK(journal_growth_bytes_per_day BETWEEN 0 AND 1125899906842624)
);
INSERT INTO ingest.health_settings(singleton) VALUES(TRUE);
CREATE TABLE ingest.health_daily_samples (
    day DATE PRIMARY KEY,
    sampled_at TIMESTAMPTZ NOT NULL,
    database_bytes BIGINT NOT NULL CHECK(database_bytes >= 0),
    live_bytes BIGINT NOT NULL CHECK(live_bytes >= 0),
    raw_bytes BIGINT NOT NULL CHECK(raw_bytes >= 0),
    journal_bytes BIGINT NOT NULL CHECK(journal_bytes >= 0),
    other_bytes BIGINT NOT NULL CHECK(other_bytes >= 0)
);
CREATE TABLE ingest.health_source_observations (
    provider_id TEXT PRIMARY KEY,
    first_observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE ingest.health_alert_states (
    scope TEXT NOT NULL,
    health_code TEXT NOT NULL CHECK(health_code IN ('storage_low','source_stale','authentication','certificate','run_stuck','journal_growth','database_unavailable')),
    subject TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    since TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    provider_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(scope,health_code,subject)
);
CREATE INDEX runs_health_success ON ingest.runs(provider_id,finished_at DESC) WHERE status='succeeded' AND mode<>'preview';
CREATE INDEX events_health_failure ON ingest.events(run_id,id DESC) WHERE data ? 'failure_code';
CREATE INDEX pages_health_failure ON ingest.pages(provider_id,created_at DESC) WHERE metadata->>'failure_code' IN ('authentication','certificate');
`
