# Configure incoming and outgoing connections

[Back to README](../README.md) · [Deployment settings](SETTINGS.md)

Use **Administration → Network** when a source or service needs a different internet connection. For example, you can send AllDebrid requests through an existing Cloudflare WARP proxy while keeping other connections unchanged.

AIOSources connects to an existing proxy; it does not install or start a VPN. Supported addresses use `http://`, `https://`, `socks5://` or `socks5h://`. HTTP and HTTPS proxies must support CONNECT tunnels. A VPN can be used when its gateway provides one of these proxy interfaces.

## Add and test a proxy

1. In **Outgoing proxies**, choose **Add proxy** and give it a recognizable name.
2. Enter the address reachable from AIOSources, such as `socks5://warp:40000`. The port must match your proxy's configuration.
3. If authentication is required, expand **Authentication** and select or add the username and password through **Secrets**. Keep credentials out of the address.
4. Save, then choose **Test connection**. This contacts Cloudflare's public connection-test endpoint through that proxy and displays its exit IP and WARP status when reported. Testing does not enable the profile or change routing, and sends no source or debrid credentials.
5. Enable the profile, open **Outgoing routing** and select where to use it. Save your routing changes.

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


## Recognize visitors behind a proxy

Open **Administration → Network → Incoming** when requests arrive through Cloudflare, a tunnel or another reverse proxy. This is independent of outgoing WARP/VPN routing.

1. Select **IP address header**. Cloudflare's orange-cloud proxy and Tunnel normally send `CF-Connecting-IP`; use **Single IP address**. `X-Real-IP` works the same way. For `X-Forwarded-For`, use **Comma-separated IP chain**. Custom header names are supported with either format.
2. Enter **Trusted proxy addresses**, one IP or CIDR network per line. The page shows the direct connection address currently seen by AIOSources. With Tunnel, trust the `cloudflared` connector address or its dedicated network. With Cloudflare's standard proxy, use [Cloudflare's published IPv4 and IPv6 ranges](https://www.cloudflare.com/ips/). An intermediate local reverse proxy must be included too when using an address chain.
3. Enable **Trust an incoming proxy header** and save. Check the displayed visitor address using the saved settings.

Trust only peers allowed to assert visitor addresses. Restrict direct access to AIOSources when relying on your proxy's protections. The application rejects a universal `/0` trust rule. Your proxy must overwrite a single-address header or append the genuine connecting address to a chain. Arbitrary regex extraction is intentionally unnecessary: supported values are plain IPv4/IPv6 addresses or comma-separated lists of those addresses. RFC `Forwarded: for=...` syntax, hostnames and address/port pairs are not accepted.

A chain is read right to left from the connected proxy, stopping at the first untrusted hop. A visitor cannot gain a new quota merely by prepending an invented address. Untrusted connections and missing, malformed, duplicate single-address or oversized headers use the direct connection address. Header trust is disabled after an upgrade until explicitly configured. It never changes authentication, HTTPS detection, the configured public origin or outgoing proxy settings.

The resolved address separates public request quotas and administrator sign-in protection. Public request counters are volatile; visitor IPs, header values and the diagnostic addresses are not added to public histories or Logs. Administration keeps its existing hashed sign-in throttling records. **Administration → Public access → Request limits** controls visitor quotas. Changes saved in this process apply immediately; other processes sharing the database reload their policy within one second. Limits and in-flight counters are per process.
