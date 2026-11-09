-- Initial AIOSources 2.3 schema. New installations use this baseline directly.
-- Release upgrades start at this version; no earlier migration is replayed.
SET LOCAL check_function_bodies = false;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE FUNCTION aiosources.access_activity_message(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_public_catalog(kind), CASE kind
 WHEN 'security.public_catalog_enabled' THEN 'Public catalog sharing enabled'
 WHEN 'security.public_catalog_disabled' THEN 'Public catalog sharing disabled'
 WHEN 'security.public_catalog_permissions_changed' THEN 'Public catalog data permissions changed'
 WHEN 'catalog.public_link_created' THEN 'Public catalog connection created'
 WHEN 'catalog.public_exported' THEN 'Public catalog page exported'
 WHEN 'catalog.public_failed' THEN 'Public catalog request failed'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_network(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_stremio_connections(kind), CASE kind
 WHEN 'stremio.created' THEN 'Stremio access created'
 WHEN 'stremio.permissions_changed' THEN 'Stremio settings changed'
 WHEN 'stremio.rotated' THEN 'Stremio installation key replaced'
 WHEN 'stremio.revoked' THEN 'Stremio access revoked'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_public_catalog(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_public_stremio(kind), CASE kind
 WHEN 'security.public_stremio_enabled' THEN 'Public Stremio enabled'
 WHEN 'security.public_stremio_disabled' THEN 'Public Stremio disabled'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_public_connections(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_torznab(kind), CASE kind
 WHEN 'torznab.created' THEN 'Torznab access created'
 WHEN 'torznab.permissions_changed' THEN 'Torznab permissions changed'
 WHEN 'torznab.rotated' THEN 'Torznab API key replaced'
 WHEN 'torznab.revoked' THEN 'Torznab access revoked'
 WHEN 'torznab.searched' THEN 'Torznab catalog search completed'
 WHEN 'torznab.failed' THEN 'Torznab catalog request failed'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_public_downloads(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT CASE kind
 WHEN 'security.mfa_enabled' THEN 'Two-step verification enabled'
 WHEN 'security.mfa_disabled' THEN 'Two-step verification disabled'
 WHEN 'security.recovery_rotated' THEN 'Recovery codes replaced'
 WHEN 'security.session_revoked' THEN 'Administrator session revoked'
 WHEN 'security.sessions_revoked' THEN 'Other administrator sessions revoked'
 WHEN 'security.public_access_enabled' THEN 'Public access enabled'
 WHEN 'security.public_access_disabled' THEN 'Public access disabled'
 WHEN 'share.created' THEN 'Catalog share created'
 WHEN 'share.permissions_changed' THEN 'Catalog share permissions changed'
 WHEN 'share.rotated' THEN 'Catalog share credentials replaced'
 WHEN 'share.revoked' THEN 'Catalog share revoked'
 ELSE NULL END;
$$;

CREATE FUNCTION aiosources.access_activity_message_before_public_limits(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_network(kind), CASE kind
 WHEN 'network.settings_changed' THEN 'Network connections changed'
 WHEN 'network.test_completed' THEN 'Proxy connection test completed'
 WHEN 'network.test_failed' THEN 'Proxy connection test failed'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_public_search(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_public_limits(kind), CASE kind
 WHEN 'security.public_limits_changed' THEN 'Public request limits changed'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_public_stremio(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_public_search(kind), CASE kind
 WHEN 'security.public_search_enabled' THEN 'Public Search enabled'
 WHEN 'security.public_search_disabled' THEN 'Public Search disabled'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_stremio_connections(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_public_connections(kind), CASE kind
 WHEN 'torznab.public_searched' THEN 'Public Torznab catalog search completed'
 WHEN 'torznab.public_failed' THEN 'Public Torznab catalog request failed'
 WHEN 'security.public_torznab_enabled' THEN 'Public Torznab enabled'
 WHEN 'security.public_torznab_disabled' THEN 'Public Torznab disabled'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.access_activity_message_before_torznab(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_public_downloads(kind), CASE kind
 WHEN 'security.public_source_downloads_enabled' THEN 'Visitor announce URLs enabled'
 WHEN 'security.public_source_downloads_disabled' THEN 'Visitor announce URLs disabled'
 ELSE NULL END);
$$;

CREATE FUNCTION aiosources.activity_kind(input text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT CASE WHEN input IN ('queued','started','resumed','succeeded','failed','paused','pause_requested','cancelled','cancel_requested','page_saved','page_error','source_error','no_progress') THEN 'run.'||input ELSE 'run.activity' END;
$$;

CREATE FUNCTION aiosources.activity_message(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
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
 ELSE aiosources.pre_information_reset_activity_message(kind) END;
$$;

CREATE FUNCTION aiosources.aio_native_book_candidates(fields jsonb) RETURNS text[]
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $_$
 WITH RECURSIVE objects(value) AS (
  SELECT fields WHERE jsonb_typeof(fields)='object'
  UNION ALL SELECT fields->'attributes' WHERE jsonb_typeof(fields->'attributes')='object'
  UNION ALL SELECT fields->'external_ids' WHERE jsonb_typeof(fields->'external_ids')='object'
 ), assertions(value) AS (
  SELECT member.value FROM objects CROSS JOIN LATERAL jsonb_each(objects.value) member
  WHERE member.key IN ('isbn','isbn10','isbn13','isbn_10','isbn_13','openlibrary','openlibrary_id')
  UNION ALL SELECT member.value FROM assertions CROSS JOIN LATERAL jsonb_array_elements(
   CASE WHEN jsonb_typeof(assertions.value)='array' THEN assertions.value ELSE '[]'::jsonb END) member(value)
 ), raw(value) AS (
  SELECT upper(btrim(value#>>'{}')) FROM assertions WHERE jsonb_typeof(value) IN ('string','number')
  UNION ALL SELECT (m)[1] FROM regexp_matches(upper(COALESCE(fields->>'title','')),'\m((?:97[89][- ]?)?[0-9](?:[- ]?[0-9]){8}[- ]?[0-9X])\M','g') m
 ), normalized(value) AS (
  SELECT regexp_replace(value,'[- ]','','g') FROM raw
 ), keys(value) AS (
  SELECT 'isbn:'||left(value,12) FROM normalized WHERE value ~ '^97[89][0-9]{10}$'
  UNION SELECT 'isbn:978'||left(value,9) FROM normalized WHERE value ~ '^[0-9]{9}[0-9X]$'
  UNION SELECT 'ol:'||regexp_replace(value,'^.*/','') FROM raw WHERE regexp_replace(value,'^.*/','') ~ '^OL[1-9][0-9]{0,14}[WM]$'
 ) SELECT COALESCE(array_agg(DISTINCT value),ARRAY[]::text[]) FROM keys;
$_$;

CREATE FUNCTION aiosources.aio_native_mal_candidates(fields jsonb) RETURNS text[]
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $_$
    WITH RECURSIVE assertions(value) AS (
        SELECT value FROM (VALUES
            (fields->'mal_id'), (fields->'mal'),
            (fields#>'{external_ids,mal}'), (fields#>'{external_ids,mal_id}'),
            (fields#>'{attributes,mal}'), (fields#>'{attributes,malid}')
        ) AS roots(value)
        UNION ALL
        SELECT member.value FROM assertions
        CROSS JOIN LATERAL jsonb_array_elements(
            CASE WHEN jsonb_typeof(assertions.value)='array' THEN assertions.value ELSE '[]'::jsonb END
        ) AS member(value)
    ), candidates AS (
        SELECT regexp_replace(value #>> '{}','^0+','') AS id
        FROM assertions WHERE jsonb_typeof(value) IN ('string','number')
    )
    SELECT COALESCE(array_agg(DISTINCT id),ARRAY[]::text[])
    FROM candidates WHERE id ~ '^[1-9][0-9]{0,15}$';
$_$;

CREATE FUNCTION aiosources.aio_queue_torrent() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.fields::text=OLD.fields::text THEN RETURN NEW; END IF;
 INSERT INTO aiosources.aio_match_queue(provider_id,source_id) VALUES(NEW.provider_id,NEW.source_id)
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1;
 RETURN NEW;
END $$;

CREATE FUNCTION aiosources.aio_review_evidence(fields jsonb) RETURNS jsonb
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
 SELECT aiosources.aio_video_review_evidence(fields) || CASE WHEN
 fields ?| ARRAY['isbn','isbn_10','isbn_13','book_title','book_format','openlibrary','openlibrary_id']
 OR COALESCE(fields->>'media_type',fields->>'type','') IN ('book','ebook','audiobook')
 OR COALESCE(fields->'categories',fields->'category','null')::text ~ '\m(70[0-9]{2}|3030)\M'
 OR fields->>'title' ~* '\m(epub|pdf|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|aaxc?|audiobook|ebook|livre audio)\M'
 OR COALESCE(fields->'attributes','{}') ?| ARRAY['isbn','isbn10','isbn13','isbn_10','isbn_13','booktitle','book_title','openlibrary','openlibrary_id']
 OR COALESCE(fields->'external_ids','{}') ?| ARRAY['isbn','isbn_10','isbn_13','openlibrary','openlibrary_id']
 THEN jsonb_strip_nulls(jsonb_build_object(
  'isbn',fields->'isbn','isbn_10',fields->'isbn_10','isbn_13',fields->'isbn_13',
  'author',fields->'author','authors',fields->'authors','book_title',fields->'book_title','book_format',fields->'book_format',
  'openlibrary_id',fields->'openlibrary_id','openlibrary',fields->'openlibrary',
  'book_attributes',NULLIF(jsonb_strip_nulls(jsonb_build_object(
   'isbn',fields#>'{attributes,isbn}','isbn10',fields#>'{attributes,isbn10}','isbn13',fields#>'{attributes,isbn13}',
   'isbn_10',fields#>'{attributes,isbn_10}','isbn_13',fields#>'{attributes,isbn_13}',
   'author',fields#>'{attributes,author}','authors',fields#>'{attributes,authors}',
   'booktitle',fields#>'{attributes,booktitle}','book_title',fields#>'{attributes,book_title}',
   'openlibrary',fields#>'{attributes,openlibrary}','openlibrary_id',fields#>'{attributes,openlibrary_id}',
   'book_format',fields#>'{attributes,book_format}')),'{}'::jsonb),
  'book_identifiers',NULLIF(jsonb_strip_nulls(jsonb_build_object(
   'isbn',fields#>'{external_ids,isbn}','isbn_10',fields#>'{external_ids,isbn_10}','isbn_13',fields#>'{external_ids,isbn_13}',
   'openlibrary',fields#>'{external_ids,openlibrary}','openlibrary_id',fields#>'{external_ids,openlibrary_id}')),'{}'::jsonb)
 )) ELSE '{}'::jsonb END;
$$;

CREATE FUNCTION aiosources.aio_video_review_evidence(fields jsonb) RETURNS jsonb
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
 SELECT jsonb_strip_nulls(jsonb_build_object(
 'title',fields->'title','name',fields->'name',
 'info_hash',fields->'info_hash','hash',fields->'hash','infohash',fields->'infohash',
 'imdb_id',fields->'imdb_id','imdb',fields->'imdb','tt_id',fields->'tt_id',
 'tmdb_id',fields->'tmdb_id','tmdb',fields->'tmdb','tvdb_id',fields->'tvdb_id','tvdb',fields->'tvdb',
 'mal_id',fields->'mal_id','mal',fields->'mal','season',fields->'season','episode',fields->'episode',
 'external_ids',COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(fields->'external_ids')='object' THEN fields->'external_ids' ELSE '{}'::jsonb END) WHERE key IN ('imdb','imdb_id','tmdb','tmdb_id','tvdb','tvdb_id','mal','mal_id')),'{}'),
 'attributes',COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(fields->'attributes')='object' THEN fields->'attributes' ELSE '{}'::jsonb END) WHERE key IN ('imdb','imdbid','tmdb','tmdbid','tvdb','tvdbid','mal','malid','season','episode')),'{}')
 ));
$$;

CREATE FUNCTION aiosources.catalog_details_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_OP='INSERT' OR OLD.fields IS DISTINCT FROM NEW.fields OR OLD.origin IS DISTINCT FROM NEW.origin THEN
  NEW.catalog_details_tx:=pg_current_xact_id()::text::bigint;
 END IF;
 RETURN NEW;
END;
$$;

CREATE FUNCTION aiosources.catalog_entity_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE entity TEXT; label TEXT;
BEGIN
 IF TG_TABLE_NAME='aio_matches' THEN
  entity:='metadata';
  IF NEW.status<>'matched' OR NEW.work_id IS NULL THEN RETURN NULL; END IF;
  SELECT media_type||':'||tmdb_id INTO label FROM aiosources.aio_works WHERE id=NEW.work_id AND catalog_import_source='' AND media_type<>'book';
  IF label IS NULL THEN RETURN NULL; END IF;
 ELSE
  IF NEW.media_type='book' THEN RETURN NULL; END IF;
  entity:='metadata'; label:=NEW.media_type||':'||NEW.tmdb_id;
  IF NEW.catalog_import_source<>'' THEN RETURN NULL; END IF;
  IF TG_OP='UPDATE' AND OLD.revision=NEW.revision AND OLD.display_title=NEW.display_title AND OLD.localized_titles=NEW.localized_titles AND OLD.catalog_import_source=NEW.catalog_import_source THEN RETURN NULL; END IF;
 END IF;
 IF TG_TABLE_NAME='aio_works' THEN
  IF NOT EXISTS(SELECT 1 FROM aiosources.aio_matches WHERE work_id=NEW.id AND status='matched') THEN RETURN NULL; END IF;
 END IF;
 IF NOT aiosources.catalog_export_active(entity) THEN RETURN NULL; END IF;
 INSERT INTO aiosources.catalog_entity_changes(kind,identity,changed_tx) VALUES(entity,label,pg_current_xact_id()::text::bigint)
 ON CONFLICT(kind,identity) DO UPDATE SET changed_tx=EXCLUDED.changed_tx;
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.catalog_entity_deleted() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 DELETE FROM aiosources.catalog_entity_changes WHERE kind='metadata' AND identity=OLD.media_type||':'||OLD.tmdb_id;
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.catalog_entity_policy_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM aiosources.prune_catalog_entity_index();
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.catalog_export_active(entity text DEFAULT ''::text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
 SELECT EXISTS(SELECT 1 FROM aiosources.shares WHERE format='catalog' AND enabled AND (expires_at IS NULL OR expires_at>now())
 AND (entity='' OR catalog_entities->>entity='true')) OR EXISTS(
 SELECT 1 FROM aiosources.aio_public_access a CROSS JOIN aiosources.public_catalog_policy p
 WHERE a.singleton AND p.singleton AND a.enabled AND a.expose_sources AND a.catalog_enabled
 AND (entity='' OR p.catalog_entities->>entity='true'));
$$;

CREATE FUNCTION aiosources.catalog_fields(value jsonb) RETURNS jsonb
    LANGUAGE sql IMMUTABLE STRICT
    AS $_$
 SELECT COALESCE(jsonb_object_agg(key,item),'{}'::jsonb) FROM jsonb_each(value) AS entry(key,item)
 WHERE key=ANY(ARRAY['title','size','info_hash','leechers','published_at','category','categories'])
 AND (jsonb_typeof(item) IN ('string','number','boolean','null') OR
 (jsonb_typeof(item)='array' AND NOT jsonb_path_exists(item,'$[*] ? (@.type() == "object" || @.type() == "array")')));
$_$;

CREATE FUNCTION aiosources.catalog_journal_immutable() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF current_setting('aiosources.storage_reset',TRUE)='on' THEN RETURN NULL; END IF;
 IF TG_OP IN ('DELETE','UPDATE') AND current_setting('aiosources.history_retention',TRUE)='on' THEN RETURN NULL; END IF;
 RAISE EXCEPTION 'Published catalogue history is immutable';
END;
$$;

CREATE FUNCTION aiosources.catalog_origin(supplied jsonb, provider text, source text) RETURNS jsonb
    LANGUAGE sql STABLE
    AS $$
    SELECT COALESCE(supplied,jsonb_build_object(
        'instance_id',instance_id,
        'provider_id',provider,
        'source_id',encode(sha256(convert_to('aiosources.catalog.native-source.v1','UTF8') || decode('00','hex') || convert_to(source,'UTF8')),'hex')))
    FROM aiosources.catalog_identity WHERE singleton;
$$;

CREATE FUNCTION aiosources.catalog_publication_lock() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1768843109, 3);
    RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.catalog_publication_record() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE previous_origin JSONB; next_origin JSONB;
BEGIN
 IF NOT (SELECT enabled FROM aiosources.catalog_tracking WHERE singleton) THEN RETURN NULL; END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.provider_id=NEW.provider_id AND OLD.source_id=NEW.source_id
   AND aiosources.catalog_fields(OLD.fields)=aiosources.catalog_fields(NEW.fields)
   AND OLD.origin IS NOT DISTINCT FROM NEW.origin THEN RETURN NULL; END IF;
 END IF;
 IF TG_OP IN ('UPDATE','DELETE') THEN previous_origin:=aiosources.catalog_origin(OLD.origin,OLD.provider_id,OLD.source_id); END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN next_origin:=aiosources.catalog_origin(NEW.origin,NEW.provider_id,NEW.source_id); END IF;
 IF TG_OP='DELETE' OR (TG_OP='UPDATE' AND (OLD.provider_id<>NEW.provider_id OR OLD.source_id<>NEW.source_id OR previous_origin<>next_origin)) THEN
  INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields,deleted)
  VALUES(OLD.provider_id,OLD.source_id,previous_origin,'{}',TRUE);
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN
  INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields)
  VALUES(NEW.provider_id,NEW.source_id,next_origin,aiosources.catalog_fields(NEW.fields));
 END IF;
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.catalog_publication_truncate() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,3);
 IF current_setting('aiosources.storage_reset',TRUE)='on'
  OR NOT (SELECT enabled FROM aiosources.catalog_tracking WHERE singleton) THEN RETURN NULL; END IF;
 INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields,deleted)
 SELECT provider_id,source_id,aiosources.catalog_origin(origin,provider_id,source_id),'{}',TRUE FROM aiosources.torrents;
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.catalog_tracking_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM aiosources.sync_catalog_tracking();
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.clear_catalog_tracking() RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
 UPDATE aiosources.catalog_retention SET floor=GREATEST(floor,COALESCE((SELECT max(sequence) FROM aiosources.catalog_journal),0));
 PERFORM set_config('aiosources.storage_reset','on',TRUE);
 TRUNCATE aiosources.catalog_journal,aiosources.catalog_baselines;
 PERFORM set_config('aiosources.storage_reset','off',TRUE);
END;
$$;

CREATE FUNCTION aiosources.core_activity_message(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT CASE kind
 WHEN 'run.queued' THEN 'Collection queued'
 WHEN 'run.started' THEN 'Collection attempt started'
 WHEN 'run.resumed' THEN 'Collection queued for resume'
 WHEN 'run.succeeded' THEN 'Collection succeeded'
 WHEN 'run.failed' THEN 'Collection failed; review its observations and source configuration'
 WHEN 'run.no_progress' THEN 'Collection has reached its useful-progress threshold without a new source identity; review the observations'
 WHEN 'run.paused' THEN 'Collection paused with a resumable checkpoint'
 WHEN 'run.pause_requested' THEN 'Collection pause requested; the current response checkpoint will be saved before stopping'
 WHEN 'run.cancelled' THEN 'Collection cancelled'
 WHEN 'run.cancel_requested' THEN 'Collection cancellation requested'
 WHEN 'run.page_saved' THEN 'Response observations and checkpoint saved'
 WHEN 'run.page_error' THEN 'Response error recorded; checkpoint unchanged'
 WHEN 'run.source_error' THEN 'Source collection could not continue; review run events and system health'
 WHEN 'schedule.failed' THEN 'Scheduled collection could not be queued; review source configuration and service status'
 WHEN 'schedule.queued' THEN 'Scheduled collection queued'
 WHEN 'schedule.skipped' THEN 'Scheduled collection skipped because a collection is already active'
 WHEN 'service.started' THEN 'Administration service started'
 WHEN 'service.stopped' THEN 'Administration service stopped'
 WHEN 'service.failed' THEN 'Administration service failed'
 WHEN 'scheduler.started' THEN 'Collection scheduler started'
 WHEN 'scheduler.stopped' THEN 'Collection scheduler stopped'
 WHEN 'scheduler.failed' THEN 'Collection scheduler failed'
 WHEN 'backup.succeeded' THEN 'Encrypted backup completed'
 WHEN 'backup.failed' THEN 'Encrypted backup failed; review backup status and available storage'
 WHEN 'backup.restore_succeeded' THEN 'Isolated backup restoration verified'
 WHEN 'backup.restore_failed' THEN 'Backup restoration verification failed; review the backup and recovery key'
 WHEN 'health.warning' THEN 'System health needs attention; review the health dashboard'
 WHEN 'health.recovered' THEN 'A system health warning has recovered'
 ELSE 'Collection activity recorded' END;
$$;

CREATE FUNCTION aiosources.lock_catalog_entity_writes() RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM pg_advisory_xact_lock_shared(1768843109,9);
 IF aiosources.catalog_export_active('metadata') THEN
  PERFORM pg_advisory_xact_lock(1768843109,8);
 END IF;
END;
$$;

CREATE FUNCTION aiosources.materialize_activity() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 INSERT INTO aiosources.activity_logs(source_event_id,event_id,level,kind,message,provider_id,run_id,data,created_at)
 SELECT NEW.id,'event-'||NEW.id,CASE WHEN NEW.kind='page_saved' THEN 'debug' WHEN NEW.kind IN ('failed','page_error','source_error') THEN 'error' WHEN NEW.kind IN ('paused','cancelled','cancel_requested','no_progress') THEN 'warn' ELSE 'info' END,
 aiosources.activity_kind(NEW.kind),aiosources.activity_message(aiosources.activity_kind(NEW.kind)),provider_id,NEW.run_id,aiosources.safe_activity_data(NEW.data),NEW.created_at
 FROM aiosources.runs WHERE id=NEW.run_id;
 RETURN NEW;
END;
$$;

CREATE FUNCTION aiosources.notify_activity() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM pg_notify('aiosources_run_changes','');
 RETURN NEW;
END;
$$;

CREATE FUNCTION aiosources.ol_aliases(key text) RETURNS text[]
    LANGUAGE sql STABLE STRICT PARALLEL SAFE
    AS $$
 WITH RECURSIVE aliases(id,seen) AS (SELECT key,ARRAY[key] UNION ALL SELECT r.source,a.seen||r.source FROM aliases a JOIN aiosources.ol_redirects r ON r.target=a.id WHERE cardinality(a.seen)<8 AND NOT r.source=ANY(a.seen))
 SELECT ARRAY(SELECT DISTINCT id FROM aliases LIMIT 1000);
$$;

CREATE FUNCTION aiosources.ol_resolve(key text) RETURNS text
    LANGUAGE sql STABLE STRICT PARALLEL SAFE
    AS $$
 WITH RECURSIVE path(id,seen) AS (SELECT key,ARRAY[key] UNION ALL SELECT r.target,p.seen||r.target FROM path p JOIN aiosources.ol_redirects r ON r.source=p.id WHERE cardinality(p.seen)<8 AND NOT r.target=ANY(p.seen))
 SELECT id FROM path p WHERE NOT EXISTS(SELECT 1 FROM aiosources.ol_redirects r WHERE r.source=p.id) LIMIT 1;
$$;

CREATE FUNCTION aiosources.pre_information_reset_activity_message(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT CASE WHEN kind='storage.sources_reset' THEN 'Source data reset; definitions and credentials retained' ELSE aiosources.pre_storage_activity_message(kind) END;
$$;

CREATE FUNCTION aiosources.pre_retention_activity_message(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT COALESCE(aiosources.access_activity_message(kind),aiosources.core_activity_message(kind));
$$;

CREATE FUNCTION aiosources.pre_storage_activity_message(kind text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
 SELECT CASE kind
 WHEN 'history.retention_failed' THEN 'History cleanup could not finish; it will be retried automatically'
 WHEN 'history.retention_recovered' THEN 'History cleanup recovered'
 ELSE aiosources.pre_retention_activity_message(kind) END;
$$;

CREATE FUNCTION aiosources.prune_catalog_entity_index() RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,8);
 IF NOT aiosources.catalog_export_active('metadata') THEN
  DELETE FROM aiosources.catalog_entity_changes WHERE kind='metadata';
 END IF;
END;
$$;

CREATE FUNCTION aiosources.public_catalog_policy_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_TABLE_NAME='aio_public_access' THEN
  IF (NEW.enabled,NEW.expose_sources,NEW.catalog_enabled) IS NOT DISTINCT FROM (OLD.enabled,OLD.expose_sources,OLD.catalog_enabled) THEN RETURN NULL; END IF;
  UPDATE aiosources.public_catalog_policy SET revision=revision+1 WHERE singleton;
 END IF;
 PERFORM aiosources.sync_catalog_tracking();
 PERFORM aiosources.prune_catalog_entity_index();
 IF EXISTS(SELECT 1 FROM aiosources.aio_public_access a CROSS JOIN aiosources.public_catalog_policy p
 WHERE a.singleton AND p.singleton AND a.enabled AND a.expose_sources AND a.catalog_enabled AND p.catalog_entities->>'metadata'='true') THEN
  INSERT INTO aiosources.catalog_entity_changes(kind,identity,changed_tx)
  SELECT 'metadata',w.media_type||':'||w.tmdb_id,pg_current_xact_id()::text::bigint
  FROM aiosources.aio_works w CROSS JOIN aiosources.public_catalog_policy p
  WHERE p.singleton AND w.catalog_import_source='' AND w.media_type<>'book' AND EXISTS(
   SELECT 1 FROM aiosources.aio_matches m JOIN aiosources.torrents t USING(provider_id,source_id)
   WHERE m.work_id=w.id AND m.status='matched' AND m.fingerprint=t.fields_fingerprint AND m.revision=w.revision
AND NOT EXISTS(SELECT 1 FROM aiosources.aio_match_queue q WHERE q.provider_id=t.provider_id AND q.source_id=t.source_id) AND NOT w.adult
   AND (cardinality(p.source_ids)=0 OR t.provider_id=ANY(p.source_ids))) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.raw_search_text(source text, fields jsonb, problem text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
    SELECT source || E'\n' || fields::text || E'\n' || problem;
$$;

CREATE FUNCTION aiosources.retire_seeder_fields() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 NEW.fields=aiosources.without_swarm_counts(NEW.fields); RETURN NEW;
END; $$;

CREATE FUNCTION aiosources.safe_activity_data(input jsonb) RETURNS jsonb
    LANGUAGE sql IMMUTABLE
    AS $_$
 SELECT COALESCE(jsonb_object_agg(key,value),'{}'::jsonb) FROM jsonb_each(input)
 WHERE (key IN ('page_id','page','pages','records','auxiliary_records','errors','committed_pages','max_pages','attempts','status_code','duration_ms','workers','bytes') AND jsonb_typeof(value)='number')
    OR (key IN ('done','emitted','payload_retained') AND jsonb_typeof(value)='boolean')
    OR (key='mode' AND value IN ('"preview"'::jsonb,'"incremental"'::jsonb,'"full"'::jsonb,'"metadata"'::jsonb))
    OR (key='trigger' AND value IN ('"manual"'::jsonb,'"scheduled"'::jsonb))
    OR (key='status' AND value IN ('"queued"'::jsonb,'"running"'::jsonb,'"succeeded"'::jsonb,'"failed"'::jsonb,'"paused"'::jsonb,'"cancelled"'::jsonb))
    OR (key IN ('provider_id','run_id','backup_id') AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$')
    OR (key='failure_code' AND value IN ('"authentication"'::jsonb,'"certificate"'::jsonb,'"network"'::jsonb,'"timeout"'::jsonb,'"http"'::jsonb,'"parse"'::jsonb,'"stalled"'::jsonb,'"configuration"'::jsonb,'"unknown"'::jsonb))
    OR (key='health_code' AND value IN ('"storage_low"'::jsonb,'"source_stale"'::jsonb,'"authentication"'::jsonb,'"certificate"'::jsonb,'"run_stuck"'::jsonb,'"journal_growth"'::jsonb,'"database_unavailable"'::jsonb))
    OR (key='version' AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^(dev|v?[0-9]+[.][0-9]+[.][0-9]+([-+][A-Za-z0-9.-]+)?)$');
$_$;

CREATE FUNCTION aiosources.sync_catalog_tracking() RETURNS void
    LANGUAGE plpgsql
    AS $$
DECLARE active BOOLEAN; previous BOOLEAN;
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,3);
 SELECT aiosources.catalog_export_active() INTO active;
 SELECT enabled INTO previous FROM aiosources.catalog_tracking WHERE singleton FOR UPDATE;
 IF active=previous THEN RETURN; END IF;
 IF NOT active THEN
  PERFORM aiosources.clear_catalog_tracking();
 ELSE
  INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields)
  SELECT provider_id,source_id,aiosources.catalog_origin(origin,provider_id,source_id),aiosources.catalog_fields(fields)
  FROM aiosources.torrents ORDER BY last_seen_at,provider_id,source_id;
 END IF;
 UPDATE aiosources.catalog_tracking SET enabled=active WHERE singleton;
END;
$$;

CREATE FUNCTION aiosources.torrent_categories(fields jsonb) RETURNS text[]
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
    SELECT COALESCE(array_agg(encode(sha256(convert_to(lower(value #>> '{}'),'UTF8')),'hex')),ARRAY[]::text[])
    FROM (
        SELECT fields->'category' AS value
        UNION ALL
        SELECT value FROM jsonb_array_elements(CASE WHEN jsonb_typeof(fields->'categories')='array' THEN fields->'categories' ELSE jsonb_build_array(fields->'categories') END)
    ) AS categories WHERE jsonb_typeof(value) IN ('string','number','boolean');
$$;

CREATE FUNCTION aiosources.torrent_hash(value jsonb) RETURNS text
    LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
    AS $_$
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
$_$;

CREATE FUNCTION aiosources.torrent_history_record() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    publication_run TEXT := NULLIF(current_setting('aiosources.publication_run_id',TRUE),'');
    old_origin JSONB;
    new_origin JSONB;
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN old_origin := aiosources.catalog_origin(OLD.origin,OLD.provider_id,OLD.source_id); END IF;
    IF TG_OP IN ('INSERT','UPDATE') THEN new_origin := aiosources.catalog_origin(NEW.origin,NEW.provider_id,NEW.source_id); END IF;
    IF TG_OP='UPDATE' AND OLD.provider_id=NEW.provider_id AND OLD.source_id=NEW.source_id THEN
        IF OLD.fields IS NOT DISTINCT FROM NEW.fields AND old_origin IS NOT DISTINCT FROM new_origin THEN RETURN NULL; END IF;
        INSERT INTO aiosources.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,before_fields,after_fields,origin,before_origin)
        VALUES(NEW.occurrence_id,NEW.provider_id,NEW.source_id,publication_run,'updated',OLD.fields,NEW.fields,new_origin,old_origin);
        RETURN NULL;
    END IF;
    IF TG_OP IN ('UPDATE','DELETE') THEN
        INSERT INTO aiosources.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,before_fields,origin,before_origin)
        VALUES(OLD.occurrence_id,OLD.provider_id,OLD.source_id,publication_run,'deleted',OLD.fields,old_origin,old_origin);
    END IF;
    IF TG_OP IN ('INSERT','UPDATE') THEN
        INSERT INTO aiosources.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,after_fields,origin)
        VALUES(NEW.occurrence_id,NEW.provider_id,NEW.source_id,publication_run,'added',NEW.fields,new_origin);
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.torrent_history_truncate() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF current_setting('aiosources.storage_reset',TRUE)='on' THEN RETURN NULL; END IF;
 PERFORM pg_advisory_xact_lock(1768843109,3);
 INSERT INTO aiosources.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,before_fields,origin,before_origin)
 SELECT occurrence_id,provider_id,source_id,NULLIF(current_setting('aiosources.publication_run_id',TRUE),''),'deleted',fields,
  aiosources.catalog_origin(origin,provider_id,source_id),aiosources.catalog_origin(origin,provider_id,source_id) FROM aiosources.torrents;
 RETURN NULL;
END;
$$;

CREATE FUNCTION aiosources.torrent_integer(value jsonb) RETURNS numeric
    LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
    AS $_$
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
$_$;

CREATE FUNCTION aiosources.torrent_integer_key(value numeric) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
    SELECT lpad(length(split_part(value::text,'.',1))::text,6,'0') || ':' || left(split_part(value::text,'.',1),256);
$$;

CREATE FUNCTION aiosources.torrent_occurrence_id(provider text, source text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
    SELECT encode(sha256(convert_to('aiosources.occurrence.v1','UTF8') || int4send(octet_length(provider)) || convert_to(provider,'UTF8') || int4send(octet_length(source)) || convert_to(source,'UTF8')),'hex');
$$;

CREATE FUNCTION aiosources.torrent_search_text(provider text, source text, fields jsonb) RETURNS text
    LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
DECLARE encoded BYTEA;
BEGIN
    encoded := substring(convert_to(left(provider,128) || ' ' || left(source,4096) || ' ' ||
        left(COALESCE(fields->>'info_hash',''),64) || ' ' ||
        CASE WHEN jsonb_typeof(fields->'title')='string' THEN left(fields->>'title',131072) ELSE '' END,'UTF8') FROM 1 FOR 131072);
    LOOP
        BEGIN
            RETURN convert_from(encoded,'UTF8');
        EXCEPTION WHEN character_not_in_repertoire THEN
            encoded := substring(encoded FROM 1 FOR octet_length(encoded)-1);
        END;
    END LOOP;
END;
$$;

CREATE FUNCTION aiosources.torrent_search_vector(provider text, source text, fields jsonb) RETURNS tsvector
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
    SELECT to_tsvector('simple'::regconfig,aiosources.torrent_search_text(provider,source,fields));
$$;

CREATE FUNCTION aiosources.torrent_timestamp(value jsonb) RETURNS timestamp with time zone
    LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
    SET "TimeZone" TO 'UTC'
    SET "DateStyle" TO 'ISO, YMD'
    AS $_$
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
$_$;

CREATE FUNCTION aiosources.without_swarm_counts(value jsonb) RETURNS jsonb
    LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
    AS $_$
BEGIN
 IF NOT (value @? '$.**.peers' OR value @? '$.**.seeders' OR value @? '$.**.leechers') THEN RETURN value; END IF;
 CASE jsonb_typeof(value)
 WHEN 'object' THEN
  RETURN COALESCE((SELECT jsonb_object_agg(key,aiosources.without_swarm_counts(item))
    FROM jsonb_each(value) AS entry(key,item) WHERE key NOT IN ('seeders','peers','leechers')),'{}'::jsonb);
 WHEN 'array' THEN
  RETURN COALESCE((SELECT jsonb_agg(aiosources.without_swarm_counts(item) ORDER BY ordinal)
    FROM jsonb_array_elements(value) WITH ORDINALITY AS entry(item,ordinal)),'[]'::jsonb);
 ELSE RETURN value;
 END CASE;
END;
$_$;

CREATE TABLE aiosources.activity_logs (
    id bigint NOT NULL,
    source_event_id bigint,
    event_id text NOT NULL,
    level text NOT NULL,
    kind text NOT NULL,
    message text NOT NULL,
    provider_id text DEFAULT ''::text NOT NULL,
    run_id text DEFAULT ''::text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    service text DEFAULT ''::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    CONSTRAINT activity_logs_level_check CHECK ((level = ANY (ARRAY['debug'::text, 'info'::text, 'warn'::text, 'error'::text]))),
    CONSTRAINT activity_logs_service_check CHECK ((service = ANY (ARRAY[''::text, 'tmdb'::text, 'openlibrary'::text, 'network'::text, 'alldebrid'::text, 'debridlink'::text, 'deepbrid'::text, 'premiumize'::text, 'realdebrid'::text, 'torbox'::text])))
);

ALTER TABLE aiosources.activity_logs ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.activity_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.admin_security (
    singleton boolean DEFAULT true NOT NULL,
    password_hash bytea,
    active_secret bytea,
    pending_secret bytea,
    pending_expires_at timestamp with time zone,
    pending_session_id text,
    last_step bigint DEFAULT '-1'::integer NOT NULL,
    recovery_hashes text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT admin_security_check CHECK ((((pending_secret IS NULL) AND (pending_expires_at IS NULL) AND (pending_session_id IS NULL)) OR ((pending_secret IS NOT NULL) AND (pending_expires_at IS NOT NULL) AND (pending_session_id IS NOT NULL)))),
    CONSTRAINT admin_security_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.admin_sessions (
    id text NOT NULL,
    token_hash bytea NOT NULL,
    user_agent text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT admin_sessions_id_check CHECK ((id ~ '^[a-f0-9]{32}$'::text)),
    CONSTRAINT admin_sessions_token_hash_check CHECK ((octet_length(token_hash) = 32)),
    CONSTRAINT admin_sessions_user_agent_check CHECK ((length(user_agent) <= 100))
);

CREATE TABLE aiosources.aio_availability (
    service text NOT NULL,
    account text NOT NULL,
    hash text NOT NULL,
    cached boolean NOT NULL,
    checked_at timestamp with time zone NOT NULL,
    files jsonb DEFAULT '[]'::jsonb NOT NULL
);

CREATE TABLE aiosources.aio_cleanup (
    account text NOT NULL,
    hash text NOT NULL,
    magnet_id bigint NOT NULL,
    failed boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE aiosources.aio_episodes (
    work_id bigint NOT NULL,
    season integer NOT NULL,
    episode integer NOT NULL,
    year integer NOT NULL
);

CREATE TABLE aiosources.aio_incidents (
    service text NOT NULL,
    active boolean NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE aiosources.aio_inventory (
    media_type text NOT NULL,
    tmdb_id text NOT NULL,
    export_date text NOT NULL
);

CREATE TABLE aiosources.aio_match_queue (
    provider_id text NOT NULL,
    source_id text NOT NULL,
    version bigint DEFAULT 1 NOT NULL
);

CREATE TABLE aiosources.aio_match_reviews (
    provider_id text NOT NULL,
    source_id text NOT NULL,
    active boolean DEFAULT true NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    fingerprint text NOT NULL,
    evidence_fingerprint text NOT NULL,
    media_type text NOT NULL,
    tmdb_id text NOT NULL,
    override_episode boolean DEFAULT false NOT NULL,
    season integer,
    episode integer,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    openlibrary_id text DEFAULT ''::text NOT NULL,
    CONSTRAINT aio_match_reviews_check CHECK (((episode IS NULL) OR (season IS NOT NULL))),
    CONSTRAINT aio_match_reviews_check1 CHECK (((media_type = 'tv'::text) OR ((season IS NULL) AND (episode IS NULL)))),
    CONSTRAINT aio_match_reviews_episode_check CHECK (((episode >= 1) AND (episode <= 99999))),
    CONSTRAINT aio_match_reviews_media_type_check CHECK ((media_type = ANY (ARRAY[''::text, 'movie'::text, 'tv'::text, 'book'::text]))),
    CONSTRAINT aio_match_reviews_revision_check CHECK ((revision > 0)),
    CONSTRAINT aio_match_reviews_season_check CHECK (((season >= 0) AND (season <= 999)))
);

CREATE TABLE aiosources.aio_matches (
    provider_id text NOT NULL,
    source_id text NOT NULL,
    fingerprint text NOT NULL,
    work_id bigint,
    revision bigint DEFAULT 0 NOT NULL,
    normalized text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    reason text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    book_kind text DEFAULT ''::text NOT NULL,
    CONSTRAINT aio_matches_book_kind_check CHECK ((book_kind = ANY (ARRAY[''::text, 'book'::text, 'ebook'::text, 'audiobook'::text]))),
    CONSTRAINT aio_matches_status_check CHECK ((status = ANY (ARRAY['matched'::text, 'ambiguous'::text, 'unmatched'::text])))
);

CREATE TABLE aiosources.aio_metadata_jobs (
    media_type text NOT NULL,
    tmdb_id text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE aiosources.aio_preparations (
    service text NOT NULL,
    account text NOT NULL,
    hash text NOT NULL,
    provider_id text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT aio_preparations_service_check CHECK ((service = ANY (ARRAY['debridlink'::text, 'deepbrid'::text, 'premiumize'::text, 'realdebrid'::text])))
);

CREATE TABLE aiosources.aio_public_access (
    singleton boolean DEFAULT true NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    generation bigint DEFAULT 1 NOT NULL,
    expose_sources boolean DEFAULT false CONSTRAINT aio_public_access_allow_private_downloads_not_null NOT NULL,
    torznab_enabled boolean DEFAULT false NOT NULL,
    torznab_generation bigint DEFAULT 1 NOT NULL,
    search_enabled boolean DEFAULT false NOT NULL,
    stremio_enabled boolean DEFAULT true NOT NULL,
    stremio_generation bigint DEFAULT 1 NOT NULL,
    catalog_enabled boolean DEFAULT false NOT NULL,
    catalog_generation bigint DEFAULT 1 NOT NULL,
    CONSTRAINT aio_public_access_singleton_check CHECK (singleton),
    CONSTRAINT aio_public_catalog_requires_sources CHECK (((NOT catalog_enabled) OR expose_sources))
);

CREATE TABLE aiosources.aio_resources (
    service text NOT NULL,
    account text NOT NULL,
    resource text NOT NULL,
    hashes text[] NOT NULL,
    checked_at timestamp with time zone NOT NULL,
    last_attempt timestamp with time zone NOT NULL,
    retry integer DEFAULT 0 NOT NULL
);

CREATE TABLE aiosources.aio_season_requests (
    tmdb_id text NOT NULL,
    season integer NOT NULL,
    CONSTRAINT aio_season_requests_season_check CHECK (((season >= 0) AND (season <= 999)))
);

CREATE TABLE aiosources.aio_seasons (
    work_id bigint NOT NULL,
    season integer NOT NULL,
    year integer NOT NULL
);

CREATE TABLE aiosources.aio_shared_availability (
    service text NOT NULL,
    hash text NOT NULL,
    cached boolean NOT NULL,
    checked_at timestamp with time zone NOT NULL,
    files jsonb NOT NULL,
    catalog_import_source text DEFAULT ''::text NOT NULL,
    CONSTRAINT aio_shared_availability_files_check CHECK ((jsonb_typeof(files) = 'array'::text))
);

CREATE TABLE aiosources.aio_state (
    id boolean DEFAULT true NOT NULL,
    last_sync timestamp with time zone,
    changes_through timestamp with time zone,
    cache_generation bigint DEFAULT 1 NOT NULL,
    metadata_generation bigint DEFAULT 1 NOT NULL,
    inventory_date text DEFAULT ''::text NOT NULL,
    sync_retry_at timestamp with time zone,
    sync_failures integer DEFAULT 0 NOT NULL,
    sync_error text DEFAULT ''::text NOT NULL,
    last_match timestamp with time zone,
    recovery_started_at timestamp with time zone,
    applied_through timestamp with time zone,
    provider_retry_at timestamp with time zone,
    CONSTRAINT aio_state_id_check CHECK (id),
    CONSTRAINT aio_state_sync_failures_check CHECK ((sync_failures >= 0))
);

CREATE TABLE aiosources.aio_titles (
    work_id bigint NOT NULL,
    title text NOT NULL,
    normalized text NOT NULL
);

CREATE TABLE aiosources.aio_works (
    id bigint NOT NULL,
    media_type text NOT NULL,
    tmdb_id text,
    imdb_id text DEFAULT ''::text NOT NULL,
    tvdb_id text DEFAULT ''::text NOT NULL,
    years integer[] NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    adult boolean DEFAULT false NOT NULL,
    display_title text DEFAULT ''::text NOT NULL,
    localized_titles jsonb DEFAULT '{}'::jsonb NOT NULL,
    catalog_import_source text DEFAULT ''::text NOT NULL,
    catalog_import_updated_at timestamp with time zone,
    openlibrary_id text DEFAULT ''::text NOT NULL,
    poster_path text DEFAULT ''::text NOT NULL,
    book_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    book_normalized text DEFAULT ''::text NOT NULL,
    ol_revision bigint DEFAULT 0 NOT NULL,
    ol_seen bigint DEFAULT 0 NOT NULL,
    ol_modified_at timestamp with time zone,
    CONSTRAINT aio_works_identifier_namespace CHECK ((((media_type = 'book'::text) AND (tmdb_id IS NULL) AND (openlibrary_id ~ '^OL[1-9][0-9]{0,14}[WM]$'::text)) OR ((media_type = ANY (ARRAY['movie'::text, 'tv'::text])) AND (tmdb_id IS NOT NULL) AND (openlibrary_id = ''::text)))),
    CONSTRAINT aio_works_localized_titles_check CHECK ((jsonb_typeof(localized_titles) = 'object'::text)),
    CONSTRAINT aio_works_media_type_check CHECK ((media_type = ANY (ARRAY['movie'::text, 'tv'::text, 'book'::text])))
);

ALTER TABLE aiosources.aio_works ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.aio_works_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.catalog_baselines (
    provider_id text NOT NULL,
    source_id text NOT NULL,
    origin jsonb NOT NULL,
    fields jsonb NOT NULL,
    deleted boolean NOT NULL,
    sequence bigint NOT NULL,
    recorded_at timestamp with time zone NOT NULL,
    CONSTRAINT catalog_baselines_fields_check CHECK ((jsonb_typeof(fields) = 'object'::text)),
    CONSTRAINT catalog_baselines_origin_check CHECK ((jsonb_typeof(origin) = 'object'::text))
);

CREATE TABLE aiosources.catalog_entity_changes (
    kind text NOT NULL,
    identity text NOT NULL,
    changed_tx bigint NOT NULL,
    CONSTRAINT catalog_entity_changes_kind_check CHECK ((kind = 'metadata'::text))
);

CREATE TABLE aiosources.catalog_identity (
    singleton boolean DEFAULT true NOT NULL,
    instance_id text NOT NULL,
    signing_key bytea NOT NULL,
    CONSTRAINT catalog_identity_signing_key_check CHECK ((octet_length(signing_key) >= 32)),
    CONSTRAINT catalog_identity_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.catalog_journal (
    sequence bigint NOT NULL,
    provider_id text NOT NULL,
    source_id text NOT NULL,
    origin jsonb NOT NULL,
    fields jsonb NOT NULL,
    deleted boolean DEFAULT false NOT NULL,
    recorded_at timestamp with time zone DEFAULT statement_timestamp() NOT NULL,
    CONSTRAINT catalog_journal_fields_check CHECK ((jsonb_typeof(fields) = 'object'::text)),
    CONSTRAINT catalog_journal_origin_check CHECK ((jsonb_typeof(origin) = 'object'::text))
);

ALTER TABLE aiosources.catalog_journal ALTER COLUMN sequence ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.catalog_journal_sequence_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.catalog_retention (
    singleton boolean DEFAULT true NOT NULL,
    floor bigint DEFAULT 0 NOT NULL,
    CONSTRAINT catalog_retention_floor_check CHECK ((floor >= 0)),
    CONSTRAINT catalog_retention_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.catalog_tracking (
    singleton boolean DEFAULT true NOT NULL,
    enabled boolean NOT NULL,
    CONSTRAINT catalog_tracking_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.collection_settings (
    singleton boolean DEFAULT true NOT NULL,
    workers integer,
    revision bigint DEFAULT 1 NOT NULL,
    max_quota_retries integer DEFAULT 3 NOT NULL,
    max_quota_wait_seconds integer DEFAULT 0 NOT NULL,
    no_progress_requests integer DEFAULT 1000 NOT NULL,
    no_progress_action text DEFAULT 'warn'::text NOT NULL,
    auto_resume_interrupted boolean DEFAULT true NOT NULL,
    CONSTRAINT collection_settings_max_quota_retries_check CHECK (((max_quota_retries >= 0) AND (max_quota_retries <= 10))),
    CONSTRAINT collection_settings_max_quota_wait_seconds_check CHECK (((max_quota_wait_seconds >= 0) AND (max_quota_wait_seconds <= 86400))),
    CONSTRAINT collection_settings_no_progress_action_check CHECK ((no_progress_action = ANY (ARRAY['warn'::text, 'pause'::text]))),
    CONSTRAINT collection_settings_no_progress_requests_check CHECK (((no_progress_requests >= 0) AND (no_progress_requests <= 100000))),
    CONSTRAINT collection_settings_revision_check CHECK ((revision > 0)),
    CONSTRAINT collection_settings_singleton_check CHECK (singleton),
    CONSTRAINT collection_settings_workers_check CHECK (((workers >= 1) AND (workers <= 32)))
);

CREATE TABLE aiosources.collection_state (
    provider_id text NOT NULL,
    initialized_at timestamp with time zone NOT NULL
);

CREATE TABLE aiosources.events (
    id bigint NOT NULL,
    run_id text NOT NULL,
    kind text NOT NULL,
    message text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT events_data_check CHECK ((jsonb_typeof(data) = 'object'::text))
);

ALTER TABLE aiosources.events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.legacy_imports (
    provider_id text NOT NULL,
    source_id text NOT NULL,
    imported_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE aiosources.network_settings (
    singleton boolean DEFAULT true NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    default_route text DEFAULT 'system'::text NOT NULL,
    proxies jsonb DEFAULT '[]'::jsonb NOT NULL,
    overrides jsonb DEFAULT '{}'::jsonb NOT NULL,
    incoming jsonb DEFAULT '{"format": "chain", "enabled": false, "ip_header": "X-Forwarded-For", "trusted_proxies": []}'::jsonb NOT NULL,
    public_limits jsonb DEFAULT '{"concurrent": 50, "setup_per_minute": 12, "requests_per_minute": 120}'::jsonb NOT NULL,
    CONSTRAINT network_settings_incoming_check CHECK ((jsonb_typeof(incoming) = 'object'::text)),
    CONSTRAINT network_settings_overrides_check CHECK ((jsonb_typeof(overrides) = 'object'::text)),
    CONSTRAINT network_settings_proxies_check CHECK ((jsonb_typeof(proxies) = 'array'::text)),
    CONSTRAINT network_settings_public_limits_check CHECK (COALESCE(((jsonb_typeof(public_limits) = 'object'::text) AND (public_limits ?& ARRAY['setup_per_minute'::text, 'requests_per_minute'::text, 'concurrent'::text]) AND ((((public_limits ->> 'setup_per_minute'::text))::integer >= 0) AND (((public_limits ->> 'setup_per_minute'::text))::integer <= 100000)) AND ((((public_limits ->> 'requests_per_minute'::text))::integer >= 0) AND (((public_limits ->> 'requests_per_minute'::text))::integer <= 100000)) AND ((((public_limits ->> 'concurrent'::text))::integer >= 0) AND (((public_limits ->> 'concurrent'::text))::integer <= 1000))), false)),
    CONSTRAINT network_settings_revision_check CHECK ((revision > 0)),
    CONSTRAINT network_settings_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.ol_authors (
    id text NOT NULL,
    name text NOT NULL,
    aliases text[] DEFAULT '{}'::text[] NOT NULL,
    normalized text DEFAULT ''::text NOT NULL,
    ol_revision bigint DEFAULT 0 NOT NULL,
    ol_seen bigint DEFAULT 0 NOT NULL,
    ol_modified_at timestamp with time zone
);

CREATE TABLE aiosources.ol_change_jobs (
    id text NOT NULL,
    revision bigint NOT NULL,
    changed_at timestamp with time zone NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ol_change_jobs_revision_check CHECK ((revision >= 0))
);

CREATE TABLE aiosources.ol_changes_state (
    singleton boolean DEFAULT true NOT NULL,
    anchor_id text DEFAULT ''::text NOT NULL,
    anchor_at timestamp with time zone,
    tail_since timestamp with time zone,
    tail_start_id text DEFAULT ''::text NOT NULL,
    bootstrap_day date,
    bootstrap_complete boolean DEFAULT false NOT NULL,
    changes_through timestamp with time zone,
    last_sync timestamp with time zone,
    checked_at timestamp with time zone,
    retry_at timestamp with time zone,
    needs_dump_after timestamp with time zone,
    dump_checked_at timestamp with time zone,
    error text DEFAULT ''::text NOT NULL,
    provider_retry_at timestamp with time zone,
    CONSTRAINT ol_changes_state_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.ol_deleted (
    id text NOT NULL,
    ol_revision bigint NOT NULL,
    ol_modified_at timestamp with time zone
);

CREATE TABLE aiosources.ol_editions (
    id text NOT NULL,
    work_keys text[] NOT NULL,
    title text NOT NULL,
    normalized text NOT NULL,
    isbn text[] DEFAULT '{}'::text[] NOT NULL,
    author_keys text[] DEFAULT '{}'::text[] NOT NULL,
    languages text[] DEFAULT '{}'::text[] NOT NULL,
    year integer DEFAULT 0 NOT NULL,
    format text DEFAULT ''::text NOT NULL,
    cover bigint DEFAULT 0 NOT NULL,
    ol_revision bigint DEFAULT 0 NOT NULL,
    ol_seen bigint DEFAULT 0 NOT NULL,
    ol_modified_at timestamp with time zone
);

CREATE TABLE aiosources.ol_import_references (
    id text NOT NULL
);

CREATE TABLE aiosources.ol_import_state (
    singleton boolean DEFAULT true NOT NULL,
    snapshot text DEFAULT ''::text NOT NULL,
    etag text DEFAULT ''::text NOT NULL,
    "position" bigint DEFAULT 0 NOT NULL,
    imported bigint DEFAULT 0 NOT NULL,
    skipped bigint DEFAULT 0 NOT NULL,
    works bigint DEFAULT 0 NOT NULL,
    editions bigint DEFAULT 0 NOT NULL,
    authors bigint DEFAULT 0 NOT NULL,
    checked_at timestamp with time zone,
    complete boolean DEFAULT false NOT NULL,
    retry_at timestamp with time zone,
    last_sync timestamp with time zone,
    error text DEFAULT ''::text NOT NULL,
    generation bigint DEFAULT 0 NOT NULL,
    snapshot_at timestamp with time zone,
    prune_table integer DEFAULT 0 NOT NULL,
    prune_after text DEFAULT ''::text NOT NULL,
    data_complete boolean DEFAULT false NOT NULL,
    sync_failures integer DEFAULT 0 NOT NULL,
    selection text DEFAULT ''::text NOT NULL,
    export_phase integer DEFAULT 0 NOT NULL,
    phase_snapshot text DEFAULT ''::text NOT NULL,
    phase_etag text DEFAULT ''::text NOT NULL,
    phase_position bigint DEFAULT 0 NOT NULL,
    filtered bigint DEFAULT 0 NOT NULL,
    selection_rebuild boolean DEFAULT false NOT NULL,
    CONSTRAINT ol_import_state_export_phase_check CHECK (((export_phase >= 0) AND (export_phase <= 5))),
    CONSTRAINT ol_import_state_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.ol_redirects (
    source text NOT NULL,
    target text NOT NULL,
    ol_revision bigint DEFAULT 0 NOT NULL,
    ol_seen bigint DEFAULT 0 NOT NULL,
    ol_modified_at timestamp with time zone,
    CONSTRAINT ol_redirects_check CHECK ((source <> target))
);

CREATE TABLE aiosources.operation_downloads (
    run_id text NOT NULL,
    work_id bigint NOT NULL,
    input jsonb NOT NULL,
    account text NOT NULL,
    CONSTRAINT operation_downloads_input_check CHECK ((jsonb_typeof(input) = 'object'::text)),
    CONSTRAINT operation_downloads_work_id_check CHECK ((work_id > 0))
);

CREATE TABLE aiosources.operation_events (
    id bigint NOT NULL,
    run_id text NOT NULL,
    phase text NOT NULL,
    processed bigint NOT NULL,
    errors bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    duration_ms bigint DEFAULT 0 NOT NULL,
    http_status integer DEFAULT 0 NOT NULL,
    error_code text DEFAULT ''::text NOT NULL
);

CREATE SEQUENCE aiosources.operation_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE aiosources.operation_events_id_seq OWNED BY aiosources.operation_events.id;

CREATE TABLE aiosources.operation_runs (
    id text NOT NULL,
    kind text NOT NULL,
    service text NOT NULL,
    trigger text NOT NULL,
    resource text DEFAULT ''::text NOT NULL,
    lock_key bigint NOT NULL,
    status text NOT NULL,
    phase text DEFAULT 'configuration'::text NOT NULL,
    processed bigint DEFAULT 0 NOT NULL,
    errors bigint DEFAULT 0 NOT NULL,
    results jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    error text DEFAULT ''::text NOT NULL,
    CONSTRAINT operation_runs_check CHECK (((status = 'running'::text) = (finished_at IS NULL))),
    CONSTRAINT operation_runs_errors_check CHECK ((errors >= 0)),
    CONSTRAINT operation_runs_kind_check CHECK ((kind = ANY (ARRAY['metadata'::text, 'matching'::text, 'cache'::text, 'cleanup'::text, 'download'::text]))),
    CONSTRAINT operation_runs_kind_service_check CHECK ((((kind = 'metadata'::text) AND (service = ANY (ARRAY['tmdb'::text, 'openlibrary'::text]))) OR ((kind = 'matching'::text) AND (service = 'local'::text)) OR ((kind = ANY (ARRAY['cache'::text, 'download'::text])) AND (service = ANY (ARRAY['alldebrid'::text, 'debridlink'::text, 'deepbrid'::text, 'premiumize'::text, 'realdebrid'::text, 'torbox'::text]))) OR ((kind = 'cleanup'::text) AND (service = 'alldebrid'::text)))),
    CONSTRAINT operation_runs_processed_check CHECK ((processed >= 0)),
    CONSTRAINT operation_runs_service_check CHECK ((service = ANY (ARRAY['tmdb'::text, 'openlibrary'::text, 'local'::text, 'alldebrid'::text, 'debridlink'::text, 'deepbrid'::text, 'premiumize'::text, 'realdebrid'::text, 'torbox'::text]))),
    CONSTRAINT operation_runs_status_check CHECK ((status = ANY (ARRAY['running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text, 'interrupted'::text]))),
    CONSTRAINT operation_runs_trigger_check CHECK ((trigger = ANY (ARRAY['manual'::text, 'automatic'::text, 'playback'::text, 'public'::text])))
);

CREATE TABLE aiosources.pages (
    id bigint NOT NULL,
    run_id text NOT NULL,
    provider_id text NOT NULL,
    page_index integer NOT NULL,
    body bytea NOT NULL,
    content_type text NOT NULL,
    fingerprint text,
    "position" text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    done boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    reset_staging boolean DEFAULT false NOT NULL,
    require_unique_ids boolean DEFAULT false NOT NULL,
    payload_retained boolean DEFAULT true NOT NULL,
    payload_id bigint,
    CONSTRAINT pages_payload_reference CHECK (((payload_id IS NULL) OR ((payload_id < id) AND payload_retained AND (octet_length(body) = 0))))
);

ALTER TABLE aiosources.pages ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.pages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.provider_requests (
    provider_id text NOT NULL,
    admitted_at timestamp with time zone NOT NULL
);

CREATE TABLE aiosources.public_catalog_policy (
    singleton boolean DEFAULT true NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    source_ids text[] DEFAULT '{}'::text[] NOT NULL,
    fields text[] DEFAULT ARRAY['title'::text, 'size'::text, 'info_hash'::text, 'published_at'::text, 'category'::text, 'categories'::text] NOT NULL,
    catalog_entities jsonb DEFAULT '{"files": true, "metadata": true, "languages": true, "technical": true, "identifiers": true, "availability": true}'::jsonb NOT NULL,
    CONSTRAINT public_catalog_policy_catalog_entities_check CHECK ((jsonb_typeof(catalog_entities) = 'object'::text)),
    CONSTRAINT public_catalog_policy_fields_check CHECK (((cardinality(fields) > 0) AND (fields <@ ARRAY['title'::text, 'size'::text, 'info_hash'::text, 'published_at'::text, 'category'::text, 'categories'::text]))),
    CONSTRAINT public_catalog_policy_revision_check CHECK ((revision > 0)),
    CONSTRAINT public_catalog_policy_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.raw_records (
    id bigint NOT NULL,
    run_id text NOT NULL,
    provider_id text NOT NULL,
    source_id text NOT NULL,
    page_id bigint NOT NULL,
    page_index integer NOT NULL,
    raw bytea NOT NULL,
    content_type text NOT NULL,
    fields jsonb DEFAULT '{}'::jsonb NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    ignored boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    auxiliary boolean DEFAULT false NOT NULL,
    payload_retained boolean DEFAULT true NOT NULL,
    payload_id bigint,
    CONSTRAINT raw_records_fields_check CHECK ((jsonb_typeof(fields) = 'object'::text)),
    CONSTRAINT raw_records_payload_reference CHECK (((payload_id IS NULL) OR ((payload_id < id) AND payload_retained AND (octet_length(raw) = 0) AND (fields = '{}'::jsonb))))
);

ALTER TABLE aiosources.raw_records ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.raw_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.remote_checkpoints (
    provider_id text NOT NULL,
    endpoint text NOT NULL,
    checkpoint text NOT NULL,
    last_synced_at timestamp with time zone DEFAULT now() NOT NULL,
    catalog_entities jsonb DEFAULT '{}'::jsonb NOT NULL
);

CREATE TABLE aiosources.run_catalog_entities (
    run_id text NOT NULL,
    kind text NOT NULL,
    identity text NOT NULL,
    data jsonb NOT NULL,
    CONSTRAINT run_catalog_entities_kind_check CHECK ((kind = ANY (ARRAY['metadata'::text, 'availability'::text, 'torrent'::text])))
);

CREATE TABLE aiosources.run_filter_records (
    page_id bigint NOT NULL,
    source_id text NOT NULL
);

CREATE TABLE aiosources.run_progress_identities (
    run_id text NOT NULL,
    source_id text NOT NULL
);

CREATE TABLE aiosources.run_record_identities (
    run_id text NOT NULL,
    source_id text NOT NULL,
    CONSTRAINT run_record_identities_source_id_check CHECK ((source_id <> ''::text))
);

CREATE TABLE aiosources.run_scope_counts (
    run_id text NOT NULL,
    scope_id text NOT NULL,
    observed_count bigint NOT NULL,
    CONSTRAINT run_scope_counts_observed_count_check CHECK ((observed_count >= 0))
);

CREATE TABLE aiosources.run_scope_memberships (
    run_id text NOT NULL,
    scope_id text NOT NULL,
    source_id text NOT NULL
);

CREATE TABLE aiosources.run_scope_refresh_candidates (
    run_id text NOT NULL,
    source_id text NOT NULL
);

CREATE TABLE aiosources.runs (
    id text NOT NULL,
    provider_id text NOT NULL,
    provider_name text NOT NULL,
    mode text NOT NULL,
    status text NOT NULL,
    cancel_requested boolean DEFAULT false NOT NULL,
    max_pages integer DEFAULT 0 NOT NULL,
    pages integer DEFAULT 0 NOT NULL,
    records integer DEFAULT 0 NOT NULL,
    errors integer DEFAULT 0 NOT NULL,
    revision text NOT NULL,
    config jsonb NOT NULL,
    cursor jsonb,
    traversal_done boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    error text DEFAULT ''::text NOT NULL,
    trigger text DEFAULT 'manual'::text NOT NULL,
    distinct_records bigint DEFAULT 0 NOT NULL,
    pause_requested boolean DEFAULT false NOT NULL,
    policy jsonb,
    pause_reason text DEFAULT ''::text NOT NULL,
    requests_without_new_ids bigint DEFAULT 0 NOT NULL,
    progress_warning_sent boolean DEFAULT false NOT NULL,
    known_page_streak integer DEFAULT 0 NOT NULL,
    metadata_parent_run_id text,
    initial_full boolean DEFAULT false NOT NULL,
    rejected_primary boolean DEFAULT false NOT NULL,
    coverage_attempt text DEFAULT ''::text NOT NULL,
    storage_mode text DEFAULT 'compact'::text NOT NULL,
    remote_catalog boolean GENERATED ALWAYS AS (((config @> '{"http": {"catalog": true}, "adapter": "json"}'::jsonb) OR (config @> '{"http": {"catalog": true}, "adapter": "http_json"}'::jsonb))) STORED,
    CONSTRAINT runs_config_check CHECK ((jsonb_typeof(config) = 'object'::text)),
    CONSTRAINT runs_distinct_records_check CHECK ((distinct_records >= 0)),
    CONSTRAINT runs_errors_check CHECK ((errors >= 0)),
    CONSTRAINT runs_initial_full_mode CHECK (((NOT initial_full) OR (mode = 'full'::text))),
    CONSTRAINT runs_known_page_streak_check CHECK ((known_page_streak >= 0)),
    CONSTRAINT runs_manual_pause_state CHECK (((NOT pause_requested) OR ((status = ANY (ARRAY['running'::text, 'paused'::text, 'failed'::text])) AND (NOT cancel_requested)))),
    CONSTRAINT runs_max_pages_check CHECK ((max_pages >= 0)),
    CONSTRAINT runs_metadata_parent_mode CHECK (((metadata_parent_run_id IS NULL) OR (mode = 'metadata'::text))),
    CONSTRAINT runs_mode_check CHECK ((mode = ANY (ARRAY['preview'::text, 'incremental'::text, 'full'::text, 'metadata'::text]))),
    CONSTRAINT runs_pages_check CHECK ((pages >= 0)),
    CONSTRAINT runs_pause_reason_check CHECK ((pause_reason = ANY (ARRAY[''::text, 'manual'::text, 'budget'::text, 'quota'::text, 'no_progress'::text, 'interrupted'::text]))),
    CONSTRAINT runs_policy_check CHECK (((policy IS NULL) OR (jsonb_typeof(policy) = 'object'::text))),
    CONSTRAINT runs_records_check CHECK ((records >= 0)),
    CONSTRAINT runs_requests_without_new_ids_check CHECK ((requests_without_new_ids >= 0)),
    CONSTRAINT runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'paused'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text]))),
    CONSTRAINT runs_storage_mode_check CHECK ((storage_mode = ANY (ARRAY['compact'::text, 'full'::text]))),
    CONSTRAINT runs_trigger_check CHECK ((trigger = ANY (ARRAY['manual'::text, 'scheduled'::text])))
);

CREATE TABLE aiosources.runtime_settings (
    section text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    "values" jsonb NOT NULL,
    CONSTRAINT runtime_settings_revision_check CHECK ((revision > 0)),
    CONSTRAINT runtime_settings_section_check CHECK ((section = ANY (ARRAY['debrid'::text, 'outgoing'::text, 'connections'::text, 'history'::text, 'identification'::text, 'matching'::text, 'metadata'::text, 'metadata_retries'::text, 'source_retries'::text, 'debrid_retries'::text, 'downloads'::text, 'security'::text, 'payloads'::text]))),
    CONSTRAINT runtime_settings_values_check CHECK ((jsonb_typeof("values") = 'object'::text))
);

CREATE TABLE aiosources.schedule_clocks (
    provider_id text NOT NULL,
    revision text NOT NULL,
    active boolean NOT NULL,
    next_run_at timestamp with time zone,
    last_attempt_at timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    full_due_at timestamp with time zone,
    profile text DEFAULT 'source'::text NOT NULL,
    CONSTRAINT schedule_clocks_check CHECK ((active = (next_run_at IS NOT NULL))),
    CONSTRAINT schedule_clocks_profile_check CHECK ((profile = ANY (ARRAY['source'::text, 'catalog'::text])))
);

CREATE SEQUENCE aiosources.secret_revisions
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE aiosources.secrets (
    name text NOT NULL,
    ciphertext bytea NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    revision bigint DEFAULT nextval('aiosources.secret_revisions'::regclass) NOT NULL,
    CONSTRAINT secrets_ciphertext_check CHECK ((octet_length(ciphertext) > 0)),
    CONSTRAINT secrets_revision_check CHECK ((revision > 0))
);

CREATE TABLE aiosources.security_attempts (
    bucket text NOT NULL,
    window_start timestamp with time zone NOT NULL,
    attempts integer NOT NULL
);

CREATE TABLE aiosources.settings (
    key text NOT NULL,
    value text NOT NULL
);

CREATE TABLE aiosources.share_requests (
    id text NOT NULL,
    share_id text NOT NULL,
    requested_at timestamp with time zone NOT NULL,
    lease_until timestamp with time zone
);

CREATE TABLE aiosources.shares (
    id text NOT NULL,
    name text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    scope text NOT NULL,
    source_ids text[] NOT NULL,
    fields text[] NOT NULL,
    password_hash bytea NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_access_at timestamp with time zone,
    last_sync_at timestamp with time zone,
    expires_at timestamp with time zone,
    requests_per_minute integer DEFAULT 120 NOT NULL,
    max_concurrent_downloads integer DEFAULT 2 NOT NULL,
    format text DEFAULT 'catalog'::text NOT NULL,
    media_types text[] DEFAULT '{}'::text[] NOT NULL,
    stream_preferences jsonb DEFAULT '{}'::jsonb NOT NULL,
    torznab_preferences jsonb DEFAULT '{}'::jsonb NOT NULL,
    catalog_entities jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT shares_catalog_entities_check CHECK ((jsonb_typeof(catalog_entities) = 'object'::text)),
    CONSTRAINT shares_catalog_media CHECK (((format <> 'catalog'::text) OR (cardinality(media_types) = 0))),
    CONSTRAINT shares_check CHECK ((((scope = 'all'::text) AND (cardinality(source_ids) = 0)) OR ((scope = 'selected'::text) AND (cardinality(source_ids) > 0)))),
    CONSTRAINT shares_check1 CHECK ((((requests_per_minute >= 0) AND (requests_per_minute <= 3600)) AND ((format <> 'catalog'::text) OR (requests_per_minute > 0)))),
    CONSTRAINT shares_check2 CHECK ((((max_concurrent_downloads >= 0) AND (max_concurrent_downloads <= 16)) AND ((format <> 'catalog'::text) OR (max_concurrent_downloads > 0)))),
    CONSTRAINT shares_fields_allowed CHECK ((((cardinality(fields) > 0) OR (NOT enabled)) AND (fields <@ ARRAY['title'::text, 'size'::text, 'info_hash'::text, 'published_at'::text, 'category'::text, 'categories'::text]))),
    CONSTRAINT shares_format_check CHECK ((format = ANY (ARRAY['catalog'::text, 'torznab'::text, 'stremio'::text]))),
    CONSTRAINT shares_media_types_check CHECK ((media_types <@ ARRAY['movie'::text, 'tv'::text, 'book'::text, 'ebook'::text, 'audiobook'::text, 'other'::text])),
    CONSTRAINT shares_password_hash_check CHECK ((octet_length(password_hash) = 32)),
    CONSTRAINT shares_revision_check CHECK ((revision > 0)),
    CONSTRAINT shares_scope_check CHECK ((scope = ANY (ARRAY['selected'::text, 'all'::text]))),
    CONSTRAINT shares_stream_preferences_check CHECK ((jsonb_typeof(stream_preferences) = 'object'::text)),
    CONSTRAINT shares_torznab_preferences_check CHECK ((jsonb_typeof(torznab_preferences) = 'object'::text))
);

CREATE TABLE aiosources.staged_torrents (
    run_id text NOT NULL,
    provider_id text NOT NULL,
    source_id text NOT NULL,
    fields jsonb DEFAULT '{}'::jsonb NOT NULL,
    raw_id bigint,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    origin jsonb,
    deleted boolean DEFAULT false NOT NULL,
    CONSTRAINT staged_torrents_fields_check CHECK ((jsonb_typeof(fields) = 'object'::text))
);

CREATE TABLE aiosources.storage_settings (
    singleton boolean DEFAULT true NOT NULL,
    mode text DEFAULT 'compact'::text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    CONSTRAINT storage_settings_mode_check CHECK ((mode = ANY (ARRAY['compact'::text, 'full'::text]))),
    CONSTRAINT storage_settings_revision_check CHECK ((revision > 0)),
    CONSTRAINT storage_settings_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.stremio_requests (
    id bigint NOT NULL,
    created_at timestamp with time zone NOT NULL,
    media_type text NOT NULL,
    resource text NOT NULL,
    duration_ms bigint NOT NULL,
    outcome text NOT NULL,
    matched integer NOT NULL,
    eligible integer NOT NULL,
    invalid_hash integer NOT NULL,
    wrong_episode integer NOT NULL,
    private_unavailable integer NOT NULL,
    file_unavailable integer NOT NULL,
    returned integer NOT NULL,
    truncated boolean NOT NULL,
    results jsonb NOT NULL,
    origin text DEFAULT 'private'::text NOT NULL,
    kind text DEFAULT 'stream'::text NOT NULL,
    service text DEFAULT ''::text NOT NULL,
    calls jsonb DEFAULT '[]'::jsonb NOT NULL,
    language_excluded integer DEFAULT 0 NOT NULL,
    CONSTRAINT stremio_requests_calls_check CHECK ((jsonb_typeof(calls) = 'array'::text)),
    CONSTRAINT stremio_requests_duration_ms_check CHECK ((duration_ms >= 0)),
    CONSTRAINT stremio_requests_eligible_check CHECK ((eligible >= 0)),
    CONSTRAINT stremio_requests_file_unavailable_check CHECK ((file_unavailable >= 0)),
    CONSTRAINT stremio_requests_invalid_hash_check CHECK ((invalid_hash >= 0)),
    CONSTRAINT stremio_requests_kind_check CHECK ((kind = ANY (ARRAY['stream'::text, 'manifest'::text, 'playback'::text]))),
    CONSTRAINT stremio_requests_language_excluded_check CHECK ((language_excluded >= 0)),
    CONSTRAINT stremio_requests_matched_check CHECK ((matched >= 0)),
    CONSTRAINT stremio_requests_origin_check CHECK ((origin = ANY (ARRAY['private'::text, 'public'::text]))),
    CONSTRAINT stremio_requests_outcome_check CHECK ((outcome = ANY (ARRAY['results'::text, 'empty'::text, 'invalid'::text, 'failed'::text, 'cancelled'::text, 'busy'::text, 'pending'::text]))),
    CONSTRAINT stremio_requests_private_unavailable_check CHECK ((private_unavailable >= 0)),
    CONSTRAINT stremio_requests_results_check CHECK ((jsonb_typeof(results) = 'array'::text)),
    CONSTRAINT stremio_requests_returned_check CHECK ((returned >= 0)),
    CONSTRAINT stremio_requests_wrong_episode_check CHECK ((wrong_episode >= 0))
);

ALTER TABLE aiosources.stremio_requests ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.stremio_requests_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.torrent_history (
    id bigint NOT NULL,
    occurrence_id text NOT NULL,
    provider_id text NOT NULL,
    source_id text NOT NULL,
    run_id text,
    kind text NOT NULL,
    occurred_at timestamp with time zone DEFAULT statement_timestamp() NOT NULL,
    before_fields jsonb,
    after_fields jsonb,
    origin jsonb NOT NULL,
    before_origin jsonb,
    CONSTRAINT torrent_history_after_fields_check CHECK ((jsonb_typeof(after_fields) = 'object'::text)),
    CONSTRAINT torrent_history_before_fields_check CHECK ((jsonb_typeof(before_fields) = 'object'::text)),
    CONSTRAINT torrent_history_before_origin_check CHECK ((jsonb_typeof(before_origin) = 'object'::text)),
    CONSTRAINT torrent_history_check CHECK ((((kind = 'added'::text) AND (before_fields IS NULL) AND (after_fields IS NOT NULL)) OR ((kind = 'updated'::text) AND (before_fields IS NOT NULL) AND (after_fields IS NOT NULL)) OR ((kind = 'deleted'::text) AND (before_fields IS NOT NULL) AND (after_fields IS NULL)))),
    CONSTRAINT torrent_history_kind_check CHECK ((kind = ANY (ARRAY['added'::text, 'updated'::text, 'deleted'::text]))),
    CONSTRAINT torrent_history_origin_check CHECK ((jsonb_typeof(origin) = 'object'::text))
);

CREATE TABLE aiosources.torrent_history_baselines (
    occurrence_id text NOT NULL,
    provider_id text NOT NULL,
    source_id text NOT NULL,
    fields jsonb,
    origin jsonb NOT NULL,
    deleted boolean NOT NULL,
    recorded_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT torrent_history_baselines_fields_check CHECK ((jsonb_typeof(fields) = 'object'::text)),
    CONSTRAINT torrent_history_baselines_origin_check CHECK ((jsonb_typeof(origin) = 'object'::text))
);

ALTER TABLE aiosources.torrent_history ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME aiosources.torrent_history_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE aiosources.torrent_history_metadata (
    singleton boolean DEFAULT true NOT NULL,
    history_since timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT torrent_history_metadata_singleton_check CHECK (singleton)
);

CREATE TABLE aiosources.torrents (
    provider_id text NOT NULL,
    source_id text NOT NULL,
    fields jsonb DEFAULT '{}'::jsonb NOT NULL,
    raw_id bigint,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    historical boolean DEFAULT false NOT NULL,
    origin jsonb,
    occurrence_id text GENERATED ALWAYS AS (aiosources.torrent_occurrence_id(provider_id, source_id)) STORED,
    search_document tsvector GENERATED ALWAYS AS (aiosources.torrent_search_vector(provider_id, source_id, fields)) STORED,
    search_size numeric GENERATED ALWAYS AS (aiosources.torrent_integer((fields -> 'size'::text))) STORED,
    search_published_at timestamp with time zone GENERATED ALWAYS AS (aiosources.torrent_timestamp((fields -> 'published_at'::text))) STORED,
    search_categories text[] GENERATED ALWAYS AS (aiosources.torrent_categories(fields)) STORED,
    search_info_hash text GENERATED ALWAYS AS (aiosources.torrent_hash((fields -> 'info_hash'::text))) STORED,
    collection_verified_at timestamp with time zone,
    fields_fingerprint text GENERATED ALWAYS AS (md5((fields)::text)) STORED,
    catalog_details_tx bigint DEFAULT 0 NOT NULL,
    CONSTRAINT torrents_check CHECK (((NOT historical) OR (raw_id IS NULL))),
    CONSTRAINT torrents_fields_check CHECK ((jsonb_typeof(fields) = 'object'::text))
);

ALTER TABLE ONLY aiosources.operation_events ALTER COLUMN id SET DEFAULT nextval('aiosources.operation_events_id_seq'::regclass);

INSERT INTO aiosources.admin_security VALUES (true, NULL, NULL, NULL, NULL, NULL, -1, '{}');

INSERT INTO aiosources.aio_public_access VALUES (true, 1, false, 1, false, false, 1, false, true, 1, false, 1);

INSERT INTO aiosources.aio_state VALUES (true, NULL, NULL, 1, 1, '', NULL, 0, '', NULL, NULL, NULL, NULL);

INSERT INTO aiosources.catalog_identity(singleton,instance_id,signing_key) VALUES (true,gen_random_uuid()::text,decode(replace(gen_random_uuid()::text || gen_random_uuid()::text || gen_random_uuid()::text,'-',''),'hex'));

INSERT INTO aiosources.catalog_retention VALUES (true, 0);

INSERT INTO aiosources.catalog_tracking VALUES (true, false);

INSERT INTO aiosources.collection_settings VALUES (true, NULL, 3, 3, 0, 1000, 'warn', true);

INSERT INTO aiosources.network_settings VALUES (true, 2, 'system', '[]', '{}', '{"format": "chain", "enabled": false, "ip_header": "X-Forwarded-For", "trusted_proxies": []}', '{"concurrent": 50, "setup_per_minute": 12, "requests_per_minute": 120}');

INSERT INTO aiosources.ol_changes_state VALUES (true, '', NULL, NULL, '', NULL, false, NULL, NULL, NULL, NULL, NULL, NULL, '', NULL);

INSERT INTO aiosources.ol_import_state VALUES (true, '', '', 0, 0, 0, 0, 0, 0, NULL, false, NULL, NULL, '', 0, NULL, 0, '', false, 0, '', 0, '', '', 0, 0, false);

INSERT INTO aiosources.public_catalog_policy VALUES (true, 1, '{}', '{title,size,info_hash,published_at,category,categories}', '{"files": true, "metadata": true, "languages": true, "technical": true, "identifiers": true, "availability": true}');

INSERT INTO aiosources.runtime_settings VALUES ('debrid', 1, '{"cache_jobs": 4, "cleanup_jobs": 4, "download_jobs": 4, "public_checks": 1}');
INSERT INTO aiosources.runtime_settings VALUES ('outgoing', 1, '{"debrid_seconds": 60, "metadata_seconds": 60, "inventory_seconds": 600}');
INSERT INTO aiosources.runtime_settings VALUES ('connections', 1, '{"catalog_seconds": 20, "stremio_seconds": 70, "torznab_seconds": 20}');
INSERT INTO aiosources.runtime_settings VALUES ('history', 2, '{"days": 7, "batch_size": 5000, "pass_seconds": 10, "cleanup_seconds": 30}');
INSERT INTO aiosources.runtime_settings VALUES ('identification', 1, '{"workers": 0, "queue_batch": 250, "transaction_batch": 25}');
INSERT INTO aiosources.runtime_settings VALUES ('matching', 1, '{"ambiguity_margin": 8, "minimum_similarity": 92}');
INSERT INTO aiosources.runtime_settings VALUES ('metadata', 1, '{"workers": 16, "queue_batch": 100, "publication_batch": 16}');
INSERT INTO aiosources.runtime_settings VALUES ('metadata_retries', 1, '{"request_attempts": 4, "sync_retry_seconds": 60, "detail_retry_seconds": 60, "request_retry_seconds": 1, "sync_retry_max_seconds": 900, "detail_retry_max_seconds": 86400}');
INSERT INTO aiosources.runtime_settings VALUES ('source_retries', 1, '{"retries": 3, "initial_seconds": 2, "maximum_seconds": 8}');
INSERT INTO aiosources.runtime_settings VALUES ('debrid_retries', 1, '{"delay_minutes": 15, "maximum_hours": 48}');
INSERT INTO aiosources.runtime_settings VALUES ('downloads', 1, '{"duration_hours": 24, "ticket_minutes": 10}');
INSERT INTO aiosources.runtime_settings VALUES ('security', 1, '{"session_hours": 12, "session_limit": 256, "login_per_minute": 10, "enrollment_minutes": 10, "login_global_per_minute": 60}');
INSERT INTO aiosources.runtime_settings VALUES ('payloads', 1, '{"source_kib": 128, "public_stremio_bytes": 8192, "public_torznab_bytes": 6000}');

INSERT INTO aiosources.storage_settings VALUES (true, 'compact', 1);

INSERT INTO aiosources.torrent_history_metadata VALUES (true,now());

SELECT pg_catalog.setval('aiosources.activity_logs_id_seq', 1, false);

SELECT pg_catalog.setval('aiosources.aio_works_id_seq', 1, false);

SELECT pg_catalog.setval('aiosources.catalog_journal_sequence_seq', 1, false);

SELECT pg_catalog.setval('aiosources.events_id_seq', 1, false);

SELECT pg_catalog.setval('aiosources.operation_events_id_seq', 1, false);

SELECT pg_catalog.setval('aiosources.pages_id_seq', 1, false);

SELECT pg_catalog.setval('aiosources.raw_records_id_seq', 1, false);

SELECT pg_catalog.setval('aiosources.secret_revisions', 1, false);

SELECT pg_catalog.setval('aiosources.stremio_requests_id_seq', 1, false);

SELECT pg_catalog.setval('aiosources.torrent_history_id_seq', 1, false);

ALTER TABLE ONLY aiosources.activity_logs
    ADD CONSTRAINT activity_logs_event_id_key UNIQUE (event_id);

ALTER TABLE ONLY aiosources.activity_logs
    ADD CONSTRAINT activity_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.activity_logs
    ADD CONSTRAINT activity_logs_source_event_id_key UNIQUE (source_event_id);

ALTER TABLE ONLY aiosources.admin_security
    ADD CONSTRAINT admin_security_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.admin_sessions
    ADD CONSTRAINT admin_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.admin_sessions
    ADD CONSTRAINT admin_sessions_token_hash_key UNIQUE (token_hash);

ALTER TABLE ONLY aiosources.aio_availability
    ADD CONSTRAINT aio_availability_pkey PRIMARY KEY (service, account, hash);

ALTER TABLE ONLY aiosources.aio_cleanup
    ADD CONSTRAINT aio_cleanup_pkey PRIMARY KEY (account, magnet_id);

ALTER TABLE ONLY aiosources.aio_episodes
    ADD CONSTRAINT aio_episodes_pkey PRIMARY KEY (work_id, season, episode);

ALTER TABLE ONLY aiosources.aio_incidents
    ADD CONSTRAINT aio_incidents_pkey PRIMARY KEY (service);

ALTER TABLE ONLY aiosources.aio_inventory
    ADD CONSTRAINT aio_inventory_pkey PRIMARY KEY (media_type, tmdb_id);

ALTER TABLE ONLY aiosources.aio_match_queue
    ADD CONSTRAINT aio_match_queue_pkey PRIMARY KEY (provider_id, source_id);

ALTER TABLE ONLY aiosources.aio_match_reviews
    ADD CONSTRAINT aio_match_reviews_pkey PRIMARY KEY (provider_id, source_id);

ALTER TABLE ONLY aiosources.aio_matches
    ADD CONSTRAINT aio_matches_pkey PRIMARY KEY (provider_id, source_id);

ALTER TABLE ONLY aiosources.aio_metadata_jobs
    ADD CONSTRAINT aio_metadata_jobs_pkey PRIMARY KEY (media_type, tmdb_id);

ALTER TABLE ONLY aiosources.aio_preparations
    ADD CONSTRAINT aio_preparations_pkey PRIMARY KEY (service, account, hash);

ALTER TABLE ONLY aiosources.aio_public_access
    ADD CONSTRAINT aio_public_access_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.aio_resources
    ADD CONSTRAINT aio_resources_pkey PRIMARY KEY (service, account, resource);

ALTER TABLE ONLY aiosources.aio_season_requests
    ADD CONSTRAINT aio_season_requests_pkey PRIMARY KEY (tmdb_id, season);

ALTER TABLE ONLY aiosources.aio_seasons
    ADD CONSTRAINT aio_seasons_pkey PRIMARY KEY (work_id, season);

ALTER TABLE ONLY aiosources.aio_shared_availability
    ADD CONSTRAINT aio_shared_availability_pkey PRIMARY KEY (service, hash);

ALTER TABLE aiosources.aio_shared_availability
    ADD CONSTRAINT aio_shared_availability_service_check CHECK ((service = ANY (ARRAY['alldebrid'::text, 'debridlink'::text, 'deepbrid'::text, 'premiumize'::text, 'realdebrid'::text, 'torbox'::text]))) NOT VALID;

ALTER TABLE ONLY aiosources.aio_state
    ADD CONSTRAINT aio_state_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.aio_titles
    ADD CONSTRAINT aio_titles_pkey PRIMARY KEY (work_id, title);

ALTER TABLE ONLY aiosources.aio_works
    ADD CONSTRAINT aio_works_media_type_tmdb_id_key UNIQUE (media_type, tmdb_id);

ALTER TABLE ONLY aiosources.aio_works
    ADD CONSTRAINT aio_works_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.catalog_baselines
    ADD CONSTRAINT catalog_baselines_pkey PRIMARY KEY (provider_id, source_id, origin);

ALTER TABLE ONLY aiosources.catalog_entity_changes
    ADD CONSTRAINT catalog_entity_changes_pkey PRIMARY KEY (kind, identity);

ALTER TABLE ONLY aiosources.catalog_identity
    ADD CONSTRAINT catalog_identity_instance_id_key UNIQUE (instance_id);

ALTER TABLE ONLY aiosources.catalog_identity
    ADD CONSTRAINT catalog_identity_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.catalog_journal
    ADD CONSTRAINT catalog_journal_pkey PRIMARY KEY (sequence);

ALTER TABLE ONLY aiosources.catalog_retention
    ADD CONSTRAINT catalog_retention_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.catalog_tracking
    ADD CONSTRAINT catalog_tracking_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.collection_settings
    ADD CONSTRAINT collection_settings_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.collection_state
    ADD CONSTRAINT collection_state_pkey PRIMARY KEY (provider_id);

ALTER TABLE ONLY aiosources.events
    ADD CONSTRAINT events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.legacy_imports
    ADD CONSTRAINT legacy_imports_pkey PRIMARY KEY (provider_id, source_id);

ALTER TABLE ONLY aiosources.network_settings
    ADD CONSTRAINT network_settings_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.ol_authors
    ADD CONSTRAINT ol_authors_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.ol_change_jobs
    ADD CONSTRAINT ol_change_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.ol_changes_state
    ADD CONSTRAINT ol_changes_state_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.ol_deleted
    ADD CONSTRAINT ol_deleted_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.ol_editions
    ADD CONSTRAINT ol_editions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.ol_import_references
    ADD CONSTRAINT ol_import_references_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.ol_import_state
    ADD CONSTRAINT ol_import_state_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.ol_redirects
    ADD CONSTRAINT ol_redirects_pkey PRIMARY KEY (source);

ALTER TABLE ONLY aiosources.operation_downloads
    ADD CONSTRAINT operation_downloads_pkey PRIMARY KEY (run_id);

ALTER TABLE ONLY aiosources.operation_events
    ADD CONSTRAINT operation_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.operation_runs
    ADD CONSTRAINT operation_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.pages
    ADD CONSTRAINT pages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.public_catalog_policy
    ADD CONSTRAINT public_catalog_policy_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.raw_records
    ADD CONSTRAINT raw_records_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.remote_checkpoints
    ADD CONSTRAINT remote_checkpoints_pkey PRIMARY KEY (provider_id);

ALTER TABLE ONLY aiosources.run_catalog_entities
    ADD CONSTRAINT run_catalog_entities_pkey PRIMARY KEY (run_id, kind, identity);

ALTER TABLE ONLY aiosources.run_filter_records
    ADD CONSTRAINT run_filter_records_pkey PRIMARY KEY (page_id, source_id);

ALTER TABLE ONLY aiosources.run_progress_identities
    ADD CONSTRAINT run_progress_identities_pkey PRIMARY KEY (run_id, source_id);

ALTER TABLE ONLY aiosources.run_record_identities
    ADD CONSTRAINT run_record_identities_pkey PRIMARY KEY (run_id, source_id);

ALTER TABLE ONLY aiosources.run_scope_counts
    ADD CONSTRAINT run_scope_counts_pkey PRIMARY KEY (run_id, scope_id);

ALTER TABLE ONLY aiosources.run_scope_memberships
    ADD CONSTRAINT run_scope_memberships_pkey PRIMARY KEY (run_id, scope_id, source_id);

ALTER TABLE ONLY aiosources.run_scope_refresh_candidates
    ADD CONSTRAINT run_scope_refresh_candidates_pkey PRIMARY KEY (run_id, source_id);

ALTER TABLE ONLY aiosources.runs
    ADD CONSTRAINT runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.runtime_settings
    ADD CONSTRAINT runtime_settings_pkey PRIMARY KEY (section);

ALTER TABLE ONLY aiosources.schedule_clocks
    ADD CONSTRAINT schedule_clocks_pkey PRIMARY KEY (provider_id);

ALTER TABLE ONLY aiosources.secrets
    ADD CONSTRAINT secrets_pkey PRIMARY KEY (name);

ALTER TABLE ONLY aiosources.security_attempts
    ADD CONSTRAINT security_attempts_pkey PRIMARY KEY (bucket);

ALTER TABLE ONLY aiosources.settings
    ADD CONSTRAINT settings_pkey PRIMARY KEY (key);

ALTER TABLE ONLY aiosources.share_requests
    ADD CONSTRAINT share_requests_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.shares
    ADD CONSTRAINT shares_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.staged_torrents
    ADD CONSTRAINT staged_torrents_pkey PRIMARY KEY (run_id, source_id);

ALTER TABLE ONLY aiosources.storage_settings
    ADD CONSTRAINT storage_settings_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.stremio_requests
    ADD CONSTRAINT stremio_requests_pkey PRIMARY KEY (id);

ALTER TABLE aiosources.stremio_requests
    ADD CONSTRAINT stremio_requests_service_check CHECK ((service = ANY (ARRAY[''::text, 'alldebrid'::text, 'debridlink'::text, 'deepbrid'::text, 'premiumize'::text, 'realdebrid'::text, 'torbox'::text]))) NOT VALID;

ALTER TABLE ONLY aiosources.torrent_history_baselines
    ADD CONSTRAINT torrent_history_baselines_pkey PRIMARY KEY (occurrence_id);

ALTER TABLE ONLY aiosources.torrent_history_metadata
    ADD CONSTRAINT torrent_history_metadata_pkey PRIMARY KEY (singleton);

ALTER TABLE ONLY aiosources.torrent_history
    ADD CONSTRAINT torrent_history_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aiosources.torrents
    ADD CONSTRAINT torrents_pkey PRIMARY KEY (provider_id, source_id);

CREATE INDEX activity_level_recent ON aiosources.activity_logs USING btree (level, created_at DESC, id DESC);

CREATE INDEX activity_provider_recent ON aiosources.activity_logs USING btree (provider_id, created_at DESC, id DESC);

CREATE INDEX activity_recent ON aiosources.activity_logs USING btree (created_at DESC, id DESC);

CREATE INDEX activity_run_recent ON aiosources.activity_logs USING btree (run_id, created_at DESC, id DESC);

CREATE INDEX activity_service_recent ON aiosources.activity_logs USING btree (service, created_at DESC, id DESC) WHERE (service <> ''::text);

CREATE INDEX admin_sessions_expiry ON aiosources.admin_sessions USING btree (expires_at);

CREATE INDEX aio_match_reviews_work ON aiosources.aio_match_reviews USING btree (media_type, tmdb_id) WHERE active;

CREATE INDEX aio_matches_fuzzy ON aiosources.aio_matches USING gin (normalized public.gin_trgm_ops);

CREATE INDEX aio_matches_normalized ON aiosources.aio_matches USING btree (normalized);

CREATE INDEX aio_matches_published_work ON aiosources.aio_matches USING btree (work_id) WHERE ((status = 'matched'::text) AND (work_id IS NOT NULL));

CREATE INDEX aio_matches_work ON aiosources.aio_matches USING btree (work_id);

CREATE INDEX aio_metadata_jobs_due ON aiosources.aio_metadata_jobs USING btree (next_attempt, media_type, tmdb_id);

CREATE INDEX aio_preparations_expiry ON aiosources.aio_preparations USING btree (expires_at);

CREATE INDEX aio_titles_exact ON aiosources.aio_titles USING btree (normalized);

CREATE INDEX aio_titles_fuzzy ON aiosources.aio_titles USING gin (normalized public.gin_trgm_ops);

CREATE INDEX aio_torrents_hash ON aiosources.torrents USING btree (lower((fields ->> 'info_hash'::text)));

CREATE INDEX aio_torrents_imdb ON aiosources.torrents USING btree (regexp_replace((fields ->> 'imdb_id'::text), '^tt'::text, ''::text, 'i'::text)) WHERE ((fields ->> 'imdb_id'::text) IS NOT NULL);

CREATE INDEX aio_torrents_mal ON aiosources.torrents USING btree (((fields ->> 'mal_id'::text)));

CREATE INDEX aio_torrents_native_books ON aiosources.torrents USING gin (aiosources.aio_native_book_candidates(fields)) WHERE ((fields ?| ARRAY['isbn'::text, 'isbn_10'::text, 'isbn_13'::text, 'openlibrary'::text, 'openlibrary_id'::text]) OR ((fields -> 'attributes'::text) ?| ARRAY['isbn'::text, 'isbn10'::text, 'isbn13'::text, 'isbn_10'::text, 'isbn_13'::text, 'openlibrary'::text, 'openlibrary_id'::text]) OR ((fields -> 'external_ids'::text) ?| ARRAY['isbn'::text, 'isbn_10'::text, 'isbn_13'::text, 'openlibrary'::text, 'openlibrary_id'::text]) OR ((fields ->> 'title'::text) ~* '[0-9][0-9 -]{8,}[0-9X]'::text));

CREATE INDEX aio_torrents_native_mal ON aiosources.torrents USING gin (aiosources.aio_native_mal_candidates(fields)) WHERE ((fields ? 'mal_id'::text) OR (fields ? 'mal'::text) OR ((fields -> 'external_ids'::text) ?| ARRAY['mal'::text, 'mal_id'::text]) OR ((fields -> 'attributes'::text) ?| ARRAY['mal'::text, 'malid'::text]));

CREATE INDEX aio_torrents_tmdb ON aiosources.torrents USING btree (((fields ->> 'tmdb_id'::text))) WHERE ((fields ->> 'tmdb_id'::text) IS NOT NULL);

CREATE INDEX aio_torrents_tvdb ON aiosources.torrents USING btree (((fields ->> 'tvdb_id'::text))) WHERE ((fields ->> 'tvdb_id'::text) IS NOT NULL);

CREATE INDEX aio_works_book_authors ON aiosources.aio_works USING gin (((book_data -> 'author_keys'::text)));

CREATE INDEX aio_works_book_entries ON aiosources.aio_works USING btree (id) WHERE (media_type = 'book'::text);

CREATE INDEX aio_works_book_titles_exact ON aiosources.aio_works USING btree (book_normalized) WHERE (media_type = 'book'::text);

CREATE INDEX aio_works_book_titles_fuzzy ON aiosources.aio_works USING gin (book_normalized public.gin_trgm_ops) WHERE (media_type = 'book'::text);

CREATE INDEX aio_works_imdb ON aiosources.aio_works USING btree (imdb_id) WHERE (imdb_id <> ''::text);

CREATE UNIQUE INDEX aio_works_openlibrary ON aiosources.aio_works USING btree (openlibrary_id) WHERE (openlibrary_id <> ''::text);

CREATE INDEX aio_works_refresh ON aiosources.aio_works USING btree (updated_at, media_type, tmdb_id) WHERE (media_type = ANY (ARRAY['movie'::text, 'tv'::text]));

CREATE INDEX aio_works_tmdb ON aiosources.aio_works USING btree (tmdb_id);

CREATE INDEX aio_works_tvdb ON aiosources.aio_works USING btree (tvdb_id) WHERE (tvdb_id <> ''::text);

CREATE INDEX aio_works_video_entries ON aiosources.aio_works USING btree (id) WHERE (media_type = ANY (ARRAY['movie'::text, 'tv'::text]));

CREATE INDEX catalog_entity_changes_cursor ON aiosources.catalog_entity_changes USING btree (changed_tx, (((kind || ':'::text) || identity)));

CREATE INDEX catalog_journal_retention ON aiosources.catalog_journal USING btree (recorded_at, sequence);

CREATE INDEX catalog_journal_source_sequence ON aiosources.catalog_journal USING btree (provider_id, source_id, sequence DESC);

CREATE INDEX events_retention ON aiosources.events USING btree (created_at, id);

CREATE INDEX events_run ON aiosources.events USING btree (run_id, id);

CREATE INDEX ol_authors_names ON aiosources.ol_authors USING gin (normalized public.gin_trgm_ops);

CREATE INDEX ol_change_jobs_due ON aiosources.ol_change_jobs USING btree (changed_at DESC, id DESC, next_attempt);

CREATE INDEX ol_change_jobs_next_attempt ON aiosources.ol_change_jobs USING btree (next_attempt);

CREATE INDEX ol_editions_authors ON aiosources.ol_editions USING gin (author_keys);

CREATE INDEX ol_editions_exact ON aiosources.ol_editions USING btree (normalized);

CREATE INDEX ol_editions_fuzzy ON aiosources.ol_editions USING gin (normalized public.gin_trgm_ops);

CREATE INDEX ol_editions_isbn ON aiosources.ol_editions USING gin (isbn);

CREATE INDEX ol_editions_works ON aiosources.ol_editions USING gin (work_keys);

CREATE INDEX ol_redirects_target ON aiosources.ol_redirects USING btree (target);

CREATE INDEX operation_events_retention ON aiosources.operation_events USING btree (created_at, id);

CREATE INDEX operation_events_run ON aiosources.operation_events USING btree (run_id, id);

CREATE INDEX operation_runs_active ON aiosources.operation_runs USING btree (lock_key) WHERE (status = 'running'::text);

CREATE INDEX operation_runs_history ON aiosources.operation_runs USING btree (kind, created_at DESC, id DESC);

CREATE INDEX operation_runs_retention ON aiosources.operation_runs USING btree (finished_at, id) WHERE (status <> 'running'::text);

CREATE INDEX pages_filter_coverage ON aiosources.pages USING btree (run_id, ((metadata ->> 'fingerprint_scope'::text))) WHERE ((error = ''::text) AND ((metadata ->> 'traversal_phase'::text) = 'options_list'::text));

CREATE INDEX pages_payload ON aiosources.pages USING btree (payload_id) WHERE (payload_id IS NOT NULL);

CREATE INDEX pages_retention ON aiosources.pages USING btree (created_at, id);

CREATE INDEX pages_run ON aiosources.pages USING btree (run_id, id);

CREATE UNIQUE INDEX pages_success_fingerprint ON aiosources.pages USING btree (run_id, fingerprint) WHERE (fingerprint IS NOT NULL);

CREATE INDEX provider_requests_recent ON aiosources.provider_requests USING btree (provider_id, admitted_at DESC);

CREATE INDEX raw_records_filter_coverage ON aiosources.raw_records USING btree (page_id, source_id) WHERE ((error = ''::text) AND (NOT ignored) AND (NOT auxiliary));

CREATE INDEX raw_records_fragments ON aiosources.raw_records USING gin (aiosources.raw_search_text(source_id, fields, error) public.gin_trgm_ops);

CREATE INDEX raw_records_page ON aiosources.raw_records USING btree (page_id);

CREATE INDEX raw_records_payload ON aiosources.raw_records USING btree (payload_id) WHERE (payload_id IS NOT NULL);

CREATE INDEX raw_records_provider ON aiosources.raw_records USING btree (provider_id, id DESC);

CREATE INDEX raw_records_rejected_primary ON aiosources.raw_records USING btree (run_id, page_id) WHERE ((error <> ''::text) AND (NOT auxiliary));

CREATE INDEX raw_records_retention ON aiosources.raw_records USING btree (created_at, id);

CREATE INDEX raw_records_run ON aiosources.raw_records USING btree (run_id, id DESC);

CREATE INDEX raw_records_source ON aiosources.raw_records USING btree (provider_id, source_id);

CREATE INDEX run_scope_memberships_source ON aiosources.run_scope_memberships USING btree (run_id, source_id);

CREATE INDEX runs_catalog_history ON aiosources.runs USING btree (remote_catalog, created_at DESC, id DESC);

CREATE INDEX runs_manual_holds ON aiosources.runs USING btree (provider_id) WHERE pause_requested;

CREATE UNIQUE INDEX runs_one_active_provider ON aiosources.runs USING btree (provider_id) WHERE (status = ANY (ARRAY['queued'::text, 'running'::text]));

CREATE UNIQUE INDEX runs_one_metadata_followup ON aiosources.runs USING btree (metadata_parent_run_id);

CREATE INDEX runs_provider_recent ON aiosources.runs USING btree (provider_id, created_at DESC, id DESC);

CREATE INDEX runs_queue ON aiosources.runs USING btree (created_at, id) WHERE (status = 'queued'::text);

CREATE INDEX runs_recent ON aiosources.runs USING btree (created_at DESC, id DESC);

CREATE INDEX runs_retention ON aiosources.runs USING btree (finished_at, id) WHERE (status = ANY (ARRAY['succeeded'::text, 'failed'::text, 'cancelled'::text]));

CREATE INDEX runs_scheduled_paused ON aiosources.runs USING btree (provider_id, created_at, id) WHERE ((status = 'paused'::text) AND (trigger = 'scheduled'::text));

CREATE INDEX runs_successful_collection ON aiosources.runs USING btree (provider_id) WHERE ((status = 'succeeded'::text) AND (mode = ANY (ARRAY['full'::text, 'incremental'::text])));

CREATE INDEX share_requests_share_lease ON aiosources.share_requests USING btree (share_id, lease_until) WHERE (lease_until IS NOT NULL);

CREATE INDEX share_requests_share_time ON aiosources.share_requests USING btree (share_id, requested_at);

CREATE INDEX staged_torrents_raw_record ON aiosources.staged_torrents USING btree (raw_id) WHERE (raw_id IS NOT NULL);

CREATE INDEX stremio_requests_recent ON aiosources.stremio_requests USING btree (created_at DESC, id DESC);

CREATE INDEX torrent_history_occurrence ON aiosources.torrent_history USING btree (occurrence_id, id DESC);

CREATE INDEX torrent_history_retention ON aiosources.torrent_history USING btree (occurred_at, id);

CREATE INDEX torrent_history_run ON aiosources.torrent_history USING btree (run_id, id DESC) WHERE (run_id IS NOT NULL);

CREATE INDEX torrents_catalog_details_cursor ON aiosources.torrents USING btree (catalog_details_tx, occurrence_id);

CREATE INDEX torrents_categories ON aiosources.torrents USING gin (search_categories);

CREATE INDEX torrents_fragments ON aiosources.torrents USING gin (aiosources.torrent_search_text(provider_id, source_id, fields) public.gin_trgm_ops);

CREATE INDEX torrents_fulltext ON aiosources.torrents USING gin (search_document);

CREATE INDEX torrents_info_hash ON aiosources.torrents USING btree (search_info_hash, provider_id, source_id) WHERE (search_info_hash IS NOT NULL);

CREATE UNIQUE INDEX torrents_occurrence_id ON aiosources.torrents USING btree (occurrence_id);

CREATE INDEX torrents_published_at ON aiosources.torrents USING btree (search_published_at, provider_id, source_id) WHERE (search_published_at IS NOT NULL);

CREATE INDEX torrents_raw_record ON aiosources.torrents USING btree (raw_id) WHERE (raw_id IS NOT NULL);

CREATE INDEX torrents_recent ON aiosources.torrents USING btree (last_seen_at DESC, provider_id, source_id);

CREATE INDEX torrents_size ON aiosources.torrents USING btree (aiosources.torrent_integer_key(search_size) COLLATE "C") WHERE (search_size IS NOT NULL);

CREATE INDEX torrents_torznab_recent ON aiosources.torrents USING btree (COALESCE(search_published_at, first_seen_at) DESC, provider_id, source_id) INCLUDE (fields_fingerprint) WHERE ((length(search_info_hash) = 40) AND (jsonb_typeof((fields -> 'title'::text)) = 'string'::text) AND ((fields ->> 'title'::text) <> ''::text) AND ((search_size IS NULL) OR ((search_size >= (0)::numeric) AND (search_size <= ('9223372036854775807'::bigint)::numeric))));

CREATE STATISTICS aiosources.torrents_torznab_fields ON length(search_info_hash), jsonb_typeof(fields -> 'title'::text) FROM aiosources.torrents;

CREATE TRIGGER activity_changed AFTER INSERT ON aiosources.activity_logs FOR EACH ROW EXECUTE FUNCTION aiosources.notify_activity();

CREATE TRIGGER activity_event_insert AFTER INSERT ON aiosources.events FOR EACH ROW EXECUTE FUNCTION aiosources.materialize_activity();

CREATE TRIGGER aio_queue_torrent AFTER INSERT OR UPDATE OF fields ON aiosources.torrents FOR EACH ROW EXECUTE FUNCTION aiosources.aio_queue_torrent();

CREATE TRIGGER catalog_baselines_immutable BEFORE DELETE OR UPDATE OR TRUNCATE ON aiosources.catalog_baselines FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_journal_immutable();

CREATE TRIGGER catalog_details_changed BEFORE INSERT OR UPDATE OF fields, origin ON aiosources.torrents FOR EACH ROW EXECUTE FUNCTION aiosources.catalog_details_changed();

CREATE TRIGGER catalog_entity_policy_changed AFTER INSERT OR DELETE OR UPDATE OF enabled, expires_at, catalog_entities ON aiosources.shares FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_entity_policy_changed();

CREATE TRIGGER catalog_journal_immutable BEFORE DELETE OR UPDATE OR TRUNCATE ON aiosources.catalog_journal FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_journal_immutable();

CREATE TRIGGER catalog_match_changed AFTER INSERT OR UPDATE ON aiosources.aio_matches FOR EACH ROW EXECUTE FUNCTION aiosources.catalog_entity_changed();

CREATE TRIGGER catalog_metadata_changed AFTER INSERT OR UPDATE ON aiosources.aio_works FOR EACH ROW EXECUTE FUNCTION aiosources.catalog_entity_changed();

CREATE TRIGGER catalog_metadata_deleted AFTER DELETE ON aiosources.aio_works FOR EACH ROW EXECUTE FUNCTION aiosources.catalog_entity_deleted();

CREATE TRIGGER catalog_publication_lock BEFORE INSERT OR DELETE OR UPDATE OR TRUNCATE ON aiosources.torrents FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_publication_lock();

CREATE TRIGGER catalog_publication_record AFTER INSERT OR DELETE OR UPDATE ON aiosources.torrents FOR EACH ROW EXECUTE FUNCTION aiosources.catalog_publication_record();

CREATE TRIGGER catalog_publication_truncate BEFORE TRUNCATE ON aiosources.torrents FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_publication_truncate();

CREATE TRIGGER catalog_tracking_policy_changed AFTER INSERT OR DELETE OR UPDATE OF enabled, expires_at ON aiosources.shares FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_tracking_changed();

CREATE TRIGGER catalog_tracking_policy_lock BEFORE INSERT OR DELETE OR UPDATE OF enabled, expires_at ON aiosources.shares FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_publication_lock();

CREATE TRIGGER public_catalog_access_policy AFTER UPDATE OF enabled, expose_sources, catalog_enabled ON aiosources.aio_public_access FOR EACH ROW EXECUTE FUNCTION aiosources.public_catalog_policy_changed();

CREATE TRIGGER public_catalog_data_policy AFTER UPDATE OF source_ids, fields, catalog_entities ON aiosources.public_catalog_policy FOR EACH ROW EXECUTE FUNCTION aiosources.public_catalog_policy_changed();

CREATE TRIGGER raw_records_retire_seeders BEFORE INSERT OR UPDATE OF fields ON aiosources.raw_records FOR EACH ROW EXECUTE FUNCTION aiosources.retire_seeder_fields();

CREATE TRIGGER staged_torrents_retire_seeders BEFORE INSERT OR UPDATE OF fields ON aiosources.staged_torrents FOR EACH ROW EXECUTE FUNCTION aiosources.retire_seeder_fields();

CREATE TRIGGER torrent_history_baselines_immutable BEFORE DELETE OR UPDATE OR TRUNCATE ON aiosources.torrent_history_baselines FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_journal_immutable();

CREATE TRIGGER torrent_history_immutable BEFORE DELETE OR UPDATE OR TRUNCATE ON aiosources.torrent_history FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_journal_immutable();

CREATE TRIGGER torrent_history_metadata_immutable BEFORE DELETE OR UPDATE OR TRUNCATE ON aiosources.torrent_history_metadata FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_journal_immutable();

CREATE TRIGGER torrent_history_record AFTER INSERT OR DELETE OR UPDATE ON aiosources.torrents FOR EACH ROW EXECUTE FUNCTION aiosources.torrent_history_record();

CREATE TRIGGER torrent_history_truncate BEFORE TRUNCATE ON aiosources.torrents FOR EACH STATEMENT EXECUTE FUNCTION aiosources.torrent_history_truncate();

CREATE TRIGGER torrents_retire_seeders BEFORE INSERT OR UPDATE OF fields ON aiosources.torrents FOR EACH ROW EXECUTE FUNCTION aiosources.retire_seeder_fields();

ALTER TABLE ONLY aiosources.activity_logs
    ADD CONSTRAINT activity_logs_source_event_id_fkey FOREIGN KEY (source_event_id) REFERENCES aiosources.events(id) ON DELETE SET NULL;

ALTER TABLE ONLY aiosources.aio_episodes
    ADD CONSTRAINT aio_episodes_work_id_fkey FOREIGN KEY (work_id) REFERENCES aiosources.aio_works(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.aio_match_queue
    ADD CONSTRAINT aio_match_queue_provider_id_source_id_fkey FOREIGN KEY (provider_id, source_id) REFERENCES aiosources.torrents(provider_id, source_id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.aio_match_reviews
    ADD CONSTRAINT aio_match_reviews_provider_id_source_id_fkey FOREIGN KEY (provider_id, source_id) REFERENCES aiosources.torrents(provider_id, source_id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.aio_matches
    ADD CONSTRAINT aio_matches_provider_id_source_id_fkey FOREIGN KEY (provider_id, source_id) REFERENCES aiosources.torrents(provider_id, source_id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.aio_matches
    ADD CONSTRAINT aio_matches_work_id_fkey FOREIGN KEY (work_id) REFERENCES aiosources.aio_works(id) ON DELETE SET NULL;

ALTER TABLE ONLY aiosources.aio_seasons
    ADD CONSTRAINT aio_seasons_work_id_fkey FOREIGN KEY (work_id) REFERENCES aiosources.aio_works(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.aio_titles
    ADD CONSTRAINT aio_titles_work_id_fkey FOREIGN KEY (work_id) REFERENCES aiosources.aio_works(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.events
    ADD CONSTRAINT events_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.operation_downloads
    ADD CONSTRAINT operation_downloads_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.operation_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.operation_events
    ADD CONSTRAINT operation_events_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.operation_runs(id);

ALTER TABLE ONLY aiosources.pages
    ADD CONSTRAINT pages_payload_id_fkey FOREIGN KEY (payload_id) REFERENCES aiosources.pages(id);

ALTER TABLE ONLY aiosources.pages
    ADD CONSTRAINT pages_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.raw_records
    ADD CONSTRAINT raw_records_page_id_fkey FOREIGN KEY (page_id) REFERENCES aiosources.pages(id);

ALTER TABLE ONLY aiosources.raw_records
    ADD CONSTRAINT raw_records_payload_id_fkey FOREIGN KEY (payload_id) REFERENCES aiosources.raw_records(id);

ALTER TABLE ONLY aiosources.raw_records
    ADD CONSTRAINT raw_records_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.run_catalog_entities
    ADD CONSTRAINT run_catalog_entities_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.run_filter_records
    ADD CONSTRAINT run_filter_records_page_id_fkey FOREIGN KEY (page_id) REFERENCES aiosources.pages(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.run_progress_identities
    ADD CONSTRAINT run_progress_identities_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.run_record_identities
    ADD CONSTRAINT run_record_identities_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.run_scope_counts
    ADD CONSTRAINT run_scope_counts_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.run_scope_memberships
    ADD CONSTRAINT run_scope_memberships_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.run_scope_refresh_candidates
    ADD CONSTRAINT run_scope_refresh_candidates_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.runs
    ADD CONSTRAINT runs_metadata_parent_run_id_fkey FOREIGN KEY (metadata_parent_run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.share_requests
    ADD CONSTRAINT share_requests_share_id_fkey FOREIGN KEY (share_id) REFERENCES aiosources.shares(id) ON DELETE CASCADE;

ALTER TABLE ONLY aiosources.staged_torrents
    ADD CONSTRAINT staged_torrents_raw_id_fkey FOREIGN KEY (raw_id) REFERENCES aiosources.raw_records(id) ON DELETE SET NULL;

ALTER TABLE ONLY aiosources.staged_torrents
    ADD CONSTRAINT staged_torrents_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.torrent_history
    ADD CONSTRAINT torrent_history_run_id_fkey FOREIGN KEY (run_id) REFERENCES aiosources.runs(id);

ALTER TABLE ONLY aiosources.torrents
    ADD CONSTRAINT torrents_raw_id_fkey FOREIGN KEY (raw_id) REFERENCES aiosources.raw_records(id) ON DELETE SET NULL;

SET LOCAL check_function_bodies = true;
