# Contributing

AIOSources keeps source collection, local metadata, availability and playback in the existing Go/React application. Source behavior belongs in validated JSON configuration; reusable protocol support belongs in connectors. Avoid source-specific code paths, external runtimes and duplicate schedulers.

## Public contributions

- Use synthetic source names and reserved `.example`, `.invalid` or `.test` domains in documentation and fixtures. Network tests use owned loopback servers. Protocol namespaces and official service API/documentation URLs are separate from source endpoints.
- Do not add source catalogs, real torrent-site links, private announce URLs, credentials, source-definition archives or production captures. The image ships no configured sources. Administrators import their own private configuration through the existing editor and vault.
- Keep source identities and provenance independent. An identical info hash does not authorize merging records, credentials or sharing scopes.
- Submit only reviewed public files. Keep private source definitions, captures, database dumps and historical archives out of every branch and tag. An ignored file can still exist in an old commit.
- Preserve copyright, attribution and license notices when reusing code or assets. Update the bundled third-party notices when dependency versions change; see [Development](docs/DEVELOPMENT.md).

## Verification

Reproduce a behavioral regression before changing it. Compare observable v1 behavior while preserving the stronger v2 publication, conflict, cancellation and ownership guarantees. Reference fixtures may document deliberate improvements over old misses; they must not require restoring unsafe old behavior.

Use a disposable database and synthetic provider responses. Follow [Development](docs/DEVELOPMENT.md) for the Go/SDK race and static checks, frontend checks, embedded-browser suite and both Linux builds. Fix the failing boundary rather than adding blanket retries or suppressing errors. Logs must identify the failed operation without storing upstream messages, credentials or playback URLs.

Add new database changes as migrations; never edit an applied migration's checksum. Exercise upgrades from supported older schemas when adding schema objects. Run snapshots, checkpoints, queue acknowledgements and published data must retain their transaction boundaries.

Report vulnerabilities through [the private reporting process](SECURITY.md), not public issues with sensitive details.
