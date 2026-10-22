# Application settings

[Back to README](../README.md) · [Deployment settings](SETTINGS.md)

Frequently used settings stay on their existing pages. Less common operational settings are folded until needed. Administrator settings are stored in PostgreSQL with independent revisions; they survive restarts and cannot silently overwrite a concurrent edit. Starting values are listed below; incoming Stremio budgets now use one policy for private and public installations.

## Available controls

| Where | What can be adjusted | Default and bounds |
| --- | --- | --- |
| Debrid → Settings → Operation capacity | Cache update jobs, download preparations, cleanup jobs and public TorBox probes per application process | 4 / 4 / 4 / 1. The first three allow 1–32; public probes allow 1–16. A cache update may issue several provider calls. |
| Network → Outgoing → Proxies → Request timeouts | Individual metadata requests, debrid requests and metadata inventory downloads | 60 / 60 / 600 seconds; bounds 5–600 / 5–600 / 30–3600. Source timeouts stay in each source definition. |
| Network → Incoming → Request timeouts | Stremio, Torznab and shared catalog processing and delivery budgets | 70 / 20 / 20 seconds; bounds 10–600 / 5–600 / 5–600. Applies to private and public connections. |
| Storage → Settings → History retention | History duration and ordinary cleanup interval | 7 days and 30 seconds; bounds 1–3650 days and 5–3600 seconds. |
| Sources → Settings → Advanced settings → Recovery | Automatically resume imports interrupted by a process or connection failure | On. Turning it off leaves interrupted imports paused for manual resume. |
| Interface preferences, beside About | Page size, date format, time zone and automatic refresh | 50 rows; browser language and time zone; Normal refresh. Page sizes: 25/50/100; date formats: browser/English/ISO; time zones: browser/UTC. |

Capacities apply to newly admitted work. Reducing a limit does not interrupt active work, replace its permits or lose its cleanup ownership. Increasing a limit lets queued work use the new capacity. Provider pacing remains enforced. Recoverable AllDebrid cache work uses the cache update queue; direct public TorBox probes have their own limit.

Timeouts apply to new requests. A parent operation or incoming request can have a shorter remaining budget. Outgoing timeouts include reading the response body; the inventory download timeout excludes subsequent database processing. Incoming request leases outlive their delivery deadline so another replica cannot admit a replacement while the old request is still authorized to write. These budgets differ from **Debrid → Stream response timeout**, which controls how long a response waits for availability checks, and from an active file download's maximum lifetime.

Cleanup remains progressive and transactional. Reducing retention can permanently delete older diagnostics; increasing it cannot restore deleted data. Published torrents, metadata, matching decisions, pending jobs and evidence needed to continue an import are preserved. Stremio history also retains its 10,000-request cap and up to 1,000 result summaries per request. A backlog is processed in bounded batches rather than one unbounded deletion.

Recovery does not override manual pauses, quota holds, cancellations or authentication, certificate and configuration failures. Metadata update and identification automation retain their existing opt-ins and durable schedules.

Interface preferences are browser-local, shared between public and administrator pages, and do not change server schedules. Reduced activity refreshes active controls every 15 seconds, statistics every 45 seconds and ordinary lists every 90 seconds, instead of 5/15/30 seconds. Dates in editable expiration fields use the selected display time zone while keeping their saved instant. Source schedule time zones remain independent.

## Intentionally unchanged

| Area | Current behavior and reason |
| --- | --- |
| Identification resources | Automatically chooses 1–10 concurrent workers from PostgreSQL pool capacity. This is the number of local records being identified concurrently, not a match confidence threshold or TMDB request rate. No override was added. |
| Active browser download duration | A transfer through the application can last at most 24 hours. This is separate from preparing a debrid item or waiting for a search response. The ten-minute authorization ticket remains a separate protection. No duration control was added. |
| Administrator session duration | 12 hours; unchanged. |
| Processing batches | Matching fetches up to 250 queued records and commits in groups of up to 25; cleanup uses bounded batches of 5,000 with a ten-second pass budget. These balance database overhead, memory use and checkpoint frequency. No new batch-size control was added. |
| Error retries | Source quota retries are already configurable. Metadata has up to four immediate attempts, then a durable retry clock; cleanup backs off from 15 minutes up to 48 hours. Writes in the browser are never automatically replayed. No additional retry switches were added. |
| Matching reliability | Candidate overflow, contradictory IDs, ambiguous works, release years, seasons, episode numbering and season packs retain their safeguards. These decide whether a result is safe to publish; loosening them could create incorrect matches. Manual review remains available. |
| Authentication and network protections | Password and two-step verification protections, replay checks, CSRF/origin checks, trusted-hop parsing, TLS validation, destination restrictions and encrypted administrator secrets remain enforced. No security opt-outs were added. |
| Payload bounds | Source definitions remain limited to 128 KiB, public Stremio configurations to 8 KiB and public Torznab configurations to 6,000 bytes. Search, response and file limits retain their existing bounded contracts. |
| Publication and recovery evidence | One active collection owns a source; full imports publish atomically on success. Diagnostic retention never discards live publication or evidence required to resume incomplete work. |

See [deployment settings](SETTINGS.md) and [source configuration](CONFIGURATION.md) for settings already available through environment variables, command-line options or individual definitions.
