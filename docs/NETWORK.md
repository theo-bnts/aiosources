# Network and proxies

[Back to README](../README.md) · [Installation settings](SETTINGS.md)

**Administration → Network** separates incoming requests from outgoing service connections. Incoming settings recognize visitors behind a reverse proxy; outgoing settings route AIOSources through an existing WARP, VPN or compatible proxy.

## Incoming proxies

Open **Network → Incoming** when using Cloudflare, a tunnel or another reverse proxy.

1. Select **IP address header**. For Cloudflare, choose `CF-Connecting-IP` with **Single IP address**. `X-Real-IP` works the same way. For `X-Forwarded-For`, choose **Comma-separated IP chain**.
2. Enter **Trusted proxy addresses**, one address or CIDR network per line. The page shows the direct address seen by AIOSources. With a tunnel, use the connector address or its dedicated network. For Cloudflare's ordinary proxy, use its [published IP ranges](https://www.cloudflare.com/ips/).
3. Enable **Trust an incoming proxy header**, save and check the displayed visitor address.

Trust only proxies you control or explicitly authorize, and prevent direct access that bypasses their protections. Missing or invalid headers use the direct connection address. Both IPv4 and IPv6 are supported. The trusted-proxy setting does not provide HTTPS or replace sign-in.

Public quotas are configured separately in **Administration → Public access → Request limits**. Each limit can be disabled independently. Correct incoming-proxy settings prevent all visitors being counted as one address.

## Outgoing proxies

AIOSources uses an existing proxy; it does not install or start WARP or a VPN. Supported addresses use `http://`, `https://`, `socks5://` or `socks5h://`. HTTP proxies must support HTTPS tunneling.

1. Open **Network → Outgoing → Proxies → Add proxy**.
2. Name the connection and enter an address reachable from AIOSources, such as `socks5://warp:40000`.
3. If needed, add its username and password through **Secrets**, under **Advanced**.
4. Save and choose **Test connection**. This checks the exit address and reports WARP status when available; it does not enable the profile.
5. Enable the proxy, open **Routing** and select the requests that should use it.

Set a default connection, or choose exceptions for individual sources, metadata, debrid services, remote catalogs and browser downloads. For an AllDebrid-only proxy, leave the default unchanged and select that proxy beside **AllDebrid**.

A selected proxy that becomes unavailable blocks its assigned requests; AIOSources does not silently fall back to a direct connection. Change its assignments before deleting it. Deleting a proxy does not delete its reusable Secrets.

### WARP and Docker

Configure WARP's proxy mode using [Cloudflare's guide](https://developers.cloudflare.com/warp-client/warp-modes/), then use the protocol and port it provides. Other VPNs work when they expose a compatible proxy.

The address must be reachable **from the AIOSources container**. `127.0.0.1` refers to that container, not the host or another container. A hostname such as `warp` works when the gateway is reachable on the same Docker network. Keep the proxy restricted to its intended clients.

### What gets routed?

Routing controls requests made by AIOSources, including imports, metadata and debrid API calls. Browser downloads pass through the instance and follow their selected connection. Playback files opened directly by Stremio or another player use that player's own network connection.

## Timeouts

Less common outgoing request timeouts are in **Network → Outgoing → Proxies → Advanced**. Incoming request and download limits are in **Network → Incoming → Advanced**. Individual sources keep their timeout in their source definition, and connections can have their own response timeout in **Filters**.

Use the settings search to find a timeout directly. Increasing it can help a slow service, but does not bypass the provider's own limits.
