# Performance verification

## 15 October 2026

Changes were compared with commit `4a997392446373e898f1ab6a6db77ddfa3b1ed73`, using identical synthetic fixtures and a disposable PostgreSQL database. No external source, TMDB or debrid account was contacted.

## Retained optimizations

Median elapsed times from five repetitions on an Apple M1 Pro, PostgreSQL 18.6, Go 1.26.9, with `GOMAXPROCS=4`:

| Workload | Before | After | Less elapsed time |
| --- | ---: | ---: | ---: |
| Save a 100-record Full page, compact storage | 17.78 ms | 9.58 ms | 46.1% |
| Save a 100-record Incremental page, compact storage | 57.56 ms | 31.45 ms | 45.4% |
| Identify 200 repeated-title releases against 10,001 works | 1,672.21 ms | 222.38 ms | 86.7% |

Compact imports validate successful records together in a PostgreSQL savepoint. If any representation is invalid, individual validation retains the same diagnostics and checkpoint/publication behavior. Full storage and Preview keep their existing observation handling.

Matching reuses candidate searches only inside a bounded transaction and its shared metadata-generation lock. Every filter field participates in the key. There is no cache across transactions, metadata changes or users. Candidate discovery, overflow, scoring and publication rules are unchanged.

A different fixture identifies 90 distinct movie, episode and season-pack releases without reusable queries. The result remains 90 matches: 53.94 ms before and 54.35 ms after (0.8% difference). The repeated-title gain should not be generalized to every matching workload.

These measurements cover local processing, not end-to-end provider latency. Source pacing, quotas, retries and the configured TMDB request rate are unchanged.

## Other measured paths

The read-path fixture contains 120,000 torrents, three sources, 1,000 works and 10,000 shared cache observations. It exercises source/metadata statistics, ordinary and filtered identification lists, title-fragment search, Stremio lookup, shared cache lookup and storage measurement.

The administration fixture contains 30,000 torrents and 20,000 events. It exercises logs, per-run history, catalog export, idle history cleanup and already-applied startup migrations. A catalog export experiment improved its fixture by 15.9%; it was discarded because it did not meet the requested 20% threshold. These other production paths were left unchanged.

The new storage breakdown remains a background measurement, once per minute. It reads allocated relation sizes and PostgreSQL identifier distributions. Retained-import detection uses indexed existence checks, not counts or payload scans over the preparation table. The expanded UI does not issue a new database scan.

## Reproduction

Use a disposable local PostgreSQL maintenance database, never a live deployment. `TEST_DATABASE_URL` makes each benchmark create and remove its own database. Build and test the same fixture files in both checkouts.

```sh
go test ./internal/store ./internal/connectors -run '^$' \
  -bench 'Benchmark(CollectionPersistence|CatalogReadPaths|AdministrationPaths|JSONCollectionPage)$' \
  -benchtime=20x -count=5 -benchmem

go test ./internal/aiosource -run '^$' \
  -bench 'BenchmarkMatching(Dense|Varied)Catalog$' \
  -benchtime=3x -count=5 -benchmem
```

Use the existing `BenchmarkMetadataRetrieval` and `BenchmarkMetadataSeasonLookup` fixtures for metadata retrieval and season discovery. They use a local synthetic HTTP service and isolated data. Keep performance measurements separate from race/browser suites to avoid competing workloads.

## 21 October 2026

Compared with `5bd3c28562deb42c4ecd4af75cad936b5d6ac606`, on the same Apple M1 Pro with Go 1.26.9 and PostgreSQL 18.6. Median times from five repetitions, using the synthetic fixtures above:

| Workload | Before | After | Less elapsed time |
| --- | ---: | ---: | ---: |
| Identify 200 repeated-title releases against 10,001 works | 245.10 ms | 133.87 ms | 45.4% |
| Replay 50,000 entries from the same dated TMDB inventory | 355.40 ms | 194.34 ms | 45.3% |

The matching dispatcher now groups up to 25 records per transaction, while keeping smaller groups for short queues to preserve available parallelism. Candidate reuse remains inside that transaction's metadata fence. The 90 distinct-title movie, episode and season-pack fixture remains 90 matches, with 57.68 ms before and 53.83 ms after; this smaller change is not a separate retained optimization. No matching evidence, thresholds, ambiguity checks or season requests were removed.

An unfinished inventory may be replayed after a restart. Entries already marked with the same export date no longer receive identical row updates. New dates, missing entries and pending metadata jobs retain their original behavior. This measures local replay, not download time or a first import.

Reproduce the inventory case with `go test ./internal/aiosource -run '^$' -bench '^BenchmarkMetadataInventoryReplay$' -benchtime=1x -count=5`. Use the matching command above with `-benchtime=1x` for the 21 October comparison. Actual gains depend on catalog size, title reuse, database latency and available workers.
