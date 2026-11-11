# Public instances

[Back to README](../README.md) · [Debrid services](DEBRID.md) · [Torznab](TORZNAB.md)

An administrator can offer Search, Stremio, Torznab and Shared catalogs to visitors without giving them access to administration. Available pages and options depend on that instance's settings.

## Use a public instance

1. Open the address supplied by an administrator you trust.
2. For **Search** or **Connections → Stremio**, enter at least one personal key for a supported debrid service, unless the administrator has enabled instance accounts. For **Torznab**, follow that page's setup; debrid keys are not used for this connection.
3. Adjust optional **Filters** and **Sorting**, then search or generate your connection link.
4. Install a Stremio manifest in Stremio or Nuvio, or add it to AIOStreams as a custom addon. If AIOSources provides playback, leave AIOStreams **Service Wrap** and **Reconfigure Service** disabled.

No AIOSources administrator account is needed. Torznab clients use the generated URL and API key; their download client handles the files.

### Copy a public catalog

Open **Connections → Shared catalogs**, choose any optional **Filters** or **Shared data**, and select **Create connection link**. No debrid key or announce URL is needed. On your own AIOSources instance, add the URL under **Sources → Remote catalogs → Settings**, save the supplied sharing password in Secrets, and enable the connection.

The receiving instance keeps a local copy and syncs changes automatically. Only the owner's permitted data is included. Torrent records can include private-source or uncached releases, but never the owner's announce URLs or download credentials: playback still needs your own setup. Each receiving instance identifies its torrents locally. See [catalog sharing](SHARING.md).

### Your keys and browser

AIOSources does not save visitor keys or personal announce URLs on the server. The browser automatically remembers an encrypted configuration so Search, Stremio and Torznab can reuse it. Use **Forget saved credentials** on a shared device. Clearing browser data also removes it.

Keep generated links and Torznab keys private: they grant access using your configuration. Encryption does not make a link safe to publish. The host receives your submitted credentials to contact the service, so use only an administrator you trust. A host can modify its software; the application cannot prevent that.

The instance keeps request history and safe diagnostics for its configured retention period. It may reuse account-independent cached-availability observations. Account keys, announce URLs, personal playback links and account-library identifiers are not shared as those observations.

Availability checks use the credentials explicitly allowed by the administrator. Personal keys are the default, with no fallback to instance keys and do not schedule automatic rechecks after your request. For checks that create temporary account items, AIOSources attempts to remove only additions it can confirm it created. A server crash or failed provider request can prevent cleanup; no visitor key is stored to retry it later. Items needed for playback or a download you requested are handled separately.

## Host a public instance

1. Configure the external HTTPS address in `PUBLIC_URL`. See [installation settings](SETTINGS.md).
2. Open **Administration → Users → Public → Settings** and enable public access.
3. In **Search** and **Connections**, enable each page you want to offer: Search, Stremio, Torznab and Shared catalogs are independent.
4. Choose whether to expose source names and source filters. Leave that switch off if you want source names kept private.
5. In **Credentials**, choose whether visitors use their own debrid accounts and announces (the default) or those of the instance. These permissions are independent. Changing either revokes old public configurations. **Use instance announce URLs** permits Torznab to transmit those URLs to clients.
6. Review **Public → Request limits**, and [trusted incoming proxies](NETWORK.md#incoming-proxies) if you are behind Cloudflare, a tunnel or another reverse proxy.

Disabled pages do not appear in the public menu. Disabling a public connection blocks its requests and revokes its existing public access; turning it on again does not restore revoked links. Private administrator-created connections are managed separately.

Public **Shared catalogs** also requires **Expose source names and filters**, because copies preserve source identity to avoid duplicates. In **Shared catalog data**, restrict the sources, published fields and optional information visitors may copy. Visitors may narrow these choices further. Optional data groups start off on the visitor page and must also be enabled by the receiving instance. Disabling source exposure also turns off public sharing and revokes its links. Sharing must be enabled explicitly again after restoring source exposure. No visitor configuration or sharing password is saved on the hosting instance; safe request diagnostics follow the normal log retention.

## Private sources

With source exposure enabled, visitors can see the permitted source controls. **Private source playback** accepts their personal announce URLs for private sources when that option is available. The URLs are remembered with their browser configuration.

In Search, Stremio and Torznab, without an authorized announce URL, private-source results are restricted to cached playback. The instance’s announce URL is used only when **Use instance announce URLs** is explicitly enabled. Cached-only connections never return an announce. Public-source uncached results can be offered when the connection permits them; hiding source names does not make public-source torrents private.

Stremio visitors receive playback information for the authorized debrid accounts. Torznab returns permitted torrent results for their download client. Neither connection grants access to administration or Secrets. Instance debrid API keys remain server-side; Torznab exposes instance announces only when explicitly authorized above.

## Filters and limits

Visitors can choose the options allowed by the instance, including media types, services, languages, sorting and result limits. Source-specific controls appear only when source exposure is enabled. **Filters** contains optional result restrictions; **Sorting** sets the result order. These sections start closed, with no optional restrictions selected for new connections.

Public request limits can be enabled or disabled independently. If a visitor is repeatedly rate-limited, check the instance's trusted proxy configuration so different visitors are recognized separately. Service account limits and request timeouts still apply.
