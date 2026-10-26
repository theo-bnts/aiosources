# Public instances

[Back to README](../README.md) · [Debrid services](DEBRID.md) · [Torznab](TORZNAB.md)

An administrator can offer Search, Stremio and Torznab to visitors without giving them access to administration. Available pages and options depend on that instance's settings.

## Use a public instance

1. Open the address supplied by an administrator you trust.
2. For **Search** or **Connections → Stremio**, enter at least one personal key for a supported debrid service. For **Torznab**, follow that page's setup; debrid keys are not used for this connection.
3. Adjust optional **Filters** and **Sorting**, then search or generate your connection link.
4. Install a Stremio manifest in Stremio or Nuvio, or add it to AIOStreams as a custom addon. If AIOSources provides playback, leave AIOStreams **Service Wrap** and **Reconfigure Service** disabled.

No AIOSources administrator account is needed. Torznab clients use the generated URL and API key; their download client handles the files.

### Your keys and browser

AIOSources does not save visitor keys or personal announce URLs on the server. The browser automatically remembers an encrypted configuration so Search and Stremio can reuse it. Use **Forget saved credentials** on a shared device. Clearing browser data also removes it.

Keep generated links and Torznab keys private: they grant access using your configuration. Encryption does not make a link safe to publish. The host receives your submitted credentials to contact the service, so use only an administrator you trust. A host can modify its software; the application cannot prevent that.

The instance keeps request history and safe diagnostics for its configured retention period. It may reuse account-independent cached-availability observations. Account keys, announce URLs, personal playback links and account-library identifiers are not shared as those observations.

## Host a public instance

1. Configure the external HTTPS address in `PUBLIC_URL`. See [installation settings](SETTINGS.md).
2. Open **Administration → Public access → Settings** and enable public access.
3. In **Search** and **Connections**, enable each page you want to offer: Search, Stremio and Torznab are independent.
4. Choose whether to expose source names and source filters. Leave that switch off if you want source names kept private.
5. Review **Public access → Request limits**, and [trusted incoming proxies](NETWORK.md#incoming-proxies) if you are behind Cloudflare, a tunnel or another reverse proxy.

Disabled pages do not appear in the public menu. Disabling a public connection blocks its requests and revokes its existing public access; turning it on again does not restore revoked links. Private administrator-created connections are managed separately.

## Private sources

With source exposure enabled, visitors can see the permitted source controls. **Advanced** can accept their personal announce URLs for private sources when that option is available. The URLs are remembered with their browser configuration.

Without a visitor announce URL, private-source results are restricted to cached playback. The administrator's announce URL is never substituted. Public-source uncached results can be offered when the connection permits them; hiding source names does not make public-source torrents private.

Stremio visitors receive playback information for their own debrid accounts. Torznab returns permitted torrent results for their download client. Neither connection grants access to administration, your Secrets or your personal announce URLs.

## Filters and limits

Visitors can choose the options allowed by the instance, including media types, services, languages, sorting and result limits. Source-specific controls appear only when source exposure is enabled. **Filters** contains optional result restrictions; **Advanced** contains less common options.

Public request limits can be enabled or disabled independently. If a visitor is repeatedly rate-limited, check the instance's trusted proxy configuration so different visitors are recognized separately. Service account limits and request timeouts still apply.
