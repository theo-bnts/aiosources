# Source compatibility

## Choose a connection

| What your source provides | What to use |
| --- | --- |
| A Torznab API | Add a **Torznab** source. |
| A JSON API listing releases | Add an **HTTP/JSON** source and map its fields. |
| A catalog shared by another AIOSources instance | Use **Sources → Remote catalogs**. |
| HTML pages or an ordinary RSS feed | Configure a compatible Torznab bridge, such as your own Prowlarr instance. Direct HTML/RSS import is not available. |
| Usenet / NZB releases | Not currently supported. |

You supply your own source configuration and credentials. Start with **Preview** and check the returned records before enabling automatic imports. Some APIs expose only recent releases: importing them cannot recover history they do not provide.

## What an import can collect

AIOSources imports a local catalog before searching, identifying or returning releases. A source must expose stable record identities and a traversal that can finish. A successful keyword search alone does not establish that the whole catalog is accessible.

## HTML sources through a Torznab bridge

The existing **Torznab** connector can consume a torrent indexer's Torznab endpoint from a separately configured Prowlarr instance. Prowlarr performs its own HTML parsing and login; AIOSources imports the returned records using its usual scheduling, checkpoints, history and publication rules. This does not add a native HTML or Cardigann engine to AIOSources.

1. Configure and test your source in Prowlarr. Copy that individual source's Torznab endpoint, without its API key, into a new **Torznab** source in AIOSources.
2. Store the bridge API key in **Security → Secrets**. Select **Query parameter**, parameter `apikey`, and that secret in the source editor. Never paste keys into a source URL or exported JSON.
3. Select the categories you need and use **Preview** to inspect stable IDs, titles, sizes and info hashes. A download URL alone cannot replace an info hash for AIOSources' debrid cache checks. Do not publish a bridge's authenticated download URLs through catalog sharing.
4. Keep strict total checking unless the endpoint's behavior requires another explicitly understood strategy. Check that successive offsets return new records, that a finite end is reachable, and that the reported total represents the full intended catalog. Test each category independently when necessary.
5. Schedule imports only after verifying these properties. A source limited to recent results remains limited through a bridge. A capped advertised total can hide older records; AIOSources cannot infer missing records that the source never exposes. Advisory totals are not evidence of complete coverage.

Use a separate AIOSources source for each bridge endpoint so names, private-source rules, credentials, schedules and import checkpoints remain independent. HTML-only sources without usable info hashes or complete enumeration cannot currently provide the same functionality as a native catalog API.

## Why CDN definitions are not an automatic source catalog

Prowlarr distributes versioned Cardigann definitions. These are parsing, authentication and search instructions, not source records or proof of complete catalog access. Their versions depend on corresponding engine behavior. See the [definition format and version notes](https://github.com/Prowlarr/Indexers/blob/master/README.md) and [updater implementation](https://github.com/Prowlarr/Prowlarr/blob/develop/src/NzbDrone.Core/IndexerVersions/IndexerDefinitionUpdateService.cs).

Downloading these definitions reliably is possible; converting every definition into an AIOSources source reliably is a different task. Supporting it natively would require the login/session flow, selectors, transforms, request templates, download steps and versioned semantics of that engine. Unsupported instructions must be rejected, never silently ignored. No third-party definition or source directory is bundled or downloaded automatically by AIOSources.

A future import assistant should use an explicitly selected, configured bridge endpoint and its locally verified capabilities. It should create a disabled draft, keep credentials in the vault, require a preview, and distinguish a recent-results feed from verified complete enumeration. It must not label an endpoint “full catalog” solely because its search capabilities or CDN definition exist. Updating a definition must not silently change an existing run snapshot.

## Usenet assessment

Usenet is not implemented. It needs a release transport distinct from BitTorrent; an NZB must never be represented as an info hash. Adding a Newznab parser alone would leave availability, playback and sharing inconsistent.

The current application uses BitTorrent hashes for debrid checks, playback magnets and shared cache identities. A complete Usenet implementation would need transport-specific identities and download descriptors, per-provider capabilities, NZB retrieval with protected credentials, and matching behavior across Search, Stremio, public access, catalog sharing, storage retention and histories. TorBox has separate Usenet endpoints in its [official API](https://api-docs.torbox.app/); the current adapter deliberately calls its torrent endpoints. A configured service without Usenet support must be omitted for NZB releases rather than queried as if they were torrents.

This is a cross-application extension, not a safe small connector change. Keeping it out until those contracts are designed avoids importing records that appear playable but cannot be cached or downloaded correctly. The local film/series metadata and identification stages can be reused when this extension is undertaken.
