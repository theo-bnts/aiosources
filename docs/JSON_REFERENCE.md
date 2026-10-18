# JSON reference

[Back to README](../README.md) · [Configuration guide](CONFIGURATION.md) · [Deployment settings](SETTINGS.md) · [Remote catalogs](SHARING.md)

This is the field-by-field reference. The [configuration guide](CONFIGURATION.md) remains the narrative guide to choosing mappings, proving coverage, and operating resumable runs. Examples below use reserved example domains and secret **names**, never real credentials or signed download links.

## Contents

- [Configuration surfaces](#three-different-configuration-surfaces), [document rules](#application-document-rules), and [editor/migration](#visual-stages-raw-json-and-migration)
- [Application root fields](#application-root-fields-and-containers): [auth](#auth), [search](#search), [HTTP](#http), [pagination](#pagination), [mapping](#mapping-identity-and-an-open-set-of-stored-fields), [output](#output)
- [Bounded traversal](#bounded-native-json-traversal) and [mode/snapshot semantics](#coverage-modes-and-resumable-snapshots)
- [Schedules](#schedules) and [all adapter options](#all-recognized-root-options)
- [Quotas, retention, and NFO](#quotas-retention-and-nfo-values-are-not-hidden-options)
- [Remote catalogue specialization](#remote-catalogue-specialization)
- [Complete application examples](#complete-application-examples)
- [Standalone SDK schema and example](#standalone-torznab-sdk-json)
- [Compose controls](#deployment-compose-controls)
- [Compatibility and rejected keys](#accepted-compatibility-forms-and-rejected-keys)
- [Implementation sources](#implementation-sources)

## Three different configuration surfaces

| Surface | Reader | Purpose |
| --- | --- | --- |
| Application source JSON | `ingest validate`, the source editor, and the source registry | Native Torznab, native HTTP/JSON, or remote Ingest catalogue collection. This is the main schema below. |
| Standalone Torznab SDK JSON | `torznab.LoadProvider`; `torznab/examples/crawl -provider` | A separate, smaller schema with environment-variable credential references. It is **not** application source JSON. |
| `compose.yaml` | Docker Compose | Services, volumes, ports, and environment substitution. Compose settings do not become source options. |

There is no general application `config.json` or `config.yaml`. Shared collection policy, concurrency and shares are not additional source JSON blocks. Console lists use 50 rows and the browser's timezone. See [Settings](SETTINGS.md), [Operations](OPERATIONS.md) and [Sharing](SHARING.md).

## Application document rules

- Exactly one UTF-8 JSON object, at most **128 KiB**, including whitespace. Comments, trailing commas, multiple documents, duplicate property names at any depth, and unknown fixed fields are rejected. Field names are case-sensitive. JSON property names and strings require double quotes; YAML anchors, aliases, merge keys, and tags are not supported.
- Decoded U+0000 is forbidden in every application source JSON string and property name, including escaped NULs. This protects the immutable JSONB source snapshot. It is separate from the reversible handling of NUL-bearing NFO **response data** described below.
- Fixed string, integer, boolean, object, and array types are checked before decoding. Use `"enabled": false`, not `"enabled": "false"`; duration fields are JSON strings using Go duration syntax. Fixed integer fields require integer number lexemes within the platform integer range: `1` is an integer, while `1.0`, `1e0`, and `"1"` are not. Null is permitted structurally for maps, arrays, and open JSON values, subject to each field's semantic constraints below; it is rejected for fixed scalar, struct, and pointer fields. In particular, `"traversal": null`, `"auth": null`, and a null `schedule.enabled` are not omission. A null entry in a string-valued map or integer/string array is also invalid.
- Only `.json` files directly in the configured source directory are loaded; suffix matching is case-insensitive. `.example.json` files are skipped. Legacy `.yml` and `.yaml` definitions are not active runtime inputs. Files must be readable regular files, not symlinks or hard links. Filenames need not equal `id`; IDs must be unique across loaded definitions.
- Application source files are private local configuration. The default `providers/` directory is ignored by Git and excluded from container builds. Do not copy a real source definition into an issue or public example.
- Tables give **effective defaults**, distinguishing registry defaults from adapter defaults. Omission does not always equal an explicit empty string or zero. Explicit `page_size` = `0`, `request_interval` = `""`, `rate_limit_reset` = `""`, and `auth.type` = `""` fail application validation. Explicit `request_timeout` = `""` means inheritance. Empty HTTP/JSON pagination strings receive the adapter defaults listed below.
- Enum spellings are case-sensitive unless explicitly stated otherwise. In particular application URL schemes are lowercase `http`/`https`, methods are uppercase `GET`/`POST`, and duration units use Go's case-sensitive syntax.
- Validation is local: it does not contact a source, resolve a secret, prove a remote field exists, or prove that an API's totals/order are trustworthy. Missing credentials and response-contract failures remain runtime errors.
- Open-map/body numbers are decoded as exact `json.Number` values, not floating-point numbers. The source loader preserves arbitrary-map numeric lexemes exactly, including large integers and spellings such as `1e3`; a consuming option can still require an integer lexeme or narrower range. Do not round-trip documents through a floating-point-only JSON editor. The built-in editor preserves untouched number lexemes. Immutable PostgreSQL JSONB run snapshots retain exact numeric values but can canonicalize exponent spelling: `1.2300e+2` becomes `123.00`.

### Visual stages, raw JSON, and migration

The source editor has two views of the same document: **Visual stages** and **JSON**. The six settings sections are **Identity**, **Connection**, **Pagination**, **Source fields**, **Imports**, and **Schedules**. They group settings; they are not a free-form execution graph, user-defined processing order, or a separate runtime. Advanced controls cover bounded traversal, scopes, partitions, option discovery, ID recovery, Incremental ordering, and Metadata enrichment. Raw strict JSON remains available for direct editing of the full schema. Common connection parameters and page size are visible directly. Incremental ordering, response checks, headers, output selection, native ID extraction, request budgets and bounded traversal are folded; configured advanced sections are marked. Torznab search mode and category scope have named controls. Suggested field names and example pointers are hints, not automatic field rewrites. Changing stages or folding controls never deletes existing JSON values.

JSON import/export operates on application source documents, not SDK or Compose documents. Incomplete local structured drafts block saving and stage switching instead of silently discarding input. Server validation remains authoritative; stale revisions require explicit conflict resolution rather than overwriting another writer. **Save** and **Check configuration** are local configuration operations and neither contacts the source nor starts a run.

The authenticated API returns `ProviderDocument.json`; `PUT /api/providers/{id}` accepts `json` and `revision`, and `POST /api/providers/validate` accepts `json`. In these API envelopes, `json` is the source text as a JSON **string**, not an embedded source object. `GET /api/provider-schema` returns the application JSON Schema for editor/help tooling. It is deliberately outside `/api/providers/{id}` so a source whose ID is `schema` stays addressable. The schema does not replace connector validation or runtime secret checks.

For existing source YAML, use the [offline source migration procedure](CONFIGURATION.md#migrating-existing-sources). Stop all source writers/services first, then explicitly run `ingest migrate-sources --providers DIR`; there is no automatic runtime YAML loading. The migration validates the entire legacy batch, preserves exact originals and its journal privately in `DIR/.source-migration`, creates JSON without clobbering unrelated files, and removes active legacy files only after committing replacements. Interrupted matching journals can resume, and completed reruns are no-ops. It does not alter existing run snapshots or checkpoints. Compose/CI YAML is unrelated and remains YAML.

### Type and naming conventions

| Term | Meaning |
| --- | --- |
| Safe identifier | `^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`: 1–128 ASCII characters. Used for source IDs, secret references, and scope IDs. |
| Top-level mapped field | `^[A-Za-z][A-Za-z0-9_-]{0,127}$`, the intersection of application schema and adapter checks. Names start with a letter. |
| Adapter field/parameter | `^[A-Za-z][A-Za-z0-9_.-]{0,127}$`. Used by traversal parameters and detail mapping field names. A dot in a mapping name is a literal name, not nested assignment. |
| JSON Pointer | RFC 6901 pointer: `""` selects the current root, `/name` selects a member, `/items/0` an array element, `~0` escapes `~`, and `~1` escapes `/`. No JSONPath, templates, expressions, or code execution. Array indices must be nonnegative decimal indices without leading zeroes, except `0`. |
| Duration | A string accepted by Go `time.ParseDuration`, such as `400ms`, `1s`, `2m30s`, or `24h`. Bare numbers and `1d` are not duration syntax. |
| Query value | String, boolean, finite number, or an array of those scalars. Arrays become repeated query parameters; an empty array sends no value. Null, objects, and nested arrays are not request query values. All strings, including dates, must be double-quoted; JSON has no timestamp scalar type. Numeric query values retain their exact numeric value, with spelling potentially canonicalized by the JSONB run snapshot. Use a string when the upstream requires a particular numeric spelling. |

## Application root fields and containers

All paths in the next sections are relative to one application source document.

| Path | Type | Required / default | Meaning and constraints |
| --- | --- | --- | --- |
| `version` | integer | Required; no default | Exactly `1`. |
| `id` | string | Required; no default | Unique safe identifier; becomes `provider_id`. Changing it creates a different source namespace, not a rename or hash merge. Use `name` to change the label shown in the console and Stremio. |
| `name` | string | Required; no default | Nonblank, at most 200 UTF-8 bytes, no control characters. Display name, not identity. |
| `adapter` | string | Required; no default | `torznab` or `http_json`. UNIT3D is a template for `http_json`, not another adapter. |
| `url` | string | Required; no default | Absolute HTTP(S) URL with a hostname; no userinfo, fragment, controls, spaces, backslashes, or credential query parameters. Explicit port must be 1–65535. Remote catalogues have stricter rules below. |
| `enabled` | boolean | Required explicitly | `true` permits collection; `false` retains the definition disabled. A schedule does not override a disabled source. |
| `auth` | mapping | Optional; `{"type":"none"}` | Authentication configuration; see the complete matrix below. |
| `request_interval` | duration string | Optional; `1s` | Positive, at most `24h`. Minimum interval between request starts; server cooldowns can lengthen it. Applies to listing and auxiliary/detail requests. |
| `request_limits` | mapping | Optional; no proactive budgets | Persistent rolling request budgets shared by all runs of this source ID. See the limits below. |
| `request_timeout` | duration string | Optional or `""`; fixed `30s` fallback | Positive and at most `15m` when set. Per-network-request limit, not quota-wait duration. Inheritance is resolved when the run is created; the fixed fallback is `30s`. |
| `rate_limit_reset` | string | Optional; `epoch` | `epoch` or `relative`, controlling `X-RateLimit-Reset` interpretation. Standard `RateLimit-Reset` is always relative seconds. |
| `page_size` | integer | Optional; `100` | 1–10000; remote catalogue maximum is 1000. Torznab additionally respects the advertised maximum. Preferred remote page size, not a whole-run limit. |
| `search` | mapping | Optional; empty query/categories | Torznab search configuration. Native JSON and remote catalogue adapters do not translate it into API filters. |
| `http` | mapping | Optional; method `GET` | Transport headers and adapter-specific request configuration. |
| `pagination` | mapping | Optional; adapter-specific defaults | HTTP/JSON pagination. Torznab requires the zero/empty configuration because it negotiates its own offset pagination. |
| `mapping` | mapping | Optional; HTTP/JSON ID `/id`, no fields | JSON source identity and retained canonical fields. Torznab owns its RSS mapping. Remote catalogue mappings are fixed. |
| `traversal` | mapping | Optional; absent disables bounded traversal | Native HTTP/JSON only. An empty object is not a valid traversal: window, totals, and scopes must be supplied. |
| `output` | mapping | Optional; all normalized fields | Catalogue response projection, not a storage or retention policy. |
| `schedule` | mapping | Optional; manual, mode `incremental` | One advanced schedule per native source. A timing expression is necessary for automatic runs. Remote catalogues accept this only as a legacy compatibility form. |
| `catalog_sync` | mapping | Remote catalogue only; optional | Simple synchronization activation and interval. See remote catalogue specialization. |
| `options` | mapping | Optional; no entries | Adapter-specific options. The Go type is open, but current adapter validators reject unknown option names. The complete recognized set is listed below. |

### `request_limits`

| Path | Type | Default | Meaning |
| --- | --- | --- | --- |
| `request_limits.per_minute` | integer | `0` | Maximum admitted primary-origin request attempts in the preceding 60 seconds. |
| `request_limits.per_hour` | integer | `0` | Maximum admitted primary-origin request attempts in the preceding 3600 seconds. |
| `request_limits.per_day` | integer | `0` | Maximum admitted primary-origin request attempts in the preceding 86400 seconds. |

Each count must be between 0 and 1000000. Zero or omission disables that window; a present object must enable at least one positive limit. Remove the object to disable all proactive budgets. These are rolling windows, not calendar resets. For example, `request_interval: "2s"` together with `request_limits: {"per_minute": 30, "per_hour": 600, "per_day": 5000}` permits short bursts at the configured spacing, then waits until every exhausted window has room.

Admission is reserved atomically in PostgreSQL before primary-origin network I/O. Capabilities, auxiliary/detail requests and retries consume slots; public requests to separately allowed origins do not. A failed or cancelled admitted attempt is not refunded. All runs of the same source ID share the ledger, including Preview, Full, Incremental and Metadata; creating a run, resuming, editing limits or restarting the process does not reset it. Participating sources retain 24 hours of admission history even if only a shorter window is enabled. Stale rows are pruned on the source's next admission.

Waits occur outside database transactions and before the network request timeout. A `quota_wait` event records the next eligible timestamp. Pause, Cancel and per-attempt duration budgets remain responsive; waiting does not advance the collection cursor. Server cooldowns and `Retry-After` can delay a request further. Configured waits do not consume HTTP quota retries. A page or duration budget can still pause an attempt and require Resume or an eligible scheduled continuation.

The ledger starts when limits are enabled and is scoped to a source ID, not an upstream account or passkey. Earlier traffic, other source IDs and other applications are not counted; leave headroom when they share an upstream quota. The standalone connector API requires a durable admission callback when these limits are configured and refuses to silently ignore them.

### `auth`

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `auth.type` | string | Optional; `none` | `none`, `query`, `header`, `api_key`, `bearer`, `basic`, or `cookie`. |
| `auth.secret_ref` | string | Required for query/header/api_key/bearer/cookie; otherwise absent | Safe identifier naming an entry in the application's encrypted vault. Not an environment variable or credential value. |
| `auth.username_ref` | string | Required only for basic | Vault reference for the Basic username. |
| `auth.password_ref` | string | Required only for basic | Vault reference for the Basic password. Both Basic references are required. |
| `auth.in` | string | Required for api_key; otherwise optional/empty | For `api_key`, exactly `query` or `header`. For other non-Basic authenticated types, if supplied it must equal `auth.type`; it is redundant, not an independent routing override. `none` and `basic` reject it. |
| `auth.name` | string | Required for query/header/api_key; otherwise absent | Exact query parameter/header name; must be a valid HTTP token. Header names additionally cannot be transport-managed. Cookie headers must use `type` = `"cookie"`. |

| Type | Legal authentication fields beyond `type` | Wire behavior |
| --- | --- | --- |
| `none` | None | No configured authentication. |
| `query` | `secret_ref`, `name`; optional `in` = `"query"` | Sets the named query parameter on requests. |
| `header` | `secret_ref`, `name`; optional `in` = `"header"` | Sets the named request header. |
| `api_key` | `secret_ref`, `name`, `in` = `"query"` or `in` = `"header"` | Explicit spelling for query/header key authentication. |
| `bearer` | `secret_ref`; optional `in` = `"bearer"` | Sets `Authorization: Bearer …`. Store the token without a prefix; an existing `Bearer ` prefix is stripped. |
| `basic` | `username_ref`, `password_ref` | HTTP Basic, not an HTML login form. Resolved username cannot contain `:`; the password can. |
| `cookie` | `secret_ref`; optional `in` = `"cookie"` | Parses a Cookie request-header value into the session jar. Do not supply a Set-Cookie response-header value. |

Unused credential references are errors, not alternative fallbacks. Auth and static/secret headers cannot configure the same header, ignoring case. Query auth cannot collide with static query parameters or a parameter already in `url`, ignoring case. Referenced secrets must exist and be nonempty when a connector is opened; CR, LF, and NUL are rejected. Resolved secret bytes are not stored in source JSON or the run's source snapshot.

### `search`

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `search.query` | string | Optional; `""` | Torznab search text; empty requests the accessible feed. Must be empty when `options.search_queries` is present. Use `http.query`/`http.body` for JSON API filters. |
| `search.categories` | sequence of integers | Optional/null/`[]`; no filter | Nonnegative category IDs. Combined Torznab search sends them as OR; no guessed parent expansion. `each` and `advertised` behavior is controlled by `options.category_scope`. |

### `http`

| Path | Type | Required / default | Meaning and constraints |
| --- | --- | --- | --- |
| `http.method` | string | Optional; `GET` | Exactly `GET` or `POST`. Torznab, bounded traversal, and remote catalogue require `GET`. |
| `http.headers` | string-to-string map | Optional/null; empty | Open map of literal **non-secret** request headers. Names must be HTTP tokens; values cannot contain forbidden control bytes. |
| `http.headers.<name>` | string | Optional entries; no implicit value | Static value for that header, such as `Accept: application/json`. No interpolation or secret resolution. |
| `http.secret_headers` | string-to-string map | Optional/null; empty | Open map of header names to vault references. Forbidden for remote catalogue mode. |
| `http.secret_headers.<name>` | string | Required value for each entry | Safe secret-reference name. `Cookie` and `Set-Cookie` cannot be placed here; use cookie auth. |
| `http.query` | string-to-query-value map | Optional/null; empty | Open map of static non-secret query parameters. Applied to Torznab and native JSON; remote catalogue forbids overrides. |
| `http.query.<name>` | query value | Required value for each entry | Names must be nonempty and control-free. Traversal has stricter names/reservations. Arrays repeat the exact key, including a literal `categories[]` key. |
| `http.incremental_query` | string-to-query-value map | Optional/null; empty | Ordinary native JSON only, without bounded traversal or remote catalogue mode. Merged over `http.query` for Incremental; Full and Preview keep the base query. Configure API-supported newest-first ordering when using known-page stopping. |
| `http.incremental_query.<name>` | query value | Required value for each entry | Same value, name, credential and auth-query collision rules as `http.query`. Overrides the same base key; other filters remain inherited. A nonempty map forces the merged query and query page size onto next URLs, including after Resume. |
| `http.body` | JSON-compatible value | Optional/null; no body | Native JSON `POST` only. Query-pagination POST can send an object, array, or scalar; body pagination requires an object or omitted/null body. Maximum encoded request body **1 MiB**, nesting at most 128 levels. Credentials in body data are forbidden. |
| `http.items_path` | JSON Pointer string | Optional; `""` for native JSON, `/items` for remote catalogue | Native JSON pointer to an array in the response. Empty selects a root array. Torznab requires empty. |
| `http.catalog` | boolean | Optional; `false` | `true` activates the fixed remote Ingest catalogue protocol on `adapter` = `"http_json"`, not ordinary user-mapped JSON. |

Header names are unique across both maps ignoring case. `Host`, `Content-Length`, `Transfer-Encoding`, `Connection`, `Trailer`, `Upgrade`, and `Proxy-Connection` are transport-managed and forbidden. Literal credential-bearing headers such as `Authorization`, `Cookie`, and API-token headers are forbidden even if their value looks like a reference. Put the reference in `auth` or `http.secret_headers` instead.

The application also checks credential-like names recursively in `http.query`, `http.incremental_query`, `http.body`, `options`, and traversal query/match data. Key matching is case-insensitive and normalizes punctuation; it recognizes authentication/password/token/session/key/signature names and common credential suffixes. A safe-looking `*_ref` entry in an arbitrary map is **not automatically resolved**: only the explicit auth and secret-header fields resolve vault entries. There is no template expansion or environment substitution in application JSON.

Torznab's static query cannot override `t`, `apikey`, `q`, `cat`, `offset`, `limit`, `extended`, or `o`. Use `search` and options for the corresponding supported controls. Source URL query parameters are subject to credential validation too; avoid putting generated request parameters in the endpoint URL.

### `pagination`

The defaults here are applied by the **HTTP/JSON adapter** to missing or empty strings. They are not Torznab defaults. `start` remains zero unless explicitly set: page APIs that are one-based need `start` = `1`.

| Path | Type | Required / default | Meaning and dependencies |
| --- | --- | --- | --- |
| `pagination.type` | string | Optional; native JSON `offset`, remote `cursor` | `none`, `offset`, `page`, or `cursor`. Torznab must omit it or leave it empty. |
| `pagination.in` | string | Optional; JSON `query` | `query` or `body`; body requires POST. Remote/bounded traversal require query. |
| `pagination.page_param` | string | Optional; JSON `page` | Parameter used by page pagination. Inactive for other types. |
| `pagination.offset_param` | string | Optional; JSON `offset` | Parameter used by offset pagination. Inactive for other types. |
| `pagination.size_param` | string | Optional; JSON `limit` | Parameter carrying `page_size` whenever pagination is not `none`. Empty also becomes `limit`; it does not disable size transmission. |
| `pagination.cursor_param` | string | Optional; JSON `cursor` | Parameter carrying a nonempty cursor in cursor mode. |
| `pagination.start` | integer | Optional; `0` | Nonnegative starting page/offset. For cursor mode, positive values become a decimal initial cursor; zero sends no cursor. No arbitrary initial string-cursor JSON field exists. |
| `pagination.next_path` | JSON Pointer string | Optional; empty, except remote `/next_cursor` | Required for cursor pagination. Selects next cursor, consecutive page/offset, same-origin next URL, or a null/empty-string/false terminator. Bounded traversal forbids it. |
| `pagination.total_path` | JSON Pointer string | Optional; empty | When configured, response must contain a nonnegative integer total. Ordinary pagination verifies it on every page. Bounded traversal uses `traversal.total_paths` instead. Remote forbids it. |
| `pagination.current_path` | JSON Pointer string | Optional; empty | When configured, response must report the requested page (page mode) or logical offset (other modes) as a nonnegative integer. Remote forbids it. |

Active position and size parameters must be distinct, safe adapter parameter names, non-credential names, and cannot overwrite auth. Generated pagination values override the same keys in static query/body data. `none` sends neither generated size nor position and accepts only a single complete page; a claimed unfinished total or explicit continuation is an error.

A missing configured next/total/current response path is an error. A next URL can be absolute, root-relative, or query-relative but must stay on the source's origin. By default it is authoritative: no static query or page-size additions are made when following it. `options.preserve_query_on_next` explicitly changes that behavior. Ordinary pagination rejects repeated positions/continuations, contradictory totals, excess records, and premature termination. A short nonempty ordinary page alone is not an EOF signal.

### `mapping`: identity and an open set of stored fields

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `mapping.id` | JSON Pointer string | Optional; native JSON `/id` | Pointer relative to each list item. Empty also receives `/id`; it does not select the whole item as ID. Must yield a nonempty stable scalar native identity. Torznab requires empty; remote fixes `/id`. |
| `mapping.fields` | string-to-string map | Optional/null; empty | Open map of canonical field names to pointers relative to each list item. No automatic copy of the full response. Torznab and remote require no entries. |
| `mapping.fields.<field>` | JSON Pointer string | Required value per entry | Top-level mapped-field name; an empty pointer retains the whole item under that field. Missing/null values remain absent. All retained data must be deliberately mapped. |

`mapping.fields` is not a finite enum: `category_name`, `external_ids`, `metadata`, or another valid field name can retain source-specific data. Special normalization currently applies to:

| Mapped field | Normalization |
| --- | --- |
| `title` | Must be a string. |
| `size` | Nonnegative signed 64-bit integers; integer-compatible source strings are accepted. |
| `info_hash` | 40/64-character hexadecimal or 32-character Base32 hash; normalized lowercase hexadecimal. Native JSON also accepts one 80-hex-character encoding layer around a 40-character ASCII SHA-1. No hash becomes native identity. |
| `published_at` | UTC RFC 3339 with available subsecond precision. Text dates are accepted; numeric epochs need `options.published_at_unit`. |
| `categories` | A scalar or sequence of nonnegative integer-compatible IDs becomes an integer list. |
| `external_ids` | Full array retained, including unknown kinds. Known external-ID kinds additionally populate compatible `attributes` arrays. |
| `metadata` | Subtree retained, with the binary-NFO representation described below. |

| Other names | Source JSON value retained without these special coercions; mapping is not a transformation language. |

The application discards `seeders`, `peers` and `leechers`, including nested attribute keys, before storing interpreted records. Existing mappings are accepted for compatibility but do not collect these fields. Enrichment candidates ignore them. The standalone Torznab SDK retains its independent protocol support.

Identifiers are read without floating-point conversion for JSON numbers, preserving large integer IDs. Arrays/objects are not IDs. `(provider_id, source_id)` is the identity: different source IDs never merge because hashes match.

Native JSON string IDs must be valid UTF-8 without U+0000. Invalid IDs are rejected rather than rewritten into another identity or copied into unrepresentable diagnostic metadata. In Metadata, that response fails at the previous committed checkpoint and retains the error observation; correcting the response and resuming retries the same candidate.

Torznab normally takes a real RSS GUID, otherwise a usable credential-free HTTP(S) link. Missing native identity is rejected; an info hash is **not** an identity fallback. `options.id_source` and `id_pattern` explicitly override that selection. Existing rows/history are not rewritten by this rule. A legacy source that previously relied on hash-derived IDs needs a deliberate future successful Full to reconcile membership; there are no compatibility hash aliases or automatic migrations.

### `output`

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `output.fields` | sequence of strings | Optional/null/`[]`; all normalized fields | Unique top-level field selectors or `attributes.NAME`. Unknown-but-valid field names select nothing when absent; this is not a finite normalized-field enum. |

Top-level selectors start with an ASCII letter and use up to 128 letters/digits/underscores/hyphens. `attributes.NAME` selects one literal safe-identifier attribute name, not an arbitrary nested path; the entire selector is also limited to 128 characters, leaving at most 117 for NAME. `metadata.nfoContent` is not a supported nested selector. Selecting `attributes` returns the entire object and takes precedence over individual attribute selectors. Values retain their types; external IDs in attributes remain lists.

This controls catalogue search and related-occurrence responses, not stored metadata, Preview samples, old archives, publication history, or public share permissions. Remote imported values are not rewritten by local output selectors. Public shares have their own restricted allowlist. **Application `output.fields` = `[]` means all fields; standalone SDK `output.fields` = `[]` is an error.**

## Bounded native JSON traversal

`traversal` requires native `adapter` = `"http_json"`, `http.method` = `"GET"`, no body/catalog mode, `pagination.type` = `"page"`, `pagination.in` = `"query"`, and no `next_path`. Page/size parameters receive the normal JSON defaults. The API must expose genuine per-scope totals, not a total clamped to its visible search window.

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `traversal.window_pages` | integer | Required; no positive default | Positive maximum number of pages accessible in one listing view. `window_pages + pagination.start` must fit the platform integer. Not a run page budget. |
| `traversal.minimum_total` | integer | Optional; `0` | Nonnegative minimum sum of selected scope targets. Rejects suspiciously empty/small visible catalogues. Not a minimum for each scope. |
| `traversal.total_mode` | string | Optional/empty; `strict` | `strict` or `at_least`; coverage semantics below. |
| `traversal.total_paths` | nonempty sequence of strings | Required | Nonempty JSON Pointers tried in order; first present path must contain a nonnegative integer. A malformed present value is not skipped in favor of a later path. |
| `traversal.scopes` | nonempty sequence of mappings | Required | Selected catalog subsets, each with a remote query and local mapped-field match. |
| `traversal.scopes[].id` | string | Required | Unique safe identifier within the traversal. |
| `traversal.scopes[].query` | string-to-query-value map | Optional/null; empty | Query overrides for this scope, applied over base `http.query`. |
| `traversal.scopes[].query.<name>` | query value | Required value per entry | Open map, subject to traversal parameter restrictions below. |
| `traversal.scopes[].match` | nonempty string-to-scalar map | Required | All entries must match for a record to belong to that scope. Keys must exist in list mapping and, when configured, detail mapping. |
| `traversal.scopes[].match.<field>` | nonempty string or integer-compatible scalar | Required value per entry | Compared using scalar string identity, not JSON object comparison, category hierarchy, or hash matching. Booleans, null, arrays, and objects are not match values. |
| `traversal.partitions` | sequence of mappings | Optional/null/`[]`; none | Each inclusive range is tried separately; not a Cartesian product of partitions. |
| `traversal.partitions[].parameter` | string | Required | Safe non-credential traversal query parameter. |
| `traversal.partitions[].start` | integer | Optional; `0` | Inclusive first partition value. Negative values are permitted if the API accepts them. |
| `traversal.partitions[].end` | integer | Exactly one end form required | Inclusive fixed end, at least `start`. Explicit null is rejected, not treated as omission. |
| `traversal.partitions[].end_year_offset` | integer | Exactly one end form required | End = UTC year at traversal initialization + offset; must not overflow or precede `start`. Frozen into the checkpoint. No fixed maximum offset apart from integer/range checks. |
| `traversal.query_variants` | sequence of nonempty query maps | Optional/null/`[]`; none | Alternative query views/sorts. Each starts from the applicable base/scope/filter query, not the previous variant. |
| `traversal.query_variants[].<name>` | query value | Required value per entry | Open map with the same traversal parameter restrictions. |
| `traversal.options` | mapping | Optional; no option discovery | Configuration for remote filter-option enumeration, **not** the root adapter `options` map. |
| `traversal.id_recovery` | mapping | Optional; no numeric recovery | Numeric-ID discovery/resolution/detail configuration. Also used to configure ordered Incremental and Metadata. |
| `traversal.incremental_order` | string | Optional/empty; `id` | `id` validates strictly descending native IDs; `published_at` uses a publication-sorted query with identity-based stopping and tolerates date reordering. Any nonempty setting requires `id_recovery`; `published_at` also requires mapped list `published_at`. Used by Incremental, not Full coverage. |
| `traversal.enrich_fields` | sequence of strings | Optional/null/`[]`; Metadata unavailable | Unique safe adapter field names present in detail mapping. Nonempty selection requires ID recovery plus `info_hash` in list and detail mappings. Missing/null fields select Metadata candidates; empty arrays are complete. |
| `traversal.metadata_after_incremental` | boolean | Optional; `false` | Requires Metadata support. After successful Incremental completion, atomically queue one separate Metadata run for newly inserted accepted native IDs still missing enrich fields. Existing encountered IDs are excluded; no eligible work means no child. Frozen into future run snapshots only. |

Traversal query parameter names use the adapter parameter syntax. They cannot be credential names, collide with `auth.name`, or overwrite any nonempty configured page/size/offset/cursor parameter, ignoring case. The rule applies to base source URL query, `http.query`, scope queries, partitions, variants, discovery queries, and traversal URL query parameters. Array-valued filters are permitted, but URL placeholders require a nonempty scalar value.

### `traversal.options`

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `traversal.options.url` | string | Required | Same-origin absolute or root-relative URL; may substitute `{name}` from base/scope query scalar values. Unknown/empty placeholders fail. |
| `traversal.options.groups_path` | JSON Pointer string | Optional; `""` | Selects the array of option groups; empty selects the response root. |
| `traversal.options.values_path` | JSON Pointer string | Optional; `""` | Relative to each group; selects its choices. Empty selects that group itself. |
| `traversal.options.value_path` | JSON Pointer string | Optional; `""` | Relative to each choice; selects the scalar string/integer ID. Empty selects the choice itself. |
| `traversal.options.query_param` | string | Required | Parameter receiving one enumerated option ID; safe non-reserved traversal parameter. |
| `traversal.options.priority_path` | JSON Pointer string | Optional; `""` disables priority | Relative to each group. Truthy groups are visited first: true booleans, nonempty strings, nonzero numbers, and nonempty arrays/objects. Missing/null, false, zero, and empty values are false; the nonempty string `"false"` is true. |

Only enumerated string/integer choices become listing queries. Headings and controls without valid choices do not become catalogue fields. Responses are not retained as new raw archives. Path/query placeholder values are encoded as data, never executable URL syntax. Successful, stable filtered totals can be proven by already-committed distinct native IDs; such a proof skips redundant queries, not unverified coverage.

### `traversal.id_recovery`

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `traversal.id_recovery.discovery_query` | nonempty query map | Required | API-supported query that seeds numeric discovery; also the listing order for known-page Incremental. |
| `traversal.id_recovery.discovery_query.<name>` | query value | Required value per entry | Open map with traversal restrictions. |
| `traversal.id_recovery.first` | integer | Required positive value | First numeric ID to scan; at least `1`. Zero/omission is invalid. |
| `traversal.id_recovery.resolve_url` | string | Required | Same-origin absolute/root-relative URL containing exactly one `{id}`. Requested with HEAD; redirects are not followed. |
| `traversal.id_recovery.resolve_pattern` | string | Required | Valid Go regexp beginning `^`, ending `$`, and not matching the empty string. Matches the last path segment from a same-origin Location. No capturing-group count requirement here. |
| `traversal.id_recovery.detail_url` | string | Required | Same-origin absolute/root-relative URL containing exactly one `{value}`. Resolver values are escaped. Metadata uses the candidate's info hash as the locator without HEAD. |
| `traversal.id_recovery.detail_path` | JSON Pointer string | Optional; `""` | Selects one detail item; empty selects the response root. |
| `traversal.id_recovery.mapping` | mapping | Required | Detail identity/field mapping. Must include every selected scope-match field. |
| `traversal.id_recovery.mapping.id` | JSON Pointer string | Required, nonempty | Detail item's actual native identity. There is no `/id` default for this nested mapping. |
| `traversal.id_recovery.mapping.fields` | string-to-string map | Required by scope/metadata dependencies | Open map from safe adapter field names to detail-relative pointers. At minimum, includes all scope-match fields; Metadata also requires `info_hash` and every enrich field. |
| `traversal.id_recovery.mapping.fields.<field>` | JSON Pointer string | Required value per entry | Same value extraction/normalization as list fields. Detail names use adapter field syntax, which also accepts literal dots. |

A 404/410 resolver response or successful HEAD without Location records a hole. Authentication, transport, and malformed-response failures are not holes. Resolver results are checkpointed before fetching details; the detail's actual ID remains authoritative even when different from the probed numeric ID. Recovery scans positive signed 64-bit IDs, checking coverage between bounded ranges of at most 1000 positions. The highest listing ID seeds a range, not a claim that no later ID exists. Exhausting the numeric domain fails rather than certifying incomplete coverage.

### Coverage, modes, and resumable snapshots

- **Full** is exhaustive and publishes native membership atomically only after successful completion. It ignores known-page stopping. Any primary record interpretation or database-retention error fails the page closed: compact error observations remain, but the previous successful cursor, staging, coverage, and identity state do not advance. It cannot publish just the valid subset or delete existing rows because another record was invalid. Correct the upstream data and explicitly Resume to retry that checkpoint; if the source JSON/mapping itself needs correction, save it and start a new Full, because Resume retains the old snapshot. Valid intentional out-of-scope skips are not errors. Failure, Pause, Cancel, or an attempt budget does not publish a partial Full. Existing detail-only metadata survives for unchanged native ID/hash pairs.
- **Incremental** merges native pages without deleting unseen records. Ordinary known-page stopping requires reliable newest-first ordering. For bounded JSON with `id_recovery` and positive `schedule.known_pages`, ordering is checked separately per scope using `discovery_query`. It does not repeat Full partitions/options/numeric recovery. Known IDs must already be published and accepted by a Full or Incremental run completed before current run creation, without skipped invalid primary records on committed pages. Recovered request errors are allowed; a failed/paused partial collection, current-run inserts and matching hashes cannot establish a boundary. A capped/short page before the advertised end is not completion unless that boundary has actually been reached. Out-of-order results or an inaccessible boundary fail closed; use a Full.
- **Preview** is bounded inspection, never publication or evidence of exhaustive coverage. Its initial shared page default is 3, not a JSON field.
- **Metadata** started manually or via `schedule.mode` walks the entire published native catalogue that existed before run creation, selecting any row missing/null in at least one enrich field. An automatic Incremental follow-up instead selects only its parent's newly inserted accepted native IDs. Both make detail requests only: no listing, option discovery, HEAD resolution, insertion, or deletion. Both returned native ID and hash must match. Missing/invalid candidate hashes, another native ID sharing the hash, and 404/410 details produce explicit skips; auth/network/parse/mismatched-hash failures remain actionable. Metadata fills absent/null fields and missing attribute keys, never overwrites known values. See [automatic follow-up eligibility and continuation](CONFIGURATION.md#automatic-metadata-after-incremental).

Incremental and Preview retain their valid-row behavior when another row is rejected. Incremental's consecutive-known-page streak survives Pause/Resume and page/duration-budget continuations; an unknown page resets it. Bounded JSON and Torznab `each`/`advertised` keep an independent streak per scope. Reaching the boundary ends only that scope, then collection continues with the next. The original completed-collection cutoff remains unchanged across every attempt.

Ordinary JSON can keep Full oldest-first while using `http.incremental_query` to request newest-first Incremental pages. The application does not infer upstream sorting parameter names or validate their meaning. Both query maps are frozen in the run snapshot: editing the source affects new runs, not Resume. Do not use this override with opaque or signed next URLs that cannot accept reapplied parameters.

In `strict` traversal, final refreshed root totals must equal distinct accepted native IDs in every scope. Overlapping searches do not inflate coverage. The latest accepted valid observation replaces that ID's scope memberships; rejected records cannot erase prior valid membership. Contracted/excess coverage triggers bounded scope re-enumeration; deficits get bounded additive catch-up and optional numeric recovery. A search-window boundary is not EOF. See the [detailed recovery rules](CONFIGURATION.md#bounded-listing-windows).

In `at_least`, each scope's first root total is its checkpointed target, and growth does not raise it. A fully enumerated single-page root can reconcile a lower target with explicit native classifications; partial responses, duplicates, errors and unselected or unclassified records cannot do so. Small deficits get a bounded root check before numeric recovery. Reaching or exceeding the accepted target stops additional views for that scope; no final moving-target refresh is performed. This is cumulative discovery, not an instantaneous mirror: reaching a count cannot prove an unseen replacement does not exist. Real deficits still fail or require recovery. Both modes enforce `minimum_total` and preserve distinct native identities.

A run freezes its source definition/revision, traversal policy, inherited timeout, execution policy, and attempt budgets when created. Resume uses that immutable configuration and committed cursor; editing JSON does not rewrite active/paused runs or replay them with a new source definition. Year-derived partition ends are frozen at traversal initialization. Secrets remain references: current vault values resolve when an execution or Resume constructs its connector, not necessarily on each request in the same attempt. Rotating a credential does not rewrite the source snapshot. Start a new run to adopt a different source definition. No documentation/example change authorizes modifying an existing run's snapshot or checkpoint.

## Schedules

These advanced controls apply to native sources. Remote catalogues use `catalog_sync` below; old remote `schedule` blocks are converted on read.

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `schedule.enabled` | boolean | Optional; enabled when timing is configured | With a nonempty `every` or `cron`, omission behaves like `true`. `false` retains a disabled schedule. Without timing, even `true` is manual. Source `enabled` must also be true. |
| `schedule.every` | duration string | Optional/empty; no interval | At least `1m`; mutually exclusive with nonempty cron. First execution waits one interval. Maximum is Go duration representability, not a separate JSON cap. |
| `schedule.full_every` | duration string | Optional/empty; disabled | At least `1m`, using Go duration syntax (`168h` = one week). Requires an Incremental base `every`/`cron` schedule; omitted/empty mode counts as Incremental. At a due base tick, Full takes priority over a fresh Incremental and uses the same `max_pages`. First due after one full interval, never immediately on activation. |
| `schedule.cron` | string | Optional/empty; no cron | Standard five fields: minute, hour, day-of-month, month, day-of-week. No seconds field or `@every` descriptor. Must have a matching calendar time. Mutually exclusive with every. |
| `schedule.timezone` | string | Optional/empty; `UTC` | IANA time zone, such as `Europe/Paris`; `Local` is rejected. Validated even for manual/interval schedules; only cron uses it to select instants. |
| `schedule.max_pages` | integer | Optional; `0` | 0–10000 committed data pages per scheduled attempt and the default for a manual non-Preview launch; `0` is unlimited. A per-run override takes precedence. A successful page containing primary records counts once, including a successful Metadata detail. Control-only, empty, skipped-hole, and errored responses do not consume this budget. |
| `schedule.mode` | string | Optional/empty; `incremental` | `incremental`, `full`, or `metadata`; never `preview`. Metadata requires an eligible native JSON definition even when the schedule is disabled. |
| `schedule.known_pages` | integer | Optional; `0` | 0–10000 consecutive already-known pages. `0` disables stopping. Used only by Incremental; Full and Metadata ignore it. Torznab `each`/`advertised` applies the boundary separately to each category, then continues with the next. |

Cron ranges are minute 0–59, hour 0–23, day-of-month 1–31, month 1–12, and day-of-week 0–6 (Sunday = 0; 7 is not accepted). Month/day names use case-insensitive three-letter abbreviations. Comma lists, inclusive `-` ranges, positive `/` steps, `*`, and `?` as a wildcard are accepted by the pinned parser; wraparound ranges are not. When both day-of-month and day-of-week are restricted, either matching day condition can trigger a run. Put the time zone in schedule.timezone, not a sixth `TZ=`/`CRON_TZ=` field.

Missed schedule slots do not create catch-up bursts. Active runs do not overlap. A scheduled run paused solely for a page/duration budget may continue at the next slot with the original snapshot; changed configuration/incompatible mode blocks automatic continuation until the old run is explicitly resumed/cancelled. A matching-revision paused Full remains compatible with an Incremental schedule having `full_every`. Manual, quota, and no-progress holds are not automatically released. Failed/cancelled runs are not automatically resumed by a schedule. Cancelling a run does not disable its schedule.

`full_every` does not create a second executable schedule or another source identity. Its nullable `full_due_at` clock is stored alongside the base deadline and survives restart. Activation/revision reconciliation starts its first interval; revision changes or reactivation reset both clocks. A due Full waits for the first eligible regular tick on or after its deadline. An existing budget continuation takes precedence over either fresh mode and keeps its immutable mode, execution policy and cursor. Blocked/running work and preparation failures retain the Full deadline even as the base slot advances. Only queueing a new scheduled Full advances it to enqueue time plus `full_every`; budget continuations retain that deadline and missed deadlines never accumulate a queue. Disabling clears active clocks, not existing run snapshots. Schedules without `full_every` are unchanged.

The schedule-summary API optionally exposes `next_full_at` as this reconciliation deadline, not a guaranteed execution time. It is absent for inactive schedules or routines without Full reconciliation.

The manual Start a run form has separate request controls: empty Known pages inherits the JSON source setting, while an explicit integer 0–10000 overrides only a new Incremental snapshot. Manual page/duration values use shared defaults when omitted. These request fields are not additional JSON keys.

## All recognized root `options`

`options` is structurally an open JSON-compatible map, not a second fixed Go struct. Current adapters deliberately whitelist its names. Arbitrary names, SDK-only fields, and guessed retry/retention options are rejected. Each recognized option below is optional; the documented default applies when the key is absent. An explicit null has the wrong type.

| Option path | Type | Default | Applies to / dependencies |
| --- | --- | --- | --- |
| `options.published_at_unit` | string | Absent: textual dates only | Native Torznab and native JSON, including bounded traversal. Exactly `seconds` or `milliseconds`; empty string is invalid. Numeric publication values and integer strings use that unit, never digit-count inference. Text dates remain accepted. Remote catalogue forbids it. |
| `options.local_categories` | nonempty sequence of integer-compatible values | Absent: no local filter | Torznab and ordinary native JSON only. Nonnegative signed 64-bit IDs. Use JSON integer number lexemes such as `2000`, or decimal integer strings such as `"2000"`. The loader preserves numbers as `json.Number`, and `Int64` rejects decimal-point/exponent spellings such as `2000.0` or `2e3` even when mathematically integral. Duplicate IDs are harmless. Native HTTP/JSON requires a nonempty `mapping.fields.categories` pointer. Exact normalized-category match; no remote filter or parent expansion. Empty sequence/null is invalid; remove the key to disable. |
| `options.category_scope` | string | `combined` | Torznab only: `combined`, `each`, `advertised`. `combined` uses one search over configured categories. `each` requires categories and searches each distinct configured ID. `advertised` requires no configured categories and visits every advertised parent/child ID; no advertised categories is a runtime error. Positive known_pages stops Incremental separately in each category; with multiple search queries, separately in each query/category pair, including combined categories. Counters and the negotiated category list survive Resume. |
| `options.search_mode` | string | `search` | Torznab only: `search`, `tvsearch`, `movie`, `music`, or `book`. Requested mode must also be available in the source's capabilities at runtime. |
| `options.search_queries` | nonempty JSON array of strings | Absent: use `search.query` | Torznab only. Ordered query plan; `""` explicitly requests the unfiltered listing. Rejects null, an empty array, nonstrings and a nonempty `search.query`. Visits all category scopes for each query before advancing to the next. One resumable run uses the ordinary client, pacing and quotas. Overlap is allowed; query unions do not certify global coverage. |
| `options.id_source` | string | Absent/empty: default native RSS identity | Torznab only. `guid` or `link` selects that source string; no fallback if selected identity is missing/invalid. |
| `options.id_pattern` | string | Absent/empty: no extraction | Torznab only. Nonempty Go regexp with exactly one capturing group; requires explicit `id_source`. The capture becomes native source ID. Anchor URL patterns to avoid identities supplied by query/fragment text. |
| `options.total_mode` | string | `strict` | Torznab only. `strict` checks advertised totals; `advisory` retains them as `advertised_total` metadata but continues by actual item count until an empty page per scope, unless Incremental reaches its configured known-page boundary first. Offset checks, overflow, malformed-response, and repeated-page safeguards remain. Not the same option as `traversal.total_mode`. |
| `options.null_items_as_empty` | boolean | `false` | Ordinary native JSON only when true. A present null at items_path becomes an empty page; a missing path or different wrong type still fails. Total/continuation checks still apply. |
| `options.preserve_query_on_next` | boolean | `false` | Ordinary native JSON only when true. Reapplies static query and query-mode page-size parameter to next URLs, with configured values taking precedence. Configured bracket arrays replace numeric indexed aliases. A nonempty `http.incremental_query` implicitly enables this preservation during Incremental, even when this option is false. Omit both for opaque/signed continuations that must remain authoritative. |
| `options.allow_total_growth` | boolean | `false` | Ordinary native JSON only when true. Requires nonempty total_path. Positive known_pages additionally requires explicit `http.incremental_query` for newest-first Incremental; without it, known_pages must be zero. Full and Preview accept monotonic growth and reject decreases. Incremental accepts increases and decreases, including stale cached counts; all modes checkpoint the latest accepted total. Integer/position/bounds/completion checks and known-page stopping remain enforced; contradictory or premature endings still fail. When false, any total change fails. Keep Full oldest-first for append-at-end feeds. Offset pagination is not a transactional snapshot: removals/reordering can shift page boundaries and new records prepended during Incremental may wait until the next run. |

There are **eleven distinct recognized option names** across the two native adapters: eight Torznab names and five HTTP/JSON names, with `published_at_unit` and `local_categories` shared. Remote catalogues accept no option entries. Bounded traversal permits `published_at_unit`; the three ordinary JSON boolean keys are accepted when explicitly false but cannot enable their behavior, and local_categories cannot be configured.

Publication-date normalization accepts textual RFC 3339/RFC 5322 dates by default. With published_at_unit, integer numeric values and integer strings are also accepted, including zero and negatives. Fractional epochs, overflow, and UTC years outside 1–9999 fail instead of being truncated or inferred. Both input formats normalize to UTC RFC 3339 with available subsecond precision; remote catalogue imports instead preserve the publisher's exact values.

Torznab query plans are frozen in the immutable run configuration. Version-1 checkpoints retain the negotiated category scopes and a zero-based `query_index` (omitted means zero), plus category position, offset, total and known-page streak. Query indices outside the configured plan are rejected before network use. Every category/query boundary resets offset, total and streak; Resume continues the saved position. Multi-query page positions and repeated-page scopes distinguish both query and category; page metadata includes numeric `query_index` and `query_count`, not query text.

For pages containing unidentified Torznab items, a durable fallback fingerprint still detects repeats across Resume. It ignores item order and mutable counters and never substitutes for a native identity or known-page proof; the rejected repeat cannot advance the committed cursor.

Finishing a query plan proves only exhaustion of its configured searches, not global coverage. Keyword partitions are best effort: filtered windows can still be capped, duplicated or incomplete. Use Incremental for historical backfill to preserve previously published records; set `schedule.known_pages` to zero when known pages do not establish the end of unseen history. Full replaces the configured corpus and can remove records absent from a partial query union. Do not use Full to claim or replace a global catalogue without a proven coverage contract.

### Quotas, retention, and NFO values are not hidden options

`request_interval`, `request_timeout`, and `rate_limit_reset` are the only source-level pacing/timeout/reset keys. `RateLimit-Remaining`/`X-RateLimit-Remaining`, reset headers, and `Retry-After` can impose a longer wait. A 429 proves exhaustion even without Remaining. Standard reset is relative seconds; X-reset follows the configured epoch/relative policy. Retry-After may be seconds or an HTTP date. Explicit Resume accepts a saved wait without shortening the server deadline.

Automatic quota retries, maximum automatic wait, no-progress limit/action, default manual page budgets, and attempt-duration budgets are **shared database settings**, frozen for each new run. Initially these are 3 retries, unlimited automatic wait, 1000 responses without new native IDs, `warn`, 3 Preview pages, and unlimited non-Preview pages/duration. They are not accepted source JSON keys. See the exact ranges in [Settings](SETTINGS.md#collection-execution-and-defaults).

Independently of the configurable 429 retry budget, complete GET/HEAD HTTP 500, 502, 503 and 504 responses receive at most three retries of the same request. Valid `Retry-After` values take precedence; absent or invalid values use 2s, 4s and 8s delays. All source intervals, server quotas and durable request limits still apply. Every failed attempt leaves a compact observation before retry without advancing the committed checkpoint. Exhaustion fails safely at that checkpoint with an HTTP status and attempt count; Resume preserves any saved cooldown. No source JSON key changes this policy, and unsafe methods, incomplete bodies, transport/parse failures and other statuses are not replayed by it.

New raw response bodies, original item bytes and duplicate non-Preview field snapshots are not retained. Diagnostic records, publication changes, logs and completed run histories expire automatically after seven days. Published records and required recovery/synchronization state remain. No source JSON setting changes this fixed retention policy. See [Operations](OPERATIONS.md) for cleanup and recovery boundaries.

For mapped `metadata.nfoContent`, ordinary text stays a string. Only a string containing U+0000 becomes an object with `encoding` = `"base64"` and a `data` member containing standard padded Base64 of the entire received UTF-8 string, including NULs and line endings. For example, `A\u0000B` becomes `{"encoding":"base64","data":"QQBC"}`. Literal backslash escape text, NFO strings without NUL, and all other metadata are unchanged. Malformed UTF-8 JSON responses are rejected rather than silently replacing bytes. This is canonical metadata preservation, not raw-body retention or a selectable encoding option.

## Remote catalogue specialization

Use the application schema with `adapter` = `"http_json"` and `http.catalog` = `true`:

- URL must be HTTPS, at most 8192 bytes, with no query (including a bare `?`), userinfo, fragment, whitespace, or unsafe host syntax. Redirects and environment HTTP proxies are disabled. Normal certificate/hostname verification is required; private LAN/loopback HTTPS is supported, while prohibited destination addresses are rejected.
- Auth must be bearer with a vault `secret_ref`. `page_size` is 1–1000, default 100.
- GET only, no body, query overrides, secret_headers, root options, or traversal. Non-secret ordinary headers can still be configured.
- The effective fixed envelope is items_path `/items`, mapping.id `/id`, no mapping.fields, cursor pagination in query, cursor_param `cursor`, size_param `limit`, next_path `/next_cursor`, start `0`, and no total_path/current_path. Omitted/empty defaultable strings are filled accordingly. Inactive page_param/offset_param and search fields do not customize the remote protocol.
- Output selectors do not transform shared occurrences. Remote Full and Incremental both stage privately and publish the exact completed mirror/checkpoint atomically, preserving provenance and removals. Metadata is unavailable. The connection wizard starts with automatic synchronization every 60 minutes; a JSON definition with neither `catalog_sync` nor a legacy timing rule remains manual.

Remote catalogues have a dedicated timing block:

```json
{
  "catalog_sync": {
    "enabled": true,
    "interval_minutes": 60
  }
}
```

| Path | Type | Required / default | Meaning |
| --- | --- | --- | --- |
| `catalog_sync.enabled` | boolean | Optional; `false` | Automatically synchronize only when the connection's root `enabled` is also true. Disabling leaves existing runs and the published copy intact. |
| `catalog_sync.interval_minutes` | integer | Required when the block is present | 1–525600 whole minutes. The first automatic execution waits one interval. |

The block is rejected on native sources. When present, it takes precedence over legacy `schedule` values. Otherwise existing interval/Cron definitions convert as described in [Sharing](SHARING.md); saving the remote connection removes the old schedule block. New automatic imports use checkpoint-based Incremental, with an initial Full snapshot when this exact endpoint has no checkpoint. They have no periodic Full, known-page boundary or scheduled page budget. Existing continuations retain their immutable snapshots and budgets. Failed attempts keep the previous published copy and become eligible for a new attempt at the next interval; explicit holds still require manual action.

## Complete application examples

Each block is a complete independent definition, not a fragment to append to another. Example endpoints are deliberately non-production; the schema is valid but the remote API must implement the documented response contract. Create referenced vault entries in **Secrets** before starting a run. Definitions are disabled initially. **Save** and **Check configuration** themselves never contact the source or start collection; an enabled source with an enabled timing expression can later be picked up by the scheduler.

### Torznab with a vault API key and explicit native-ID extraction

```json
{
  "version": 1,
  "id": "example_feed",
  "name": "Example Torznab feed",
  "adapter": "torznab",
  "url": "https://indexer.example/api",
  "enabled": false,
  "auth": {
    "type": "query",
    "name": "apikey",
    "secret_ref": "example_feed_key"
  },
  "request_interval": "1s",
  "request_timeout": "30s",
  "rate_limit_reset": "epoch",
  "page_size": 100,
  "search": {
    "query": "",
    "categories": [
      2000,
      5000
    ]
  },
  "http": {
    "method": "GET",
    "headers": {
      "Accept": "application/rss+xml"
    },
    "query": {
      "minsize": 1048576
    }
  },
  "options": {
    "category_scope": "combined",
    "search_mode": "search",
    "id_source": "guid",
    "id_pattern": "^release:([0-9]+)$",
    "total_mode": "strict"
  },
  "output": {
    "fields": [
      "title",
      "guid",
      "size",
      "info_hash",
      "categories",
      "published_at",
      "attributes.imdbid"
    ]
  },
  "schedule": {
    "enabled": false,
    "every": "10m",
    "mode": "incremental",
    "max_pages": 0,
    "known_pages": 2
  }
}
```

The upstream GUID contract in this example is `release:` followed by a numeric native ID. If that is not the real GUID format, omit the identity override or choose a verified expression; do not substitute an info hash.

### Ordinary JSON, offset pagination, exact local categories

```json
{
  "version": 1,
  "id": "example_json",
  "name": "Example JSON feed",
  "adapter": "http_json",
  "url": "https://indexer.example/api/releases",
  "enabled": false,
  "auth": {
    "type": "bearer",
    "secret_ref": "example_json_token"
  },
  "request_interval": "1s",
  "request_timeout": "30s",
  "rate_limit_reset": "relative",
  "page_size": 100,
  "http": {
    "method": "GET",
    "headers": {
      "Accept": "application/json"
    },
    "secret_headers": {
      "X-Client-Token": "example_client_token"
    },
    "query": {
      "sort": "newest",
      "includeArchived": false
    },
    "items_path": "/data/items"
  },
  "pagination": {
    "type": "offset",
    "in": "query",
    "offset_param": "offset",
    "size_param": "limit",
    "start": 0,
    "total_path": "/data/total",
    "current_path": "/data/offset"
  },
  "mapping": {
    "id": "/id",
    "fields": {
      "title": "/name",
      "size": "/size",
      "info_hash": "/hash",
      "categories": "/categories",
      "published_at": "/created_at",
      "external_ids": "/externalIds",
      "metadata": "/metadata"
    }
  },
  "options": {
    "local_categories": [
      2000,
      5000
    ],
    "published_at_unit": "milliseconds",
    "null_items_as_empty": false
  },
  "output": {
    "fields": [
      "title",
      "size",
      "info_hash",
      "categories",
      "published_at",
      "attributes.imdbid"
    ]
  },
  "schedule": {
    "enabled": false,
    "cron": "0 */6 * * *",
    "timezone": "Europe/Paris",
    "mode": "incremental",
    "max_pages": 200,
    "known_pages": 0
  }
}
```

### POST JSON with an opaque body cursor

```json
{
  "version": 1,
  "id": "example_post",
  "name": "Example POST feed",
  "adapter": "http_json",
  "url": "https://indexer.example/api/search",
  "enabled": false,
  "auth": {
    "type": "basic",
    "username_ref": "example_post_username",
    "password_ref": "example_post_password"
  },
  "request_interval": "2s",
  "page_size": 50,
  "http": {
    "method": "POST",
    "items_path": "/results",
    "body": {
      "filters": {
        "category": "movies"
      },
      "order": "newest"
    }
  },
  "pagination": {
    "type": "cursor",
    "in": "body",
    "cursor_param": "after",
    "size_param": "count",
    "start": 0,
    "next_path": "/nextCursor"
  },
  "mapping": {
    "id": "/releaseId",
    "fields": {
      "title": "/title",
      "size": "/bytes",
      "info_hash": "/infoHash",
      "published_at": "/publishedAt"
    }
  },
  "output": {
    "fields": [
      "title",
      "size",
      "info_hash",
      "published_at"
    ]
  },
  "schedule": {
    "mode": "incremental",
    "known_pages": 0
  }
}
```

Here `nextCursor` must be an opaque non-URL cursor or a terminator. A one-page API would instead use `pagination.type` = `"none"`, remove next_path, and return its complete result in that request.

### Bounded JSON with strict coverage, numeric recovery, and Metadata

```json
{
  "version": 1,
  "id": "example_bounded",
  "name": "Example bounded catalogue",
  "adapter": "http_json",
  "url": "https://indexer.example/api/releases",
  "enabled": false,
  "auth": {
    "type": "bearer",
    "secret_ref": "example_bounded_token"
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
      "category_id": "/category/id",
      "published_at": "/publishedAt"
    }
  },
  "traversal": {
    "window_pages": 100,
    "minimum_total": 1,
    "total_mode": "strict",
    "total_paths": [
      "/data/total",
      "/meta/total"
    ],
    "scopes": [
      {
        "id": "films",
        "query": {
          "category": 1
        },
        "match": {
          "category_id": 1
        }
      },
      {
        "id": "series",
        "query": {
          "category": 2
        },
        "match": {
          "category_id": 2
        }
      }
    ],
    "partitions": [
      {
        "parameter": "year",
        "start": 1900,
        "end_year_offset": 1
      },
      {
        "parameter": "quality",
        "start": 1,
        "end": 5
      }
    ],
    "query_variants": [
      {
        "sort": "oldest"
      },
      {
        "sort": "id_desc"
      }
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
      "discovery_query": {
        "sort": "created_desc"
      },
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
          "category_id": "/category/id",
          "published_at": "/publishedAt",
          "external_ids": "/externalIds",
          "metadata": "/metadata"
        }
      }
    },
    "incremental_order": "published_at",
    "enrich_fields": [
      "external_ids",
      "metadata"
    ]
  },
  "output": {
    "fields": [
      "title",
      "size",
      "info_hash",
      "category_id",
      "published_at",
      "metadata",
      "attributes.imdbid",
      "attributes.tmdbid",
      "attributes.tvdbid"
    ]
  },
  "schedule": {
    "enabled": false,
    "every": "24h",
    "mode": "metadata",
    "max_pages": 200,
    "known_pages": 2
  }
}
```

The example API must implement both `year` and `quality` filters, the option endpoint, the resolver, and detail-by-hash lookup. They are illustrative **API parameters**, not built-in source options. Remove unsupported traversal strategies rather than sending invented filters. The Metadata schedule ignores known_pages; the same definition can use the value for a separately started Incremental. Change total_mode to `at_least` only when its cumulative semantics are intended, and only for a new run.

### Remote catalogue with its fixed protocol defaults

```json
{
  "version": 1,
  "id": "example_remote",
  "name": "Example remote catalogue",
  "adapter": "http_json",
  "url": "https://ingest.example/api/catalogs/11111111-2222-4333-8444-555555555555",
  "enabled": false,
  "auth": {
    "type": "bearer",
    "secret_ref": "example_remote_password"
  },
  "request_interval": "1s",
  "request_timeout": "30s",
  "rate_limit_reset": "epoch",
  "page_size": 100,
  "http": {
    "catalog": true,
    "method": "GET",
    "items_path": "/items"
  },
  "pagination": {
    "type": "cursor",
    "in": "query",
    "cursor_param": "cursor",
    "size_param": "limit",
    "start": 0,
    "next_path": "/next_cursor"
  },
  "mapping": {
    "id": "/id"
  },
  "schedule": {
    "enabled": false,
    "every": "24h",
    "mode": "incremental",
    "max_pages": 0,
    "known_pages": 0
  }
}
```

### UNIT3D profile values are not universal defaults

The **UNIT3D** picker expands a normal HTTP/JSON document using `/api/torrents/filter`, `/data`, page start 1, `perPage`, `/meta/current_page`, `/links/next`, `preserve_query_on_next` = `true`, and oldest-first `created_at` ordering. Its initial interval is `3s`, page size 25, and known_pages zero; it is disabled and has no timing expression. These are template values, not loader defaults or guaranteed tracker quotas. Categories are that site's native IDs in `http.query."categories[]"`, not Torznab IDs. Its explicit mapping/output excludes signed download and magnet links. See the [UNIT3D configuration guide](CONFIGURATION.md#unit3d-source-profile).

## Standalone Torznab SDK JSON

This is the entire separate schema read by [`torznab.LoadProvider`](../torznab/provider.go): exactly one UTF-8 JSON object limited to **64 KiB**, including whitespace. Comments, trailing commas, extra documents, duplicate property names, unknown or incorrectly cased fields, wrong types, invalid version/ID/URL, and unsafe endpoint hints are errors. Fixed integer fields require in-range integer number lexemes, not numeric strings, decimal-point forms, or exponents. Null is rejected for scalar/duration fields and fixed objects (`auth`, `search`, and `output`); only the collection arrays `search.categories` and `output.fields` may be null. The SDK is not the application schema: it has no vault, adapter, traversal, or schedule fields, and does not apply the application's blanket decoded-NUL precheck to every string.

| Path | Type | Required / default | Meaning and constraints |
| --- | --- | --- | --- |
| `version` | integer | Required; no default | Exactly `1`. |
| `id` | string | Required; no default | `^[a-z0-9][a-z0-9_-]*$`: lowercase only; unlike the app, no explicit 128-character ID cap. |
| `name` | string | Optional; empty | Descriptive metadata; SDK does not require or otherwise validate it. |
| `url` | string | Required; no default | Absolute HTTP(S) site/endpoint URL without userinfo or fragment; valid hostname and port. SDK discovers capabilities on the same origin. |
| `api_path` | string | Optional; empty | Root-relative endpoint hint, such as `/api/torznab`; no host, scheme, userinfo, or fragment. Cannot change origin. Its query overrides matching base URL query values; protocol-reserved parameters are cleaned by the client. |
| `auth` | mapping | Optional; no credentials | Environment-name references; multiple mechanisms can be combined. No `auth.type` or vault secret_ref here. |
| `auth.api_key_env` | string | Optional; empty | Environment variable supplying the API key; explicit resolved key overrides one extracted from URL. |
| `auth.cookie_env` | string | Optional; empty | Environment variable containing a valid Cookie request-header value. Malformed names/values, missing `=` segments, partially malformed strings, CR, and LF are rejected before HTTP rather than silently dropping credentials. |
| `auth.username_env` | string | Optional; empty | Environment variable for HTTP Basic username; its resolved value cannot contain `:`. |
| `auth.password_env` | string | Optional; empty | Environment variable for Basic password; requires username_env. Username without password_env is allowed. |
| `request_interval` | Go duration string | Optional/`"0s"`; effective `1s` | Nonnegative; a positive value overrides local pacing. Use a duration string such as `"0s"` or `"2s"`, never a JSON number; explicit null is invalid. JSON marshaling of the SDK Provider also emits a duration string, not nanoseconds. No separate application-style 24h upper bound beyond duration representation. Server cooldowns still win. |
| `page_size` | integer | Optional/zero; capability-negotiated | Nonnegative. Zero chooses advertised default, then advertised max, then 100; advertised max clamps the chosen size. No application 10000 JSON cap. |
| `published_at_unit` | string | Optional/empty; text dates only | `seconds` or `milliseconds` enables explicit integer Unix date interpretation. This is a root SDK field, not an options map. |
| `search` | mapping | Optional; unfiltered | Collection-layer settings, not capability definitions. |
| `search.categories` | sequence of integers | Optional/null/`[]`; unfiltered | Nonnegative category IDs, sent as OR; no automatic hierarchy expansion. |
| `output` | mapping | Optional; complete normalized item | Collection-layer output projection. |
| `output.fields` | sequence of strings | Optional/null; complete item | If present and non-null, must be nonempty. Unknown normalized names reject; duplicates deduplicate rather than fail. |

Environment names must match `^[A-Za-z_][A-Za-z0-9_]*$`. Loading JSON does not resolve them or make requests. `Provider.Config(nil)` uses `os.LookupEnv`; applications may supply a custom lookup. Referenced missing or empty values are errors, never anonymous fallback. Search categories and output projection are not silently applied by `Provider.Config`/`Open`; the collection layer must use them, as the example command does.

SDK output names are exactly `guid`, `title`, `link`, `comments`, `published_at`, `size`, `info_hash`, `magnet_url`, `categories`, `seeders`, `peers`, `attributes`, and `attributes.NAME`. The attribute suffix is a literal nonempty name with no surrounding whitespace; it is not restricted by the application's safe-identifier regex and is not a nested path. `attributes` wins over individual members. Missing optional values are omitted and explicit numeric zero is preserved.

For compatibility the SDK's programmatic URL parsing can extract an inline `apikey`, including a hinted endpoint query; the application rejects credential URLs. **Use environment references anyway**: accepted legacy syntax is not a safe reason to put credentials in JSON, shell history, or example URLs. SDK `Config.HTTPClient`, `AdvisoryTotals`, and `DisableRetries` are Go API controls, not JSON fields. The default SDK HTTP client timeout is 30 seconds; changing it requires Go configuration, not an invented `request_timeout` JSON key.

### Complete standalone SDK definition

```json
{
  "version": 1,
  "id": "example_sdk",
  "name": "Example standalone Torznab feed",
  "url": "https://indexer.example",
  "api_path": "/api/torznab",
  "auth": {
    "api_key_env": "EXAMPLE_TORZNAB_API_KEY",
    "cookie_env": "EXAMPLE_TORZNAB_COOKIE",
    "username_env": "EXAMPLE_TORZNAB_USERNAME",
    "password_env": "EXAMPLE_TORZNAB_PASSWORD"
  },
  "request_interval": "2s",
  "page_size": 100,
  "published_at_unit": "seconds",
  "search": {
    "categories": [
      2000,
      5000
    ]
  },
  "output": {
    "fields": [
      "guid",
      "title",
      "size",
      "info_hash",
      "published_at",
      "categories",
      "attributes.imdbid",
      "attributes.tmdbid",
      "attributes.tvdbid"
    ]
  }
}
```

This example intentionally shows all SDK fields. Set all four referenced environment variables securely, or remove auth references for mechanisms the endpoint does not require. No actual secret belongs in this document. The crawl command accepts `-provider`, not an application source file; its command-line `-query`, `-offset`, `-pages`, and `-published-at-unit` are CLI controls, not additional JSON fields.

## Deployment Compose controls

[`compose.yaml`](../compose.yaml) is a Docker Compose document, not a project-specific open-ended application schema. The repository uses the complete set of Compose paths below; Docker Compose itself supports additional standard fields, which this source reference does not redefine.

| Current Compose path | Type / repository value | Effect |
| --- | --- | --- |
| `name` | string; `aiosources` | Compose project name; `COMPOSE_PROJECT_NAME` can override it. |
| `services` | mapping | Service definitions. |
| `services.postgres` | mapping | PostgreSQL service. |
| `services.postgres.image` | string; digest-pinned PostgreSQL image | Exact pinned image is in compose.yaml; do not replace it with an unreviewed latest tag. |
| `services.postgres.environment` | mapping | PostgreSQL initialization variables. |
| `services.postgres.environment.POSTGRES_DB` | string; `${POSTGRES_DB:-ingest}` | Initial database name. |
| `services.postgres.environment.POSTGRES_USER` | string; `${POSTGRES_USER:-ingest}` | Initial database role. |
| `services.postgres.environment.POSTGRES_PASSWORD` | string; required `${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD in .env}` substitution | Database password; missing/empty causes Compose to fail before startup. |
| `services.postgres.ports` | sequence; `127.0.0.1:${POSTGRES_PORT:-5432}:5432` | Loopback-only host database port. |
| `services.postgres.volumes` | sequence; `postgres_data:/var/lib/postgresql` | Persistent PostgreSQL data. |
| `services.postgres.healthcheck` | mapping | Database readiness policy. |
| `services.postgres.healthcheck.test` | command sequence; `CMD-SHELL`, pg_isready | Runs `pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}`; doubled dollar defers expansion into the container. |
| `services.postgres.healthcheck.interval` | duration; `5s` | Probe interval. |
| `services.postgres.healthcheck.timeout` | duration; `5s` | Per-probe timeout. |
| `services.postgres.healthcheck.retries` | integer; `10` | Consecutive failure threshold. |
| `services.postgres.stop_grace_period` | duration; `1m` | Graceful database shutdown window. |
| `services.postgres.restart` | string; `unless-stopped` | Container restart policy, not run Resume policy. |
| `services.aiosources` | mapping | Application service. |
| `services.aiosources.image` | string; `${AIOSOURCES_IMAGE:-ghcr.io/theo-bnts/aiosources:stable}` | Stable GHCR application image; no default local build. |
| `services.aiosources.environment` | mapping | Values actually passed into the app container. |
| `services.aiosources.environment.DATABASE_URL` | string assembled from PostgreSQL substitutions | Connects to internal service host `postgres:5432`, not the host-side published port. |
| `services.aiosources.environment.ADMIN_PASSWORD` | string; `${ADMIN_PASSWORD:-}` | Optional initial administrator password, 12–72 bytes when supplied; unset/empty lets the application generate it. |
| `services.aiosources.environment.PUBLIC_URL` | string; `${PUBLIC_URL:-}` | Optional exact public origin; HTTPS required for sharing. Does not terminate TLS. |
| `services.aiosources.ports` | sequence; `${BIND_IP:-127.0.0.1}:${PORT:-7000}:7000` | Host panel bind address and port; loopback-only by default. |
| `services.aiosources.volumes` | sequence; `state:/data` | Private application state, vault key, and source definitions. |
| `services.aiosources.tmpfs` | sequence; `/var/lib/postgresql` | Ephemeral PostgreSQL client-tool working path. |
| `services.aiosources.depends_on` | mapping | Startup dependencies. |
| `services.aiosources.depends_on.postgres` | mapping | Database readiness dependency. |
| `services.aiosources.depends_on.postgres.condition` | string; `service_healthy` | Waits for successful database healthcheck. |
| `services.aiosources.stop_grace_period` | duration; `45s` | Graceful application shutdown window. |
| `services.aiosources.restart` | string; `unless-stopped` | Container restart policy. |
| `volumes` | mapping | Named persistent volumes. |
| `volumes.postgres_data` | mapping | Database volume declaration. |
| `volumes.postgres_data.name` | string; `${POSTGRES_VOLUME:-aiosources_v2_postgres_data}` | Actual global database-volume name; retain the existing value on upgrade. |
| `volumes.state` | mapping | App-state volume declaration. |
| `volumes.state.name` | string; `${STATE_VOLUME:-aiosources_v2_state}` | Actual global state-volume name; retain the existing value on upgrade. |

Compose substitution controls are documented in [Settings](SETTINGS.md#docker-compose), including image selection, bind address and both volume names. Host ports must be valid available TCP ports. Use URL-safe database/user names and a URL-safe random password because they are interpolated into `DATABASE_URL`. PostgreSQL initialization variables do not rotate credentials in an already-initialized database volume.

Adding a native application variable such as `PROVIDERS_DIR` to `.env` alone does not pass it to the container: it also needs an explicit service environment entry or another supported deployment mechanism. Keep the backend ports private, place an HTTPS reverse proxy in front for remote access, and back up both PostgreSQL and the state/vault key. See [Settings](SETTINGS.md#native-environment-and-cli) for the environment and CLI reference.

Both volume names are explicit and global: changing the Compose project name with `-p` alone does **not** isolate the database or state. A separate disposable deployment must override both volume names and host ports. Never run `down -v` against a live deployment to perform a configuration check.

The explicit [`compose.build.yaml`](../compose.build.yaml) development override changes `services.aiosources.image` to `aiosources:local`, sets `pull_policy: build`, and adds `build.context: .` with `build.args.VERSION: 2.0.0`. It never tags or pulls the published application image. Base-image pulls are still required. See [Local Docker build](DEVELOPMENT.md#local-docker-build).

## Accepted compatibility forms and rejected keys

| Form | Application source JSON | Standalone SDK JSON |
| --- | --- | --- |
| `auth.type` = `"api_key"` with name/in/secret_ref | Accepted explicit query/header auth form. | Rejected unknown auth fields. |
| `auth.api_key_env`, cookie_env, username_env, password_env | Rejected; use vault references. | Accepted environment references. |
| Root `api_path` | Rejected; put the endpoint in url. | Accepted safe root-relative hint. |
| Root `published_at_unit` | Rejected; use options.published_at_unit. | Accepted. |
| Root `adapter`, `enabled`, `http`, `pagination`, `mapping`, `traversal`, `schedule`, `options`, `request_timeout`, `rate_limit_reset` | Accepted only as defined above. | Rejected unknown fields. |
| Root `api_key`, `password`, `cookie`, literal auth values | Rejected unknown/unsafe fields; credentials belong in the vault. | Rejected unknown fields; use auth environment references. |
| Inline URL apikey | Rejected credential query. | Legacy parsing accepts it; unsafe for stored configuration, use api_key_env instead. |
| `options.api_path`, `options.advisory_totals`, `options.disable_retries` | Rejected unknown option names. Use options.total_mode for application Torznab advisory totals. | No options mapping; Go-only Config fields are not JSON. |
| `pagination.type` = `"search"`, `adapter` = `"unit3d"`, `adapter` = `"catalog"` | Rejected; select supported pagination and http_json specialization. | No adapter/pagination schema. |
| `output.fields` = `[]` | Accepted: all fields. | Rejected: empty explicit projection. |
| `output.fields` = `null` or omission | Accepted: all fields. | Accepted: complete normalized item. |
| `schedule.mode` = `"preview"` | Rejected; Preview is manual. | No schedule schema. |
| `max_pages`, `known_pages`, `max_duration_seconds` at root | Rejected. Schedule page/known controls are nested; manual start overrides are API/CLI controls. | Rejected; crawl page count is a CLI flag. |
| `quota`, `retries`, `max_quota_retries`, `max_quota_wait_seconds`, `no_progress`, `retention`, `raw`, `archive`, `save_raw`, `store_raw` as extra source fields/options | Rejected unknown fields/options. Shared policy is configured in **Sources → Settings → Advanced settings**; no JSON raw-retention switch exists. | Rejected. |
| `${ENV_NAME}`, `{{ template }}`, scripts, JSONPath | No interpolation or execution. A string can be a literal only where valid; it is not a secret reference mechanism. | No template execution; auth values must be actual environment **names**. |
| Unknown fixed keys or unknown adapter option names | Rejected, including keys from old/private examples not in this reference. | Rejected by strict case-sensitive schema decoding. |

A legacy **run checkpoint** is not an active source-definition file. Compatibility code for an existing immutable run does not add a second source format, silently convert an old definition, change its native identity, or reset its checkpoint. Active legacy definitions require the explicit offline migration linked above.

## Implementation sources

This reference is derived from the following checked-in implementation contracts, not private provider files:

- [Application JSON structs](../internal/model/types.go): Provider, Auth, Search, HTTPConfig, Pagination, Mapping, Output, Schedule.
- [Traversal JSON structs](../internal/model/traversal.go): JSONTraversal, JSONScope, JSONPartition, JSONOptions, JSONIDRecovery.
- [Registry/default/schema validation](../internal/providers/validation.go), [file loading](../internal/providers/registry.go), [JSON Schema generation](../internal/providers/schema.go), and [offline migration](../internal/providers/migrate.go).
- [Adapter defaults/common validation](../internal/connectors/factory.go), [Torznab options](../internal/connectors/torznab.go), [ordinary JSON options/pagination](../internal/connectors/http_json.go), and [normalization](../internal/connectors/normalize.go).
- [Bounded traversal validation/recovery](../internal/connectors/http_json_window.go), [ordered Incremental](../internal/connectors/http_json_incremental.go), and [Metadata](../internal/connectors/http_json_metadata.go).
- [Remote catalogue protocol](../internal/connectors/remote_catalog.go), [transport/auth/quota policy](../internal/connectors/client.go), and [UNIT3D template](../internal/connectors/unit3d.go).
- [Schedule parsing](../internal/scheduling/schedule.go), [run creation](../internal/jobs/manager.go), [scheduled run creation](../internal/store/schedules.go), and [immutable execution policy](../internal/model/collection.go).
- [Worker page budgets](../internal/jobs/worker.go) and [page retention/counting](../internal/store/retention.go).
- The pinned cron v3.0.1 [field parser](https://github.com/robfig/cron/blob/v3.0.1/parser.go) and [field ranges/day matching](https://github.com/robfig/cron/blob/v3.0.1/spec.go), as used by the schedule parser.
- [Standalone SDK provider loader](../torznab/provider.go), [SDK Config and search modes](../torznab/types.go), [SDK client defaults](../torznab/client.go), [SDK page-size negotiation](../torznab/pagination.go), [SDK projection](../torznab/projection.go), and [crawl configuration usage](../torznab/examples/crawl/main.go).
- [Compose deployment](../compose.yaml), [environment example](../.env.example), and the linked narrative guides for operations and deployment boundaries.
