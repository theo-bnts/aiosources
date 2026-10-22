# Share or import a catalog

[Back to README](../README.md) · [Source configuration](CONFIGURATION.md) · [Security policy](../SECURITY.md)

Share selected records with another AIOSources instance, or keep a local copy of a catalog shared with you. Each connection has its own access and can be stopped independently.

## Share your catalog

1. Configure a trusted HTTPS reverse proxy and set `PUBLIC_URL` to its exact HTTPS origin. Keep the backend port private or loopback-only. This setting declares the deployment origin; it does **not** terminate TLS. Forwarded headers cannot enable sharing, and an absent/non-HTTPS public origin prevents activation.
2. Open **Connections → Shared catalogs → Settings → Create share**. Give each recipient their own share. **Filters → Sources** includes all current and future sources by default. Use its multiselect dropdown to restrict a recipient to specific sources, including imported catalogs.
3. Select exportable fields: `title`, `size`, `info_hash`, `published_at`, `category`, `categories`. The server enforces this allowlist; recipients cannot expand it or select extra providers.
4. Save the one-time generated password, then explicitly enable the share. Send its catalogue URL and password through a secure channel. Never append the password to the URL or reuse the administrator password.

New shares created in the panel are disabled; API clients explicitly control activation with `enabled`. Passwords contain 256 bits of randomness and are stored only as domain-separated hashes. They authorize only `GET`/`HEAD` on their catalogue feed, not administration, source definitions, secrets, raw archives or writes. Rotation replaces the password; disabling/revoking a share denies subsequent requests. Permission checks run on every page, and responses use `Cache-Control: no-store`.

Expiration and limits are in **Advanced**. Expiration is optional; an expired share rejects access without deleting published or imported data. Per-share limits apply across instances: 1–3600 requests per rolling minute (default 120) and 1–16 simultaneous catalogue downloads (default 2). Valid `GET` and `HEAD` requests consume the request allowance; invalid passwords do not. A download holds its concurrency lease until its response finishes or disconnects; interrupted leases expire. Limit responses are HTTP 429 with `Retry-After`. Policy changes, password rotation and revocation are recorded in Logs without secret values.

Download/magnet URL fields and arbitrary nested objects are not exportable. Native source identifiers are opaque hashes because an original Torznab GUID or JSON ID may itself contain credentials. Authorized field values remain unchanged, including zero, null and large integers; do not place secrets in free-text fields you authorize. Imported provenance is retained across subsequent sharing.

**Revocation is not remote deletion or DRM.** It stops future access, not copies already downloaded. A recipient can retain or re-share those copies outside this application's controls. “Last completed download” means the server delivered the final page, not that the recipient committed its import.

## Import a shared catalog

1. Open **Sources → Remote catalogs → Settings → Add remote catalog**.
2. Enter the HTTPS URL supplied by the other instance's administrator.
3. Under **Sharing password**, choose an existing secret or use **Add sharing password** to store the supplied password securely.
4. Choose the automatic update interval, save, then enable the connection with its switch. New connections start disabled.

For an immediate import, choose **Run manual import**. **Sync changes** is selected by default: the first sync downloads the catalog and later syncs fetch changes. Choose **Full resync** in the same menu when you need a fresh copy. Both keep your current published copy available until the import succeeds.

Use **Statistics** for imported, pending and locally identified records. **History** contains only remote imports, with the same run details, filters and pagination as source history. Native Sources history does not include remote imports. **View imported data** opens Torrents filtered to the connection; native source cards offer the same action. **Delete** removes the connection, its local torrent copy, saved progress, history and local identifications. Active imports must finish or pause first. Secrets, shared metadata and debrid caches are retained.

Only authorized published torrent fields and their provenance are imported. TMDB information, remote matching decisions, manual reviews, debrid caches, secrets, announce URLs, source definitions and remote history are not transferred. Identification runs against your own local metadata and can be reviewed independently.

## Advanced synchronization behavior

The editor writes ordinary strict source JSON with `"adapter": "json"` and `"catalog": true` inside `http`; it does not introduce another adapter or configuration store. The fixed versioned envelope, provenance identity, field policy and cursor mapping are validated rather than user-remapped. An endpoint cannot contain credentials, query parameters or a fragment. The private definition remains compatible with JSON import/export and existing source-editor bookmarks; importing a definition is not importing catalogue records. Remote connections have their own page under Sources rather than appearing in the native source list. Source revisions protect concurrent edits, and existing runs keep their immutable source snapshot. See the [configuration guide](CONFIGURATION.md) and [JSON reference](JSON_REFERENCE.md); legacy YAML definitions require [offline migration](CONFIGURATION.md#migrating-existing-sources).

New remote connections default to **automatic synchronization every 60 minutes**, ready to start after the connection is enabled. The connection editor contains only **Automatic synchronization** and a whole-minute interval from 1 to 525600. Connection activation and synchronization activation are separate controls. Remote connections do not appear in native source scheduling and do not use Cron, known-page boundaries or periodic Full reconciliation. Validating or saving makes no remote HTTP request and does not start a manual sync; an enabled interval waits until its next due time. **Run manual import** opens the same action dialog as native sources, with **Sync changes** selected by default and **Full resync** available for downloading a fresh snapshot. Both use the durable import engine and preserve the current published copy until completion. Outcomes appear in Logs and the dedicated remote catalog history.

Existing remote definitions retain their activation choice: manual connections stay manual, disabled synchronization stays disabled, and elapsed intervals convert to whole minutes rounded up. Legacy Cron converts to a fixed interval between its first two occurrences after a fixed reference date; daily and weekly frequencies are preserved, while irregular calendar rules become a representative interval. Review the displayed interval before saving. Saving the remote form writes `catalog_sync` and removes the legacy `schedule` block. The database upgrades remote timing clocks once; native deadlines, published copies, checkpoints and immutable run snapshots remain unchanged. An existing budget-paused scheduled Full can finish with its saved cursor and budget under the converted interval. Explicit holds and changed source revisions still require manual action.

The first sync downloads a consistent full snapshot. Later syncs use signed, share-bound checkpoints for additions, exact replacements and deletion tombstones. Both full and incremental imports stage privately; only successful traversal publishes the remote namespace and checkpoint in the same transaction. Missing fields are removed rather than merged with stale values. Failed, cancelled, paused and preview runs leave the last published copy unchanged. Saved progress survives interruptions; a resumed run continues from its cursor. Manual holds remain paused, and an older run cannot overwrite a newer committed mirror.

Changing permissions or rotating a password invalidates unfinished page cursors. After updating the vault credential when necessary, start a new sync: an authentic older checkpoint requests an authoritative full snapshot, removing records or fields no longer authorized. Changing the endpoint also starts a fresh snapshot. Imported values and provenance are read-only in the panel; records originating from this installation are ignored to prevent replication loops.

## Transport and storage boundaries

- Remote connections require normal certificate and hostname verification. There is no insecure TLS option. Private LAN/loopback HTTPS is supported with a certificate trusted by the receiving process.
- Remote redirects and environment HTTP proxies are disabled. DNS destinations are checked and pinned at connection time; unspecified, multicast and link-local destinations are rejected.
- `GET /api/catalogs/{share-id}` accepts `Authorization: Bearer <sharing-password>`, an optional `limit` from 1 to 1000 (default 100), and either `checkpoint` or `cursor`. A non-final page has `next_cursor`; only the final page provides the next durable `checkpoint`. Version 1 responses contain `instance_id`, `mode`, `items` and opaque origin identities.
- Ordinary writes cannot change publication history. Automatic cleanup expires diagnostic history according to Storage settings (seven days by default) and compacts the sharing journal into its latest reference state plus recent changes. Older checkpoints restart with Full. Plan disk capacity for the live catalog, metadata and unfinished staging; new raw bodies are not stored.
- Back up PostgreSQL and the state volume together. The installation identity, signing key, shares, journal and checkpoints live in PostgreSQL; vault decryption and source definitions also require the state volume.
