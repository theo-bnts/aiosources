package store

const catalogSearchSchema = `
ALTER TABLE ingest.aio_works ADD COLUMN display_title TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.operation_runs DROP CONSTRAINT operation_runs_kind_check;
ALTER TABLE ingest.operation_runs DROP CONSTRAINT operation_runs_check1;
ALTER TABLE ingest.operation_runs ADD CONSTRAINT operation_runs_kind_check
 CHECK(kind IN ('metadata','matching','cache','cleanup','download'));
ALTER TABLE ingest.operation_runs ADD CONSTRAINT operation_runs_kind_service_check
 CHECK ((kind='metadata' AND service='tmdb') OR (kind='matching' AND service='local')
 OR (kind IN ('cache','download') AND service IN ('alldebrid','torbox')) OR (kind='cleanup' AND service='alldebrid'));
`

const localizedTitlesSchema = `ALTER TABLE ingest.aio_works ADD COLUMN localized_titles JSONB NOT NULL DEFAULT '{}'::jsonb CHECK(jsonb_typeof(localized_titles)='object');
ALTER TABLE ingest.stremio_requests ADD COLUMN language_excluded INTEGER NOT NULL DEFAULT 0 CHECK(language_excluded>=0);`
