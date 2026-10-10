package store

// sharingSchema keeps publication and journal insertion in the same transaction.
// The statement lock must precede row locks and sequence allocation: sequence
// values alone do not describe commit order when writers overlap.
const sharingSchema = `
ALTER TABLE ingest.torrents ADD COLUMN origin JSONB;
ALTER TABLE ingest.staged_torrents ADD COLUMN origin JSONB;
ALTER TABLE ingest.staged_torrents ADD COLUMN deleted BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE ingest.remote_checkpoints (
    provider_id TEXT PRIMARY KEY,
    endpoint TEXT NOT NULL,
    checkpoint TEXT NOT NULL,
    last_synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE ingest.catalog_identity (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    instance_id TEXT NOT NULL UNIQUE,
    signing_key BYTEA NOT NULL CHECK (octet_length(signing_key) >= 32)
);
INSERT INTO ingest.catalog_identity(instance_id,signing_key)
VALUES(gen_random_uuid()::text,decode(replace(gen_random_uuid()::text || gen_random_uuid()::text || gen_random_uuid()::text,'-',''),'hex'));
CREATE TABLE ingest.shares (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    scope TEXT NOT NULL CHECK (scope IN ('selected','all')),
    source_ids TEXT[] NOT NULL,
    fields TEXT[] NOT NULL,
    password_hash BYTEA NOT NULL CHECK (octet_length(password_hash)=32),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_access_at TIMESTAMPTZ,
    last_sync_at TIMESTAMPTZ,
    CHECK ((scope='all' AND cardinality(source_ids)=0) OR (scope='selected' AND cardinality(source_ids)>0)),
    CHECK (cardinality(fields)>0 AND fields <@ ARRAY['title','size','info_hash','seeders','leechers','published_at','category','categories']::text[])
);
CREATE TABLE ingest.catalog_journal (
    sequence BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    origin JSONB NOT NULL CHECK (jsonb_typeof(origin)='object'),
    fields JSONB NOT NULL CHECK (jsonb_typeof(fields)='object'),
    deleted BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX catalog_journal_source_sequence ON ingest.catalog_journal(provider_id,source_id,sequence DESC);
CREATE FUNCTION ingest.catalog_publication_lock() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1768843109, 3);
    RETURN NULL;
END;
$$;
CREATE TRIGGER catalog_publication_lock BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON ingest.torrents
FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_publication_lock();
CREATE FUNCTION ingest.catalog_origin(supplied JSONB, provider TEXT, source TEXT) RETURNS JSONB LANGUAGE sql STABLE AS $$
    SELECT COALESCE(supplied,jsonb_build_object('instance_id',instance_id,'provider_id',provider,'source_id',source))
    FROM ingest.catalog_identity WHERE singleton;
$$;
CREATE FUNCTION ingest.catalog_publication_record() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    previous_origin JSONB;
    next_origin JSONB;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF OLD.provider_id=NEW.provider_id AND OLD.source_id=NEW.source_id AND OLD.fields=NEW.fields AND OLD.origin IS NOT DISTINCT FROM NEW.origin THEN
            RETURN NULL;
        END IF;
    END IF;
    IF TG_OP IN ('UPDATE','DELETE') THEN
        previous_origin := ingest.catalog_origin(OLD.origin,OLD.provider_id,OLD.source_id);
    END IF;
    IF TG_OP IN ('INSERT','UPDATE') THEN
        next_origin := ingest.catalog_origin(NEW.origin,NEW.provider_id,NEW.source_id);
    END IF;
    IF TG_OP='DELETE' OR (TG_OP='UPDATE' AND (OLD.provider_id<>NEW.provider_id OR OLD.source_id<>NEW.source_id OR previous_origin<>next_origin)) THEN
        INSERT INTO ingest.catalog_journal(provider_id,source_id,origin,fields,deleted)
        VALUES(OLD.provider_id,OLD.source_id,previous_origin,'{}',TRUE);
    END IF;
    IF TG_OP IN ('INSERT','UPDATE') THEN
        INSERT INTO ingest.catalog_journal(provider_id,source_id,origin,fields)
        VALUES(NEW.provider_id,NEW.source_id,next_origin,NEW.fields);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER catalog_publication_record AFTER INSERT OR UPDATE OR DELETE ON ingest.torrents
FOR EACH ROW EXECUTE FUNCTION ingest.catalog_publication_record();
CREATE FUNCTION ingest.catalog_publication_truncate() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1768843109, 3);
    INSERT INTO ingest.catalog_journal(provider_id,source_id,origin,fields,deleted)
    SELECT provider_id,source_id,ingest.catalog_origin(origin,provider_id,source_id),'{}',TRUE FROM ingest.torrents;
    RETURN NULL;
END;
$$;
CREATE TRIGGER catalog_publication_truncate BEFORE TRUNCATE ON ingest.torrents
FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_publication_truncate();
INSERT INTO ingest.catalog_journal(provider_id,source_id,origin,fields)
SELECT provider_id,source_id,ingest.catalog_origin(origin,provider_id,source_id),fields FROM ingest.torrents ORDER BY provider_id,source_id;
CREATE FUNCTION ingest.catalog_journal_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Published catalogue history is immutable';
END;
$$;
CREATE TRIGGER catalog_journal_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON ingest.catalog_journal
FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_journal_immutable();
`

// Native identifiers can be credential-bearing Torznab GUIDs or download URLs.
// Keep them private without changing authorized field values or imported origin
// identities. Existing readers must restart rather than mix identity schemes.
const catalogIdentityPrivacySchema = `
LOCK TABLE ingest.shares IN ACCESS EXCLUSIVE MODE;
LOCK TABLE ingest.torrents IN SHARE ROW EXCLUSIVE MODE;
CREATE OR REPLACE FUNCTION ingest.catalog_origin(supplied JSONB, provider TEXT, source TEXT) RETURNS JSONB LANGUAGE sql STABLE AS $$
    SELECT COALESCE(supplied,jsonb_build_object(
        'instance_id',instance_id,
        'provider_id',provider,
        'source_id',encode(sha256(convert_to('ingest.catalog.native-source.v1','UTF8') || decode('00','hex') || convert_to(source,'UTF8')),'hex')))
    FROM ingest.catalog_identity WHERE singleton;
$$;
ALTER TABLE ingest.catalog_journal DISABLE TRIGGER catalog_journal_immutable;
UPDATE ingest.catalog_journal AS journal
SET origin=ingest.catalog_origin(NULL,journal.provider_id,journal.source_id)
FROM ingest.catalog_identity AS identity
WHERE identity.singleton AND journal.origin=jsonb_build_object(
    'instance_id',identity.instance_id,'provider_id',journal.provider_id,'source_id',journal.source_id);
ALTER TABLE ingest.catalog_journal ENABLE TRIGGER catalog_journal_immutable;
UPDATE ingest.shares SET revision=revision+1,updated_at=NOW();
`
