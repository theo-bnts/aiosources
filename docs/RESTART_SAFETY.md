# Restarts and temporary failures

[Operations](OPERATIONS.md)

A container restart preserves committed data when both the PostgreSQL volume
and the application's `/data` volume are retained. `/data` contains source
definitions, the vault key and the administrator password. Recreating these
volumes is a different operation from restarting a container; losing the vault
key can make existing encrypted secrets unreadable.

## Slow startup

Container logs report each startup stage and its completed duration in
milliseconds, before the HTTP interface is available. An unfinished stage
identifies whether startup is waiting for PostgreSQL, a schema migration,
local source definitions, application services, sign-in protection or collection
recovery. These messages contain fixed stage names, not configuration or secrets.
Schema upgrades can take longer on a large existing catalog and remain cancellable;
they must complete before the new service accepts requests. Recovery skips work
still owned by another process without waiting for its publication transaction.

## Interrupted operations

| Operation | What survives | What happens next |
| --- | --- | --- |
| Full source import | Published catalogue, private staging and committed page checkpoints | Eligible interrupted runs resume automatically. The complete catalogue is replaced only in the successful finish transaction. |
| Incremental source import | Each committed page and its checkpoint | Interrupted runs resume from that checkpoint. After a source failure, a new scheduled run can rediscover the unfinished tail; partially accepted failed runs cannot create an early known-page boundary. |
| Source metadata import | Committed enrichment of existing native identities and its checkpoint | Technical interruptions use the same recovery policy as other imports. A failed request cannot skip the current identity. |
| TMDB synchronization | Published works and versioned metadata retry jobs | The next synchronization continues durable work. A partially downloaded inventory is read again before reconciliation. Enable automatic synchronization or start another synchronization manually. |
| Local matching | Published correspondences and the versioned matching queue | The next matching pass consumes remaining work. A source or metadata revision that changed meanwhile cannot receive a stale result. |
| AD/TB availability check | Existing observations, resource retry state and confirmed temporary AD ownership | Current resources retry with backoff; expired resources are checked again on a later search. An interrupted or unsuccessful request does not become a negative observation. |

Pauses requested by the administrator, quota holds and useful-progress holds
remain deliberate stops. Automatic recovery does not bypass them or resume a
deleted, disabled or invalid source.

## Several tracker failures in a row

Retryable HTTP failures are recorded before another request is sent. They do
not advance the traversal cursor or certify the page as complete. Once the
request retry budget is exhausted, the run fails with its previous committed
checkpoint intact. Existing published torrents remain available.

Resume retries the failed request. A fresh scheduled Incremental also remains
safe: only identities accepted by an earlier completed collection can establish
its known-page stopping boundary. A prefix published by a failed Incremental
cannot hide the previously unreachable records from subsequent runs.

Authentication errors, certificate errors and invalid source definitions require
operator attention. Their records and checkpoints remain stored; repeating a
request does not repair invalid credentials or an invalid definition.

## Cases requiring attention

- **TMDB more than 14 days behind:** synchronization stops rather than skip an
  unavailable change-history interval. Existing works and source torrents remain
  stored. The Metadata page explains the required metadata reset and fresh
  synchronization. Resetting metadata intentionally removes local works and
  correspondence results; keep a deployment backup if preserving the previous
  metadata snapshot matters. Source torrent records are retained.
- **Failed temporary AD cleanup:** confirmed ownership remains recorded, but
  automatic deletion stops after a failed delete. Use **Debrid → Retry
  temporary cleanup** after inspecting the account. Existing account magnets are
  not treated as temporary owned downloads.
- **A tracker returns a valid but incorrect catalogue:** a successful Full treats
  missing records as removed, including a source that falsely advertises an
  empty catalogue with a successful response. Transport-error protection cannot
  distinguish that response from a genuinely empty source. Use source coverage
  constraints where supported and retain deployment backups. Seven-day
  publication history keeps before/after fields, while a later valid Full can
  rediscover records the tracker still exposes.
- **Records disappear upstream before collection resumes:** the application
  preserves what it already committed, but cannot recover never-collected
  torrents that the tracker no longer exposes. An incomplete or capped tracker
  listing cannot provide an exhaustive backfill guarantee.

## Regression coverage

The automated suite covers transaction rollback, interrupted publication,
source metadata checkpoints, orphaned leases, seven-day history cleanup and
persisted matching/cache queues. Additional end-to-end scenarios
kill a real worker process during Full and Incremental collection, reopen state
between three consecutive tracker failures, and verify metadata and AD/TB
recovery without erasing earlier published results. Separate crash tests also kill a real process during TMDB detail retrieval and local matching: committed details are not fetched again, completed matches are not rewritten, and the remaining durable jobs finish after restart. An unfinished inventory file or in-flight, uncommitted batch can be read again; that does not erase saved progress.
