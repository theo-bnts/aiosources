package store

// Referential checks cannot use the former status='matched' partial index:
// ON DELETE SET NULL has to find every referencing row, regardless of status.
// The complete index also serves the existing matched-work lookups.
const informationResetIndexSchema = `
DROP INDEX ingest.aio_matches_work;
CREATE INDEX aio_matches_work ON ingest.aio_matches(work_id);

ALTER FUNCTION ingest.activity_message(TEXT) RENAME TO pre_information_reset_activity_message;
CREATE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN kind LIKE 'maintenance.%' THEN
   CASE split_part(split_part(kind,'.',2),'_',1)
    WHEN 'books' THEN 'Book information reset'
    WHEN 'videos' THEN 'Video information reset'
    WHEN 'metadata' THEN 'Information reset'
    WHEN 'mapping' THEN 'Automatic identification reset'
    WHEN 'cache' THEN 'Cache observations reset'
    ELSE 'Maintenance' END
   || CASE split_part(split_part(kind,'.',2),'_',2)
    WHEN 'completed' THEN ' completed'
    WHEN 'interrupted' THEN ' interrupted'
    ELSE ' failed' END
 ELSE ingest.pre_information_reset_activity_message(kind) END;
$$;
`
