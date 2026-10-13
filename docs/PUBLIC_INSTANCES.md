# Public instances

Public access is optional and disabled by default. An administrator can enable it in **Stremio → Settings → Public access** after setting `PUBLIC_URL` to the instance's external HTTPS origin. Literal loopback HTTP addresses are accepted for local testing.

When enabled, the homepage lets visitors enter an AllDebrid key, a TorBox key, or both, and create an installation link for Stremio or AIOStreams. At least one service is required. **Admin login** opens the ordinary administration login. Visitors do not receive access to Sources, Settings, Secrets, Logs or histories.

When disabled, the homepage keeps its ordinary login. Disabling public access revokes existing public installation links; enabling it again does not restore those links. Private installations and administrator sessions retain their behavior.

## Credentials and installation links

Visitor keys are validated with the provider and processed only in memory during the request. The application does not save them or visitor announce URLs in its database, encrypted secret store, request history, logs or browser storage. Neither enters a background job that would need it after a restart.

Provider demonstration keys and static demo responses cannot validate an account or supply cache observations. Previously issued links containing a documented demonstration key are rejected without revoking legitimate visitor installations.

The manifest URL contains an authenticated encrypted configuration, bound to this instance and its current public-access generation. It uses a separate encryption purpose from administration, two-step verification and source references. Keep that URL private: its ciphertext is a bearer credential. The existing `/data` volume contains the encryption key; preserve it across updates.

A public installation cannot authenticate an administrator endpoint or a private installation. Keys, account inventory, provider file IDs and download links are not shared between accounts. Public playback always uses the visitor's selected service account. There is no fallback to an administrator key or announce URL.

Visitors still need to trust the host. Encryption protects the URL and avoids saving keys; the server must decrypt keys to call the providers. A host that changes the application can read them. Upstream providers receive their own keys in the requests required by their APIs. An external proxy or browser may retain URLs independently of this application; protect encrypted installation links accordingly.

## Shared availability

The shared cache is indexed independently by **service and info hash**. It contains only cache state, check time, and file names and sizes. Account IDs, API keys, provider file IDs, account fingerprints, download links and announce URLs are excluded.

A visitor's fresh TorBox check can be reused by the administrator or another visitor; administrator observations can be reused in the other direction. A request with only AllDebrid does not mark TorBox as checked. Each service has independent freshness. Positive observations use the configured cache validity; negatives expire after at most 15 minutes. Failed or malformed responses stay unknown. Older results cannot overwrite newer observations, and a cache reset rejects writers from the previous cache generation.

**TorBox** offers a read-only cache check. Missing observations are checked with the visitor's key during the stream request, without adding torrents. Cached-only private-source playback uses its documented cached-only creation option. The selected file is resolved again within the visitor's account.

**AllDebrid** availability probing can create account entries. Missing shared observations use the administrator's existing recoverable probes and ownership-aware cleanup, with one public-triggered probe active at a time. An administrator AllDebrid account is therefore needed to fill a cold AllDebrid cache for private-source results. A confirmed new upload that is immediately ready, supplies usable files and successfully resolves visitor playback also contributes a positive shared observation. An existing account download alone is not promoted into global availability.

The services are checked independently, with a bounded response budget. A slow provider does not prevent the other provider's check. Unknown availability is not presented as cached.

An existing AllDebrid download still waiting in one account does not erase a verified positive shared observation. Playback readiness within that account is still checked separately.

## Private sources and playback

A source with unknown privacy is treated as private. By default, public responses include a private-source result only when its shared cache observation is fresh and a usable video matches the request. Season packs must contain the exact requested episode. A missing file list cannot certify that episode.

Results use the same Stremio formatting as private installations, including compatible file and service hints. Private-source responses contain no announce URL, torrent download URL, raw magnet fallback or exposed native source reference. Their playback URL contains an encrypted source locator; the source record and media match are checked again before playback.

If a TorBox private-source item is no longer cached, cached-only creation prevents starting its download. AllDebrid does not offer the same atomic cached-only operation: if a new upload is no longer ready, the application immediately attempts to delete only the new entry it has confirmed it created, invalidates the old observation and reports that playback is pending. It never deletes an entry already present in the visitor's inventory. A process crash or failed deletion during that small window can leave the new entry in the visitor's account; no stored visitor key is available to recover it later. Public operation does not make that provider-side race disappear.

Sources explicitly marked public may provide uncached playback using a plain hash-based magnet. Administrator source credentials are still excluded.

### Optional visitor announce URLs

**Stremio → Settings → Public access → Advanced → Allow visitor announce URLs** is a separate permission, disabled by default, including after an upgrade. Enabling it publishes the IDs and display names of sources with valid definitions explicitly marked private. It does not publish their API addresses, definitions, secret references or administrator announce URLs. Unknown source privacy or an invalid definition cannot authorize this exception. Disabling imports alone still allows playback from an existing, valid source catalog.

The public homepage then has a folded **Advanced** section. Visitors may enter their own complete HTTP, HTTPS or UDP announce URL for each listed source, including any personal passkey wherever it belongs in that URL. At least one debrid key is still required. Empty fields preserve cached-only access. An announce for one source does not unlock other private sources.

The visitor's URLs are included only in the encrypted installation configuration. They are cleared from the form after submission and are never returned separately in Stremio results or retained in shared availability, histories or Logs. A cached playback uses a plain hash and does not send an announce. When an uncached private item is played, only the visitor's own supplied announce is sent to their selected debrid account. There is no fallback to the administrator's announce or service credentials.

The permission and current source privacy are checked again for stream and playback requests. Turning the option off removes the public source list and restores cached-only private playback for existing links. It does not revoke cached playback. Removing a source definition also withdraws its uncached exception. If definitions become unavailable, the optional list is hidden and cached-only setup and playback remain usable; supplied announces cannot authorize a download until their source can be verified again. Disabling public access still revokes the complete installation as usual. Already started downloads remain in the visitor's provider account; the instance cannot undo a provider operation already in progress.

Uncached downloads may remain pending until the provider finishes them. They are intentional visitor downloads and are not treated as temporary cache probes. They contribute to the shared cache only through the existing verified provider-wide availability rules. A season pack with a known file list that lacks the requested episode is excluded, even when a visitor supplied an announce.

## History and Logs

Public activity uses the existing pages, visible only to administrators:

- **Stremio → History:** stream, manifest and playback requests, including requests served entirely from cache. Filters distinguish public and private installations and request type. Details include normalized media identifiers, duration, outcome, selection counts and returned display fields. Playback details include bounded provider steps, duration, HTTP status and fixed error categories; the playback link is never saved.
- **Debrid → History:** actual availability and cleanup work. Operation details include safe provider-call diagnostics. Reusing availability does not fabricate an external call.
- **Logs:** public-access and visitor-announce permission changes, and meaningful provider failures through the existing sanitized logging. Access changes and their logs commit together; a failed or conflicting save does not change access or create a misleading log. Provider error bodies, request URLs, submitted keys and announce URLs are excluded.

There is no visitor database or stable visitor identifier. History does not save IP addresses, account email, username or visitor account fingerprints. Media identifiers and filenames remain activity data: the homepage explains this retention before visitors submit keys.

History follows the existing seven-day cleanup. Stremio history is also capped at 10,000 requests, with at most 32 provider diagnostics per playback and 1,000 provider diagnostics per debrid operation. Summary pages do not load detailed provider-call bodies.

## Admission and verification

Public requests have bounded sizes, timeouts and separate concurrency limits. Setup bodies and encrypted configurations are each limited to 8 KiB; encryption overhead means the complete configuration must still fit inside that limit. The public configuration page shares the concurrency limit without consuming the setup or addon request quota. Per-client budgets use the direct connection address, without trusting arbitrary forwarded headers; clients behind a single proxy share that address's budget. Cache probing is bounded separately to preserve administrator capacity. The public form cannot supply provider endpoints, source URLs or arbitrary hashes to warm.

Automated backend tests cover installation encryption, revocation, administration isolation, independent cache expiry, reset fencing, error handling, account isolation, cache disappearance and history privacy. Browser tests cover enabled and disabled modes, credential clearing, mobile layout and history navigation. Provider HTTP fixtures exercise actual adapter requests and response parsing without contacting live accounts. This is not live validation with two independently subscribed provider accounts; provider account or network restrictions can still affect a particular deployment.

Provider references: [AllDebrid API](https://docs.alldebrid.com/), [TorBox API](https://api-docs.torbox.app/).
