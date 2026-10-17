package store

const connectionDefaultsSchema = `
ALTER TABLE ingest.shares
 DROP CONSTRAINT shares_requests_per_minute_check,
 DROP CONSTRAINT shares_max_concurrent_downloads_check,
 ADD CHECK(requests_per_minute BETWEEN 0 AND 3600 AND (format<>'catalog' OR requests_per_minute>0)),
 ADD CHECK(max_concurrent_downloads BETWEEN 0 AND 16 AND (format<>'catalog' OR max_concurrent_downloads>0));

-- Named accesses replace the former global Stremio installation credential.
DELETE FROM ingest.secrets WHERE name='aiosource_installation';
`
