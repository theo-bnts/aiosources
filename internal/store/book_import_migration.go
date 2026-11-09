package store

const bookImportSelectionSchema = `
ALTER TABLE aiosources.ol_import_state ADD COLUMN selection TEXT NOT NULL DEFAULT '';
ALTER TABLE aiosources.ol_import_state ADD COLUMN export_phase INT NOT NULL DEFAULT 0 CHECK(export_phase BETWEEN 0 AND 5);
ALTER TABLE aiosources.ol_import_state ADD COLUMN phase_snapshot TEXT NOT NULL DEFAULT '';
ALTER TABLE aiosources.ol_import_state ADD COLUMN phase_etag TEXT NOT NULL DEFAULT '';
ALTER TABLE aiosources.ol_import_state ADD COLUMN phase_position BIGINT NOT NULL DEFAULT 0;
ALTER TABLE aiosources.ol_import_state ADD COLUMN filtered BIGINT NOT NULL DEFAULT 0;
ALTER TABLE aiosources.ol_import_state ADD COLUMN selection_rebuild BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE aiosources.ol_import_references(id TEXT PRIMARY KEY);
`
