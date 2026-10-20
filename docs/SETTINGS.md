# Deployment and settings

[Back to README](../README.md) · [Source configuration](CONFIGURATION.md)

## Docker Compose

Start from [`.env.example`](../.env.example). Compose uses these values:

| Variable                 | Default    | Purpose                                                                       |
| ------------------------ | ---------- | ----------------------------------------------------------------------------- |
| `POSTGRES_PASSWORD`      | Required   | Use a URL-safe random password, such as the output of `openssl rand -hex 24`. |
| `POSTGRES_DB`            | `ingest`   | Database created by PostgreSQL. Preserve the existing value on upgrade.       |
| `POSTGRES_USER`          | `ingest`   | PostgreSQL role. Preserve the existing value on upgrade.                      |
| `POSTGRES_PORT`          | `5432`     | Host-side PostgreSQL port, bound to loopback.                                 |
| `PORT`            | `7000`     | Host-side panel port.                                                        |
| `BIND_IP`         | `127.0.0.1` | Host interface for the panel; loopback-only by default.                      |
| `ADMIN_PASSWORD`  | Generated  | Optional administrator password, 12–72 bytes.                                |
| `PUBLIC_URL`      | Unset      | Exact external origin; HTTPS is required to activate sharing.                |
| `AIOSOURCES_IMAGE`       | `ghcr.io/theo-bnts/aiosources:stable` | Application image; stable GHCR releases by default. |
| `POSTGRES_VOLUME`        | `aiosources_v2_postgres_data` | PostgreSQL volume name. Preserve the actual existing volume on upgrade. |
| `STATE_VOLUME`    | `aiosources_v2_state` | Private state volume name. Preserve the actual existing volume on upgrade. |
| `COMPOSE_PROJECT_NAME`   | `aiosources`   | Compose project identity; preserve it when upgrading an existing deployment. |

The container runs as UID/GID `65532:65532`. State lives in `/data`, and private source JSON in `/data/providers`. Compose constructs `DATABASE_URL` from its PostgreSQL settings. Source migration does not change `compose.yaml` or CI YAML. For an existing installation, stop all application replicas and source writers and follow [Migrating existing sources](CONFIGURATION.md#migrating-existing-sources) before restarting with JSON-only loading.

The distributed Compose file selects `AIOSOURCES_IMAGE` (default `ghcr.io/theo-bnts/aiosources:stable`); it has no implicit local build. Use `:preview` to test pre-releases. See [Local Docker build](DEVELOPMENT.md#local-docker-build) for the explicit development override.

### First AIOSources deployment

Create a fresh PostgreSQL database and private state volume. The previous TypeScript/Python schemas, settings and scraper checkpoints are not compatible with this application. Preserve the previous deployment separately, then configure sources and secrets in the v2 console. Never reuse the legacy state/database volumes as an implicit migration.

The container executable and Compose application service are `/aiosource` and `aiosources`. Deployment variables use unprefixed names. Rename previous `INGEST_` variables before upgrading; the application rejects obsolete deployment names before touching private state. The [offline source migration](CONFIGURATION.md#migrating-existing-sources) applies only to Ingest YAML source definitions; it does not migrate AIOSources v1 data.

## Native environment and CLI

These seven variables are read directly by AIOSources. Adding them to Compose's `.env` does not pass them into the container unless they are also added to the service's `environment` configuration. The native defaults below differ from the distributed image defaults: the image sets `BIND=0.0.0.0:7000`, `DATA_DIR=/data` and `PROVIDERS_DIR=/data/providers`.

| Variable                       | Native default                   | Purpose                                                                                                            |
| ------------------------------ | -------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `DATABASE_URL`                 | Required for database commands   | PostgreSQL connection string. Keep credentials out of command-line arguments.                                      |
| `DATA_DIR`             | `.ingest`                        | Private state directory; override with `--data-dir`.                                                               |
| `PROVIDERS_DIR`        | `providers`                      | Authoritative private source JSON directory; override with `--providers`.                                         |
| `BIND`                 | `127.0.0.1:8080`                  | Listen address; override with `serve --bind`.                                                                      |
| `PUBLIC_URL`           | Unset                            | External origin; override with `serve --public-url`. This does not terminate TLS.                                  |
| `ADMIN_PASSWORD`       | Generated in the state directory | Optional administrator password, 12–72 bytes.                                                                      |
| `MASTER_KEY`           | Generated `vault.key`            | Optional existing 32-byte vault key, encoded as base64 or hexadecimal. Never replace the key of an existing vault. |

`serve --workers` seeds the collection limit only when no value has been saved: default **5**, range **1–32**. Subsequent starts retain the database setting, even with a different flag value. `serve --web-dir` overrides the embedded frontend directory. Run `ingest help` or a command's `--help` for the complete CLI.

TMDB and debrid credentials are encrypted Secrets referenced by application settings; they have no dedicated environment variables. Collection concurrency, schedules and matching settings live in PostgreSQL or the source definitions. Backup-tool variables were retired with Backups and are no longer read by the application.

Development additionally recognizes these variables; they do not configure the running application:

| Variable | Default | Purpose |
| --- | --- | --- |
| `API_PROXY_TARGET` | `http://127.0.0.1:8080` | Backend origin for the Vite development server. |
| `TEST_DATABASE_URL` | Unset | Maintenance PostgreSQL URI with permission to create disposable test databases. |
| `E2E_BINARY` | `../bin/ingest` relative to `web` | Production binary used by browser tests. |
| `QA_DIRECTORY` | Unset | Optional directory for the statistics browser test's screenshot. |

The retained CI workflow's `INGEST_TEST_DATABASE_URL` is accepted only by the test harness, as a fallback for `TEST_DATABASE_URL`; it never configures the running application.

Crash tests pass `CRASH_TEST_DATABASE_URL` and `CRASH_TEST_PROVIDERS` to their child process internally; these are test-harness inputs, not deployment settings. General operating-system, Go, Node, Docker Compose and PostgreSQL driver environment variables are outside this application-specific list.

The unauthenticated liveness endpoint is `GET /healthz`. Database and storage measurements require an authenticated session.

## Outgoing connections

Configure existing WARP, VPN or other HTTP(S)/SOCKS5 proxies in **Administration → Network**. Profiles, the default connection and per-source/service choices are saved in the database; authentication uses encrypted Secrets. No new environment variable or application restart is required. See [proxy setup](NETWORK.md) for Docker addresses, routing rules and connection testing.

## Collection concurrency

Open **Sources → Settings → Advanced settings** to change **Simultaneous imports** from **1 to 32**. The setting persists in PostgreSQL and applies immediately to all collection processes sharing that database, without restarting.

- Increasing the limit wakes queued work.
- Decreasing it leaves active runs and their checkpoints intact. New runs wait until the active count falls below the new limit.
- Only one run per source is admitted at a time; each source's request interval and rate-limit handling remain unchanged.
- All instances support the full 1–32 range, independently of CPU count or the ordinary database connection pool size. Import sessions have a separate bounded pool and open only as work is admitted; startup does not open 32 connections. Administration, scheduling, metadata and recovery keep their ordinary connection budget. `pool_max_conns` in `DATABASE_URL` configures that ordinary pool, not the number of simultaneous imports.

Concurrent edits use revisions. If another administrator changes shared settings, the page preserves the draft and asks whether to use the saved values or keep the edit before saving again.

## Collection execution

The remaining shared settings also persist in PostgreSQL. Execution policies are captured when a run is created: changing them does not rewrite queued, paused or active runs, and Resume retains the same policy. Runs created before these policies were introduced keep their legacy behavior.

| Setting | Default | Range and behavior |
| --- | --- | --- |
| Automatic quota retries | `3` | `0–10` additional attempts after a 429; `0` records the first rejection and pauses without an automatic retry. |
| Maximum automatic quota wait | `0` | `0–86400` seconds; `0` means unlimited. A longer server-requested wait pauses after saving its status and deadline, not its response body. |
| Responses without new native IDs | `1000` | `0–100000`; `0` disables the useful-progress guard. |
| No-progress action | `warn` | `warn` notifies once per stalled streak and continues; `pause` retains the checkpoint and requires explicit Resume. |

**Explicit Resume accepts an already saved quota wait; it never shortens the server deadline.** A successful response that exhausts a quota is committed before pausing. Pause and Cancel remain responsive during cooldowns.

Useful progress means a previously unseen **native source ID within that run**, not another observation, another hash or a changed total. Quota errors and auxiliary/control responses do not consume this allowance. New IDs reset the streak. Explicitly resuming a no-progress pause grants a fresh allowance without forgetting previously seen IDs or deleting archived evidence.

Page and duration budgets apply independently to each attempt. A time limit is checked at safe response boundaries and during cooldown waits; it does not interrupt an in-flight response or impose a timeout on atomic Full publication. Preview starts with a fixed three-page budget. Other manual imports use the source’s `schedule.max_pages` limit, also used for scheduled imports, or unlimited pages when it is omitted. The **Run manual import** dialog can override pages and duration for one run; `0` explicitly selects unlimited. Duration is otherwise unlimited. A source without `request_timeout` uses the fixed `30s` timeout.

Technical interruptions always recover automatically from the last committed checkpoint when the source remains valid and enabled and no other worker holds its lease. Manual, quota and no-progress holds, cancellations and authentication, certificate or configuration failures remain deliberate stops. Restore marks restored work as manually paused until explicit review; it does not change this fixed recovery behavior.

Open **Sources → Settings → Advanced settings** for the remaining shared concurrency, quota and useful-progress controls. One **Save** applies the complete form atomically. Validation, revision conflicts, folding and the unsaved-changes guard retain drafts. Configure source-specific timing and page limits in each source’s **Schedules** section, and request timeouts in **Imports**. Statistics appear in their dedicated first tab.

### Durable operation

Start the installation from its deployment directory with `docker compose up -d`. Pull the selected image explicitly before an upgrade. Keep its private `.env`, any local `compose.override.yaml`, and both named volumes together as the deployment configuration. Both services use `restart: unless-stopped`: they return when the Docker engine restarts, but deliberately stopped containers remain stopped. On macOS, arrange for Docker Desktop to start at login, using its preference or a user LaunchAgent. The machine must remain awake for scheduled collection.

An existing installation must be migrated, not replaced with empty volumes. Take a verified deployment backup first, preserve the vault key, pause active collection, and stop PostgreSQL before copying a physical cluster. Retain the original storage until the new installation has been checked. Preserve the PostgreSQL major version, source bytes, vault key, run snapshots and checkpoints; changing paths or volume names is not a migration. Never use `docker compose down --volumes` or prune these volumes as a restart procedure.

Technical restart recovery is always enabled. It does not release manual, quota or no-progress holds; budget-paused scheduled runs follow their existing continuation rules. Recovery keeps the same run and immutable source snapshot. Use external deployment backup tooling to preserve PostgreSQL, private definitions and the vault key together.

## Administrator security

**Security** opens **Statistics** by default. **Settings** groups **Secrets**, **Two-step verification** and **Active sessions**. The current device appears first in Active sessions, followed by the most recently active devices. Security and sharing outcomes appear in **Logs**, which replaces the separate security audit. Enroll an RFC 6238 authenticator, confirm a six-digit code, and save the one-time recovery codes outside this server. Authenticator codes cannot be replayed, including across service instances; each recovery code works once. Enabling, disabling or replacing recovery codes requires the current password, and changes to active protection also require an unused second factor. These changes revoke other sessions.

Sessions persist in PostgreSQL, expire after twelve hours, and can be revoked individually or together. The browser uses an HttpOnly cookie; passwords, authenticator secrets and recovery codes are not kept in browser storage. Device descriptions are deliberately coarse, not stored IP addresses or raw user-agent strings. Password changes invalidate old sessions. Logs records security and sharing outcomes in the same transaction as their changes, without credentials or historical secret values.

## Vault key

Back up **both** the PostgreSQL database and the state volume. Losing `vault.key` makes stored credentials unrecoverable. Do not regenerate it for an existing vault. A supplied `MASTER_KEY` must be exactly 32 bytes encoded as base64 or hexadecimal.
