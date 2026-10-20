# Public instances

Use an existing public instance to connect your apps, or enable a visitor page on your own instance. Public access is optional; private connections and administration remain separate.

## Use a public instance

1. Open the instance address supplied by its administrator. No administrator account is needed.
2. Choose **Connections → Stremio**, enter your personal AllDebrid or TorBox key (at least one is required), then create your installation link.
3. Adjust optional **Filters** and install the generated link in Stremio or AIOStreams. When AIOSources handles playback, leave **Service Wrap** and **Reconfigure Service** disabled for this addon in AIOStreams.
4. Keep the link private. It contains your encrypted configuration and grants access using your account.

If the administrator offers **Torznab**, choose that page instead to create a connection for a compatible client. Torznab does not accept debrid keys or return ready video links; your download client handles downloading.

When permitted by the administrator, **Advanced** accepts your personal announce URLs for private sources. Leave them empty for cached-only private-source access. Never use someone else's account credentials.

AIOSources does not save your submitted keys or announces on the server. Validated keys are automatically remembered in an encrypted configuration on your browser; use **Forget saved keys** on a shared device. It does retain media-request history and safe diagnostics for seven days. You must trust the host processing your keys, and an encrypted installation link is still a credential. The sections below explain these boundaries.

## Enable public access on your instance

Public access is optional and disabled by default. An administrator can enable it in **Administration → Public access** after setting `PUBLIC_URL` to the instance's external HTTPS origin. Literal loopback HTTP addresses are accepted for local testing.

When enabled, the homepage provides **Connections → Stremio** and **Torznab**. Stremio visitors enter an AllDebrid key, a TorBox key, or both, and create an installation link for Stremio or AIOStreams. At least one service is required for Stremio. Public Torznab has a separate administrator opt-in and uses the recipient's download client for debrid accounts. **Admin login** opens the ordinary administration login. Visitors do not receive access to Sources, Settings, Secrets, Logs or histories.

When disabled, the homepage keeps its ordinary login. Disabling public access revokes existing public installation links; enabling it again does not restore those links. Private installations and administrator sessions retain their behavior.

## Credentials and installation links

Visitor keys are validated with the provider and processed only in memory during the request. The application does not save them or visitor announce URLs in its database, encrypted secret store, request history, logs or browser storage in plaintext. After validation, only a keys-only encrypted configuration is remembered automatically on that browser and reused between Search and Stremio. **Forget saved keys** removes it; this also updates other open tabs. Personal announce URLs are excluded from the remembered credential. They can remain in a generated installation link or the current Search session. Neither enters a background job that would need it after a restart.

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

**Administration → Public access → Advanced → Expose source names and filters** is a separate permission, disabled by default. Upgrades preserve a previous explicit visitor-announce permission. Enabling it publishes the IDs and display names of valid sources for selection. The optional announce fields include only sources explicitly marked private. The same permission applies to public Search, Stremio and Torznab; it also controls source names in their responses. It does not publish their API addresses, definitions, secret references or administrator announce URLs. Unknown source privacy or an invalid definition cannot authorize this exception. Disabling imports alone still allows playback from an existing, valid source catalog.

Both public connection pages group their optional controls in a folded **Filters** section. The folded **Advanced** section appears below it when source disclosure is permitted. Visitors may enter their own complete HTTP, HTTPS or UDP announce URL for each listed source, including any personal passkey wherever it belongs in that URL. Stremio still requires at least one debrid key. Empty fields preserve cached-only private access. An announce for one source does not unlock other private sources. Submitting the form clears personal credentials without resetting the chosen filters. No public connection settings or access grants are saved in the database. With **Cached results only**, Torznab also omits the visitor’s own announce URLs, including when the cache observation has expired.

The visitor's URLs are included only in the encrypted installation configuration. They are cleared from the form after submission and are never returned separately in Stremio results or retained in shared availability, histories or Logs. A cached playback uses a plain hash and does not send an announce. When an uncached private item is played, only the visitor's own supplied announce is sent to their selected debrid account. There is no fallback to the administrator's announce or service credentials.

The permission and current source privacy are checked again for stream and playback requests. Turning the option off removes the public source list and restores cached-only private playback for existing links. It does not revoke cached playback. Removing a source definition also withdraws its uncached exception. If definitions become unavailable, the optional list is hidden and cached-only setup and playback remain usable; supplied announces cannot authorize a download until their source can be verified again. Disabling public access still revokes the complete installation as usual. Already started downloads remain in the visitor's provider account; the instance cannot undo a provider operation already in progress.

Uncached downloads may remain pending until the provider finishes them. They are intentional visitor downloads and are not treated as temporary cache probes. They contribute to the shared cache only through the existing verified provider-wide availability rules. A season pack with a known file list that lacks the requested episode is excluded, even when a visitor supplied an announce.

## History and Logs

Public activity uses the existing pages, visible only to administrators:

- **Stremio → History:** stream, manifest and playback requests, including requests served entirely from cache. Filters distinguish public and private installations and request type. Details include normalized media identifiers, duration, outcome, selection counts and returned display fields. Playback details include bounded provider steps, duration, HTTP status and fixed error categories; the playback link is never saved.
- **Debrid → History:** actual availability, cleanup and download work. Public Search downloads carry the **Public Search** trigger and retain safe preparation steps without a saved visitor key or continuation configuration. Operation details include safe provider-call diagnostics. Reusing availability does not fabricate an external call.
- **Logs:** public-access and visitor-announce permission changes, and meaningful provider failures through the existing sanitized logging. Access changes and their logs commit together; a failed or conflicting save does not change access or create a misleading log. Provider error bodies, request URLs, submitted keys and announce URLs are excluded.

There is no visitor database or stable visitor identifier. History does not save IP addresses, account email, username or visitor account fingerprints. Media identifiers and filenames remain activity data: the homepage explains this retention before visitors submit keys.

History follows the existing seven-day cleanup. Stremio history is also capped at 10,000 requests, with at most 32 provider diagnostics per playback and 1,000 provider diagnostics per debrid operation. Summary pages do not load detailed provider-call bodies.

## Admission and verification

Public requests have bounded sizes, timeouts and separate concurrency limits. Setup bodies and encrypted configurations are each limited to 8 KiB; encryption overhead means the complete configuration must still fit inside that limit. The public configuration page shares the concurrency limit without consuming the setup or addon request quota. Configure **Administration → Network → Request limits** to choose link creations per minute per visitor IP (default **12**), public requests per minute per visitor IP (default **120**) and simultaneous public requests per application process (default **50**). Search, Stremio and Torznab share these budgets. Each restriction can be switched off independently (saved as `0`); this does not disable input-size, timeout or operation-capacity safeguards; ordinary homepage configuration reads consume concurrency only. Changing limits preserves active requests and installation links. Counters and concurrency are local to each application process, not a shared multi-replica quota.

By default, budgets use the direct connection address. Behind a reverse proxy, configure **Administration → Network → Incoming proxy** so visitors do not share the proxy's budget. Select the exact header, its single-address or comma-separated chain format, and trusted proxy IP addresses/CIDR networks. Headers from other connections are ignored. Cloudflare uses `CF-Connecting-IP` with a single address; generic proxies commonly use `X-Real-IP` or `X-Forwarded-For`. Header trust also applies to administrator sign-in attempts, without changing their limits or granting access. Missing, invalid or oversized headers fall back to the direct address. See [incoming network setup](NETWORK.md#recognize-visitors-behind-a-proxy). Cache probing is bounded separately to preserve administrator capacity. The public form cannot supply provider endpoints, source URLs or arbitrary hashes to warm.

Automated backend tests cover installation encryption, revocation, administration isolation, independent cache expiry, reset fencing, error handling, account isolation, cache disappearance and history privacy. Browser tests cover enabled and disabled modes, credential clearing, mobile layout and history navigation. Provider HTTP fixtures exercise actual adapter requests and response parsing without contacting live accounts. This is not live validation with two independently subscribed provider accounts; provider account or network restrictions can still affect a particular deployment.

Provider references: [AllDebrid API](https://docs.alldebrid.com/), [TorBox API](https://api-docs.torbox.app/).

## Public navigation and Torznab

Public visitors use the same sidebar and breadcrumb layout as administrators, with **Connections → Stremio** and **Torznab**, plus **Search** when enabled. Administration, storage, logs, definitions and secrets remain protected. When public access is off, the homepage returns to the ordinary login.

**Administration → Public access → Allow public Torznab connections** is disabled by default, including after an upgrade. Visitors can create a configuration without an account. In Generic Torznab clients, use the displayed URL, API path and encrypted API key. Other clients can use the complete generated endpoint URL. Only personal announces are encrypted; URL preferences can be changed without re-entering them or creating a database record. Each configuration can choose sources (only when disclosed), media types, a cache service and cached-only results. It creates no share, request lease or visitor profile in the database.

Torznab returns standard magnets, not debrid video links. Debrid credentials belong in the recipient's download client and are not accepted by public Torznab setup. Its cache filters use fresh, positive shared availability already known by the instance; they do not trigger provider or source calls. An unknown cache state never counts as cached. Standard pagination totals include only eligible results.

Sources explicitly classified public can appear uncached. Private or unclassified sources require fresh known cached availability, or the visitor's own currently permitted announce URL for that particular source. Personal announces are encrypted in the configuration and attached only to that visitor's returned magnets. Administrator announce URLs, native source download URLs and source credentials are never used for this feed. A cached private magnet without an announce is intended for a compatible debrid-aware client; an ordinary peer-to-peer client still needs its own authorized access.

Turning source disclosure off hides names and source selectors on both pages, ignores source selections in existing visitor links and withdraws their announce exceptions. Turning public Torznab off revokes its encrypted keys; enabling it again does not revive them. This does not revoke Stremio installations. Turning all public access off revokes both. The Torznab, Stremio and administrator credentials use separate authentication boundaries and cannot be interchanged.

Public Torznab shares the existing bounded, volatile anonymous admission limits. It records only safe request counts, timings and failures in Logs, under the ordinary seven-day retention. It stores no visitor key, announce, query, IP, profile or configuration. Protect the generated URL/key and redact query strings in external access logs.

## Public Search

Enable **Administration → Public access → Allow public Search** to add Search above Connections in the visitor sidebar. This is off by default, including after an upgrade. Search reads the identified local catalog; it does not request titles from TMDB. Visitors can search title fragments or IMDb/TMDB IDs, filter by year and type, then choose a version using the ordinary language, cache and sorting controls.

Visitors need at least one personal debrid key. A key validated in Stremio is available in Search without retyping it, and vice versa. Only an authenticated encrypted, keys-only credential is stored in the visitor’s browser; the server keeps no visitor profile. Optional personal announce URLs for Search last only in the current page session, unless included in a Stremio installation link.

Private and unclassified sources require fresh cached availability and a usable file, unless the visitor supplies their own currently permitted announce URL. Hidden source names remain hidden in Search. The administrator’s keys, announce URLs, raw source data and native record identities are never returned. Search results contain encrypted references for explicit download actions, not public torrent or announce links.

**Download** starts a browser attachment within the application. **Add to debrid** prepares an allowed uncached version and checks readiness while this page remains open. The client carries an encrypted continuation bound to the source, account, instance and selection; neither visitor keys nor provider download links are saved for background work. Actual provider calls appear in the administrator’s Debrid history with safe diagnostics. Visitors cannot open that history.

Browser downloads pass through AIOSources and use the host’s bandwidth, without saving file contents to its disk. Short-lived file links expire after ten minutes. Turning public Search off blocks catalog requests, new preparation checks and new file transfers; it does not interrupt transfers already started. Stremio connections remain available. Turning all public access off also revokes the remembered browser credentials.
