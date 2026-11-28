package store

const releaseAlertsSchema = `
CREATE TABLE aiosources.notification_channels (
 id text PRIMARY KEY,
 name text NOT NULL,
 type text NOT NULL CHECK (type IN ('discord','ntfy','gotify','telegram','slack','webhook')),
 enabled boolean NOT NULL DEFAULT true,
 is_default boolean NOT NULL DEFAULT false,
 allow_trusted boolean NOT NULL DEFAULT true,
 config jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
 secret_ref text NOT NULL DEFAULT '',
 header_secret_ref text NOT NULL DEFAULT '',
 revision bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX notification_channels_single_default
 ON aiosources.notification_channels (is_default)
 WHERE is_default;

CREATE TABLE aiosources.release_alerts (
 id text PRIMARY KEY,
 owner_type text NOT NULL CHECK (owner_type IN ('admin','trusted')),
 owner_id text NOT NULL DEFAULT '',
 work_id bigint NOT NULL REFERENCES aiosources.aio_works(id) ON DELETE CASCADE,
 media_type text NOT NULL CHECK (media_type IN ('movie','tv','game','software','music','book')),
 season integer CHECK (season IS NULL OR (season >= 0 AND season <= 999)),
 episode integer CHECK (episode IS NULL OR (episode >= 1 AND episode <= 9999)),
 channel_id text NOT NULL REFERENCES aiosources.notification_channels(id) ON DELETE CASCADE,
 filters jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(filters) = 'object'),
 status text NOT NULL DEFAULT 'watching' CHECK (status IN ('watching','retrying')),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 next_attempt timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT '',
 revision bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT release_alerts_owner_check CHECK (
  (owner_type = 'admin' AND owner_id = '') OR
  (owner_type = 'trusted' AND owner_id <> '')
 ),
 CONSTRAINT release_alerts_tv_episode_check CHECK (
  (media_type = 'tv' AND season IS NOT NULL AND episode IS NOT NULL) OR
  (media_type <> 'tv' AND season IS NULL AND episode IS NULL)
 )
);

CREATE UNIQUE INDEX release_alerts_unique_target
 ON aiosources.release_alerts (owner_type, owner_id, work_id, COALESCE(season, -1), COALESCE(episode, -1));

CREATE INDEX release_alerts_work_idx
 ON aiosources.release_alerts (work_id, next_attempt);

CREATE INDEX release_alerts_owner_idx
 ON aiosources.release_alerts (owner_type, owner_id, created_at DESC);

UPDATE aiosources.trusted_users
 SET permissions=jsonb_set(permissions,'{alerts_enabled}',to_jsonb(false),true)
 WHERE NOT (permissions ? 'alerts_enabled');
`
