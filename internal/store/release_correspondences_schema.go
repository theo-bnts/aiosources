package store

const releaseCorrespondencesSchema = `
CREATE TABLE aiosources.aio_release_correspondence_queue (
    scope text NOT NULL CHECK (scope IN ('work', 'hash')),
    scope_key text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    PRIMARY KEY (scope, scope_key)
);

CREATE TABLE aiosources.aio_release_groups (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    scope text NOT NULL CHECK (scope IN ('work', 'hash')),
    scope_key text NOT NULL,
    work_id bigint REFERENCES aiosources.aio_works(id) ON DELETE CASCADE,
    match_rule text NOT NULL CHECK (match_rule IN ('exact_info_hash', 'work_exact_size', 'work_release_name')),
    match_key text NOT NULL UNIQUE,
    confidence text NOT NULL CHECK (confidence IN ('exact', 'high', 'verified')),
    canonical_size bigint NOT NULL DEFAULT 0,
    canonical_title text NOT NULL DEFAULT '',
    member_count integer NOT NULL CHECK (member_count >= 2),
    provider_count integer NOT NULL CHECK (provider_count >= 2),
    enriched_count integer NOT NULL DEFAULT 0,
    propagated_audio text[] NOT NULL DEFAULT ARRAY[]::text[],
    propagated_subtitles text[] NOT NULL DEFAULT ARRAY[]::text[],
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE TABLE aiosources.aio_release_group_members (
    group_id bigint NOT NULL REFERENCES aiosources.aio_release_groups(id) ON DELETE CASCADE,
    provider_id text NOT NULL,
    source_id text NOT NULL,
    fingerprint text NOT NULL,
    match_rule text NOT NULL CHECK (match_rule IN ('exact_info_hash', 'work_exact_size', 'work_release_name')),
    own_audio text[] NOT NULL DEFAULT ARRAY[]::text[],
    own_subtitles text[] NOT NULL DEFAULT ARRAY[]::text[],
    contributed_audio text[] NOT NULL DEFAULT ARRAY[]::text[],
    contributed_subtitles text[] NOT NULL DEFAULT ARRAY[]::text[],
    PRIMARY KEY (provider_id, source_id),
    FOREIGN KEY (provider_id, source_id) REFERENCES aiosources.torrents(provider_id, source_id) ON DELETE CASCADE
);

CREATE TABLE aiosources.aio_release_correspondence_exclusions (
    provider_id text NOT NULL,
    source_id text NOT NULL,
    match_key text NOT NULL,
    fingerprint text NOT NULL,
    reason text NOT NULL DEFAULT 'manually unlinked',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, source_id, match_key),
    FOREIGN KEY (provider_id, source_id) REFERENCES aiosources.torrents(provider_id, source_id) ON DELETE CASCADE
);

CREATE INDEX aio_release_groups_scope ON aiosources.aio_release_groups USING btree (scope, scope_key);
CREATE INDEX aio_release_groups_work ON aiosources.aio_release_groups USING btree (work_id) WHERE (work_id IS NOT NULL);
CREATE INDEX aio_release_groups_rule ON aiosources.aio_release_groups USING btree (match_rule, id DESC);
CREATE INDEX aio_release_groups_enriched ON aiosources.aio_release_groups USING btree (id DESC) WHERE (enriched_count > 0);
CREATE INDEX aio_release_group_members_group ON aiosources.aio_release_group_members USING btree (group_id, provider_id, source_id);
CREATE INDEX aio_release_correspondence_exclusions_key ON aiosources.aio_release_correspondence_exclusions USING btree (match_key);

CREATE FUNCTION aiosources.aio_queue_deleted_correspondence() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
 SELECT g.scope,g.scope_key
 FROM aiosources.aio_release_group_members m
 JOIN aiosources.aio_release_groups g ON g.id=m.group_id
 WHERE m.provider_id=OLD.provider_id AND m.source_id=OLD.source_id
 ON CONFLICT(scope,scope_key) DO UPDATE SET version=aiosources.aio_release_correspondence_queue.version+1;
 RETURN OLD;
END $$;

CREATE TRIGGER aio_queue_deleted_correspondence
    BEFORE DELETE ON aiosources.torrents
    FOR EACH ROW EXECUTE FUNCTION aiosources.aio_queue_deleted_correspondence();

INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT 'work', m.work_id::text
FROM aiosources.aio_matches m
JOIN aiosources.aio_works w ON w.id=m.work_id
WHERE m.status='matched' AND m.work_id IS NOT NULL AND w.media_type IN ('movie','tv') AND NOT w.adult
GROUP BY m.work_id
HAVING count(DISTINCT m.provider_id) >= 2
ON CONFLICT DO NOTHING;

INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT 'hash', t.search_info_hash
FROM aiosources.torrents t
WHERE t.search_info_hash IS NOT NULL AND length(t.search_info_hash) = 40
GROUP BY t.search_info_hash
HAVING count(DISTINCT t.provider_id) >= 2
ON CONFLICT DO NOTHING;
`
