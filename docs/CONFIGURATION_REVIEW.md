# Application settings

[Back to README](../README.md) · [Deployment settings](SETTINGS.md)

Frequently used settings stay on their existing pages. Less common operational settings are folded until needed. Administrator settings are stored in PostgreSQL with independent revisions; they survive restarts and cannot silently overwrite a concurrent edit. Starting values are listed below; incoming Stremio budgets now use one policy for private and public installations.

## Find a setting

Use **Search settings** in the sticky top bar, on desktop or mobile. You can also press **Ctrl+K** or **⌘K**. Search by name or topic, then choose a result; the matching page opens, folded sections expand, and the field is highlighted. For settings belonging to a source, connection, remote catalog or proxy, choose which one to edit first.

Typing searches a local index of setting names. It does not send search text to the server, inspect credentials or save changes. Unsaved edits keep their normal navigation protection. A disabled field stays disabled; a source option that needs additional configuration opens its owning section. Public visitors only see the controls enabled for them and their browser preferences.

## Available controls

| Where | What can be adjusted | Default and bounds |
| --- | --- | --- |
| Debrid → Settings → Advanced → Operation capacity | Cache update jobs, download preparations, cleanup jobs and request-scoped public availability checks per application process | 4 / 4 / 4 / 1. The first three allow 1–32; public probes allow 1–16. A cache update may issue several provider calls. |
| Network → Outgoing → Proxies → Advanced → Request timeouts | Individual metadata requests, debrid requests and metadata inventory downloads | 60 / 60 / 600 seconds; bounds 5–600 / 5–600 / 30–3600. Source timeouts stay in each source definition. |
| Network → Incoming → Trusted proxy → Advanced → Request timeouts | Stremio, Torznab and shared catalog processing and delivery budgets | 70 / 20 / 20 seconds; bounds 10–600 / 5–600 / 5–600. Applies to private and public connections. |
| Network → Incoming → Trusted proxy → Advanced → Browser downloads | Maximum active transfer duration and validity of new file links | 24 hours (1–168) and 10 minutes (1–60). Applies to public and administrator downloads. |
| Network → Incoming → Trusted proxy → Advanced → Configuration limits | Maximum new source definition and public link configuration sizes | 128 KiB per source (16–128); 8,192 bytes for Search/Stremio (1,024–8,192); 6,000 bytes for Torznab (1,024–6,000). |
| Metadata → Settings → Advanced → Identification processing | Local identification workers, fetched queue size and saved batch size | Automatic workers (`0`, or 1–32); 250 queued records (10–2,000); 25 records per transaction (1–100). |
| Metadata → Settings → Advanced → Matching reliability | Minimum close-title similarity and ambiguity margin | 92% (92–100) and 8% (8–25). Higher values make automatic matches more conservative. |
| Metadata → Settings → Advanced → Information processing | Concurrent information requests, fetched queue size and saved batch size | 16 workers (1–32); 100 queued records (1–1,000); 16 records per saved batch (1–100). |
| Metadata → Settings → Advanced → Information retries | Immediate attempts and initial retry delays, followed by saved update/record retry clocks | 4 request attempts (1–10), starting at 1 second (1–60); updates back off from 60 to 900 seconds; deferred records from 60 to 86,400 seconds. Initial delays allow 10–3,600 seconds; maximum delays allow 60–86,400 for updates and 60–604,800 for records. |
| Sources → Settings → Advanced → Temporary source errors | Retries for temporary GET/HEAD errors and increasing delay | 3 retries (0–10), starting at 2 seconds (1–60), capped at 8 seconds (1–900). Captured for each new import. HTTP 429 controls remain separate. |
| Debrid → Settings → Advanced → Availability retries | Increasing delays between incomplete availability checks | Starts at 15 minutes (1–1,440), capped at 48 hours (1–168). |
| Security → Settings → Advanced → Authentication settings | New session duration, active session capacity, sign-in limits and two-step setup validity | 12 hours (1–168); 256 sessions (1–1,024); 10 attempts per address/minute (1–60) and 60 total/minute (1–600); 10-minute setup (5–30). |
| Storage → Settings → Advanced → History retention | History duration, ordinary cleanup interval, rows per table batch and pass duration | 7 days (1–3,650); 30 seconds (5–3,600); 5,000 rows (100–20,000); 10 seconds (1–60). |
| Sources → Settings → Advanced → Import execution → Recovery | Automatically resume imports interrupted by a process or connection failure | On. Turning it off leaves interrupted imports paused for manual resume. |
| Interface preferences, beside About | Page size, date format, time zone and automatic refresh | 50 rows; browser language and time zone; Normal refresh. Page sizes: 25/50/100; date formats: browser/English/ISO; time zones: browser/UTC. |

Capacities apply to newly admitted work. Reducing a limit does not interrupt active work, replace its permits or lose its cleanup ownership. Increasing a limit lets queued work use the new capacity. Provider pacing remains enforced. Recoverable AllDebrid cache work uses the cache update queue; other request-scoped public availability checks use the public-check capacity.

Timeouts apply to new requests. A parent operation or incoming request can have a shorter remaining budget. Outgoing timeouts include reading the response body; the inventory download timeout excludes subsequent database processing. Incoming request leases outlive their delivery deadline so another replica cannot admit a replacement while the old request is still authorized to write. These budgets differ from **Debrid → Stream response timeout**, which controls how long a response waits for availability checks, and from an active file download's maximum lifetime.

Browser download duration starts when a transfer begins, independently of preparation. Link validity determines how long a newly generated link can be opened, not how long an already-started transfer can run. Existing links keep their original expiry. Session revocation, current public Search permission and destination restrictions still guard new transfers. File contents are streamed without being saved to the instance's disk.

Identification resources control local record processing, not match confidence or TMDB request rate. Automatic capacity chooses 1–10 workers from the database pool; the interface shows the current value. An explicit worker limit applies to the next batch. Actual parallelism also depends on pending work and available database connections. Information requests still respect the configured TMDB request rate. Saved batches cannot exceed their fetched queue.

Matching thresholds can tighten the existing close-title and ambiguity checks. Changing them withdraws and queues automatic results for review under the new policy; identification processes them when enabled. Manual decisions are preserved. Saving unchanged thresholds does not requeue the catalog. Candidate publication and policy changes share a database fence, so another process cannot commit an automatic result under an obsolete policy. Contradictory identifiers, incompatible years, invalid numbering, episode conflicts and candidate overflow retain their safeguards.

Retries apply to recoverable failures and preserve provider pacing and quota deadlines. Metadata retries use the saved settings when the next request or retry is scheduled. Temporary source retries are frozen in each new run, including scheduled work; pauses, resumes and restarts retain that snapshot. Existing imports keep their original behavior. Availability retry delays apply to incomplete debrid checks; they do not turn failed temporary-item deletions into automatic cleanup loops. Failed deletions retain their ownership evidence and manual attention. Browser writes are never replayed automatically.

Authentication duration applies to new sessions and new two-step setups; existing expiry times remain unchanged. Lowering session capacity does not revoke active sessions. Sign-in limits are shared across application replicas, and the total limit must cover the per-address limit. Password verification, two-step replay protection, CSRF checks and session revocation remain enforced.

Configuration limits apply to new saves and newly generated public configurations, including their optional filters. Lowering a limit does not make an existing definition or encrypted link unreadable. The hard protocol ceilings remain unchanged; the editor displays the instance's source limit before saving. Runtime settings cannot disable encryption, TLS validation or destination checks.

Cleanup remains progressive and transactional. Reducing retention can permanently delete older diagnostics; increasing it cannot restore deleted data. Published torrents, metadata, matching decisions, pending jobs and evidence needed to continue an import are preserved. Stremio history also retains its 10,000-request cap and up to 1,000 result summaries per request. A backlog is processed in bounded batches rather than one unbounded deletion.

Recovery does not override manual pauses, quota holds, cancellations or authentication, certificate and configuration failures. Metadata update and identification automation retain their existing opt-ins and durable schedules.

Interface preferences are browser-local, shared between public and administrator pages, and do not change server schedules. Reduced activity refreshes active controls every 15 seconds, statistics every 45 seconds and ordinary lists every 90 seconds, instead of 5/15/30 seconds. Dates in editable expiration fields use the selected display time zone while keeping their saved instant. Source schedule time zones remain independent.

## Safeguards that remain enforced

| Area | Current behavior and reason |
| --- | --- |
| Matching evidence | Candidate overflow, contradictory IDs, release years, seasons, episode numbering and season packs cannot be bypassed by changing thresholds. Manual review remains available. |
| Authentication and network protections | Password and two-step verification protections, replay checks, CSRF/origin checks, trusted-hop parsing, TLS validation, destination restrictions and encrypted administrator secrets remain enforced. |
| Protocol and response bounds | Configurable creation limits can be reduced within their hard ceilings. Search results, response bodies, archive expansion and file handling retain their bounded contracts. |
| Publication and recovery evidence | One active collection owns a source; full imports publish atomically on success. Diagnostic retention never discards live publication or evidence required to resume incomplete work. |

See [deployment settings](SETTINGS.md) and [source configuration](CONFIGURATION.md) for settings already available through environment variables, command-line options or individual definitions.
