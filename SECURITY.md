# Security policy

## Supported code

Security fixes target the current `main` branch. Older commits and forks are not maintained as separate security branches. Include the output of `ingest version` or your commit/image digest when reporting a problem.

## Report a vulnerability privately

**Do not publish vulnerability details in issues, pull requests or discussions.**

Use [GitHub's private vulnerability reporting](https://github.com/theo-bnts/aiosources/security/advisories/new) if enabled for this repository. Otherwise, [request a private reporting contact](https://github.com/theo-bnts/aiosources/issues/new?title=Private%20security%20contact%20request). That request must contain only a request for contact: no exploit, affected endpoint, credentials or technical details. Wait for a private channel before sending the report.

A useful private report includes:

- Affected version or commit, deployment method and relevant configuration with secrets removed.
- Expected behavior, observed behavior and security impact.
- Minimal reproduction steps or a proof of concept using synthetic data.
- Any known mitigation or proposed fix.

Never attach a live database dump, unredacted raw responses, passwords, API keys, cookies, `vault.key`, an age recovery identity, MFA secrets or recovery codes. Test only systems you own or are authorized to assess. Coordinate disclosure with a maintainer; no response-time or remediation SLA is promised.

## Deployment responsibilities

- **Keep the backend and PostgreSQL private.** For remote access, use a trusted HTTPS reverse proxy and configure `PUBLIC_URL` to its exact origin. The setting does not provide TLS itself. Direct HTTP access is suitable only on a trusted private network or through an encrypted VPN/SSH tunnel: passwords, cookies and installation tokens are otherwise observable and alterable in transit. The application currently serves HTTP, so a public internet deployment needs HTTPS termination.
- **Limit administrator access at the edge.** Prefer a private network or VPN. Login throttling deliberately uses the backend connection's remote address, not arbitrary forwarded headers: a reverse proxy therefore shares one application-level attempt bucket across its clients. Configure per-client throttling on the trusted proxy to reduce temporary lockouts caused by another client; never assume an untrusted `X-Forwarded-For` value establishes identity.
- **Enable two-step verification** and keep recovery codes outside the server. Revoke unused administrator sessions.
- **HTTPS does not replace authentication.** Anonymous visitors can load the sign-in page, static assets and a minimal health/session response. Administration data requires an authenticated session; the Stremio installation token, each catalog-sharing password and each Torznab API key are separate credentials with their own scope. Possession of a valid installation URL authorizes its catalog and playback without the administrator password.
- **Configure Cloudflare end to end.** A Cloudflare Tunnel can keep the origin private; with ordinary Cloudflare proxying, use Full (strict), an HTTPS origin and restrict direct origin access. Edge HTTPS alone does not encrypt a public HTTP origin. Do not cache authenticated API responses or installation-token routes with a rule that overrides `no-store`.
- **Protect source definitions, raw archives and backups.** Source JSON, exported editor drafts and `.source-migration/` recovery originals/journals are private administrator configuration, not a public provider catalogue. Original source data can contain sensitive metadata even when a shared catalog exposes only a small field allowlist.
- **Grant the minimum sharing scope.** Use separate recipients and appropriate expiration/limits. Revocation stops future requests, not copies already downloaded or re-shared.
- **Back up the vault key and database together.** Use deployment backup tooling; the application no longer creates or restores backup archives. Test recovery before relying on an external archive.
- **Restore only trusted database dumps.** PostgreSQL dumps contain executable SQL.

## Security boundaries

The vault encrypts credentials at rest; it does not protect against an attacker controlling the running application or its host and key material. Security and sharing outcomes are recorded in Logs in the same transaction as their changes.

Source definitions are trusted administrator configuration. They can cause outbound requests, including to private networks where supported. Read-only catalog access is not administrator access and does not authorize source configuration, secrets or raw archives.

Torznab is disabled until an administrator enables an access key. Capabilities and searches require that key, whose hash is stored instead of its value. Source and media permissions on the key are enforced independently of optional URL filters; query parameters can only narrow them. Requests read published records and emit rebuilt, tracker-free magnets, never native download URLs or administrator announce credentials. Torznab keys do not authorize debrid playback and cannot authenticate against catalog-sharing or administration routes. Torznab has its own global admission bound, per-key durable quotas and a 20-second request deadline. Redact query strings in external access logs because standard Torznab clients send `apikey` in the query.

Stremio connection filters are URL preferences, not separate access grants. Holders of an installation token can change its filters; rotating the parent token revokes every derived private link. Public installation keys and visitor announce URLs remain inside their encrypted credential, while ordinary filter choices contain no credentials. Disabling public visitor access still revokes public installations; it does not revoke separately enabled Torznab access. See [Torznab setup and boundaries](docs/TORZNAB.md).

Authentication work has separate resource bounds. Login and password-verification bodies are limited to 4 KiB, with at most eight admitted requests and two concurrent password-hash checks. Private addon token checks have a separate limit of 64 and a five-second database budget, without shortening playback requests. Excess work receives a retryable HTTP 429 before entering the protected queue. Invalid sharing passwords are rejected before waiting for a recipient's download lock; valid credentials and permissions are checked again under that lock so concurrent revocation, rotation and expiration still take effect.

The source editor's six settings sections configure the existing collector, not arbitrary code or an executable graph. Strict JSON validation rejects malformed definitions but does not establish that an upstream service is trustworthy. Saving or validating a source does not contact its endpoint or start collection; enabling its schedule authorizes later requests. Keep credentials in **Security → Secrets** and use vault references, including in imported/exported JSON. Normal loading accepts only source `.json` files; legacy YAML belongs only in the explicit [offline migration](docs/CONFIGURATION.md#migrating-existing-sources). Stop all services and other source writers before migration, and protect its retained originals as carefully as live configuration.

Successful logout and detected session expiry remove the authenticated interface without waiting for a later refresh. The panel cancels in-flight queries and discards private query and mutation caches; an older session response cannot restore the signed-out interface. This does not erase files or other copies already saved outside the panel.

See [deployment settings](docs/SETTINGS.md) and [sharing boundaries](docs/SHARING.md) for operational details.

## Interpreting dependency reports

Run source-aware checks as well as package/version scans. A reported module is not necessarily an imported package or a reachable vulnerable call, and a clean scan is not a security certification.

[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) applies to the unmaintained `golang.org/x/crypto/openpgp` package family. The application uses other `golang.org/x/crypto` primitives but does not import that OpenPGP family. Source-aware `govulncheck -tags production ./...` and the Linux production import graph were checked during this audit; no affected imported package or reachable call was found. Binary scanning can still report this package-wide advisory conservatively. Do not suppress the entire crypto module or replace unrelated primitives merely to hide that report; reassess it if dependencies or imports change.

Local release checks scan the full Git history with redacted Gitleaks output, run source-aware vulnerability checks for both Go modules, and audit frontend dependencies. Repository owners manage GitHub secret scanning, push protection and dependency updates separately. These checks complement review; neither ignored local files nor a clean scan prove that every credential is absent.
