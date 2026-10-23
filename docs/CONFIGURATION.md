# Source configuration

[Back to README](../README.md) · [Complete JSON reference](JSON_REFERENCE.md) · [Connector compatibility](CONNECTORS.md) · [Deployment settings](SETTINGS.md) · [Remote catalogs](SHARING.md)

Add a source once, check a sample, then let automatic imports keep its catalog up to date. This guide covers the visual editor and your own Torznab, HTTP/JSON or HTML definitions. For websites, see the [HTML setup guide](HTML_SOURCES.md). If you are unsure which connection to choose, start with [Source compatibility](CONNECTORS.md).

## Automatic information updates and identification

Open **Metadata → Settings → Schedules** to choose how often information is updated and waiting torrents are identified. Information updates default to every 15 minutes (5 minutes to 24 hours). Identification defaults to 30 seconds after the previous attempt finishes (5 seconds to 24 hours). Disable either switch to run that task manually.

Saved progress and the next eligible time survive a restart. New imports wait in the identification queue; metadata updates do not bypass the saved identification interval. A first identification can start as soon as information is available. **Manual actions** can run immediately. Upstream quotas and failure retries still apply; the intervals do not override them.

## Configure your sources

1. Open **Sources → Settings → Add source**, choose a source template or **Import JSON**, then configure the **Visual stages** or advanced **JSON** editor.
2. If the API requires a credential, add it directly in **Connection** or select an existing entry from **Secrets**. Only the encrypted reference enters the source definition.
3. Select **Check configuration**, save, then enable the source when ready.
4. Choose **Run manual import → Preview** to inspect a sample without publishing torrents.
5. Once the sample looks right, choose **Update** or **Rebuild source catalog** in the same dialog. Use **Schedules** in the source editor for automatic imports.

**Check configuration** verifies the settings, not the source's live response. **Preview** contacts the source and shows what AIOSources can read. Use it to check titles, IDs, sizes and hashes before importing.

### Where each setting belongs

The visual editor follows six sections:

1. **Identity:** source name, stable ID, adapter, version and enabled state.
2. **Connection:** endpoint, authentication references, HTTP method, headers, query parameters, request body and adapter-specific connection settings.
3. **Pagination:** page strategy, size, position parameters and response checks. HTML sources can follow next-page links.
4. **Source fields:** native record ID, canonical field mappings and output selectors; separate from film/series identification. HTML sources use CSS selectors and optional detail pages in the same editor.
5. **Imports:** pacing, timeout, rate-limit policy and options, with advanced bounded traversal and numeric-ID recovery controls.
6. **Schedules:** automatic timing, timezone and mode, with advanced page budgets and periodic rebuilds.

The display name labels the source throughout the console and in Stremio results. Rename it without changing the stable ID: routes, playback, filters and collection checkpoints keep that ID. Historical runs and Stremio results retain a saved display name for use after a source definition is removed.

### Several listings on one source

For an ordinary JSON or HTML source split into several listings, add their paths in **Connection → Additional listings** (`http.listing_paths`). The main **Source URL** is read first, followed by these paths on the same origin. They share field mappings, credentials, request pacing and the source ID. Each listing has its own pagination and known-page stopping boundary, so an up-to-date first listing cannot hide new records in a later one. Pause and Resume retain the current listing and its checkpoint.

Use this only for listings with the same response structure. Include every relevant entry point when you want full coverage. A finite recent feed can collect future entries while automatic Incremental imports keep previous ones; it cannot recover history no longer exposed upstream. Set an interval short enough for successive feeds to overlap, allowing for source outages and update delays.

### Advanced editing and configuration reference

Expand advanced sections only when your source needs them. **JSON** edits the same definition as the visual editor; it is useful for importing a configuration or adjusting settings not needed for a typical connection. For all accepted fields, defaults and validation rules, see the [complete JSON reference](JSON_REFERENCE.md). Existing YAML sources require [offline migration](#migrating-existing-sources).

The panel writes real `.json` files in its configured provider directory. Existing filenames and untouched JSON formatting/numeric lexemes survive normal edits; JSON does not support comments. Saves use revisions to reject conflicting edits. You can also manage the files yourself and run `ingest validate`.

An existing source ID cannot be renamed. **Add source** rejects an ID that already exists and cannot adopt that source's revision to overwrite it; choose another ID or open the existing editor.

Use structured map/array controls for query parameters, headers, scopes, partitions, variants and mappings. Expand advanced traversal/ID recovery sections only when supported by the source. **JSON** exposes the complete definition for advanced editing and repairing invalid definitions; both modes edit the same document. Invalid local field drafts block saving and stage/mode switching until corrected or reset. Malformed JSON must be repaired in JSON mode before returning to visual editing.

Authentication and pagination show the fields relevant to the selected strategy. Torznab keeps native XML handling while its JSON-specific controls remain available under **Advanced**. Hidden fields remain in the draft and exported JSON. If a new authentication type conflicts with retained fields, the editor explains this and offers an explicit, confirmed **Clear incompatible fields** action; changing a type never silently deletes configuration or vault entries.

Clearing an existing numeric/JSON array or map value leaves an invalid, editable row and blocks saving; it does not delete the row or shift its siblings. Use the explicit removal control to delete an entry.

**Import JSON** accepts a UTF-8 `.json` source definition up to 128 KiB. In an existing editor, confirm replacing its contents; this replaces the draft, not the saved source. Review, validate and save separately. **Export JSON** downloads the current editor document, including valid unsaved edits; it is not a run snapshot or proof of server validation. Invalid local JSON/field drafts block export. Imports and exports remain private configuration and must not contain literal credentials.

**Check configuration** checks the definition on the server; **Save** also validates before writing. Neither contacts the source nor starts a collection. Enabling a schedule separately authorizes later scheduled work. Existing queued, active, paused and failed runs retain their original immutable source snapshots; save changes and create a new run to use a changed definition.

The **Imports → Rolling request budgets** section can combine fast request spacing with persistent per-minute, per-hour and per-day ceilings. For a service limited to 30 requests/minute, 600/hour and 5000/day, use `request_interval: "2s"` and `request_limits: {"per_minute": 30, "per_hour": 600, "per_day": 5000}`. Requests proceed at that spacing until a rolling window fills, then wait for a slot rather than repeatedly hitting the server's rate limit. Counters are shared across this source's runs and survive Pause/Resume and process restarts; capabilities and retries count too. Other applications or source IDs using the same upstream account are outside these counters, so allow headroom for their traffic.

Quota waiting preserves the committed cursor and remains responsive to Pause, Cancel and duration budgets. A `quota_wait` run event includes `retry_at`. For one continuous Full traversal, manual run limits of zero pages and zero duration remove per-attempt budgets; upstream quotas and explicit operator controls still apply. A paused budget-limited run can instead use the existing eligible scheduled continuation. Waiting for a quota is not a completed catalogue, and source edits do not change an existing run's snapshot.

On a **Revision conflict**, the local draft is preserved. Select **Compare server version**, review and manually merge changes, then choose **Keep my edits on this revision** before saving again. That action adopts the current revision, not an automatic content merge. **Discard my edits** explicitly reloads the displayed server version after confirmation. Do not overwrite another administrator's changes without reviewing them.

Source definitions are private local configuration, not a versioned provider catalogue. The default `providers/` directory is ignored by Git and excluded from container builds. Normal loading accepts only `.json` (case-insensitive), skips `.example.json` and never loads legacy `.yml`/`.yaml`. No real source is enabled automatically.

JSON definitions use `"adapter": "json"`. On startup, the application automatically updates this name in valid older `"adapter": "http_json"` source files, preserving their filenames, IDs and other values. Imported data and collection baselines remain in place: the rename does not trigger another Full import. Saved run snapshots and checkpoints are unchanged and keep working. If a file cannot be updated, its older adapter name remains supported.

Each app definition is a single strict UTF-8 JSON object, at most 128 KiB, with an explicit boolean `enabled`. Comments, trailing commas, duplicate keys, unknown fixed-schema fields, wrong types, scalar nulls and decoded NUL in keys/values are rejected. Arbitrary JSON is allowed only in declared open maps/body; numeric lexemes there remain exact rather than rounding through floating point. The authenticated `GET /api/provider-schema` exposes JSON Schema for editor tooling, but server connector/credential-reference validation remains authoritative. The standalone Torznab SDK has a separate smaller JSON schema and 64 KiB limit, not an interchangeable app definition.

The HTTP/JSON and Torznab examples below are complete source documents. Other JSON blocks illustrate object fragments to merge into an existing definition, not standalone sources.

### HTTP/JSON definition

```json
{
  "version": 1,
  "id": "my_index",
  "name": "My index",
  "adapter": "json",
  "url": "https://indexer.example/api/releases",
  "enabled": true,
  "auth": {
    "type": "bearer",
    "secret_ref": "my_index_token"
  },
  "request_interval": "1s",
  "request_timeout": "30s",
  "rate_limit_reset": "epoch",
  "page_size": 100,
  "http": {
    "method": "GET",
    "items_path": "/data/items",
    "query": {
      "sort": "newest"
    }
  },
  "pagination": {
    "type": "offset",
    "in": "query",
    "offset_param": "offset",
    "size_param": "limit",
    "start": 0,
    "total_path": "/data/total"
  },
  "mapping": {
    "id": "/id",
    "fields": {
      "title": "/name",
      "size": "/size",
      "info_hash": "/hash",
      "published_at": "/created_at"
    }
  },
  "output": {
    "fields": ["title", "size", "info_hash"]
  }
}
```

Paths use **JSON Pointer**, not executable expressions. An empty `items_path` selects a root array. Mapping names become canonical fields; configure every source field you want to retain because new original bodies are not archived. Large integer identifiers stay exact. For native sources, missing/null canonical values do not erase existing values during incremental updates; explicit zero does. For metadata-capable sources, a changed native info hash instead replaces the old field set so details from the previous hash cannot contaminate the replacement. Remote catalogues preserve exact shared values and removals; see the [sharing guide](SHARING.md).

Full fails closed if any primary record cannot be interpreted or retained. The failed page keeps compact error observations but does not advance its previous successful checkpoint, staged catalogue, scope coverage or native identities; it cannot publish only the valid subset or delete existing rows because of a malformed record. Fix the upstream data and explicitly Resume to retry that checkpoint. If the mapping or another JSON setting must change, save the corrected definition and start a new Full: Resume uses the original immutable source snapshot. Valid intentional out-of-scope skips are not errors, and Preview/Incremental retain their valid-row behavior.

HTTP/JSON supports `GET` and `POST`, static non-secret headers/query parameters and a JSON body. Use `http.secret_headers` for credential-bearing headers. Ordinary pagination supports `offset`, `page`, `cursor` or `none`, in the query or a POST body. Configure the applicable `offset_param`, `page_param`, `cursor_param`, `size_param`, `start`, `next_path`, `total_path` and `current_path`. A next link must stay on the configured origin. By default, ordinary pagination rejects contradictory positions, changing advertised totals and repeated pages; bounded search windows instead need the coverage-aware traversal below.

Ordinary JSON can use a separate non-secret `http.incremental_query` map for Incremental, merged over the base `http.query`. This lets Full remain oldest-first while Incremental uses newest-first ordering and a positive known-page boundary. For an API supporting these parameter names:

```json
{
  "http": {
    "query": {
      "sort_by": "added_date",
      "order": "asc"
    },
    "incremental_query": {
      "order": "desc"
    }
  }
}
```

Base filters remain inherited. The application does not guess sorting parameters or their meaning: verify that the API actually returns newest-first results. Full and Preview retain the base query. With a nonempty override, Incremental also reapplies the merged query and configured query page size to next URLs, even when `preserve_query_on_next` is false; do not use it with opaque/signed links that prohibit added parameters. Both maps use the same credential restrictions and are frozen when a run is created. Start a new run to pick up changed ordering; Resume keeps the original snapshot. This field is not supported for Torznab, remote catalogues or bounded traversal.

Ordinary HTTP/JSON sources can opt into these `options` when their API contract requires them:

- `"local_categories": [2000, 5000]` selects exact IDs from the mapped `categories` field, without sending an undocumented remote filter. Nonmatching records leave compact ignored observations, and pagination advances over all returned records. A nonempty category mapping is required; this is not a parent-category expansion.
- `"null_items_as_empty": true` treats an explicitly present JSON `null` at `items_path` as an empty page. A missing path or another value type still fails, and advertised-total/continuation checks still apply.
- `"preserve_query_on_next": true` reapplies `http.query` and the configured query page-size parameter when following a next URL; configured values take precedence. Configured bracket arrays such as `categories[]` replace numeric indexed aliases such as `categories[0]`, preventing repeated filters from accumulating across pages or resumed checkpoints. Enable this for links that omit or rewrite required listing filters. By default, a next URL remains authoritative, without added parameters, for APIs with opaque or signed continuations.
- `"allow_total_growth": true` checkpoints the latest advertised total. Full and Preview accept monotonic growth but still reject decreases; keep Full oldest-first so new records append at the end. Incremental accepts both increases and decreases, including counts from differently aged cached pages, without deleting unseen records. Requires `pagination.total_path`; positive known pages additionally requires an explicit `http.incremental_query` configured for newest-first Incremental. Without that override, known pages must remain zero. This does not disable pagination validation: totals must be nonnegative integers, positions must match, records cannot exceed the current advertised total, and premature empty pages still fail. The latest total still participates in completion checks, and known-page stopping and its saved streak remain unchanged. With the option disabled, any total change still fails. Flexible totals do not make offset pagination a transactional snapshot: removals or reordered records can shift page boundaries, and records prepended during Incremental may be collected by the next run.

These options do not apply to remote catalogues or bounded traversal. HTTP/JSON hash normalization also accepts an 80-character hex encoding of an ASCII 40-character SHA-1 hash; invalid inner hashes remain errors. Existing historical raw archives are unchanged.

#### UNIT3D source profile

The **UNIT3D** template expands to an ordinary `"adapter": "json"` definition, not a separate transport. Set the tracker URL, a unique source ID and a bearer-token vault reference. Choose the tracker's own `categories[]` IDs, not Torznab category IDs; an empty list leaves categories unrestricted.

The profile follows `/links/next` through `/api/torrents/filter`, preserves filters and `perPage`, and maps native `/id` values from `/data`. It requests `created_at` ascending, oldest first, and disables the newest-first known-page shortcut. Its 25-row page size and three-second interval are starting values: check the site's actual quota. Saving leaves the source disabled and unscheduled until explicitly configured.

Standard UNIT3D may not expose a direct info hash; forks can expose `/attributes/info_hash`. Missing hashes stay absent, and distinct native IDs remain distinct even when hashes match. Signed download and magnet links can contain tracker credentials and are deliberately excluded from mappings and published fields. New original responses are not stored; sensitive bytes in historical archives remain private and are not deleted.

#### Bounded listing windows

An API may expose only the first 100 pages of each search even when more records exist. Increasing the run's page budget does not remove that API limit. Native `GET` sources with query-based `page` pagination can declare `traversal` to search overlapping subsets and verify their combined coverage.

Configure the actual filters and paths supported by the API. For example, these blocks describe a listing filtered by category and release year:

```json
{
  "pagination": {
    "type": "page",
    "in": "query",
    "page_param": "page",
    "size_param": "limit",
    "start": 1
  },
  "mapping": {
    "id": "/id",
    "fields": {
      "title": "/name",
      "size": "/size",
      "info_hash": "/hash",
      "category_id": "/category/id"
    }
  },
  "traversal": {
    "window_pages": 100,
    "minimum_total": 1,
    "total_paths": ["/data/total", "/meta/total"],
    "scopes": [
      {
        "id": "films",
        "query": { "category": 1 },
        "match": { "category_id": 1 }
      }
    ],
    "partitions": [
      {
        "parameter": "year",
        "start": 1900,
        "end_year_offset": 5
      }
    ],
    "query_variants": [
      { "sort": "oldest" },
      { "sort": "id_desc" }
    ],
    "options": {
      "url": "/api/categories/{category}/options",
      "groups_path": "/data",
      "values_path": "/values",
      "value_path": "/id",
      "query_param": "options",
      "priority_path": "/isRequired"
    },
    "id_recovery": {
      "discovery_query": { "sort": "id_desc" },
      "first": 1,
      "resolve_url": "/releases/{id}",
      "resolve_pattern": "^[a-fA-F0-9]{40}$",
      "detail_url": "/api/releases/{value}",
      "detail_path": "/data/item",
      "mapping": {
        "id": "/id",
        "fields": {
          "title": "/name",
          "size": "/size",
          "info_hash": "/hash",
          "category_id": "/category/id"
        }
      }
    }
  }
}
```

`total_paths` replaces `pagination.total_path` in this mode. The first present path must contain a nonnegative integer. The API must expose genuine totals for each root scope, not totals clamped to its search window. Scope `match` predicates compare mapped string/integer fields; items outside all selected scopes leave ignored observations but are not published. `minimum_total` defaults to zero; set a positive value to reject an apparently empty target when credentials may hide its data. It applies to the sum of scope targets, not individually to every scope.

`total_mode` defaults to `strict`: completion requires exact coverage against refreshed root totals. Set `"total_mode": "at_least"` explicitly for Python-style cumulative collection. Each scope's first root total becomes its checkpointed target; growth never raises that target. Further root pages and overlapping views are skipped once distinct accepted native IDs reach or exceed it. A complete single-page root response can reconcile a lower target with its current native classifications: the response must contain exactly the advertised number of distinct, valid native IDs, and every record must have explicit classification fields and belong to a selected scope. This handles reclassified records still counted by an upstream search index. Small deficits are checked with a bounded additive root pass before numeric recovery, including when resuming an existing numeric checkpoint. Partial responses, duplicates, invalid or missing classifications, and unselected records cannot lower a target. The history records each verified adjustment. There is no final moving-target refresh, and count changes alone do not invalidate collected scopes. Genuine deficits still require recovery or fail without publishing a partial Full. Both modes enforce `minimum_total` and keep different native IDs separate even when hashes match.

`at_least` is cumulative discovery, not an instantaneous catalogue mirror: disappeared records may remain, and reaching a count does not prove that no unseen replacement exists. Use `strict` for exhaustive discovery with exact per-scope reconciliation, and choose pacing appropriate to the upstream API. If coverage cannot be proven, the run fails or pauses rather than publishing a falsely complete catalogue. Existing run snapshots keep their original policy and cursor.

Traversal tries root searches, inclusive partition ranges, alternate queries, option-value filters, and then optional numeric-ID recovery. A partition requires exactly one fixed `end` or `end_year_offset`; the latter is frozen in the checkpoint using the UTC year at the start. Each configured partition parameter is tried separately, not as a Cartesian product. Omit `partitions`, `query_variants`, `options` or `id_recovery` when the API does not support them.

Option URLs can substitute scope/base query values such as `{category}`. Only enumerated values with string/integer IDs become queries; non-choice controls and headings do not become catalogue fields, and their raw response is not retained. `priority_path` puts truthy groups first. Each option starts with the base query; variants provide alternate views of the same option-filtered result set, such as different sort orders.

Before another variant, and when resuming a base query or variant, Ingest checks successful query observations from this run and traversal pass. If their distinct, currently accepted source IDs exactly cover the stable advertised filtered total, it checkpoints the next option without another source request. This also combines complementary capped queries and skips proven empty filters. Duplicate IDs, rejected records, invalidated memberships, missing totals and changing totals cannot certify completeness. Proof comes from indexed compact observations, not raw bodies, an in-memory ID list or a growing checkpoint; existing checkpoints reuse their already-committed evidence without restarting. Overall Full completion follows the configured `total_mode`.

Numeric recovery requires canonical positive numeric source IDs, not a reliable descending sort. The largest ID returned by discovery seeds the search but is not treated as the catalogue's upper bound. Starting at `first`, recovery scans at most 1,000 numeric positions between coverage checks, refreshing root totals only in strict mode. It skips IDs already collected in this run unless their scope membership needs refresh. If coverage is still missing, it extends the range beyond the listing's observed maximum without repeating completed holes. Unvisited IDs are preserved when an early coverage match is followed by growing root totals. Exhausting the positive signed 64-bit ID domain fails rather than certifying incomplete coverage.

Every `HEAD` resolver result is checkpointed before its detail request; redirects are not followed. A same-origin `Location` must end in a value matching `resolve_pattern`. A 404/410 or a successful HEAD without a Location is a recorded hole; authentication, transport and malformed-response failures are not holes. Detail mapping preserves the response's actual ID, even if it differs from the requested numeric ID. Sparse ID spaces or incorrect source totals can require a long scan: configured pacing, page budgets, Pause and Cancel still apply. Empty discovery responses and invalid numeric IDs produce specific, credential-safe failure messages.

Coverage counts distinct **source IDs per run and scope**, never hashes or old catalogue rows. The latest accepted valid observation replaces that ID's scope memberships rather than accumulating historical categories. A valid observation outside every selected scope retires the ID's derived coverage and Full staging, not historical observations; invalid or rejected observations cannot do so. Overlapping searches do not inflate coverage; separate IDs sharing a hash remain separate occurrences. New list, option, resolver and detail bodies are not retained. Network failures, including quota responses, leave compact error observations rather than being retried invisibly; pacing and server quotas still apply.

The following reconciliation and catch-up rules apply to **strict mode**.

Before declaring a Full run complete, Ingest refreshes every root scope's total and requires exact equality with committed coverage. A window boundary, short page, exhausted ID range or excess count is not completion. Contracted totals or excess observations trigger a fresh enumeration of only the invalidated scopes; their derived coverage and orphaned staging are retired atomically with the new checkpoint. Unaffected scopes and the global numeric frontier remain intact. Invalidated IDs behind that frontier are revisited separately, with their own durable position, before forward scanning continues. Holes and aliases advance that targeted position without inventing source identities or restarting the global scan. Growth alone does not discard collected identities.

Deficits first receive a bounded additive catch-up, even when numeric recovery is configured. Only deficient scopes are enumerated; accepted identities remain intact. The pass starts with root listings, stops their remaining pages once coverage reaches the refreshed target, then tries metadata options before repeating year partitions and alternate queries. This lets filters skipped against an older, already-satisfied total recover a later deficit. With numeric recovery, an unchanged or larger gap does not renew the same ineffective pass; a smaller gap can justify another pass, up to three per explicit attempt. Catch-up position and gap history survive ordinary Pause/Resume and process restarts.

Invalidated-scope re-enumeration permits at most three consecutive rounds without forward numeric progress. Advancing the numeric frontier renews that allowance; targeted rechecks do not. Additive catch-up has a separate three-round allowance and does not rewind the numeric frontier. Unresolved invalidation, exhausted catch-up without numeric recovery, or exhaustion of the numeric domain fails with structured expected/observed counts and no partial Full publication. Explicit Resume after failure renews the bounded allowances using the same immutable source snapshot, numeric frontier and retained archives.

Legacy recovery checkpoints are upgraded once by refreshing root totals and enabling additive catch-up before the next numeric range. An already-resolved detail is finished first, and unvisited numeric positions remain eligible if recovery is still needed. Ordinary Pause/Resume and scheduled budget continuation do not renew allowances. Start a new Full run only when a different source definition is required. Interrupted requests and page-budget pauses resume from the last committed checkpoint. Full ignores known-page early stopping. Preview remains a bounded, nonpublishing inspection rather than a claim of full coverage, and native Incremental collections retain their per-page merge behavior.

#### Ordered JSON incremental collection

With `traversal.id_recovery` and a positive known-page limit, Incremental uses `id_recovery.discovery_query` to fetch each selected scope newest-first. `traversal.incremental_order` selects strict descending numeric native-ID validation (`id`, the default) or publication-sorted listing with native-identity boundaries (`published_at`). Use `published_at` when publication and numeric ID allocation follow different chronologies; choose the upstream's documented publication sort. Returned dates can be reordered within or across pages and do not discard records or determine completion. A scope stops after X consecutive pages entirely known from the completed-collection baseline below, or its actual advertised end. Incremental does not repeat Full's partitions, option variants or numeric recovery. Zero disables early stopping and retains exhaustive bounded discovery.

A known native ID must already be published and have been accepted by a Full or Incremental collection that completed **before the current run was created**. The prior collection must not have skipped invalid primary records on committed pages. Recovered request errors do not disqualify an otherwise complete collection. A partially published failed/paused collection, current-run writes and matching hashes cannot establish this boundary. Resume retains the original cutoff.

Use a verified newest-first listing query for known-page stopping. Invalid numeric-ID ordering in `id` mode, a missing boundary within `window_pages`, or a short/empty capped response before the advertised end without a proven known-page boundary fails closed rather than claiming complete catch-up. Record validation and repeated-page safeguards still apply in `published_at` mode; date inversions alone are not failures. Existing publication-mode checkpoints resume at their saved scope/page and known-page streak; the old timestamp frontier is discarded on upgrade. Start a Full when new records exceed the accessible incremental window. The original run-creation baseline survives every Resume.

#### Standalone Metadata collection

Set `traversal.enrich_fields` to `["external_ids", "metadata"]` to identify fields whose absence or null value makes a published native record eligible for **Metadata** mode. This mode requires `"adapter": "json"`, no `http.catalog`, and `traversal.id_recovery` with a detail URL, detail mapping and mapped `info_hash` in both list/detail records. Every configured field must exist in the detail mapping; for an API returning `externalIds`, for example, map `"external_ids": "/externalIds"` and the relevant `metadata` subtree.

Start a separate Metadata run manually or set `schedule.mode` to `"metadata"`. It walks **all published native records that existed before run creation**, not just recently encountered listing pages, selecting those missing any configured field. Candidates are queried in bounded batches ordered by native source ID; only the last committed ID and terminal state persist in the resume cursor. No listing, option-discovery or HEAD resolver requests are made. Empty arrays count as complete, unlike absent or null fields, so a known empty external-ID list is not repeatedly fetched.

A hash is only a detail locator. Both the returned native ID and info hash must match the candidate before metadata is applied. A missing/invalid hash, a shared-hash detail for another native ID, or an HTTP 404/410 creates an explicit skipped observation with a metadata reason and advances past that candidate; neither identity is replaced or merged. Authentication, network, parse and mismatched-hash failures preserve the previous checkpoint and remain actionable. Raw detail bodies are not stored.

Metadata updates only the existing native row with that exact ID and hash. It fills absent/null fields and missing `attributes` keys without overwriting known values; it never inserts or removes catalogue rows. `external_ids` retains its full array, including unknown kinds. Known `imdb`, `tmdb_movie`, `tmdb_tv`, `tvdb`, `tvdb_series` and `tvdb_movie` kinds additionally populate string arrays in `attributes.imdbid`, `attributes.tmdbid` and `attributes.tvdbid`. Use the existing output selectors below to show these IDs.

Native JSON normalization preserves binary NFO text instead of removing NUL characters that PostgreSQL JSONB cannot store. When the mapped `metadata.nfoContent` string contains U+0000, that value becomes an object with `"encoding": "base64"` and `data` containing the standard padded Base64 encoding of the received string's UTF-8 bytes. For example, `A\u0000B` becomes `{"encoding":"base64","data":"QQBC"}`. Decoding `data` recovers the complete received value, including NULs and line endings. NFO strings without NULs, literal backslash escape text and all other metadata remain unchanged. This does not retain raw response bodies or change the native identity.

Full, Incremental and Preview perform discovery without inline missing-field enrichment; Full may still use genuine detail recovery to discover otherwise inaccessible identities. Full preserves previously collected detail-only metadata for an unchanged native ID and hash while publishing exactly its completed membership atomically. Metadata requests obey the same authentication, interval, quota, timeout and per-attempt budgets; auxiliary response observations do not incur discovery no-progress penalties. Pause/Resume keeps the original source snapshot and last committed candidate without replaying the entire catalogue.

#### Automatic Metadata after Incremental

Set `traversal.metadata_after_incremental` to `true`, or enable **Metadata after Incremental** under **Sources → Settings → source → Pagination → Bounded traversal**. It requires the Metadata configuration above and is off when omitted or false. Saving affects future run snapshots only: it does not modify existing runs or backfill completed Incrementals.

A successful Incremental, including its automatic initial Full, atomically queues at most one separate Metadata run for accepted native IDs first added during that collection and still missing a configured enrichment field. Previously existing torrents encountered by the listing are excluded, even if their metadata is missing. Already complete records, including known empty arrays, need no detail request. If no eligible new records remain, no follow-up is created. Pause, failure and cancellation do not enqueue a follow-up; an explicitly resumed collection can enqueue one once it succeeds. An explicitly requested or periodic Full, Preview and Metadata never trigger this chain.

The parent remains **Succeeded**. Its linked Metadata run inherits the exact source revision, configuration, execution policy, per-attempt budgets and manual/scheduled trigger. It uses the normal source lock, pacing and request quotas; no detail request overlaps the parent. Manual holds require explicit Resume. Scheduled follow-ups paused by an attempt budget give discovery a turn before continuing. This alternates new-torrent collection with enrichment of a large initial catalog. A budgeted discovery finishes before optional enrichment resumes; the original metadata snapshot and checkpoint are retained. Changed configuration or a manual hold still blocks automatic continuation.

The parent run's **Events** link opens the follow-up, and the child links back to its originating Incremental. To enrich older catalogue records instead, start a standalone Metadata run.

### Torznab definition

```json
{
  "version": 1,
  "id": "my_feed",
  "name": "My feed",
  "adapter": "torznab",
  "url": "https://indexer.example/api",
  "enabled": true,
  "auth": {
    "type": "query",
    "name": "apikey",
    "secret_ref": "my_feed_key"
  },
  "request_interval": "1s",
  "request_timeout": "30s",
  "page_size": 100,
  "search": {
    "categories": []
  },
  "options": {
    "category_scope": "combined",
    "search_mode": "search"
  }
}
```

Capabilities are discovered from the configured endpoint. `category_scope` can be `combined`, `each` or `advertised`; `search_mode` selects the supported Torznab search operation. Additional query parameters can be supplied through `http.query`. Mapped attributes remain in the canonical record; new XML response bodies and original item fragments are not stored.

To collect an ordered union of keyword searches in one resumable run, set `options.search_queries`, for example `["", "2010", "2010 1080p"]`. It must be a nonempty JSON array of strings; an empty string explicitly includes the unfiltered listing. Do not combine it with a nonempty `search.query`. Without this option, the existing scalar `search.query` behavior is unchanged. Every category scope is exhausted for one query before the next query starts. Overlapping native identities across queries remain legitimate observations, not additional distinct records.

The ordered queries are frozen in the run's immutable source configuration. The version-1 continuation checkpoint freezes category scopes and records `query_index` alongside the category position, offset, total and known-page streak; older checkpoints without `query_index` start at query zero. Scope and query boundaries reset offset, total and streak. Pause/Resume and quota or per-attempt budget waits continue the same plan rather than replaying completed queries. Completion means only that the configured query/category plan has finished.

Keyword partitioning is **best-effort discovery, not proof of global catalogue coverage**. A source may cap or duplicate each filtered result window, and titles outside the chosen keywords may never appear. Use **Incremental** for historical backfill to preserve published records, normally with `schedule.known_pages` set to `0` when older unseen records can lie behind known pages. **Full replaces the configured corpus**; a partial query union can remove previously published records outside that union and is unsafe when global coverage has not been proven. An empty query, advertised totals or an exhausted plan alone do not establish that proof.

For Incremental, a positive `schedule.known_pages` applies independently to every `each` or `advertised` category. With multiple `search_queries`, it applies independently to every query/category pair, including `combined` categories. Reaching X consecutive pages whose native identities all belong to the pre-run completed-collection baseline advances to the next category or query, not past the remaining plan. A new identity or an invalid/unidentified record resets the current streak; locally filtered records are still checked and cannot make an incomplete page count as entirely known. The category list, query position, offset and streak are checkpointed together and survive Pause/Resume and per-attempt budgets. A single query with `combined` retains its single-search boundary. Full ignores known-page stopping.

Equivalent Newznab and Torznab pagination response elements are accepted together when both report the same offset and total, including whether either counter is present. Conflicting counters, malformed values and repeated response elements in the same namespace remain errors. Historical XML archives are unchanged; new XML response bodies are not retained.

Repeated-page detection also covers Torznab pages with missing native identities. Their fallback fingerprint uses stable item data, ignores item order and mutable counters, and survives Pause/Resume. A repeat fails without advancing the last committed cursor. This fingerprint is pagination evidence only: it never becomes a native ID, publishes an unidentified item or establishes a known-page boundary.

The default native RSS identity is a real GUID, otherwise a usable credential-free HTTP(S) link; an info hash is never an identity fallback. Set `options.id_source` to `"guid"` or `"link"` to select the authoritative native string explicitly. An optional `id_pattern` is a Go regular expression with exactly one capturing group, applied to the full selected string; only that capture becomes `source_id`. Anchor URL patterns so query strings or fragments cannot supply a path identity. Empty or unmatched selected identities are rejected without another fallback. The original GUID/link remains mapped metadata; this does not enable raw XML retention. Existing rows and history are not rewritten. A legacy source that previously used hash-derived identities needs a deliberate future successful Full to reconcile membership; there are no compatibility hash aliases or automatic identity migrations.

`options.local_categories` applies the same exact local allowlist described above to normalized RSS categories; it is independent of the remote `search.categories` parameter. `options.total_mode` defaults to `strict`. Set it to `advisory` only for an indexer whose advertised totals are capped: exhaustive traversal then continues by actual item count until an empty page in each scope. Incremental may instead stop that scope at its configured known-page boundary. Reported totals remain in `advertised_total` page metadata; they no longer certify completion or bound the offset. Offset mismatches, overflow, malformed XML and repeated-page safeguards still apply. The standalone Go SDK exposes the equivalent total policy as `Config.AdvisoryTotals`.

### Output fields

`output.fields` selects fields returned by catalogue searches and related-occurrence responses. Omit it to return all normalized fields. Use `attributes.NAME` to select an individual member of the `attributes` object:

```json
{
  "output": {
    "fields": [
      "title",
      "guid",
      "categories",
      "size",
      "info_hash",
      "published_at",
      "attributes.imdbid",
      "attributes.tmdbid",
      "attributes.tvdbid"
    ]
  }
}
```

Attribute names follow the same safe-identifier rules as top-level fields; selectors do not traverse arbitrary nested paths. Values keep their original types: Torznab external IDs remain lists inside `attributes`, not flattened keys or scalar strings. Missing attributes are omitted. Selecting `attributes` returns the entire object and takes precedence over individual selectors, regardless of their order.

This selection does not filter stored catalogue metadata, parsed Preview samples, historical XML/JSON archives or publication history. It does not enable retention of new raw bodies or duplicate non-Preview field snapshots. Imported catalogue occurrences retain their remotely shared fields; local output selectors do not rewrite them. Public catalogue shares have their own restricted field allowlist; see [Sharing](SHARING.md).

Torznab `category`, `infohash` and `pubDate` normalize to `categories`, `info_hash` and `published_at`.

### Publication date inputs

Native Torznab and HTTP/JSON sources store `published_at` as a UTC RFC 3339 string, preserving available subsecond precision. Textual RFC 3339 and RFC 5322 dates are accepted by default. For a source that supplies integer Unix timestamps, declare its unit explicitly:

```json
{
  "options": {
    "published_at_unit": "milliseconds"
  }
}
```

Use `seconds` or `milliseconds`; the unit is never inferred from the number of digits. This applies to Torznab `pubDate` and the HTTP/JSON field mapped to `published_at`. Numeric values and integer strings are accepted when a unit is configured; ordinary textual dates remain supported.

For example, `1790084204` in seconds becomes `2026-09-22T13:36:44Z`, while `1790084204123` in milliseconds becomes `2026-09-22T13:36:44.123Z`. Zero and negative timestamps are valid. Fractional numeric epochs, overflow and dates outside UTC years 1–9999 are reported as invalid rather than truncated or replaced with zero. Missing dates remain absent.

This option changes input interpretation, not the stored date format. Historical XML/JSON archives remain unchanged; new raw bodies are not stored. Remote catalogue imports preserve the publisher's exact values and reject this normalization option.

### Authentication, pacing and schedules

Authentication types: `none`, `query`, `header`, `api_key`, `bearer`, `basic`, and `cookie`. Credentials are vault references, never literal JSON values. Basic authentication uses `username_ref` and `password_ref`; cookie authentication uses a secret containing a Cookie header. The vault stores AES-256-GCM ciphertext, not plaintext, and refuses a mismatched master key.

Source snapshots freeze secret-reference names, not secret values. Current vault values are resolved when an execution or Resume constructs its connector, not necessarily for every request in that attempt. Rotating a credential does not rewrite a run's JSON snapshot or checkpoint. Source JSON rejects decoded NUL in all scalar values and keys; response `metadata.nfoContent` follows the separate reversible Base64 rule above.

`request_interval` sets the minimum request-start interval; server cooldowns take precedence. All adapters use the shared transport to inspect `RateLimit-Remaining` / `X-RateLimit-Remaining`, reset headers and `Retry-After`. `RateLimit-Reset` is relative; `rate_limit_reset` selects `epoch` or `relative` semantics for `X-RateLimit-Reset`. A 429 itself proves exhaustion even when Remaining is absent. Every rejected response leaves a compact error observation before any retry; its raw body is not retained. The **Sources → Settings → Advanced → Import execution** page controls automatic 429 retries and maximum automatic wait for new runs; defaults are three retries and unlimited wait. Explicit Resume accepts a saved wait without shortening the source's deadline. Pause and Cancel remain responsive during cooldowns; missing reset information fails closed. Recovered failures remain visible in history.

Complete HTTP 500, 502, 503 and 504 responses to GET/HEAD requests receive three automatic retries by default (four attempts total). Configure their count and increasing delays in **Sources → Settings → Advanced → Temporary source errors**. Each new import captures those settings; existing runs retain their policy through pauses, resumes and restarts. A valid `Retry-After`, including zero, is authoritative; otherwise the default delays are 2, 4 and 8 seconds. Source intervals, server quotas, durable request limits and run controls still apply. Each failed attempt is recorded before another request, without advancing the committed checkpoint or retaining the raw body. Exhaustion fails the run with the HTTP status and attempt count; explicit Resume retains the checkpoint and saved cooldown. This policy is separate from configurable 429 retries and has no source JSON option. It does not replay unsafe methods, transport failures, incomplete bodies, parse failures or other HTTP statuses. See [application settings](CONFIGURATION_REVIEW.md) for defaults and bounds.

`request_timeout` limits each network request, not quota waiting. An explicit source value takes precedence; an omitted value uses the fixed `30s` timeout when the run is created (an explicit source timeout can be at most `15m`). The run also freezes its quota/no-progress policies and attempt-duration budget. See [Advanced](SETTINGS.md#collection-execution). Other failed remote-catalogue responses retain compact error observations for explicit or scheduled retry, not raw bodies.

Native source schedules are opt-in and editable in the source’s **Schedules** settings section or the same source JSON. Remote catalogues use a simple synchronization interval in **Connections → Catalog sharing → Remote catalogs**, described in [Sharing](SHARING.md). There is one schedule per source. Supported scheduled modes are `incremental`, `full` and `metadata`; Metadata is available only for eligible native HTTP/JSON definitions, never remote catalogues or Torznab:

```json
{
  "schedule": {
    "enabled": true,
    "every": "10m",
    "mode": "incremental",
    "max_pages": 0,
    "known_pages": 0
  }
}
```

Choose an interval of at least one minute, or a standard five-field cron expression, never both:

```json
{
  "schedule": {
    "enabled": true,
    "cron": "0 */6 * * *",
    "timezone": "Europe/Paris",
    "mode": "full",
    "max_pages": 200,
    "known_pages": 0
  }
}
```

The visual schedule controls offer **Manual only**, **Every…**, **Daily at…** and **Custom schedule (Cron)** in each source’s Schedules section. Daily time becomes a five-field Cron expression in the selected timezone. **Every 24 hours** continues to mean an elapsed interval, not a fixed daily wall-clock time; existing intervals are not converted. Custom durations and Cron expressions remain exact when unrelated settings are changed. Clearing a required frequency blocks saving rather than silently changing to manual. **Advanced** retains periodic Full, page budgets and known-page boundaries.

Cron uses the named IANA time zone, defaulting to UTC. Time-zone data is included in the binary. An interval waits its first interval; cron waits for its next matching instant. Next-run times and attempts survive restarts. Missed slots do not cause catch-up bursts, and concurrent service instances cannot claim the same slot twice.

For frequent Incremental discovery plus controlled Full reconciliation, keep **one source and one base schedule**. Set **Full reconciliation interval** in either schedule editor, or add `full_every` to an Incremental schedule:

```json
{
  "schedule": {
    "enabled": true,
    "every": "30m",
    "mode": "incremental",
    "full_every": "168h",
    "max_pages": 50,
    "known_pages": 3
  }
}
```

`full_every` is an optional Go duration of at least `1m` (`168h` is one week); absent or empty disables it. It requires an Incremental base interval or cron schedule, including the default Incremental mode. It is not a second timer or another source identity. The first Full deadline is one complete `full_every` interval after activation or source-revision reconciliation. At the first eligible regular tick on or after that deadline, Full takes precedence over a fresh Incremental. It uses the same `max_pages` budget and ignores Incremental known-page stopping.

The Full deadline persists through restarts and skipped ticks. Running work, manual/error holds and enqueue preparation failures leave an overdue deadline due; they do not create catch-up work. A budget continuation has priority over creating either mode, including a matching-revision Full under this Incremental routine. Its mode, policy and cursor stay frozen. Only creating a new scheduled Full advances its next deadline to enqueue time plus `full_every`; a budget continuation is the same reconciliation and leaves that deadline unchanged. A source revision or reactivation resets both clocks; disabling removes active deadlines until reactivation. **Schedules** shows the optional Full deadline as a first-eligible-tick target, not a guaranteed start time.

The database migration adds nullable `full_due_at` to the existing per-source schedule clock. Existing clocks retain their base deadlines; existing source definitions and immutable run snapshots are not rewritten. Definitions without `full_every` keep their prior behavior.

Setting `schedule.enabled` to `false` retains a schedule without running it. With a timing expression, an omitted `schedule.enabled` means enabled; without a timing expression the schedule is manual even when enabled is true. A disabled source never runs automatically. The panel shows the next execution, last run, errors and blocking conditions.

A scheduled run that reaches its page or duration budget resumes at the next slot, using its original snapshot. A changed configuration or incompatible mode blocks automatic continuation of that paused run; a matching-revision Full from an Incremental `full_every` routine is compatible. Resume or cancel incompatible work explicitly before starting a new traversal. Active runs are never overlapped. Manual, quota and no-progress holds are not automatically released. Cancelled or failed runs are not automatically resumed. Cancelling an individual run does not disable its schedule.

`"known_pages": 0` disables early stopping. A positive boundary applies to Incremental only when the source is reliably ordered newest-first. Torznab `each`/`advertised` and ordered bounded JSON apply this boundary separately to each scope, using only native identities that predate the run; reaching it advances to the next scope. In **Run manual import**, an empty **Known pages (X)** inherits this source setting. An explicit integer from 0 to 10000 overrides only the new Incremental run snapshot, not the source JSON. Full and Metadata never use this override.

The consecutive-known-page streak persists across Pause/Resume and page/duration-budget continuations; an unknown page resets it. The boundary remains based on native identities that predate the original run, not the latest attempt. Setting `schedule.max_pages` to `0` means unlimited scheduled pages, not inheritance from the separate manual-run default.

## Migrating existing sources

Migration is an explicit **offline** operation, not a startup feature, editor import mode or dual-format loader. It converts app source `.yml`/`.yaml` files to strict `.json`; it does not alter Compose/CI YAML or accept the standalone SDK's separate provider format. SDK JSON has its own 64 KiB schema; migrate SDK configuration separately. The app retains its YAML parser only for this one-time legacy ingestion path.

1. Back up PostgreSQL, the vault key/private state and the entire source directory using your existing installation. Keep a protected independent copy of the exact pre-migration files. Schedule a maintenance window.
2. Stop **every application instance and all other source writers**, including editors, automation and file synchronization. A migration lock coordinates cooperating registries, but it does not make a live migration safe against external writers. Keep writers stopped through review and validation.
3. Use the new binary against the existing real source directory, not a symlink. For a native installation:

   ```sh
   ./bin/ingest migrate-sources --providers /private/ingest/providers
   ./bin/ingest validate --providers /private/ingest/providers
   ```

   For the repository's Compose deployment, after separately stopping any other writers/replicas and taking the backup, use the `aiosources` service. These commands migrate legacy YAML source definitions only; AIOSources V1 instead requires the [fresh deployment described here](SETTINGS.md#first-aiosources-deployment).

   ```sh
   docker compose stop aiosources
   docker compose pull aiosources
   docker compose run --rm --no-deps aiosources migrate-sources --providers /data/providers
   docker compose run --rm --no-deps aiosources validate --providers /data/providers
   ```

   Do not proceed to restart after a failed command. The migrator itself needs no database, network access or secret resolution. `--providers` otherwise defaults to `PROVIDERS_DIR`, then `providers`.
4. Review the converted definitions privately, including enabled flags, schedules, mappings and credential references. Keep the generated `<providers-dir>/.source-migration/` directory and its `.manifest.json` journal intact. It stores exact original YAML bytes, including comments/formatting, in a private `0700` directory; journal, originals and new JSON files are written with `0600` permissions. Back up this recovery material separately; external deployment backups must include this recovery material explicitly.
5. Only after successful migration and validation, restart the new service (for Compose, `docker compose up -d --no-deps aiosources`) and review **Sources**. Existing enabled schedules can become eligible after restart; do not enable automation unintentionally.

Before any source writes, migration preflights the entire legacy batch and existing active JSON for valid app definitions, duplicate source IDs, unsafe files and case-insensitive target-name collisions. `.example.yml`/`.example.yaml` are skipped, just as `.example.json` is excluded from runtime loading. An unrelated existing `.json` target is never overwritten, even if its contents look equivalent. Fix invalid definitions or collisions offline; do not rename YAML to `.json` without conversion. The batch safety limit is 10,000 legacy sources, and generated documents must meet the app's strict validation and 128 KiB limit.

Legacy input must itself be UTF-8 and at most 128 KiB, containing one YAML mapping with correctly typed app fields. Duplicate/non-string keys, aliases/merge keys, unsupported tags, nonfinite numbers and decoded NUL are rejected rather than guessed. Conversion preserves numeric precision, but JSON necessarily changes formatting and cannot carry comments; exact originals are retained for that reason. JSON expansion can also exceed the output limit even when the original YAML fits.

The recovery journal and exact originals become durable before any JSON is installed. All converted JSON is committed before active YAML is removed. An interrupted invocation can therefore leave both formats or a partially removed legacy batch; keep services stopped and rerun the **same command with the same directory and matching migration binary**. Recovery accepts only originals and already-installed outputs matching the journal. It rejects changed originals, modified output, new unjournaled legacy files, collisions or a missing/invalid journal. Preserve all originals/journal/output and inspect the reported problem offline rather than deleting recovery state, overwriting targets or starting a fresh migration over the partial result. Originals remain in the recovery directory or their original locations if an early failure prevented copying them.

A completed rerun with no active legacy files is a no-op; it neither reimports archived originals nor rewrites JSON. Retain the recovery directory after success rather than using it as an active source folder. Do not add a different legacy batch to an existing journal; use a separate offline staging directory and review the resulting JSON before installation.

Migration never opens or rewrites the database. Existing historical run snapshots and checkpoints remain unchanged, as do native identities, published records, observations and archives. Resume keeps the run's original configuration and checkpoint; a new run is required to use an edited source. Source revisions change with file bytes, so inspect paused scheduled runs that no longer match their current definition instead of assuming automatic continuation.
