package store

const trustedUsersSchema = `
ALTER TABLE aiosources.aio_public_access
 ADD COLUMN use_instance_debrid boolean NOT NULL DEFAULT false,
 ADD COLUMN use_instance_announces boolean NOT NULL DEFAULT false;

CREATE TABLE aiosources.trusted_users (
 id text PRIMARY KEY,
 email text NOT NULL UNIQUE CHECK (length(email) BETWEEN 3 AND 254 AND email=lower(email)),
 password_hash text NOT NULL,
 enabled boolean NOT NULL DEFAULT false,
 permissions jsonb NOT NULL DEFAULT '{}',
 revision bigint NOT NULL DEFAULT 1,
 generation bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE aiosources.trusted_sessions (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
 id text NOT NULL UNIQUE,
 user_id text NOT NULL REFERENCES aiosources.trusted_users(id) ON DELETE CASCADE,
 generation bigint NOT NULL,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX trusted_sessions_user ON aiosources.trusted_sessions(user_id);
CREATE INDEX trusted_sessions_expiry ON aiosources.trusted_sessions(expires_at);
`
