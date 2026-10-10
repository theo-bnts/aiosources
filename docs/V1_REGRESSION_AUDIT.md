# Compatibility regression coverage

Synthetic fixtures preserve earlier metadata, matching and playback behavior while testing the current collection and publication guarantees. This compares observable behavior and work scheduling; it does not bundle a legacy server or source-specific integrations.

## Confirmed regressions and corrections

| Area | Regression | Correction and coverage |
| --- | --- | --- |
| Information retrieval | Sequential requests, followed by an extra 50 ms delay per work | A bounded pipeline overlaps 16 requests. One limiter covers details, seasons, changes and retries; the default is v1's 35 requests/s, configurable from 5 to 40. Cancellation and HTTP 429 cooldown apply to every worker. |
| Metadata publication | Individual statements for each alias/episode and a separate acknowledgement | Small bulk transactions publish validated works, affected correspondence jobs and versioned acknowledgements together. Failure rolls back the complete batch. Confirmed deletions use the same atomic acknowledgement rule. |
| Routine refresh | Identical responses advanced revisions and temporarily withdrew working streams | Compare decision-relevant metadata, independently of title/year order. Identical responses refresh the audit timestamp without rewriting aliases, increasing revisions or queuing matching. Raw title spelling still matters for short-title safeguards. |
| Local matching | One torrent at a time, with separate discovery and publication transactions | Small parallel chunks retain one shared metadata fence and one connection through discovery, decision and publication. Worker capacity respects small database pools. Source rows are re-read and locked, and queue versions are acknowledged only after commit. |
| Candidate discovery | A 64-candidate cap applied before type/year eligibility; the trigram index used its less selective default threshold | Restore v1's 100-candidate safety cap and transaction-local 0.5 index threshold. Apply type, release-year, season-year and episode-year eligibility before the cap. Real overflow remains ambiguous. |
| Release names | Leading bracket groups polluted titles; DTS/AAC and word-based season ranges were incomplete | Reference fixtures generated with v1's actual parser and chooser cover 144 releases, plus conflicting-ID controls. All 141 v1 matches remain supported. Three v1 misses now resolve using the intact title and corroborating IMDb ID. Existing conservative decision fixtures also remain in force. |
| Series information | Repeated season calls; nonexistent source season numbers could defer the whole series forever | Fetch only seasons listed by the validated series response, append multiple seasons in groups of at most 20, and use season air dates. Missing or malformed season/episode data retains the old publication and a durable retry. Missing episode years can request follow-up information even when the initial year filter excludes that candidate. |
| Metadata freshness | The bounded age-based refresh from v1 was absent | Queue up to 100 works older than 30 days per update, without resetting existing retry jobs. A due information job can wake automation before the regular interval, while respecting outage backoff and disabled automation. |
| Metadata checkpoint reads | Each update read full catalog/identification counts to get two checkpoints | Reuse the indexed automation/state read; counts remain available for the console. |
| Adult metadata | v1 excluded adult works from correspondence; the flag was absent | Retain TMDB's flag and exclude marked works from candidate discovery and stream publication. Existing works acquire the flag when refreshed. |
| AllDebrid checks | Slow requests were handled in series | Four workers overlap responses under the existing shared provider pacing. Results publish as they arrive. Each hash keeps a fresh inventory/ownership check; v1's shared inventory snapshot is deliberately not restored over the newer durable cleanup logic. |
| Stremio cache identity | IMDb/TMDB/TVDB aliases created separate resource checks | Agreeing identifiers from the verified published work share a resource key, including when native source fields contain no IDs. Different seasons, episodes, absolute anime numbering and conflicting IDs stay separate. Old observations remain usable. |
| Stremio file evidence | A ready torrent could advertise an unverified episode as cached; selected-file hints lost pack context | A cached episode marker needs a selected matching video. File-list failures keep playback availability unknown, while known wrong-episode files remain excluded. Restore the pack's name and size alongside selected-file hints. |

## Upgrade and retained guarantees

Migration 34 adds indexes and the adult flag. It queues prior unmatched/ambiguous decisions once so corrected discovery and parsing take effect on existing data. It preserves working correspondences, existing queue versions, source data, secrets and saved settings. Old configuration documents and clients receive the default request limit without having to add a field manually.

Native source collection, enrichment, provenance, conservative supplied-ID verification, exact episode file selection, AD/TB account isolation, revocable Stremio access, private announce secrets and interruption recovery remain covered by the application suite. Upgrades from supported older schemas preserve publication and recovery state. The later history cleanup change retires the application backup product. The current Go/React architecture and its typed metadata catalog are retained; unused TMDB media and full JSON responses are not reintroduced.

No source definitions or defaults are added. GitHub workflows are unchanged. The frontend lockfile updates only `source-map-js` from 1.2.1 to 1.2.2 to satisfy the current dependency audit; see the [upstream security advisory](https://github.com/advisories/GHSA-68fv-2mgg-jv7q).

## Reproducible measurements

`internal/aiosource/performance_test.go` uses an isolated PostgreSQL database and loopback HTTP responses. Run with `GOMAXPROCS=1`, `-benchtime=1x` and the three named benchmarks. No public API, credentials or production database is needed. These measure the previous Go implementation against the corrections, not the old TypeScript server's elapsed time.

The final controlled comparison measured:

| Scenario | Before | Corrected |
| --- | ---: | ---: |
| 100 metadata requests, 100 ms simulated response latency | 15.92 s; 6.28 requests/s | 3.10 s; 32.26 requests/s |
| 200 matching jobs among 10,000 similar wrong-year titles | 7.60 s; 0/200 correct matches | 2.00 s; 200/200 correct matches |
| Requested-season lookup among 100,000 source records | 42.70 ms | 3.95 ms |

Actual throughput depends on upstream response times, PostgreSQL, storage and configured request rate. Shared cooldowns and retained retries take priority over throughput during an outage.

TMDB describes its current soft limits and HTTP 429 handling in its [rate-limit documentation](https://developer.themoviedb.org/docs/rate-limiting), and resource aggregation in [append-to-response](https://developer.themoviedb.org/docs/append-to-response).

## Extended audit beyond TMDB

The broader pass compares v1's stream mapping, file selection, native lookup, cache scheduling and provider playback with the existing v2 collection, storage and console contracts.

| Area | Confirmed difference | Result |
| --- | --- | --- |
| AllDebrid playback | The file-list endpoint can return a quoted magnet ID. v1 compared string identities; v2 decoded only an integer, rejecting the list after global cache readiness had succeeded. | Accept exact numeric or quoted numeric IDs. Regression tests first reproduce a visible `[AD+]` stream whose playback fails, then verify link resolution and the HTTP redirect. Wrong IDs and malformed values remain rejected. |
| AllDebrid magnet upload | v2 put the display name before the exact topic and encoded the `urn:btih:` colons. A production comparison with the same account and hash returned `MAGNET_INVALID_URI` for that input; the v1 URI and bare hash were accepted. | Restore the literal `xt=urn:btih:` prefix first, with escaped display names and announce URLs. The Stremio HTTP regression now covers an `[AD+]` stream whose account entry is absent when playback starts, requiring upload before file selection and the 307 redirect. Existing-account playback and TorBox remain covered. |
| Playback diagnostics | Repeated playback errors collapsed into a generic provider incident. | Each failed AD/TB playback records its stage and a fixed failure category in service-filtered Logs. Inventory, file listing, selection and link generation remain distinguishable without storing keys, source identities, upstream error bodies or playback URLs. Normal cancellation and pending downloads retain their existing handling. |
| Warm cache response | A valid resource waited for a slow background retry before returning already available observations. | Valid resources return their observations while their owned retry continues. Cold checks still share and wait for their first result; expiration and changed candidates retain the response budget. Repeated boundary tests cover a warm retry overlapping expiration and reset cancellation of its queued full check. |
| Language presentation | ISO 639-2 codes and many language names supported by v1 were missing. Substring tests invented French/English flags for `MULTI` and some ordinary titles. | Normalize source codes and release tokens at the presentation boundary. Keep multiple tracks as a label without inventing their languages. 140 reference cases come from v1's actual mapping/parser functions. Native fields stay unchanged. |
| Episode listing | Missing numbering in a verified release title was treated as contradictory evidence, preventing provider file lists from proving the requested episode. | Restore candidate eligibility when numbering is absent; explicit wrong season/episode evidence still vetoes it. Playback and cached episode markers still require the exact, unambiguous video. Eighteen v1 cases and seven file-selection cases retain the newer parser's correct rejection/selection where v1 missed a number. |
| Consecutive playback | Identifier aliases changed the binge group; equivalent language spellings/order could change it too. | Use the published work identity, source, quality and canonical language set. Keep this stable across episodes and IMDb/TMDB/TVDB aliases. Different sources, qualities, languages and absolute MAL scopes remain separate. |
| Native MAL lookup | SQL accepted fewer native representations than the decoder and could return conflicting assertions. Its transformed predicate bypassed the original scalar index. | Migration 35 adds an indexed candidate extractor for the same source fields and arrays. The existing decoder remains the final conflict veto. No source fields, queue versions or metadata are rewritten. |
| Result order and sizes | Streams sorted by provider/source IDs and displayed every size in GB. | Restore newest publication first, use first-seen time when a source provides no date, and retain deterministic identity ties. Restore readable size units and an explicit unknown-size label; exact byte hints remain unchanged. |
| Setup and public examples | The initial setup treated optional debrid credentials as required. Historical connector fixtures still named real sources and endpoints. | Complete setup after the essential steps without requiring a provider account. Keep the current menu organization, link source timing to Schedules, use reserved fictitious hosts in fixtures, and document public contribution boundaries. |

The [AllDebrid file-list documentation](https://docs.alldebrid.com/#get-files-and-links) illustrates quoted IDs even though its attribute table labels the ID as an integer. Both representations are now covered. A public demo response was also checked without a real account; that response used numeric IDs. Controlled tests establish this compatibility regression, not the exact response of every production account. Operator validation remains necessary for actual account restrictions and downstream media delivery.

The upload comparison on 9 October 2026 confirmed the rejection through the real API rather than its static demo responses. The rejected URI still returned HTTP 200 and an outer `success` status, but the magnet member carried `MAGNET_INVALID_URI` instead of an ID, readiness or matching hash. This explains why a hash-only cache check could succeed while playback failed during torrent preparation. The [documented upload form](https://docs.alldebrid.com/#upload-magnet) and v1 both put the literal exact topic first. New tests also cover escaped titles, hash casing and exact private announce preservation; the earlier file-ID compatibility fix remains necessary and is retained.

[Stremio's stream contract](https://github.com/Stremio/stremio-addon-sdk/blob/master/docs/api/responses/stream.md) describes how a matching binge group selects the next episode. The new tests check group stability, separate source/quality/language choices, exact episode redirects for both providers, repeated failures and diagnostic privacy.

`BenchmarkNativeMALLookup` compares the previous SQL predicate and corrected lookup against the same isolated 100,001-record catalog. Ten iterations measured about **13.75 ms → 3.63 ms** per lookup on the audit host. It measures local lookup only, not upstream latency or the old TypeScript server. The warm-cache test uses a blocked provider response to prove that existing observations return before the retry completes; it does not depend on a public provider being fast.

The broader suites exercise Full/Incremental/source-metadata imports, repeated failures and process restarts, scheduler/manual holds, conservative publication, account isolation, AD ownership and cleanup, raw/semantic history, remote mirrors and sharing, secret revisions, security and the embedded console. No weakening of these stronger v2 guarantees was found necessary to restore v1 behavior.

The public release contains reviewed source and synthetic regression fixtures. Private configuration and historical source archives are excluded. [Contributing](../CONTRIBUTING.md) and [Development](DEVELOPMENT.md#release-verification-and-publication) describe public contributions and release verification. The [legal notes](../LEGAL.md) distinguish software licensing from content rights and service terms.
