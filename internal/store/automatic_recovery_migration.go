package store

const automaticRecoveryDefaultSchema = `
ALTER TABLE ingest.collection_settings ALTER COLUMN auto_resume_interrupted SET DEFAULT TRUE;
-- Apply the new default only to settings that have never been saved. A saved
-- opt-out and restore's deliberate disablement must survive this migration.
UPDATE ingest.collection_settings SET auto_resume_interrupted=TRUE,revision=revision+1
 WHERE singleton AND revision=1 AND NOT auto_resume_interrupted;
`
