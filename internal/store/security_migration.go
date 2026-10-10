package store

const securitySchema = `
CREATE TABLE ingest.admin_security (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    password_hash bytea,
    active_secret bytea,
    pending_secret bytea,
    pending_expires_at timestamptz,
    pending_session_id text,
    last_step bigint NOT NULL DEFAULT -1,
    recovery_hashes text[] NOT NULL DEFAULT '{}',
    CHECK ((pending_secret IS NULL AND pending_expires_at IS NULL AND pending_session_id IS NULL) OR
           (pending_secret IS NOT NULL AND pending_expires_at IS NOT NULL AND pending_session_id IS NOT NULL))
);
INSERT INTO ingest.admin_security(singleton) VALUES(true);
CREATE TABLE ingest.admin_sessions (
    id text PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    user_agent text NOT NULL CHECK (length(user_agent) <= 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX admin_sessions_expiry ON ingest.admin_sessions(expires_at);
CREATE TABLE ingest.security_attempts (
    bucket text PRIMARY KEY,
    window_start timestamptz NOT NULL,
    attempts integer NOT NULL
);
CREATE TABLE ingest.security_audit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    action text NOT NULL CHECK (action IN (
      'security.mfa_enabled','security.mfa_disabled','security.recovery_rotated',
      'security.session_revoked','security.sessions_revoked','share.created',
      'share.permissions_changed','share.rotated','share.revoked')),
    target_id text NOT NULL CHECK (target_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX security_audit_recent ON ingest.security_audit(created_at DESC, id DESC);
CREATE FUNCTION ingest.guard_security_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'security audit is append-only';
END;
$$;
CREATE TRIGGER security_audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON ingest.security_audit
FOR EACH STATEMENT EXECUTE FUNCTION ingest.guard_security_audit();
`
