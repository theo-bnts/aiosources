package store

const shareControlsSchema = `
ALTER TABLE ingest.shares
    ADD COLUMN expires_at TIMESTAMPTZ,
    ADD COLUMN requests_per_minute INTEGER NOT NULL DEFAULT 120 CHECK (requests_per_minute BETWEEN 1 AND 3600),
    ADD COLUMN max_concurrent_downloads INTEGER NOT NULL DEFAULT 2 CHECK (max_concurrent_downloads BETWEEN 1 AND 16);

-- Completed requests remain for one sliding minute. Active leases expire even
-- when their serving process crashes; deletion never touches catalog records.
CREATE TABLE ingest.share_requests (
    id TEXT PRIMARY KEY,
    share_id TEXT NOT NULL REFERENCES ingest.shares(id) ON DELETE CASCADE,
    requested_at TIMESTAMPTZ NOT NULL,
    lease_until TIMESTAMPTZ
);
CREATE INDEX share_requests_share_time ON ingest.share_requests(share_id,requested_at);
CREATE INDEX share_requests_share_lease ON ingest.share_requests(share_id,lease_until) WHERE lease_until IS NOT NULL;
`
