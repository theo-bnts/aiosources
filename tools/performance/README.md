# Local performance checks

This suite checks complete application paths against an **isolated copy** of a private PostgreSQL dump. No sample catalog, account, source definition or private report is included in this repository. It runs locally and refuses CI execution.

Requirements: PostgreSQL 18 command-line tools, Python 3.9+, the project's Go/Node versions, installed web dependencies and Playwright Chromium. Build the production application first with `make build`.

```sh
python3 tools/performance/run.py prepare --dump /path/to/private.dump
python3 tools/performance/run.py run --binary bin/aiosources --label baseline --save-baseline
```

For PostgreSQL tools outside `PATH`, add `--pg-bin /path/to/postgresql/bin` to `prepare`. The default report/configuration workspace is `.aiosources/performance`, which is ignored by Git. PostgreSQL files live in an owned system temporary directory outside synchronized folders. If the operating system removes that temporary database, prepare the snapshot again. The original dump is read only. Preparation disables automatic jobs and replaces stored secrets with synthetic values. It never accepts a production database URI.

After a passing baseline is saved, `make check` also builds the current code and runs the local performance gate. GitHub checks continue using synthetic data only. An explicit run is also available:

```sh
make performance-check
# or compare a candidate with a particular local report:
python3 tools/performance/run.py run --binary bin/aiosources --label candidate \
  --baseline .aiosources/performance/baseline.json
```

The suite covers source HTTP/HTML/Torznab parsing and complete publication, metadata retrieval and change processing, matching, book imports and reconciliation, information resets, private/public Search (including direct torrent results without metadata matches), Stremio, Torznab and sharing, provider validation and download preparation, optional storage choices and index/retention settings, administration API reads, and desktop/mobile pages and optional sections. Provider-backed scenarios use loopback fixtures; no real provider account is required. The snapshot's public interface is prepared directly as a test fixture, without generating a duplicate bulk sharing journal. Public policy writes and exports are exercised by the synthetic end-to-end scenarios. Private catalog searches must return a known matching work: an empty response is not a performance success.

Reports record warm repetitions, median and p95, Go allocations, startup time and sampled application/PostgreSQL memory and CPU. Application pages share their route inventory with browser regressions so additions stay visible. Every complete run must retain the same cases and result contracts. Five repetitions are the default; `--samples` accepts 3–20.

Each measured document navigation uses a fresh browser context with the same synthetic administrator session. Context setup is outside the page timer; the warm-up and measured repetitions still load the complete document. Optional controls share the final document’s context. Closing the previous context releases its requests and live-event connections before the next navigation, including repetitions of the same page. This profile measures a fresh document load rather than a browser asset-cache replay; its environment identity prevents comparison with older navigation profiles. A functional failure or measured deadline miss stops the browser run immediately and retains a private diagnostic; partial coverage cannot become a reference.

Each book-parser and account-validation measurement averages 100 iterations so regexp or HTTP buffer pools cannot dominate a single tiny call. Account fixtures make the pacing slot immediately available on each call to measure validation work rather than deliberate provider waiting. Complete imports, matching batches and other HTTP flows use one iteration per measured repetition. The iteration profile is part of baseline compatibility.

Browser navigation has a 350 ms noise allowance, and opening controls has a 150 ms allowance. Repeated runs of the same production build show React lazy-route commits and Playwright frame/actionability waits varying by about 300 ms and 130 ms respectively. API timings retain a 50 ms allowance and Go operations 20 ms. These allowances complement the 20% comparison; hard page deadlines and result checks still apply to every repetition.

A relative tail slowdown must appear in at least two repetitions; all raw samples and p95 remain in the report. A single absolute deadline miss always fails. Complete one-iteration flows allow 32 KiB allocation noise because Go HTTP/PostgreSQL buffer pools can introduce a 16 KiB difference between identical runs. Warm micro profiles retain the 4 KiB allowance; larger changes, allocation counts and process memory remain guarded.

Memory sampling sums process RSS, including PostgreSQL shared mappings more than once. It is a consistent upper-bound signal, not exclusive physical memory usage. A conservative 4 GiB absolute ceiling complements the relative regression check; allocations and application limits are measured separately.

Requests have hard deadlines; typical list/search pages have a five-second budget. Complete source/identification statistics allow ten seconds, including their browser pages. The unscoped identified-record filter allows fifteen seconds for its exact whole-catalog count; unfiltered Torznab media feeds allow twenty seconds for their full metadata/visibility count. Relative comparisons still catch smaller regressions in these slower aggregates: a slowdown must exceed both 20% and a noise allowance before comparison fails. Absolute budgets still apply without a baseline. A different dump, machine profile or coverage set cannot be compared as if it were the same experiment. The suite does not flush operating-system caches or include real Internet latency.

All diagnostics, credentials, queries and reports stay in the ignored workspace with private permissions. Never upload that directory as an artifact. Terminal summaries contain case IDs and numeric timings only. To establish a new baseline after an intentional, reviewed behavior/coverage change, use `--save-baseline` on a complete passing run; failed runs cannot replace it.
