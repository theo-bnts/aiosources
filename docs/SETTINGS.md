# Installation, updates and deployment settings

[Back to README](../README.md) · [Network](NETWORK.md) · [Security](../SECURITY.md)

The [README](../README.md#install-your-instance) includes a minimal Docker Compose installation using the stable image. Configure sources and service accounts in the application after signing in.

## Addresses and access

Set `PUBLIC_URL` to the address your applications can reach, such as `http://192.0.2.10:7000` on a trusted private network or `https://catalog.example` over the internet. Use the exact origin, without a path, query or credentials.

The application serves HTTP. For internet access, provide HTTPS through a trusted reverse proxy or tunnel. Setting `PUBLIC_URL` does not create a certificate or enable TLS. Keep PostgreSQL and the backend address private when relying on an access gateway.

Stremio and Torznab can use trusted local HTTP addresses. Catalog sharing requires an external HTTPS address. A player or app running on another device must be able to reach the installation address; a container-only hostname will not work from a television.

## Supported environment variables

| Variable | Default in the image | When to change it |
| --- | --- | --- |
| `DATABASE_URL` | Required | Point to the PostgreSQL database and its credentials. |
| `PUBLIC_URL` | Unset | Set the address used in generated connection and sharing links. |
| `ADMIN_PASSWORD` | Generated if omitted | Set your administrator password explicitly; 12–72 bytes. If omitted on first use, startup output identifies the password file in the application data volume. |
| `DATA_DIR` | `/data` | Change only when moving persistent application data deliberately. |
| `PROVIDERS_DIR` | `/data/providers` | Use a different persistent folder for source definitions. |
| `BIND` | `0.0.0.0:7000` | Change the listening interface or port. Update port mappings as needed. |
| `MASTER_KEY` | Generated and kept with application data | Supply your own 32-byte base64 or hexadecimal encryption key only when managing it deliberately. |

These names have no `INGEST_` prefix. Source credentials, TMDB tokens and debrid keys are configured through **Secrets**, not separate environment variables.

Changing a database password requires updating the PostgreSQL account as well as its connection setting. Editing `POSTGRES_PASSWORD` alone does not change a password in an already initialized database.

## Preserve your data

Keep both Compose volumes: the PostgreSQL volume and the AIOSources data volume. Back up both together before an update or a move. Source definitions and the generated encryption key must stay with the database that uses them.

Never replace an existing encryption key as a routine update. Losing it can make stored credentials unreadable. Keep backups and exported source configurations private.

Use a consistent database backup or stop the services before copying their volumes with your backup tool. Do not copy a running PostgreSQL data directory as though it were an ordinary folder. `docker compose down -v` deletes named volumes.

## Update channels

- **`ghcr.io/theo-bnts/aiosources:stable`** follows releases promoted to stable.
- **`:preview`** follows automatic prereleases for testing.
- **`:sha-FULL_COMMIT_SHA`** keeps an exact published build.

After choosing the tag, run `docker compose pull` and `docker compose up -d`. Preserve your volumes. Read the release's upgrade guidance and back up first; switching back to an older image may require restoring the backup made before the update.

## Upgrade from V1

Keep a backup of the V1 data and start V2 with fresh volumes. V1 volumes are not directly compatible. Configure your sources and accounts in V2, and keep your original backup until the new instance is working as expected.

## Where other settings live

Use the settings search at the top of the application to find a control directly.

- **Sources → Settings → Advanced:** import concurrency, retry and recovery preferences.
- **Metadata → Settings:** information updates and identification intervals, with manual actions and maintenance under **Advanced**.
- **Debrid → Settings:** service accounts, availability checks, operation limits and cleanup.
- **Storage → Settings:** storage mode, history retention and maintenance.
- **Security:** Secrets, two-step verification and signed-in sessions.
- **Network:** trusted incoming proxies and outgoing connections.
- **Public access:** offered visitor pages and request limits.

Page size, date display and refresh preferences are available from the application's browser settings. They apply to that browser. Leave defaults in place unless you need different behavior.
