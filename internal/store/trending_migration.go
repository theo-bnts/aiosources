package store

// Cover the trending time-window scan without reading the torrents JSONB heap.
const trendingIndexSchema = `
CREATE INDEX torrents_trending_recent
 ON aiosources.torrents (COALESCE(search_published_at, first_seen_at) DESC, provider_id, source_id)
 INCLUDE (fields_fingerprint, search_published_at, first_seen_at);
`

// Extract a canonical episode or season-pack key once at write time into a
// compact covering side table (only populated for torrents with episode/season
// markers) so TV trending ranks by average torrents per active episode using
// index-only scans on torrents_trending_recent and torrent_episode_keys_pkey
// with zero JSONB heap fetches and zero table rewrite on torrents.
const trendingEpisodeKeySchema = `
CREATE OR REPLACE FUNCTION aiosources.torrent_episode_key(fields jsonb) RETURNS text
 LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
 AS $_$
DECLARE
 raw_title TEXT;
 s TEXT;
 m TEXT[];
 season_num INTEGER;
 ep_num INTEGER;
 s1 INTEGER;
 s2 INTEGER;
 struct_season INTEGER;
 struct_episode INTEGER;
 v JSONB;
 txt TEXT;
 is_pack BOOLEAN := FALSE;
BEGIN
 v := fields->'season';
 IF v IS NULL OR jsonb_typeof(v) = 'null' THEN
  v := fields#>'{attributes,season}';
 END IF;
 IF jsonb_typeof(v) = 'array' AND jsonb_array_length(v) = 1 THEN
  v := v->0;
 END IF;
 IF jsonb_typeof(v) IN ('number', 'string') THEN
  txt := btrim(v #>> '{}');
  IF txt ~ '^[0-9]{1,3}$' THEN
   struct_season := txt::integer;
  END IF;
 END IF;

 v := fields->'episode';
 IF v IS NULL OR jsonb_typeof(v) = 'null' THEN
  v := fields#>'{attributes,episode}';
 END IF;
 IF jsonb_typeof(v) = 'array' AND jsonb_array_length(v) = 1 THEN
  v := v->0;
 END IF;
 IF jsonb_typeof(v) IN ('number', 'string') THEN
  txt := btrim(v #>> '{}');
  IF txt ~ '^[0-9]{1,5}$' AND txt::integer >= 1 THEN
   struct_episode := txt::integer;
  END IF;
 END IF;

 raw_title := '';
 IF jsonb_typeof(fields->'title') = 'string' THEN
  raw_title := btrim(left(fields->>'title', 512));
 END IF;
 IF raw_title = '' AND jsonb_typeof(fields->'name') = 'string' THEN
  raw_title := btrim(left(fields->>'name', 512));
 END IF;

 IF raw_title <> '' THEN
  s := regexp_replace(raw_title, '^(?:\s*\[[^\]]+\])+\s*', '');
  IF s = '' THEN
   s := raw_title;
  END IF;
  s := translate(s, '._', '  ');

  -- 1. Standard SxxExx / Sxx Eyy / Sxx EPyy / Sxx Episode yy (fast path).
  m := regexp_match(s, '\mS([0-9]{1,3})\s*[-–]?\s*(?:[eéÉ]pisode\s*|ep\s*|E)([0-9]{1,5})(?:\M|E[0-9]|[-–](?:[eéÉ]pisode|ep|E)?[0-9]|v[0-9]|END\M)', 'i');
  IF m IS NOT NULL THEN
   RETURN 'e:' || m[1]::integer || ':' || m[2]::integer;
  END IF;

  -- Normalize ordinal seasons ("2nd Season" -> "Season 2") only when needed.
  IF s ~* '\mseason\M' THEN
   s := regexp_replace(s, '\m([0-9]{1,3})(?:st|nd|rd|th)\s+season\M', 'Season \1', 'gi');
  END IF;

  -- 2. Spelled "Season 1 Episode 02" / "Saison 1 Épisode 02".
  IF s ~* '\m(?:season|saison)\M' THEN
   m := regexp_match(s, '\m(?:season|saison)\s*([0-9]{1,3})\s*[-–]?\s*(?:[eéÉ]pisode|ep|e)\s*([0-9]{1,5})(?:\M|[-–][0-9]|v[0-9])', 'i');
   IF m IS NOT NULL THEN
    RETURN 'e:' || m[1]::integer || ':' || m[2]::integer;
   END IF;
  END IF;

  is_pack := s ~* '\m(?:complete|int[eéÉ]grale|pack)\M';

  -- 3. Separated "S01 - 02" / "Season 1 - 02" (excluding years and pack ranges).
  IF NOT is_pack AND (strpos(s, '-') > 0 OR strpos(s, '–') > 0) THEN
   m := regexp_match(s, '\m(?:S|season\s*|saison\s*)([0-9]{1,3})\s+[-–]\s+([0-9]{1,5})(?:\M|[-–][0-9]|v[0-9])', 'i');
   IF m IS NOT NULL THEN
    ep_num := m[2]::integer;
    IF ep_num < 1800 OR ep_num > 2199 THEN
     RETURN 'e:' || m[1]::integer || ':' || ep_num;
    END IF;
   END IF;
  END IF;

  -- 4. Cross format "1x02" / "01x02".
  IF strpos(s, 'x') > 0 OR strpos(s, 'X') > 0 THEN
   m := regexp_match(s, '\m([0-9]{1,2})x([0-9]{1,3})\M', 'i');
   IF m IS NOT NULL THEN
    RETURN 'e:' || m[1]::integer || ':' || m[2]::integer;
   END IF;
  END IF;

  -- 5. Daily / date-based episodes (YYYY-MM-DD, YYYY.MM.DD, DD.MM.YYYY).
  IF s ~ '(?:19|20)[0-9]{2}' THEN
   m := regexp_match(s, '\m((?:19|20)[0-9]{2})[\s-]+(0[1-9]|1[0-2])[\s-]+(0[1-9]|[12][0-9]|3[01])\M');
   IF m IS NOT NULL THEN
    RETURN 'e:d:' || m[1] || '-' || m[2] || '-' || m[3];
   END IF;
   m := regexp_match(s, '\m(0[1-9]|[12][0-9]|3[01])[\s-]+(0[1-9]|1[0-2])[\s-]+((?:19|20)[0-9]{2})\M');
   IF m IS NOT NULL THEN
    RETURN 'e:d:' || m[3] || '-' || m[2] || '-' || m[1];
   END IF;
  END IF;

  -- Explicit multi-season pack range (e.g. S01-S05).
  m := regexp_match(s, '\mS([0-9]{1,3})\s*[-–]\s*S?([0-9]{1,3})\M', 'i');
  IF m IS NULL AND s ~* '\m(?:season|saison)\M' THEN
   m := regexp_match(s, '\m(?:season|saison)\s*([0-9]{1,3})\s*[-–]\s*(?:season|saison)?\s*([0-9]{1,3})\M', 'i');
  END IF;
  IF m IS NOT NULL THEN
   s1 := m[1]::integer;
   s2 := m[2]::integer;
   IF s1 <= s2 AND s2 - s1 <= 100 THEN
    RETURN 's:' || s1 || '-' || s2;
   END IF;
  END IF;

  m := regexp_match(s, '\mS([0-9]{1,3})\M', 'i');
  IF m IS NULL AND s ~* '\m(?:season|saison)\M' THEN
   m := regexp_match(s, '\m(?:season|saison)\s*([0-9]{1,3})\M', 'i');
  END IF;
  IF m IS NOT NULL THEN
   season_num := m[1]::integer;
  END IF;

  -- 6. Standalone episode markers ("Ep 14", "Episode 14", "E14").
  m := regexp_match(s, '\m(?:[eéÉ]pisode\s*|ep\s*|E)([0-9]{1,5})(?:\M|[-–](?:[eéÉ]pisode|ep|E)?[0-9]|v[0-9]|END\M)', 'i');
  IF m IS NOT NULL THEN
   ep_num := m[1]::integer;
   IF season_num IS NOT NULL THEN
    RETURN 'e:' || season_num || ':' || ep_num;
   ELSIF struct_season IS NOT NULL THEN
    RETURN 'e:' || struct_season || ':' || ep_num;
   END IF;
   RETURN 'e:a:' || ep_num;
  END IF;

  -- 7. Absolute dash episode ("[SubsPlease] Anime - 1122 (1080p)") when no season in title.
  IF season_num IS NULL AND (strpos(s, '-') > 0 OR strpos(s, '–') > 0) THEN
   m := regexp_match(s, '(?:^|\s)[-–]\s+([0-9]{1,5})(?:v[0-9]+)?(?:\M|\s|[\(\[])', 'i');
   IF m IS NOT NULL THEN
    ep_num := m[1]::integer;
    IF ep_num < 1800 OR ep_num > 2199 THEN
     IF struct_season IS NOT NULL THEN
      RETURN 'e:' || struct_season || ':' || ep_num;
     END IF;
     RETURN 'e:a:' || ep_num;
    END IF;
   END IF;
  END IF;
 END IF;

 -- 8. Structured episode fallback when title is not explicitly a pack.
 IF struct_episode IS NOT NULL AND NOT is_pack THEN
  IF season_num IS NOT NULL THEN
   RETURN 'e:' || season_num || ':' || struct_episode;
  ELSIF struct_season IS NOT NULL THEN
   RETURN 'e:' || struct_season || ':' || struct_episode;
  END IF;
  RETURN 'e:a:' || struct_episode;
 END IF;

 -- 9. Single-season pack key from title or structured season.
 IF season_num IS NOT NULL THEN
  RETURN 's:' || season_num;
 END IF;
 IF struct_season IS NOT NULL THEN
  RETURN 's:' || struct_season;
 END IF;

 RETURN '';
END;
$_$;

CREATE TABLE aiosources.torrent_episode_keys (
 provider_id TEXT NOT NULL,
 source_id TEXT NOT NULL,
 episode_key TEXT NOT NULL CHECK (episode_key <> ''),
 PRIMARY KEY (provider_id, source_id) INCLUDE (episode_key),
 FOREIGN KEY (provider_id, source_id) REFERENCES aiosources.torrents(provider_id, source_id) ON DELETE CASCADE
);

CREATE OR REPLACE FUNCTION aiosources.torrent_episode_key_sync() RETURNS trigger
 LANGUAGE plpgsql
 AS $$
DECLARE
 k TEXT := aiosources.torrent_episode_key(NEW.fields);
BEGIN
 IF k <> '' THEN
  INSERT INTO aiosources.torrent_episode_keys(provider_id, source_id, episode_key)
  VALUES (NEW.provider_id, NEW.source_id, k)
  ON CONFLICT (provider_id, source_id) DO UPDATE
   SET episode_key = EXCLUDED.episode_key
   WHERE aiosources.torrent_episode_keys.episode_key IS DISTINCT FROM EXCLUDED.episode_key;
 ELSIF TG_OP = 'UPDATE' THEN
  DELETE FROM aiosources.torrent_episode_keys
  WHERE provider_id = NEW.provider_id AND source_id = NEW.source_id;
 END IF;
 RETURN NULL;
END;
$$;

CREATE TRIGGER torrents_episode_key_sync
 AFTER INSERT OR UPDATE OF fields ON aiosources.torrents
 FOR EACH ROW EXECUTE FUNCTION aiosources.torrent_episode_key_sync();

INSERT INTO aiosources.torrent_episode_keys(provider_id, source_id, episode_key)
SELECT provider_id, source_id, ep_key
FROM (
 SELECT provider_id, source_id, aiosources.torrent_episode_key(fields) AS ep_key
 FROM aiosources.torrents
) extracted
WHERE ep_key <> '';
`
