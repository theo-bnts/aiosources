# AIOSources architecture

Upstream: moodiness/ingest at b8332d4d1fc0236071cc6ddaa6f20cae5241f017 (MIT).

The Go ingestion engine remains the sole owner of source records and provenance. No providers ship with this fork. The React panel extends the existing authenticated console. Stremio has its own revocable installation token; administrator cookies never authorize public stream endpoints.

AIOSources extensions live in `internal/aiosource`: release parsing and conservative correspondence, typed local TMDB metadata, durable import/retry queues, account-scoped availability, deferred playback and lifecycle ownership. `internal/store/aiosource*` owns SQL, transactions and migrations in the existing `ingest` schema alongside publication and recovery state. Secrets resolve through Ingest’s encrypted vault.

Request paths read only published local metadata; they never query a tracker or TMDB. Cache checks use official provider APIs with bounded concurrency, cancellation and persisted state. An unknown response never becomes an uncached observation. Only verified mappings publish IMDb/TMDB/TVDB IDs. Native MAL IDs retain explicitly unverified provenance.

Correspondence conformance fixtures come from the previous AIOSources test suite. They preserve conflicts, ambiguous homonyms, title numbers, Unicode, season/episode dates, incomplete candidate sets and supplied-ID verification.

This is a clean deployment into a fresh database and state volume. Legacy tables and files are not migrated or deleted automatically.

## Feature ownership

| Previous capability | v2 implementation |
| --- | --- |
| Built-in indexer cards and Python sync | Generic Ingest source editor, native Go collectors, immutable job snapshots and per-source schedules; no bundled trackers |
| Manual sync / stop / rebuild and automatic collection | Ingest full/incremental/metadata/preview jobs, pause/resume/cancel and collection policies; Full atomically replaces published source membership while retaining history |
| TMDB local catalog and IMDb correspondence | `aiosource/metadata.go`, `matching.go`, `release.go`; typed works/titles/seasons/episodes, durable versioned jobs and change/inventory checkpoints |
| Matching safety | Every supplied external identifier is checked, including Torznab arrays; source observations veto conflicts; stale torrent fingerprints and metadata revisions cannot publish |
| Metadata reset / match / independent stops | Authenticated extension API and Metadata page; cross-process worker locks protect resets |
| AllDebrid / TorBox settings and availability | Encrypted vault references, transactional configuration revisions and account-scoped resource/observation tables |
| Response budget and retry state | Owned single-flight resource tasks, bounded concurrency, independent positive validity, backoff and one final pre-expiry attempt; restart resumes current-account work |
| Temporary AD deletion | Confirmed ownership persists before publication, account/hash locks separate cleanup from playback, failed jobs remain for explicit retry and inventory reconciliation |
| Deferred Stremio playback | Revocable installation token, CORS, recognized AIOStreams markers, metadata-rich streams and exact episode file selection |
| Notifications / health / restart safety | Native console plus transactional provider incident history, a database-aware healthcheck and owned cancellable workers |
| Private source uncached playback | Explicit private/public configuration and encrypted full announce URL references; detection sends bare hashes only |

## Storage and transport guarantees

The extension never modifies native torrent fields or combines provenance records. Work revisions and current torrent fingerprints guard publication. New source assertions can invalidate a correspondence immediately, before matching runs again. Configuration writes lock referenced encrypted secrets and compare the previous revision; stale editors cannot overwrite current settings.

A cache reset increments a durable generation and cancels this instance's cache workers. Results from checks started before that reset cannot repopulate the cache, including another process's late result. Restart recovery reads account fingerprints, never reuses another account's results, and only resumes unexpired resource retries. The public stream route has its own concurrency admission limit. Temporary AD cleanup uses at most four immediate workers; excess confirmed ownership remains durable for recovery.

Native torrent inserts and field updates queue their records in the same transaction; unchanged fields and rolled-back writes do not create work. Matching consumes this indexed queue rather than repeatedly scanning the complete source catalog. Candidate lookup combines separate indexed ID/title branches. Metadata changes advance a candidate generation and queue only affected normalized titles, similar aliases and existing work references. The queue immediately blocks old publication, even when a new homonym leaves the previous work's revision unchanged. Matching commits only against the observed generation and queue version. Daily inventories are stored in bulk, reconciled only after both exports validate, and retain works refreshed after the snapshot date began. Missing works and removed aliases queue their affected decisions again.

Remote errors do not manufacture a negative availability or overwrite its observation time. Malformed TorBox members leave the affected hash unknown and prevent missing hashes from becoming negative in an incomplete response. AllDebrid tree/file IDs are validated. Video selection rejects ambiguous episodes and samples. Only valid HTTPS provider links become playback redirects.

The local release parser is now Go code, covered by the previous suite's parser and correspondence fixtures; it has no Node subprocess or runtime download. Metadata catalogs keep only the typed data needed to verify correspondence, rather than unused TMDB JSON or media files.

## Deployment changes

Start v2 with fresh database/state volumes. Legacy data is neither selected nor erased implicitly. Native source provenance and resumable runs use Ingest's schema; the previous addon settings and Python scraper schemas are replaced. Source definitions are supplied by the administrator rather than hardcoded provider defaults. The new panel retains Ingest's English interface.

Private Stremio installations use the shared access-grant lifecycle from Torznab and catalog sharing, separated by protocol. Each named access stores source permissions, playback filters, quotas and expiration, with only a hash of its independently revocable key. Saved changes apply to the same manifest; a key replacement requires reinstalling. Legacy installation tokens and their URL filters remain compatible, and legacy rotation does not affect named access. Public Stremio and Torznab remain stateless and never create these grants. Named Stremio admission leases allow 70 seconds for existing playback preparation, then recheck revision and expiration before delivery. Other export protocols retain their 20-second delivery limit.

The container port remains 7000; native development retains Ingest's 8080 default. Internal package and binary names (`cmd/ingest`, `bin/ingest`) are retained where upstream compatibility benefits maintenance. The production container uses `/aiosource`.

## Performance and v1 parity

See [the v1 regression audit](V1_REGRESSION_AUDIT.md) for confirmed regressions, reference fixtures and reproducible measurements. Metadata uses bounded parallel retrieval and atomic bulk publication; local matching runs independently with a shared metadata fence. Routine unchanged metadata keeps working streams published. The default TMDB request limit is 35/s and remains subject to provider cooldowns.
