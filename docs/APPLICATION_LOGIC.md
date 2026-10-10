# Application logic and shared contracts

[Back to README](../README.md) · [Development](DEVELOPMENT.md) · [Restart safety](RESTART_SAFETY.md)

The application uses shared rules at its boundaries while keeping the guarantees required by each operation. This review covers source definitions, collection and scheduling, metadata and identification, debrid and Stremio, search and history, sharing and remote catalogs, activity logs, security, storage and browser navigation. It does not replace the operational limits documented in [Restart safety](RESTART_SAFETY.md).

## Editing and concurrency

- A form retains the version it opened. Background refreshes update server data without replacing its local draft or silently adopting a newer edit revision.
- Versioned writes reject stale updates. Users explicitly load or compare the current version before saving again. Forms that offer a merge retain changed fields and adopt only untouched server fields. Source JSON editors preserve exact numeric lexemes and use their existing comparison workflow.
- A confirmation keeps the action and revision selected when it opened, even if the list refreshes underneath it. This includes deletion and credential replacement.
- The metadata, debrid and private-playback forms save independently. Another pending form can adopt the revision produced by that same writer's successful save. If a further writer changes settings before the follow-up read, an explicit review is required instead.
- Leaving an unsaved form or closing an edited dialog uses the common draft guard. Newly generated sharing passwords and recovery codes are also guarded until explicitly acknowledged. These values remain in memory, not browser storage.

Secret metadata has database revisions protecting rotations, deletion and recreation. Secret revisions come from a monotonically increasing database sequence: deleting and recreating a name cannot make an old editor valid again. Sequence gaps are normal; clients treat revisions as opaque values.

## Requests and refresh

Authenticated JSON commands use one bounded decoder: one UTF-8 JSON object, a maximum of 1 MiB, no trailing document, no duplicate field names at any depth, and no unknown DTO fields. Individual commands retain their required-field and domain validation. Source documents and open JSON values keep their stricter source schema and exact-number behavior; this decoder does not redefine their contents.

Query parsing rejects malformed encoding and ambiguous repeated parameters. Multi-source torrent search explicitly permits repeated `provider` values. Lists share bounded pagination parsing with offsets from 0 to 1,000,000, while retaining endpoint-specific default and maximum page sizes. A competing active operation returns a generic conflict rather than a collection-specific error on unrelated pages.

Browser query reads retry a transient network error, HTTP 408/429, or a server error once. Validation, authentication, permission, conflict and not-found errors do not retry automatically; cancelled reads never retry. Writes do not replay automatically after an uncertain acknowledgement. JSON responses, raw-page reads and downloads share interrupted-response handling. Proxy HTML or malformed error payloads produce a readable error rather than breaking error rendering.

Data refresh covers source documents, schedules, remote documents, raw record/page details, identification diagnostics, secrets, and the other live query families. Storage measurements are read-only and do not scan observation history. The simplified console has no diagnostic settings API.

## Service lifecycle

Collection and storage services accept one successful start, reject duplicate or post-close starts with a typed conflict, and support idempotent close, including close before start. A cancelled initial start leaves an unused service available for a later valid start. Close waits for the worker's completion through its existing context deadline.

AIOSources owns its metadata/cache maintenance from construction and retains that existing lifecycle. Its database lock now distinguishes an actual competing instance from a storage failure. Startup registers cleanup before constructing that service, so an early failure also closes the already-created storage monitor. Collection snapshots, leases, checkpoints and shutdown recovery retain their domain-specific persistence rules.

## Domain review

| Area | Common guarantee and intentional behavior | Regression coverage |
| --- | --- | --- |
| Sources and remote definitions | Strict JSON, immutable admitted-run snapshots, guarded edits and atomic versioned file writes; validation and editing do not contact trackers. | Provider registry, source editor and remote editor tests. |
| Imports and schedules | UI, API and CLI share durable admission, pause/resume/cancel, quotas and saved source configuration. Incremental starts with Full when no successful baseline exists; manual holds remain explicit. | Jobs lifecycle, initial collection, policy, schedule and restart tests. |
| Publication and history | Page data and cursor commit together. Full publishes its staged catalog only on successful completion; Incremental preserves accepted page progress. Raw archives, compact observations and semantic history remain distinct. | Publication, run records, raw API, retention and history tests. |
| Metadata and identification | Automatic information updates use persisted completion and retry clocks; local IMDb/TMDB/TVDB matching starts only for queued work with available information. Saved manual preferences survive upgrades, and restored archives disable both tasks. Invalid or temporary upstream responses do not erase published data or confirm a completed update. | Metadata, matching, diagnostics and resilience tests. |
| Debrid and Stremio | AD/TB availability separates present, absent and unknown, scoped to the provider account. Playback uses encrypted references and deferred private announce URLs. Installation tokens remain distinct from administrator authentication. | Cache/provider, tracker, stream and Stremio API/browser tests. |
| Torrent search and saved views | Search filters remain in the URL; saved view names and filters persist with revision checks. A stale saved-view edit keeps the name until explicit review. | Search/history store and API tests; concurrent saved-view browser test. |
| Catalog sharing and remote mirrors | Permissions and credentials stay scoped to each share, and revisions protect permission changes. Remote checkpoints are endpoint-bound and commit with published mirror state. | Sharing controls, lease/privacy, remote connector and checkpoint tests. |
| Activity | Logs contain operational results and alerts, with service context and optional detailed activity. Historical notifications and external delivery outcomes are consolidated without copying private payloads. | Activity, upgrade, restore and browser tests. |
| Secrets and administrator security | Values remain encrypted; public metadata contains only name, revision and update time. Stale rotations/deletions fail, dependency checks remain atomic, and MFA/password/session operations retain their transaction and proof requirements. | Vault, secret admission/deletion, security and browser concurrency tests. |
| Storage | Read-only PostgreSQL and local storage measurements retain stale values on failure without fabricating zeroes. Collection leases and history counts are not needed for readiness. | Storage, failure-snapshot, service lifecycle and browser tests. |
| Navigation and display | Page views, including histories and Security, use horizontal tabs. Breadcrumbs own the page title and short description; panels retain guidance and distinct detail names. Tab changes preserve scroll/focus; menu navigation resets them. One-entry menu groups are direct links. Dates use the browser timezone; lists use 50 rows. Shared guards protect pending writes and unsaved forms. | Navigation, tab scroll/focus, fixed pagination, timestamps and application browser tests. |

## Upgrade and API clients

Migrations 38–40 consolidate notifications and security/share outcomes in Logs, index observation search, and retire external deliveries, security audit and diagnostic storage. Secrets, authentication, collection settings and catalog data remain unchanged. Historical migration SQL remains immutable for upgrades from older schemas. No environment, Compose or GitHub workflow changes are required.

Migration 41 removes seeders from interpreted torrent data and snapshots, preserving exact numbers, source progress, trigger modes and the freshness of current/stale identification fingerprints. Cleanup emits no new publication or history records. Matching queues remain intact, original raw archive bytes are unchanged, and seeders-only shares are disabled without broadening permissions. Legacy enrichment definitions ignore retired counters when selecting and validating metadata candidates; their saved JSON and run snapshots remain unchanged.

Migrations 46–48 retire the backup product and peers, preserve compact recovery evidence independently of seven-day diagnostic history, and store the existing matching fingerprint at field writes. History cleanup is bounded and transactional. Sharing retains recent changes and a compact reference state; expired checkpoints require a fresh Full. Published records, metadata, matches and administrator credentials remain intact.

Mutable console data refreshes automatically while visible, with more frequent polling for active operations. Live events still invalidate related queries promptly. Successful immutable response archives stay cached instead of downloading their bytes again on each tick; failed reads can recover automatically. Drafts retain their base revision and changed fields; periodic reads cannot replay writes or silently rebase concurrent edits. The search form uses the existing combined filter contract with a multiple-source dropdown and complete/partial hashes in its text field.

Manual API clients must use the following versioned commands:

- `GET /api/secrets` exposes revision metadata, never values. `PUT /api/secrets/{name}` takes `{ "value": "...", "revision": 0 }` to create a new reference, or the selected positive revision to replace it. An explicit create retry with the exact same reference and current value succeeds without changing ciphertext, revision or timestamp; it cannot replace a different value. `DELETE /api/secrets/{name}` takes `{ "revision": selectedRevision }` and also checks dependencies. A creation collision with a different value or a stale positive revision returns HTTP 409. Existing success responses remain HTTP 204.
- `ingest secret set` remains an explicit operator rotation, including replacement of an existing reference. It advances the revision and invalidates stale browser forms; it does not reuse the create-only browser behavior.

## Verification

The shared contract tests cover malformed requests, pagination, concurrent source merges, concurrent secret creation/rotation/deletion/recreation, migration preservation, and service startup/shutdown. Browser regressions cover local drafts under refresh, explicit conflict resolution, frozen deletion confirmations, independent debrid forms and one-time sharing credentials.

`make check` includes frontend unit checks. The existing browser command runs those unit checks before Chromium through `pree2e`, so the retained GitHub test workflow also executes them without workflow edits. The complete Go suite and race detection in both modules, static analysis, frontend formatting/type checking/build, and the production-browser suite remain the release gates for this change.

Source configuration supports an explicit three-way field merge after reviewing the latest server version. Untouched settings come from the server; independent local changes retain exact JSON numeric tokens and custom Cron expressions. Conflicting values, object removal and contested array edits require manual review instead of silently replacing concurrent changes. The existing explicit command to keep a reviewed local definition on the new revision remains available for manual conflict resolution.

## Statistics and fixed import behavior

Sources, Metadata, Debrid and Stremio begin with Statistics, followed by Settings and History. Metadata also has Identification for reviewing individual decisions. Aggregate counts and per-source counts are separate from configuration. Source processing details are available only within a specific run. Reviewed choices are independent of imported fields and fenced by source evidence, metadata revisions and an optimistic review revision. Stremio history uses a fixed display-field projection with bounded retention. Root tab panels omit introductory descriptions and repeated headings; detail pages retain their specific context. Navigation icons reflect the view and shared semantic badges distinguish success, warning, activity and failure. Tab selection commits synchronously before automatic focus activation can duplicate its history entry; Back and the unsaved-change guard keep their normal behavior.

Migration 42 removes mutable recovery and global launch-default columns and invalidates stale collection-settings drafts. Existing run policies, timeouts, page budgets and checkpoints are not rewritten. Technical recovery is unconditional for eligible orphaned work while leases, explicit holds, cancellations and configuration failures keep their safety rules. Restore uses manual holds and quarantined source files, independent of a global toggle. New Preview runs default to three pages; other manual imports inherit the source page limit unless overridden. Omitted source timeouts use 30 seconds and omitted duration budgets are unlimited.

## Identification evidence and episode playback

Candidate retrieval and final decisions use the same scope: structured source season/episode fields fill missing title numbering without replacing conflicting parsed evidence. A season pack can corroborate its year with verified episode dates in a declared season. Missing dates request the season once through the durable request table; later imports cannot reset an existing metadata retry deadline. Identical information updates do not requeue matching.

A complete alias and consistent verified native identifier can resolve a yearless homonym. Explicit conflicting identifiers, media types, years, numbering, numbered titles and candidate overflow still prevent automatic acceptance. An unverified competing identifier remains insufficient evidence. Episode markers inside ordinary words do not turn film titles into episode numbering; explicit Ep/Episode markers remain recognized alongside season numbering. Reversed or oversized ranges are rejected rather than becoming unscoped packs.

Explicit multi-season packs retain all declared seasons when the source season field labels their first season. Independent source/title contradictions still exclude the stream. Playback requires exactly one matching video; duplicate episode files remain ambiguous. Leading numeric filenames require an immediate, unambiguous season directory. Malformed directory ranges, extras and unscoped numeric filenames cannot supply an episode. Windows path separators are normalized only for analysis, preserving provider paths.

Migrations 44–45 queue prior misses and affected numbering decisions once, preserving active reviews, existing queue versions and working unrelated decisions. Migration 45 extends the recheck to explicit word-based numbering without rewriting migration 44. Current status counts use one grouped aggregate instead of four reads of a materialized decision catalog. Exact freshness and snapshot rules remain unchanged; joins and broad catalog reads can still require temporary storage.

Authentication forms provide named password and one-time-code fields with the appropriate autocomplete hints. Submission reads the native form values, including password-manager fills that do not emit React change events. Authenticator enrollment uses a six-digit text field with a numeric keyboard, preserving leading zeroes; login and recovery proof fields continue to accept recovery codes.

Creating a source commits the saved editor's replacement synchronously with its URL change. The outgoing creation form cannot accept an edit that would disappear on remount. Ordinary updates retain their draft, revision and pending-write rules.
