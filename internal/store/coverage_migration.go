package store

const coverageSchema = `
CREATE TABLE ingest.run_scope_memberships (
    run_id TEXT NOT NULL REFERENCES ingest.runs(id),
    scope_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    PRIMARY KEY (run_id, scope_id, source_id)
);
CREATE INDEX run_scope_memberships_source ON ingest.run_scope_memberships(run_id, source_id);
CREATE TABLE ingest.run_scope_counts (
    run_id TEXT NOT NULL REFERENCES ingest.runs(id),
    scope_id TEXT NOT NULL,
    observed_count BIGINT NOT NULL CHECK (observed_count >= 0),
    PRIMARY KEY (run_id, scope_id)
);
`
