package store

// Existing source clocks keep their deadlines. Legacy catalog clocks restart
// once with the interval profile, retaining publication and run checkpoints.
const catalogSyncSchema = `
ALTER TABLE ingest.schedule_clocks ADD COLUMN profile TEXT NOT NULL DEFAULT 'source'
    CHECK (profile IN ('source','catalog'));
`
