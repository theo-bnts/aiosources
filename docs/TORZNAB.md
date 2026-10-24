# Torznab

AIOSources can serve its published local catalog to Prowlarr and other Torznab clients. Searches are offline: they do not contact your sources, TMDB or debrid accounts. Full imports become visible only after publication, just as in Torrents.

## Connect a client

1. Set `PUBLIC_URL` to the exact origin used by your clients. Private Torznab and Stremio connections support HTTP on an internal network; use HTTPS when traffic crosses an untrusted network. Catalog sharing and public visitor connections retain their HTTPS requirements.
2. Open **Connections → Torznab** and create an access key. New accesses include all current and future sources. Choose media types, sources, cache services and **Cached results only** in **Filters**; optional expiration and per-access limits belong to **Advanced**. Save the generated key; only its hash is retained and the key cannot be retrieved later.
3. Enable access. In Prowlarr, add **Generic Torznab**. Split the displayed Torznab URL into its origin (**URL**) and path (**API path**), and enter the generated **API key**.
4. To narrow a single client further, optional URL parameters can be placed in Prowlarr's **Additional parameters** field. Do not append another `cat` there: Prowlarr already supplies its selected categories.

Private connections save their filters with each access key, just as Stremio installations do. Changes apply to the same key without reconfiguring the client. Each key can still be used with multiple URLs that narrow its settings: query parameters cannot expand source or media permissions, select an excluded cache service, or disable a saved cached-only policy. Cache services select existing shared observations; Torznab never calls the debrid providers. Keys can be disabled, rotated, expired or revoked independently of administrator sessions and Stremio links.

## Endpoint and filters

The endpoint is `/torznab/ACCESS_ID/api`. Every call, including capabilities, requires `apikey`. Query parameter names are case-insensitive; duplicates are rejected.

| Parameter | Purpose |
| --- | --- |
| `t=caps` | Advertised search modes, page limits and authorized categories. |
| `t=search` | Browse or search release titles, source identifiers and hashes using the same fragment and punctuation handling as Torrents. |
| `t=movie` | Identified movies; accepts `q`, `imdbid`, `tmdbid`, `year`. |
| `t=tvsearch` | Identified series; accepts `q`, `imdbid`, `tmdbid`, `tvdbid`, `season`, `ep`. |
| `q` | Optional search text. A supplied verified external ID takes precedence over text, so translated or alternative release titles are not lost. |
| `cat` | Standard comma-separated category IDs: `2000` Movies, `5000` TV, `8000` Other. Unknown categories return no additional results. |
| `sources` | AIOSources extension: one or more source IDs separated by commas. Use source IDs; the access editor displays their names. |
| `media` | AIOSources extension: `movie`, `tv`, `other`, or a comma-separated combination. Intersects the standard `cat` filter and the key's permissions. |
| `imdbid`, `tmdbid`, `tvdbid` | Current verified identifiers, never obsolete or conflicting source assertions. IMDb accepts a numeric ID or an ID starting with `tt`. |
| `season`, `ep` | Nonnegative season/episode numbers. Use `t=tvsearch` with a title or external ID; `ep` also requires `season`. Uses the playback numbering rules, including season packs and current manual corrections. Unnumbered releases are excluded because Torznab cannot verify their downloaded files. |
| `limit`, `offset` | Page size and offset. Default 100, maximum 200. Zero limit and an offset beyond the total return an empty page with its total. |
| `minage`, `maxage` | Minimum/maximum age in whole days, applied to known publication dates. |
| `minsize`, `maxsize` | Minimum/maximum size in whole MiB. |
| `services`, `cached_only` | AIOSources extensions: select cache services and require fresh positive shared cache availability. Supported IDs: `alldebrid`, `debridlink`, `deepbrid`, `premiumize`, `realdebrid`, `torbox`. No provider calls are made; account-only evidence from Real-Debrid and Deepbrid is not included. See [debrid playback](DEBRID.md). |
| `extended`, `attrs`, `o=xml` | Compatibility parameters. Responses include all supported safe attributes. JSON output and unsupported search families are not advertised. |

For example, a client can use `&sources=alpha,beta&media=movie` while another uses `&sources=beta&media=tv` with the same endpoint and key. `sources` and `media` are AIOSources extensions; `cat`, capabilities, RSS, errors and pagination follow Torznab conventions. Prefer your client's parameter fields instead of manually combining URLs with duplicate parameter names.

Movies and TV categories use current verified identification. Releases that have not been identified remain available under Other. Native source categories are not reinterpreted as standard categories because their numbering can mean different things. Only records with a usable title and v1 info hash are returned. Separate source occurrences keep separate stable GUIDs, even when hashes match. Results are newest first, with consistent rows and totals within each request; ordinary Torznab offsets are not a snapshot across subsequent requests when the catalog changes.

## Data and access

Results contain a release title, size, publication date (first import time when missing), hash, standard category, source display name and available verified identifiers, language and numbering. Magnets are rebuilt from the hash and title. AIOSources never forwards native download URLs, tracker parameters, administrator announce URLs, source credentials, debrid credentials or arbitrary JSON. Private downloads require the recipient's own authorized source configuration; this API does not provide authenticated torrent files or debrid playback links.

Torznab access is separate from visitor Stremio access. Explicitly enabling a Torznab key authorizes its selected published metadata, including uncached releases. Disabling public visitor mode does not revoke a separately enabled Torznab key. Revoke that key in Torznab settings when needed.

Per-key request and concurrency limits are disabled by default. When configured in **Advanced**, they apply across replicas. Empty limit fields (API value `0`) mean no per-key limit; instance-wide resource protections remain in effect. Existing configured policies are preserved on upgrade. Slow searches have a 20-second deadline. HTTP 429 includes `Retry-After`; other errors use Torznab XML codes. API keys travel as standard Torznab query parameters: protect them and redact query strings in any external proxy/access logs. The application records only safe request counts and timings in Logs (debug level), never the key, visitor IP, query, hashes, magnets or credentials. Its ordinary seven-day log retention applies. This feature reads current publications directly and does not enable catalog-sharing snapshots or copy the catalog.

Protocol references: [Torznab specification](https://torznab.github.io/spec-1.3-draft/torznab/Specification-v1.3.html), [Prowlarr request generation](https://github.com/Prowlarr/Prowlarr/blob/develop/src/NzbDrone.Core/Indexers/Definitions/Newznab/NewznabRequestGenerator.cs).

## Optional public connections

Administrators can separately enable public Torznab in **Administration → Public access**. Its visitor setup uses encrypted, stateless configurations instead of the administrator's named grants. Source disclosure is shared with public Stremio and remains opt-in. Public catalog eligibility, optional personal announces, cache behavior and revocation are described in [Public instances](PUBLIC_INSTANCES.md#public-navigation-and-torznab).

If Cloudflare Access or another sign-in gateway protects the instance, external clients must also be allowed through that gateway. A browser's login cookie is not sent by Prowlarr. Public Torznab uses `/public/torznab/api` and its encrypted configuration key; private Torznab uses `/torznab/ACCESS_ID/api` and its separate access key. Keep administration routes protected when configuring gateway access.
