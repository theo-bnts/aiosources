# Debrid services and downloads

[Back to README](../README.md) · [Public instances](PUBLIC_INSTANCES.md)

A debrid service can prepare a torrent and provide a playable or downloadable file through your account. AIOSources supports **AllDebrid, Debrid-Link, Deepbrid, Premiumize, Real-Debrid and TorBox**.

## Add your account

1. Open **Debrid → Settings** and choose the service.
2. Add its API key through **Secrets**, select that secret and save.
3. Choose your availability-check and cleanup preferences. Less common options are in **Advanced**.
4. Select the services you want when creating a Stremio or Torznab connection. You can use several services together.

Only configured services appear as download choices for the administrator. Public visitors use their own keys. Use accounts and access methods permitted by each service.

## Understand availability

| Status | Meaning |
| --- | --- |
| Cached | The service has a usable cached-availability observation for this release. |
| Not cached | The service confirmed that it was not cached when checked. |
| Unknown | It has not been checked, the service could not verify it, or the observation is no longer current. |

Services do not all expose the same availability information:

- **AllDebrid, Debrid-Link, Premiumize and TorBox** can provide reusable cached observations through their supported checks. Some checks can add an item to your account.
- **Deepbrid and Real-Debrid** rely on ready files already prepared in your account when a global cache check is unavailable. An absent account item remains **Unknown**.

Availability can change. A file removed from an account or no longer available may need preparation again. Errors and account limits are shown in the operation history; unknown results are not presented as verified cache misses.

## Download or prepare a release

In **Search**, open a title and choose a release. **Download** starts a ready file in your browser. **Add to debrid** starts preparation; use **View operation** to follow it in **Debrid → History** and download when ready.

Browser downloads pass through your AIOSources instance without being saved to its disk. Stremio and compatible players open the supplied playback link themselves. Your service's bandwidth and account limits still apply.

An uncached private-source release requires an announce URL authorized for that account. Public visitors cannot use the administrator's announce URL. [Visitor announce URLs](PUBLIC_INSTANCES.md#private-sources) are optional and controlled by the administrator.

## Control checks and cleanup

Connection **Filters** can restrict results to cached files or set result and cache-check limits. With a maximum cached-results target, existing local observations are reused; new checks stop after reaching the target. Unchecked releases remain **Unknown**. These settings do not guarantee that a provider will return a particular number of files.

**Stream response timeout** controls how long to wait for availability results. Leave it empty to use **Instance default**, which is five seconds on a new instance, or set a different value for that connection.

AIOSources cleans up only temporary items it can confirm it created and that are eligible under your cleanup settings. It does not remove unrelated personal library items. Use **Debrid → Statistics** and **History** to review availability, preparation and cleanup.

If a service blocks your server's address, configure an existing WARP or compatible outgoing proxy in [Network](NETWORK.md).
