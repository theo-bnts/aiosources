package store

const secretRevisionSchema = `
CREATE SEQUENCE ingest.secret_revisions AS BIGINT;
ALTER TABLE ingest.secrets ADD COLUMN revision BIGINT NOT NULL DEFAULT nextval('ingest.secret_revisions') CHECK(revision > 0);
`
