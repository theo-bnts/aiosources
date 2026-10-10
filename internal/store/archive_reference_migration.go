package store

// Archive references remove duplicate bytes without changing observation IDs,
// provenance or checkpoint evidence. References point directly to an older
// inline payload; new collections still never retain response/item bytes.
const archiveReferenceSchema = `
ALTER TABLE ingest.raw_records ADD COLUMN payload_id BIGINT REFERENCES ingest.raw_records(id);
ALTER TABLE ingest.raw_records ADD CONSTRAINT raw_records_payload_reference CHECK (
    payload_id IS NULL OR (payload_id<id AND payload_retained AND octet_length(raw)=0 AND fields='{}'::jsonb)
);
CREATE INDEX raw_records_payload ON ingest.raw_records(payload_id) WHERE payload_id IS NOT NULL;

ALTER TABLE ingest.pages ADD COLUMN payload_id BIGINT REFERENCES ingest.pages(id);
ALTER TABLE ingest.pages ADD CONSTRAINT pages_payload_reference CHECK (
    payload_id IS NULL OR (payload_id<id AND payload_retained AND octet_length(body)=0)
);
CREATE INDEX pages_payload ON ingest.pages(payload_id) WHERE payload_id IS NOT NULL;
`
