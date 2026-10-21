# Configuration options review

[Back to README](../README.md) · [Current deployment settings](SETTINGS.md)

This inventory separates useful future settings from fixed behavior that can stay automatic. It covers operational choices an administrator could reasonably tune, grouped by purpose. Protocol identifiers, database column sizes, cryptographic constants and library implementation details are not user settings.

These are proposals, not newly available controls. Existing source schedules, request pacing and quotas, import concurrency, TMDB request rate, identification intervals, cache lifetime, response waiting time, storage mode, connection filters, proxy routing and public request limits are already configurable and are not missing options.

## Worth making configurable

| Setting | Current behavior | Benefit and suggested location |
| --- | --- | --- |
| Identification resource budget | Automatically selects 1–10 workers from the PostgreSQL pool capacity. | An optional bounded override could reserve resources on small servers or use larger servers. Keep Automatic as the default, in Metadata → Settings → Advanced settings. |
| Debrid operation capacity | Four cache/preparation slots, four cleanup slots, four download-preparation slots and one shared public cache-probe slot per process. | Separate bounded limits could adapt to provider quotas and instance load. Group them in Debrid advanced settings; they are different from visitor request admission limits. |
| Outgoing service request timeouts | Metadata and debrid clients use 60 seconds; the TMDB inventory download uses 10 minutes. Source request timeouts are already individually configurable. | An advanced timeout for each service class could help slow connections and proxies without changing source pacing. Place it under Network → Outgoing. |
| Administrator session duration | 12 hours. | A bounded duration could suit a shared machine or a private installation. Keep it in Security advanced settings. |
| Browser download duration | Transfers stop after at most 24 hours. | A bounded setting could accommodate large files and slow connections. Keep download authorization and destination checks independent of it. |
| Connection request budgets | Catalog/Torznab requests have a 20-second budget; Stremio has a 70-second budget. | Advanced budgets could help unusually large catalogs. They must remain consistent with leases and provider waiting time, rather than exposing unrelated timeout fields. |
| Interface refresh pacing | Active controls refresh every 5 seconds, collection statistics every 15 seconds and ordinary reads every 30 seconds. | Lower priority: one Normal / Reduced activity choice could reduce background requests without exposing a separate interval on every page. |

The first three have the clearest operational value. Any future control needs bounds, revision handling, restart persistence and the same effective limit in the workers as the value shown in the interface.

## Fixed settings that can stay fixed

| Area | Current fixed behavior | Why a new control is not necessary |
| --- | --- | --- |
| Diagnostic retention | Seven days for expired history content. Live data and evidence needed to continue an import are preserved. | A single retention policy keeps storage predictable and follows the existing product choice. It is not a promise to delete every preparation row after seven days. |
| History cleanup cadence | Runs at startup, every 30 seconds when idle and after one second when a pass removed content. | Backlog handling should remain automatic. |
| History cleanup batches | Bounded batches of 5,000, with a 10-second pass budget. | These are interruption and database-load controls, not a user workflow. |
| Stremio request history bounds | At most 10,000 requests within seven days, and 1,000 display summaries per large response, alongside the full returned count. | Keeps request diagnostics bounded without hiding the real result count. |
| Storage measurement cadence | PostgreSQL storage is measured every minute. | Independent of how often a browser page refreshes; no additional page control is needed. |
| Pagination | 50 rows per interface page. | A consistent page size is simpler. API-specific page limits retain their own contracts. |
| Displayed dates | Use the browser's language and time zone. | No separate application preference is needed. |
| Automatic recovery | Enabled for interrupted work with saved checkpoints; manual/error holds and cancellations retain their existing rules. | Recovery should follow the recorded cause, rather than a second global switch. |
| Import defaults | Preview starts at three pages; other defaults come from the selected source or the run's explicit overrides. | The source and the per-run form already provide the relevant controls. |
| Import ownership and publication | One active collection owns a source; Full imports publish atomically after successful completion. | These protect correctness and should not be disabled. |
| Text search bounds | Up to 500 query characters and 100 source filters; searchable text has a bounded 128 KiB UTF-8 prefix per occurrence. | Bounds query and indexing cost. Original stored fields are not rewritten by the search projection. |
| Matching batches | Fetch up to 250 queued records, using transaction groups of up to 25 with smaller groups for short queues. | The worker already adapts to available work. Resource concurrency is the useful potential setting, not every batch size. |
| Matching candidates and decisions | Candidate overflow, contradictory identifiers, ambiguous works, dates and invalid numbering prevent unsafe matches. | Loosening these rules is not a normal setting; corrections should retain testable matching behavior. |
| TMDB incremental window | A 14-day change window and dated inventory recovery are handled automatically. | These follow upstream capabilities and checkpoint semantics. |
| Metadata request retries | Up to four immediate attempts with bounded delays; interrupted/failed automatic updates use the durable retry clock, with delays capped at 15 minutes. | Provider deadlines and restart recovery should stay coherent. The ordinary update interval is already configurable. |
| Debrid cleanup retries | Exponential retry delay from 15 minutes up to 48 hours. | Temporary provider failures should not become a tight retry loop. |
| Final checkpoint budgets | Short bounded writes preserve completed work while shutting down or cancelling. | These are internal completion guarantees. Source and service request timeouts are separate concerns. |
| Browser retries | Reads have a bounded automatic retry; writes are not automatically replayed. | Avoids duplicate actions and preserves clear failure handling. |
| Live event transport | A 15-second heartbeat and bounded write deadlines detect disconnected consoles. | This is transport maintenance, separate from import or metadata schedules. |
| Incoming settings propagation | Other processes reload the shared incoming policy within one second. | Keep enforcement automatic after saving. |
| Sign-in protection | The per-address front gate allows at most ten attempts per minute, alongside durable authentication protections. | Keep a protective minimum independent of public addon request limits. |
| Two-step verification | Six-digit authenticator codes, replay protection and a ten-minute enrollment window. | These are interoperability and account-protection rules. |
| Download authorization tickets | Ten-minute expiry, bound to the relevant session or public installation state. | Expiry protects the generated link; it is separate from the allowed duration of an already authorized transfer. |
| Payload and definition bounds | Source definitions are bounded to 128 KiB, public Stremio configurations to 8 KiB and public Torznab configurations to 6,000 bytes. Other endpoints retain their own smaller body, query and response limits. | Input bounds protect memory, URLs and client interoperability; do not make arbitrary payload growth an ordinary option. |
| Network and credential safeguards | TLS validation, trusted-hop parsing, destination checks, CSRF/origin checks, encrypted secrets and administrator credential isolation remain enforced. | These are security guarantees, not optional preferences. |
| Public credential handling | Keys and optional announce URLs can be remembered locally in the visitor's browser; the server does not save the visitor's credentials. | Preserve the same privacy behavior across Search, Stremio and Torznab. |

Values already available through a command-line option, environment variable or source definition remain configurable even when they have no separate global form field. See [deployment settings](SETTINGS.md) and [source configuration](CONFIGURATION.md) before adding a duplicate control.
