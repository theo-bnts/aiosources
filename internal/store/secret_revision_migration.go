package store

const secretRevisionSchema = `
CREATE SEQUENCE aiosources.secret_revisions AS BIGINT;
ALTER TABLE aiosources.secrets ADD COLUMN revision BIGINT NOT NULL DEFAULT nextval('aiosources.secret_revisions') CHECK(revision > 0);
`
