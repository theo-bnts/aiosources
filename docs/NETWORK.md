# Network and proxies

[Back to README](../README.md) · [Installation settings](SETTINGS.md)

**Administration → Network** separates incoming requests from outgoing service connections. Incoming settings recognize visitors behind a reverse proxy. Outgoing settings route requests through an existing WARP, VPN, standard proxy or MediaFlow Proxy Light.

## Incoming proxies

Open **Network → Incoming** when using Cloudflare, a tunnel or another reverse proxy.

1. Select **IP address header**. For Cloudflare, choose `CF-Connecting-IP` with **Single IP address**. `X-Real-IP` works the same way. For `X-Forwarded-For`, choose **Comma-separated IP chain**.
2. Enter **Trusted proxy addresses**, one address or CIDR network per line. The page shows the direct address seen by AIOSources. With a tunnel, use the connector address or its dedicated network. For Cloudflare's ordinary proxy, use its [published IP ranges](https://www.cloudflare.com/ips/).
3. Enable **Trust an incoming proxy header**, save and check the displayed visitor address.

Trust only proxies you control or explicitly authorize, and prevent direct access that bypasses their protections. Missing or invalid headers use the direct connection address. Both IPv4 and IPv6 are supported. The trusted-proxy setting does not provide HTTPS or replace sign-in.

Public quotas are configured separately in **Administration → Security → Limits**. Each limit can be disabled independently. Correct incoming-proxy settings prevent all visitors being counted as one address.

## Outgoing proxies

For WARP, a VPN or another standard proxy, use an address starting with `http://`, `https://`, `socks5://` or `socks5h://`. HTTP proxies must support HTTPS tunneling.

1. Open **Network → Outgoing → Proxies → Add proxy**.
2. Name the connection and enter an address reachable from AIOSources, such as `socks5://warp:40000`.
3. If needed, add its username and password through **Secrets**, under **Authentication**.
4. Save and choose **Test connection**. This checks the exit address and reports WARP status when available; it does not enable the profile.
5. Enable the proxy, open **Routing** and select the requests that should use it.

Set a default connection, or choose exceptions for individual sources, metadata, debrid services, remote catalogs and browser downloads. For an AllDebrid-only proxy, leave the default unchanged and select that proxy beside **AllDebrid**.

A selected proxy that becomes unavailable blocks its assigned requests; AIOSources does not silently fall back to a direct connection. Change its assignments before deleting it. Deleting a proxy does not delete its reusable Secrets.

### MediaFlow Proxy

Configure MediaFlow Proxy Light with a strong API password and these environment variables:

```yaml
APP__PROXY__FOLLOW_REDIRECTS: "false"
RUST_LOG: "info,actix_web::middleware::logger=off"
```

Current MediaFlow Proxy Light releases can log decrypted provider keys in their access logs. Disable those logs before routing requests and keep TLS verification enabled. Use the relay's default forwarding route. The Python version's forwarding endpoint follows redirects automatically and is incompatible with this setup. [MediaFlow Light settings](https://github.com/mhdzumair/mediaflow-proxy-light/blob/main/docs/configuration/environment.md#proxy) · [Python forwarding endpoint](https://github.com/mhdzumair/mediaflow-proxy/blob/main/mediaflow_proxy/routes/proxy.py)

1. Under **Add proxy**, select **MediaFlow Proxy** and enter its base URL, such as `http://mediaflow-proxy:8888` on a private Docker network.
2. Save its **API password** in Secrets and select it in the form.
3. Save and select **Test connection**. The test verifies the relay's exit address and redirect handling using `www.cloudflare.com`.
4. Enable the proxy and select it for a service under **Routing → Debrid**.

MediaFlow forwards the selected services' API requests. Choose a separate **Browser downloads** connection if you download through those services. For an IP-bound download, use a standard proxy on the same exit IP. Stremio playback links continue to open in the player.

Keep the relay on a trusted network and use HTTPS across hosts you do not control. The relay processes provider authorization and request bodies, so limit who can access it and its logs.

### WARP and Docker

Configure WARP's proxy mode using [Cloudflare's guide](https://developers.cloudflare.com/warp-client/warp-modes/), then use the protocol and port it provides. Other VPNs work when they expose a compatible proxy.

The address must be reachable **from the AIOSources container**. `127.0.0.1` refers to that container, not the host or another container. A hostname such as `warp` works when the gateway is reachable on the same Docker network. Keep the proxy restricted to its intended clients.

### What gets routed?

Routing controls requests made by AIOSources, including imports, metadata and debrid API calls. Browser downloads pass through the instance and follow their selected connection. Playback files opened directly by Stremio or another player use that player's own network connection.

## Timeouts

Less common outgoing request timeouts are in **Network → Outgoing → Proxies → Request timeouts**. In **Network → Incoming → Request timeouts**, Stremio, Torznab and shared catalogs each default to 5 seconds and accept 1–300 seconds. Increase the relevant timeout if large catalog requests or slow providers need longer. Browser downloads have their own section. Individual sources keep their timeout in their source definition, and connections can set a shorter response timeout in **Filters**.

Use the settings search to find a timeout directly. Increasing it can help a slow service, but does not bypass the provider's own limits.
