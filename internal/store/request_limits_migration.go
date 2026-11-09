package store

const requestLimitsSchema = `
CREATE TABLE aiosources.provider_requests (
    provider_id TEXT NOT NULL,
    admitted_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX provider_requests_recent
    ON aiosources.provider_requests(provider_id, admitted_at DESC);
`
