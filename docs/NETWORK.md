# Choose an outgoing connection

[Back to README](../README.md) · [Deployment settings](SETTINGS.md)

Use **Administration → Network** when a source or service needs a different internet connection. For example, you can send AllDebrid requests through an existing Cloudflare WARP proxy while keeping other connections unchanged.

AIOSources connects to an existing proxy; it does not install or start a VPN. Supported addresses use `http://`, `https://`, `socks5://` or `socks5h://`. HTTP and HTTPS proxies must support CONNECT tunnels. A VPN can be used when its gateway provides one of these proxy interfaces.

## Add and test a proxy

1. In **Proxies**, choose **Add proxy** and give it a recognizable name.
2. Enter the address reachable from AIOSources, such as `socks5://warp:40000`. The port must match your proxy's configuration.
3. If authentication is required, expand **Authentication** and select or add the username and password through **Secrets**. Keep credentials out of the address.
4. Save, then choose **Test connection**. This contacts Cloudflare's public connection-test endpoint through that proxy and displays its exit IP and WARP status when reported. Testing does not enable the profile or change routing, and sends no source or debrid credentials.
5. Enable the profile, open **Routing** and select where to use it. Save your routing changes.

The test verifies that an HTTPS connection works through the proxy. A particular service may still refuse that exit IP or have an unrelated outage. Settings changes and test outcomes appear under **Network** in **Logs**; credentials, proxy addresses and exit IPs are not included in those log entries.

## Choose which requests use it

**Default connection** applies to sources, remote catalogs, metadata, debrid services and browser downloads. New sources inherit it automatically.

Use **Sources**, **Metadata** and **Debrid** for exceptions, in the same order as the main menu:

- **Use default connection** follows the saved default.
- **System connection** preserves the application's existing connection behavior, including environment proxy settings where previously supported.
- **Direct connection** explicitly bypasses both managed and environment proxies.
- A named proxy uses that profile.

For an AllDebrid-only setup, leave the default on **System connection** and select your proxy next to **AllDebrid**. TorBox, metadata and sources keep their own connections. Source routing applies to imports, previews and source-detail requests, including already queued runs. TMDB routing covers both information requests and export downloads.

**Browser downloads** follow their debrid service's connection unless you select an explicit override. These files pass through AIOSources without being saved to its disk. **Stremio playback links are opened by the player**: this setting routes AIOSources' provider requests, not the player's file transfers.

Saved changes apply to new requests without restarting the application. Requests already in progress may finish on their existing connection. An unavailable or disabled selected proxy blocks its assigned requests; AIOSources never retries them through a direct connection. Change the assignments before deleting a profile. Deleting one leaves its encrypted Secrets available for reuse.

## Cloudflare WARP and Docker

Configure WARP's local proxy mode first, following [Cloudflare's WARP modes guide](https://developers.cloudflare.com/warp-client/warp-modes/). Use the protocol and listening port provided by that installation. Other VPN gateways work the same way when they expose a compatible proxy; the VPN name alone does not imply proxy support.

The address must work **from inside the AIOSources container**. `127.0.0.1` there refers to AIOSources itself. A hostname such as `warp` works only if a proxy with that name is reachable on the same Docker network. A proxy listening only on the host's loopback interface is not automatically reachable through `host.docker.internal`; provide a connection reachable from your container using your gateway's supported configuration. Keep the proxy restricted to the intended clients rather than exposing an unauthenticated proxy to the internet.

The gateway's own request, traffic and account limits still apply. Proxy routing preserves HTTPS certificate checks and the existing destination protections for remote catalogs and browser downloads. Public visitors inherit the administrator's routing and cannot view or change proxy profiles or credentials.
