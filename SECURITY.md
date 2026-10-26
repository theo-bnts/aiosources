# Security policy

## Report a vulnerability privately

Do not publish vulnerability details in issues, pull requests or discussions.

Use [GitHub private vulnerability reporting](https://github.com/theo-bnts/aiosources/security/advisories/new) when available. Otherwise, [request a private reporting contact](https://github.com/theo-bnts/aiosources/issues/new?title=Private%20security%20contact%20request) without including the vulnerability, affected endpoint or exploit details.

Include the affected version or build, expected and observed behavior, impact and minimal reproduction steps using synthetic data. Never attach live credentials, installation links, private source definitions, session cookies, encryption keys or database dumps. Test only systems you own or are authorized to assess.

Security fixes target the current maintained code. Older builds and forks are not maintained as separate security branches.

## Protect your instance

- Use HTTPS for internet access and a trusted reverse proxy or tunnel. Setting `PUBLIC_URL` alone does not enable HTTPS.
- Keep PostgreSQL and any backend port protected by your access gateway private. Do not let direct access bypass that gateway.
- Use a strong administrator password, enable two-step verification and keep recovery codes safely outside the instance. Revoke unused sessions.
- Configure [trusted incoming proxies](docs/NETWORK.md#incoming-proxies) explicitly. Never trust visitor-supplied address headers from arbitrary connections.
- Protect installation URLs, Torznab keys and sharing passwords like credentials. Give separate recipients separate access and revoke access that is no longer needed.
- Avoid proxy or CDN rules that cache authenticated responses or private connection URLs.
- Back up both data volumes, preserve the encryption key and keep backups private.

## Public instances

Visitor keys and personal announce URLs are not saved by AIOSources on the server. They are processed to use the visitor's account and can be remembered in an encrypted browser configuration. A generated link remains a credential even when its contents are encrypted.

Visitors must trust their host: an operator can modify the software or surrounding infrastructure. Request history is retained according to the instance's policy. See [Public instances](docs/PUBLIC_INSTANCES.md) for source exposure, personal announces and public access controls.

AIOSources does not grant permission to access or redistribute third-party content. Only connect authorized sources and accounts; see [usage and service terms](LEGAL.md).
