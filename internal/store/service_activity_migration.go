package store

const serviceActivitySchema = `
ALTER TABLE ingest.activity_logs ADD COLUMN service TEXT NOT NULL DEFAULT '' CHECK(service IN ('','tmdb','alldebrid','torbox'));
ALTER TABLE ingest.notifications ADD COLUMN service TEXT NOT NULL DEFAULT '' CHECK(service IN ('','tmdb','alldebrid','torbox'));
CREATE INDEX activity_service_recent ON ingest.activity_logs(service,created_at DESC,id DESC) WHERE service<>'';
-- Reconstruct earlier incident context from its linked notification, whose
-- durable title identified the service even when the log message did not.
UPDATE ingest.activity_logs SET service='alldebrid' WHERE kind='aiosource.incident' AND message='A temporary AllDebrid magnet could not be deleted. Inspect the account before retrying.';
UPDATE ingest.notifications SET service=CASE WHEN title LIKE 'tmdb %' THEN 'tmdb' WHEN title LIKE 'torbox %' THEN 'torbox' WHEN title LIKE 'alldebrid %' OR title LIKE 'cleanup %' THEN 'alldebrid' ELSE '' END WHERE kind='aiosource.incident';
UPDATE ingest.activity_logs l SET service=n.service FROM ingest.notifications n WHERE n.activity_id=l.id AND l.kind='aiosource.incident' AND n.service<>'';
`
