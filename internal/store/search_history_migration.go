package store

// searchHistorySchema adds independent occurrence search and semantic history.
// It leaves the existing catalogue publication lock, feed journal and immutable
// journal guard intact. Baselines intentionally have no run or transition kind.
const searchHistorySchema = `
LOCK TABLE ingest.torrents IN SHARE ROW EXCLUSIVE MODE;
CREATE FUNCTION ingest.torrent_occurrence_id(provider TEXT, source TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT encode(sha256(convert_to('ingest.occurrence.v1','UTF8') || int4send(octet_length(provider)) || convert_to(provider,'UTF8') || int4send(octet_length(source)) || convert_to(source,'UTF8')),'hex');
$$;
CREATE FUNCTION ingest.torrent_integer(value JSONB) RETURNS NUMERIC
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
    text_value TEXT;
    number_value NUMERIC;
BEGIN
    IF jsonb_typeof(value) NOT IN ('number','string') THEN RETURN NULL; END IF;
    text_value := btrim(value #>> '{}');
    IF text_value !~ '^[+]?[0-9]+([.][0-9]+)?([eE][+-]?[0-9]+)?$' THEN RETURN NULL; END IF;
    BEGIN
        number_value := text_value::numeric;
    EXCEPTION WHEN numeric_value_out_of_range OR invalid_text_representation THEN RETURN NULL;
    END;
    IF number_value < 0 OR trunc(number_value) <> number_value THEN RETURN NULL; END IF;
    RETURN trunc(number_value);
END;
$$;
-- The prefix key is bounded even for maximum-size PostgreSQL NUMERIC values.
-- SQL always rechecks the exact numeric predicate, including prefix collisions.
CREATE FUNCTION ingest.torrent_integer_key(value NUMERIC) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT lpad(length(split_part(value::text,'.',1))::text,6,'0') || ':' || left(split_part(value::text,'.',1),256);
$$;
CREATE FUNCTION ingest.torrent_timestamp(value JSONB) RETURNS TIMESTAMPTZ
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE SET timezone='UTC' SET datestyle='ISO, YMD' AS $$
DECLARE text_value TEXT;
BEGIN
    IF jsonb_typeof(value) <> 'string' THEN RETURN NULL; END IF;
    text_value := value #>> '{}';
    IF length(text_value)>64 OR text_value !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt][0-9]{2}:[0-9]{2}:[0-9]{2}([.][0-9]+)?([Zz]|[+-][0-9]{2}:[0-9]{2})$' THEN RETURN NULL; END IF;
    BEGIN
        RETURN text_value::timestamptz;
    EXCEPTION WHEN datetime_field_overflow OR invalid_datetime_format OR invalid_time_zone_displacement_value OR invalid_text_representation THEN RETURN NULL;
    END;
END;
$$;
CREATE FUNCTION ingest.torrent_categories(fields JSONB) RETURNS TEXT[]
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT COALESCE(array_agg(encode(sha256(convert_to(lower(value #>> '{}'),'UTF8')),'hex')),ARRAY[]::text[])
    FROM (
        SELECT fields->'category' AS value
        UNION ALL
        SELECT value FROM jsonb_array_elements(CASE WHEN jsonb_typeof(fields->'categories')='array' THEN fields->'categories' ELSE jsonb_build_array(fields->'categories') END)
    ) AS categories WHERE jsonb_typeof(value) IN ('string','number','boolean');
$$;
CREATE FUNCTION ingest.torrent_hash(value JSONB) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
    hash_value TEXT;
    buffer_value INTEGER := 0;
    bits INTEGER := 0;
    digit INTEGER;
    result BYTEA := ''::bytea;
BEGIN
    IF jsonb_typeof(value)<>'string' THEN RETURN NULL; END IF;
    hash_value := lower(btrim(value #>> '{}'));
    IF hash_value ~ '^([0-9a-f]{40}|[0-9a-f]{64})$' THEN RETURN hash_value; END IF;
    IF hash_value !~ '^[a-z2-7]{32}$' THEN RETURN NULL; END IF;
    FOR i IN 1..32 LOOP
        digit := strpos('abcdefghijklmnopqrstuvwxyz234567',substr(hash_value,i,1))-1;
        buffer_value := (buffer_value << 5) | digit;
        bits := bits+5;
        IF bits>=8 THEN
            bits := bits-8;
            result := result || decode(lpad(to_hex((buffer_value >> bits) & 255),2,'0'),'hex');
            buffer_value := buffer_value & ((1 << bits)-1);
        END IF;
    END LOOP;
    RETURN encode(result,'hex');
END;
$$;
CREATE FUNCTION ingest.torrent_search_vector(provider TEXT, source TEXT, fields JSONB) RETURNS TSVECTOR
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
    encoded BYTEA;
    document TEXT;
BEGIN
    encoded := substring(convert_to(left(provider,128) || ' ' || left(source,4096) || ' ' ||
        left(COALESCE(fields->>'info_hash',''),64) || ' ' ||
        CASE WHEN jsonb_typeof(fields->'title')='string' THEN left(fields->>'title',131072) ELSE '' END,'UTF8') FROM 1 FOR 131072);
    -- Cutting a valid UTF-8 value needs at most three trailing bytes removed.
    LOOP
        BEGIN
            document := convert_from(encoded,'UTF8');
            EXIT;
        EXCEPTION WHEN character_not_in_repertoire THEN
            encoded := substring(encoded FROM 1 FOR octet_length(encoded)-1);
        END;
    END LOOP;
    RETURN to_tsvector('simple'::regconfig,document);
END;
$$;
ALTER TABLE ingest.torrents
    ADD COLUMN occurrence_id TEXT GENERATED ALWAYS AS (ingest.torrent_occurrence_id(provider_id,source_id)) STORED,
    ADD COLUMN search_document TSVECTOR GENERATED ALWAYS AS (ingest.torrent_search_vector(provider_id,source_id,fields)) STORED,
    ADD COLUMN search_size NUMERIC GENERATED ALWAYS AS (ingest.torrent_integer(fields->'size')) STORED,
    ADD COLUMN search_seeders NUMERIC GENERATED ALWAYS AS (ingest.torrent_integer(fields->'seeders')) STORED,
    ADD COLUMN search_published_at TIMESTAMPTZ GENERATED ALWAYS AS (ingest.torrent_timestamp(fields->'published_at')) STORED,
    ADD COLUMN search_categories TEXT[] GENERATED ALWAYS AS (ingest.torrent_categories(fields)) STORED,
    ADD COLUMN search_info_hash TEXT GENERATED ALWAYS AS (ingest.torrent_hash(fields->'info_hash')) STORED;
CREATE UNIQUE INDEX torrents_occurrence_id ON ingest.torrents(occurrence_id);
CREATE INDEX torrents_fulltext ON ingest.torrents USING GIN(search_document);
CREATE INDEX torrents_categories ON ingest.torrents USING GIN(search_categories);
CREATE INDEX torrents_size ON ingest.torrents((ingest.torrent_integer_key(search_size) COLLATE "C")) WHERE search_size IS NOT NULL;
CREATE INDEX torrents_seeders ON ingest.torrents((ingest.torrent_integer_key(search_seeders) COLLATE "C")) WHERE search_seeders IS NOT NULL;
CREATE INDEX torrents_published_at ON ingest.torrents(search_published_at,provider_id,source_id) WHERE search_published_at IS NOT NULL;
CREATE INDEX torrents_info_hash ON ingest.torrents(search_info_hash,provider_id,source_id) WHERE search_info_hash IS NOT NULL;
CREATE TABLE ingest.saved_views (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    filters JSONB NOT NULL CHECK (jsonb_typeof(filters)='object'),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE ingest.torrent_history_metadata (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    history_since TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO ingest.torrent_history_metadata DEFAULT VALUES;
CREATE TABLE ingest.torrent_history_baselines (
    occurrence_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    fields JSONB CHECK (jsonb_typeof(fields)='object'),
    origin JSONB NOT NULL CHECK (jsonb_typeof(origin)='object'),
    deleted BOOLEAN NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
-- Journal sequence establishes the last retained pre-history state, not its
-- time or run. In particular, tombstones are not fabricated new deletions.
INSERT INTO ingest.torrent_history_baselines(occurrence_id,provider_id,source_id,fields,origin,deleted)
SELECT ingest.torrent_occurrence_id(provider_id,source_id),provider_id,source_id,
    CASE WHEN deleted THEN NULL ELSE fields END,origin,deleted
FROM (SELECT DISTINCT ON (provider_id,source_id) provider_id,source_id,fields,origin,deleted
      FROM ingest.catalog_journal ORDER BY provider_id,source_id,sequence DESC) AS latest;
INSERT INTO ingest.torrent_history_baselines(occurrence_id,provider_id,source_id,fields,origin,deleted)
SELECT occurrence_id,provider_id,source_id,fields,ingest.catalog_origin(origin,provider_id,source_id),FALSE FROM ingest.torrents
ON CONFLICT(occurrence_id) DO UPDATE SET fields=EXCLUDED.fields,origin=EXCLUDED.origin,deleted=FALSE;
CREATE TABLE ingest.torrent_history (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurrence_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    run_id TEXT REFERENCES ingest.runs(id),
    kind TEXT NOT NULL CHECK (kind IN ('added','updated','deleted')),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    before_fields JSONB CHECK (jsonb_typeof(before_fields)='object'),
    after_fields JSONB CHECK (jsonb_typeof(after_fields)='object'),
    origin JSONB NOT NULL CHECK (jsonb_typeof(origin)='object'),
    before_origin JSONB CHECK (jsonb_typeof(before_origin)='object'),
    CHECK ((kind='added' AND before_fields IS NULL AND after_fields IS NOT NULL)
        OR (kind='updated' AND before_fields IS NOT NULL AND after_fields IS NOT NULL)
        OR (kind='deleted' AND before_fields IS NOT NULL AND after_fields IS NULL))
);
CREATE INDEX torrent_history_occurrence ON ingest.torrent_history(occurrence_id,id DESC);
CREATE INDEX torrent_history_run ON ingest.torrent_history(run_id,id DESC) WHERE run_id IS NOT NULL;
CREATE FUNCTION ingest.torrent_history_record() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    publication_run TEXT := NULLIF(current_setting('ingest.publication_run_id',TRUE),'');
    old_origin JSONB;
    new_origin JSONB;
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN old_origin := ingest.catalog_origin(OLD.origin,OLD.provider_id,OLD.source_id); END IF;
    IF TG_OP IN ('INSERT','UPDATE') THEN new_origin := ingest.catalog_origin(NEW.origin,NEW.provider_id,NEW.source_id); END IF;
    IF TG_OP='UPDATE' AND OLD.provider_id=NEW.provider_id AND OLD.source_id=NEW.source_id THEN
        IF OLD.fields IS NOT DISTINCT FROM NEW.fields AND old_origin IS NOT DISTINCT FROM new_origin THEN RETURN NULL; END IF;
        INSERT INTO ingest.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,before_fields,after_fields,origin,before_origin)
        VALUES(NEW.occurrence_id,NEW.provider_id,NEW.source_id,publication_run,'updated',OLD.fields,NEW.fields,new_origin,old_origin);
        RETURN NULL;
    END IF;
    IF TG_OP IN ('UPDATE','DELETE') THEN
        INSERT INTO ingest.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,before_fields,origin,before_origin)
        VALUES(OLD.occurrence_id,OLD.provider_id,OLD.source_id,publication_run,'deleted',OLD.fields,old_origin,old_origin);
    END IF;
    IF TG_OP IN ('INSERT','UPDATE') THEN
        INSERT INTO ingest.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,after_fields,origin)
        VALUES(NEW.occurrence_id,NEW.provider_id,NEW.source_id,publication_run,'added',NEW.fields,new_origin);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER torrent_history_record AFTER INSERT OR UPDATE OR DELETE ON ingest.torrents
FOR EACH ROW EXECUTE FUNCTION ingest.torrent_history_record();
CREATE FUNCTION ingest.torrent_history_truncate() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1768843109,3);
    INSERT INTO ingest.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,before_fields,origin,before_origin)
    SELECT occurrence_id,provider_id,source_id,NULLIF(current_setting('ingest.publication_run_id',TRUE),''),'deleted',fields,
        ingest.catalog_origin(origin,provider_id,source_id),ingest.catalog_origin(origin,provider_id,source_id) FROM ingest.torrents;
    RETURN NULL;
END;
$$;
CREATE TRIGGER torrent_history_truncate BEFORE TRUNCATE ON ingest.torrents
FOR EACH STATEMENT EXECUTE FUNCTION ingest.torrent_history_truncate();
CREATE TRIGGER torrent_history_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON ingest.torrent_history
FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_journal_immutable();
CREATE TRIGGER torrent_history_baselines_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON ingest.torrent_history_baselines
FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_journal_immutable();
CREATE TRIGGER torrent_history_metadata_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON ingest.torrent_history_metadata
FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_journal_immutable();
`
