# Debrid playback

Add your service key in **Debrid → Settings**, choose its encrypted secret reference, then save. You can configure several services and select different ones for each Stremio or Torznab connection. The service lists use alphabetical order throughout the interface.

| Service | Availability shown before playback | Important behavior |
| --- | --- | --- |
| AllDebrid | Verified cache observations, with recoverable checks through the administrator's configured account | Checks may create temporary entries. Only entries confirmed as created by AIOSources are eligible for automatic cleanup. |
| Debrid-Link | Files verified ready after submitting a bare hash | The official API accepts hashes only when cached. A check may add a ready account item. An unsuccessful check remains unknown. |
| Deepbrid | Ready files from an earlier preparation | No documented global hash-cache check exists. Preparing a new download requires permission to download that source. |
| Premiumize | Its global cache check and cached file list | Cached playback does not create a cloud transfer. An explicit **Add to debrid** action can create an uncached transfer. |
| Real-Debrid | Ready files already in your account | The current documented API does not provide global instant availability. Missing account entries remain unknown. |
| TorBox | Its global cache check and file list | Cached-only preparation uses the provider's cached-only creation option. |

## Search, watch and download

**Search** starts with **Cached results only** enabled. Turn it off to see versions that need preparation or whose cache cannot be checked. **Unknown** means the service has not supplied sufficient evidence; it does not mean that the torrent is absent from its cache.

Choose **Download** for a ready file, or **Add to debrid** to authorize preparation. Follow a pending operation in **Debrid → History**. A readiness check reuses the same transfer; it does not silently recreate one that you deleted. Account changes, modified source records and expired preparation links require a new explicit action.

The selected video must match the requested episode. Samples, ambiguous episodes and archives that cannot be resolved into named video files are not substituted for a playable file. Deepbrid's multi-file playback depends on the file metadata its link generator supplies; a ZIP or an unnamed file is rejected rather than guessed.

Private-source uncached downloads require the appropriate announce URL. A source marked private without an announce stays cached-only. Visitors can supply their own announce only when the administrator permits it; the administrator's announces are never used for their downloads.

Stremio playback uses the same connection links and file metadata for all six services. [AIOStreams currently recognizes](https://github.com/Viren070/AIOStreams/blob/main/packages/core/src/utils/constants.ts) five of them as named debrid services; Deepbrid playback remains an ordinary HTTP stream, without AIOStreams-specific Deepbrid service filters. AIOSources does not mislabel it as another service.

Audio languages use full names and flags so short codes are not confused with service names. Source names remain in the description and structured indexer hint. AIOSources omits an unknown cache flag; AIOStreams currently uses a binary cache flag and can display that unknown state as uncached.

## Shared availability and privacy

Only verified, reusable provider-wide observations enter the shared cache. Account inventories, object IDs, account file links and credentials are not promoted into that cache. Torznab's cache filters read shared observations; they do not query a recipient's personal account. Real-Debrid and Deepbrid therefore cannot provide a complete global cache list through those filters.

Administrator transfer references can be retained for seven days to avoid duplicate preparation and support readiness checks after a restart. Visitor references remain in bounded, short-lived memory or encrypted continuation links, without visitor keys or announces being saved to PostgreSQL. Histories retain the service, safe steps and fixed error categories, never upstream response bodies or credential-bearing URLs.

Authentication failures, expired subscriptions, rate limits, full accounts, oversized torrents, unavailable services and missing items use the application's common error handling. Provider failures do not manufacture negative cache results or authorize another download.

## Verification and provider documentation

Protocol and integration tests cover success, pending and deleted transfers, credential errors, account quotas, malformed responses, episode selection, private-source restrictions and credential isolation. The new adapters are verified against documented response contracts with simulated provider servers; paid-account delivery cannot be confirmed without real accounts.

Official references: [AllDebrid](https://docs.alldebrid.com/), [Debrid-Link](https://debrid-link.com/api_doc/v2/seedbox-add), [Deepbrid](https://www.deepbrid.com/api-docs/), [Premiumize](https://www.premiumize.me/api), [Real-Debrid](https://api.real-debrid.com/), [TorBox](https://api-docs.torbox.app/).
