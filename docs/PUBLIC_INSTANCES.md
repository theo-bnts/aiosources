# Public instance mode — design proposal

Status: evaluation only. Public instance mode is not implemented or enabled by this document.

An instance could optionally offer a public Stremio configuration page. Visitors would use their own debrid accounts, while all administration, source configuration, secrets and history remain behind administrator authentication. The existing private installation would keep working independently.

## Interface

- Add an opt-in **Public access** section to **Stremio → Settings**, disabled by default. Do not add another sidebar category or duplicate debrid settings.
- When enabled, the homepage shows service key fields, **Create installation link**, and **Admin login**. At least one supported service key is required. When disabled, the homepage keeps the current login behavior and public addon routes stop working.
- Keep the same field spacing, buttons, validation, notices and source display names as the existing interface. Key fields are masked and never prefilled from administrator settings.
- Visitors receive an installation URL and the usual Stremio/AIOStreams stream format. They never receive the administrator's installation token, account links, source definitions or announce addresses.
- Explain briefly that the host processes the keys without saving them, and retains request diagnostics without a visitor identity. The requested media and returned filenames can appear in administrator history.

## Stateless credentials

Accept keys in the body of a same-origin HTTPS POST, validate supported services, and return an authenticated encrypted URL configuration. Use a versioned map of service identifiers and credentials so another provider can be added later without changing the URL format. Reject unknown services, oversized payloads and a configuration without a usable key.

Reuse the vault's authenticated encryption through a separate, explicitly allowed public-installation purpose. Include the instance's public-access generation inside the encrypted payload. Disabling public access advances that generation; re-enabling it does not reactivate old links. Keep this generation separate from the private installation token and secret revisions.

No visitor credentials, encrypted installation configurations or installation URLs are written to PostgreSQL, Secrets, request history, activity logs, exports, browser storage or telemetry. Decrypted credentials live only for the request and bounded in-flight work. Clear byte buffers where possible; do not promise that a managed runtime can erase every transient copy. Background work that requires a visitor key cannot survive a restart without that visitor making another request.

An encrypted installation URL is still a bearer credential: anyone who obtains it can use that configuration. Use `Cache-Control: no-store` and `Referrer-Policy: no-referrer`, redact complete public-installation paths from application logs, and avoid third-party scripts or analytics on the setup page. Document equivalent redaction for a reverse proxy or CDN. HTTPS alone does not prevent URL secrets appearing in access logs or browser history; see [OWASP's URL information exposure guidance](https://community.owasp.org/vulnerabilities/Information_exposure_through_query_strings_in_url).

The visitor must trust the host. Encryption prevents readable keys in the installation URL; it cannot prevent the machine processing a request, or a modified application, from reading them. The administrator UI must offer no way to inspect, export or recover visitor credentials.

## Shared availability, separate accounts

The current availability cache is indexed by service, account fingerprint and info hash. Do not remove its account boundary. Add a separate shared availability projection, indexed only by **service and info hash**, for observations verified as reusable across accounts.

| Data | Shared availability | Request history | Transient playback |
| --- | --- | --- | --- |
| Service, hash, observation time and expiry | Yes | Service and safe result references | Yes |
| Cached state, file path and file size | Yes | Selected display fields | Yes |
| Provider account ID, magnet ID or account file ID | No | No | Only for that account's operation |
| API key, authorization header or encrypted installation payload | No | No | Only for the visitor's request |
| Provider download URL, private source URL or announce URL | No | No | Visitor download URL only; no administrator private URL |

The existing `AIOFile` persistence already excludes playback links and account file IDs. Reuse its safe file projection and the existing file selection and stream formatting functions. Resolve provider IDs and playback URLs again under the visitor's own credentials. A cached observation never grants access to somebody else's account.

Check each selected service independently. If AllDebrid is fresh and TorBox is missing or expired, check TorBox only. Both administrator requests and visitor requests can consume proven shared observations and publish new ones. Coalesce availability probes by service and hash, but never coalesce account-bound playback or authentication across different credentials.

Keep **cached**, **uncached** and **unknown** distinct. Authentication failures, quotas, transport errors, invalid responses and account restrictions are unknown, not proof that a torrent is globally uncached. They must not overwrite a valid positive observation with a negative one. Use the existing cache lifetime setting rather than introducing another schedule or user-facing interval. Resetting availability advances its generation so in-flight checks cannot repopulate a cache that was just reset.

An AllDebrid account entry still downloading is not a global negative. Likewise, do not automatically promote an account's completed-download status into shared availability unless its meaning has been verified across accounts. Record the kind of provider evidence internally so an account observation cannot accidentally become a global one.

Validate cross-account reuse with two independently authorized accounts before enabling it for a provider. Documentation describes availability endpoints, but does not establish that every account-specific result has an identical meaning for every subscription or network location.

## Cached playback and private sources

For a private source, or a source whose privacy is unknown, a public response requires a fresh positive cache observation and a usable file selection. A season pack must contain the requested episode; a globally ready torrent with an unavailable file list cannot certify that episode.

Return the existing stream description and compatible `behaviorHints`, plus an AIOSources playback URL carrying the visitor's encrypted configuration. Do not return private-source `sources`, announce parameters, torrent download URLs, or a raw torrent/magnet fallback. Keep the hash needed for provider resolution server-side for these streams. AIOStreams follows the playback URL; it does not need the private source's download credentials.

At playback, validate that the requested source record is still published for the requested media and resolve the selected file under the visitor's account. Never fall back to an administrator key or announce URL. If the cache has disappeared, invalidate the relevant observation and report that cached playback is unavailable. Do not try downloading a private torrent. For a source explicitly marked public, uncached playback may use a plain hash or magnet without any administrator credentials.

Provider availability can change between listing and playback. Where an API lacks an atomic cached-only playback operation, the adapter must handle that race explicitly and must not claim that checking first makes provider-side changes impossible.

## Existing history and Logs

Keep public calls visible in the application's existing histories. Do not create a separate public-history page or a visitor database.

- **Stremio → History:** retain normalized media identifiers, request kind, an origin label (**Private installation** or **Public installation**), timestamps, duration, outcome, match and exclusion counts, and safe display fields for returned results. Keep details behind the existing row arrow. Record stream requests even when their results came entirely from shared cache. Record manifest and playback requests with an appropriate outcome; a playback entry reports whether a link was resolved, without storing that link. Non-stream entries do not claim to have returned stream results.
- **Debrid → History:** retain actual availability and cleanup operations, independently per service. In the operation details, record bounded provider-call diagnostics: a fixed operation name, duration, HTTP status when known, a normalized error code, and processed/result counts. A cache hit is identified as reused availability and does not fabricate an external provider call.
- **Logs:** retain meaningful service errors and recovery messages through the existing activity log. Do not copy every successful history entry into Logs. Log malformed public requests as bounded aggregate failures so unauthenticated traffic cannot force an unbounded number of database writes.

Use explicitly typed allowlists at the storage boundary. Never serialize a request object, an arbitrary error string, headers, URL, POST body, provider response body or debrid account inventory into history. Do not save IP addresses, email addresses, usernames, account fingerprints or a stable visitor identifier. A random operation ID can link a request to its provider work without identifying the visitor or containing credentials.

Extend existing filters and statistics with request origin and kind where useful. Keep newest-first ordering, the standard 50-row pagination, bottom pagination controls, status colors and automatic refresh. Public rows remain visible only to administrators. Apply the existing seven-day retention and Stremio's 10,000-request cap, plus a fixed per-operation diagnostic cap. Do not add a request-time scan of the unbounded history.

The historical media and filenames are activity data, even without a visitor identity. The public setup page must disclose that they are retained. This is compatible with keeping keys and account details out of storage; it is not a claim that the instance retains no request information.

## Provider constraints

**TorBox:** the documented authenticated [`checkcached` endpoint](https://www.postman.com/torbox/torbox-api/documentation/b6l9hbv/main-api?entity=request-29572726-062b717f-4866-4fc0-a3e6-6e4c2520eefa) supplies availability and optionally file metadata without creating a torrent. Its successful cache observations are the straightforward candidate for shared availability. Playback still requires the visitor's own account operation and download link.

**AllDebrid:** the current adapter establishes availability using an account inventory and, when necessary, magnet upload. The official [API documentation](https://docs.alldebrid.com/) exposes an upload `ready` result and account magnet operations; it also documents server/network restrictions. A readiness probe can therefore create account entries. A stateless server cannot guarantee deleting a visitor-owned temporary entry after a crash if it no longer has that visitor's key. Deleting an existing entry belonging to the visitor is unacceptable.

The conservative design is to use the administrator's existing recoverable AllDebrid probing and cleanup for missing shared observations, and let verified successful visitor playback contribute a positive observation too. Public-source playback remains account-bound. This provides reuse in both directions without probing many candidates into a visitor's account. It requires a configured administrator AllDebrid account to fill a cold AllDebrid cache for private-source results.

Fully symmetric background probing with arbitrary visitor AllDebrid keys is not yet a proven safe capability under the no-key-storage requirement. Do not silently enable it or pretend a missing observation is cached. A read-only provider capability or an explicitly accepted change to that behavior would be needed to remove this constraint. TorBox-only public mode does not have this temporary-magnet constraint.

Represent those differences with a small provider capability registry: credential validation, read-only availability support, shareable result projection, cached playback and side effects. Reuse existing provider clients, matching, file selection, pacing and error handling rather than creating a second debrid stack.

## Implementation and acceptance checks

Implementation would proceed through the existing modules: scoped vault sealing; additive settings and shared-cache/history migrations; request-scoped debrid credentials and playback policy; isolated public routes; the homepage and Stremio settings. Existing private routes must retain their behavior.

Before enabling public access, verify:

1. Disabled mode, malformed/tampered configurations, missing keys and revoked generations deny public access. A public configuration never authenticates an administrator API or a private installation route.
2. Sentinel keys, encrypted URLs, announce addresses and provider URLs are absent from database rows, histories, logs, error responses, exports and browser storage after success, failure, cancellation and restart.
3. Histories contain real public requests and provider operations with useful counters, safe details, newest-first ordering and bounded retention, including cache-hit-only requests.
4. AD and TB state and expiry are independent. Unknown/error responses do not poison known shared observations. Cache resets reject stale writers. Two authorized accounts demonstrate equivalent reusable positives before a provider publishes them globally.
5. Private uncached/unknown torrents are excluded. Cached movies and exact episodes in season packs play under the visitor account without an announce URL or administrator fallback. Cache disappearance produces a bounded failure without starting a private-source download.
6. Provider inventory, file IDs and download links never cross accounts. Existing user magnets are not removed. A crash does not leave work that requires a saved visitor key for recovery.
7. Public traffic has bounded concurrency, request sizes, timeouts and per-client/global budgets, with capacity reserved for administrator work. Preserve provider rate limiting; do not allow arbitrary provider endpoints, source URLs or hash-warming lists from the public form.
8. The public homepage, credential fields, copy/install actions, notices and admin login follow the existing interface patterns; browser tests cover both enabled and disabled modes.

No live two-account validation was performed during this evaluation. Existing application and browser tests do not constitute validation of an unimplemented public mode.
