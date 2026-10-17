# AIOSources

Your sources, one searchable catalog, ready for Stremio.

AIOSources is a self-hosted application that organizes torrent metadata from your own sources, identifies films and series, and connects your catalog to Stremio or AIOStreams. Configure everything from a web interface and keep control of your accounts and data.

## What you can do

- **Manage your sources:** configure connections visually or import your own JSON definitions, preview results and schedule automatic imports.
- **Search your catalog:** find partial titles, source identifiers or info hashes, and combine filters across sources.
- **Identify films and series:** use TMDB information to verify IMDb, TMDB and TVDB identifiers, then review or correct uncertain results.
- **Check availability:** connect AllDebrid or TorBox to see cached availability and prepare playback when you select a stream.
- **Connect Stremio and AIOStreams:** create independent private installations with saved filters and browse the results from your catalog.
- **Connect Torznab applications:** expose a revocable, read-only endpoint for Prowlarr and other clients, with saved filters and optional URL parameters for each client.
- **Offer optional public access:** let visitors install the addon with their own debrid keys and reuse shared cache observations, while keeping administration private.
- **Share selected metadata:** create revocable, read-only catalog shares and import catalogs shared with you.
- **Follow activity:** see source statistics, processing history, returned Stremio results, logs and storage usage.

AIOSources ships with no configured sources or account credentials. Use sources you are authorized to access and content you are entitled to use and share. Example configurations use fictitious addresses.

## Install with Docker Compose

This HTTP example is for a trusted local network. Save the following as `compose.yaml`. Replace `XXX.XXX.XXX.XXX` with your server's IP address or domain, and `XXX` with a strong administrator password. Change the example database password in both `POSTGRES_PASSWORD` and `DATABASE_URL` before deployment.

```yaml
services:
  aiosources_postgres:
    image: postgres:18-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: aiosources
      POSTGRES_USER: aiosources
      POSTGRES_PASSWORD: aiosources
    volumes:
      - aiosources_postgres_data:/var/lib/postgresql
    networks:
      - aiosources_postgres
  aiosources:
    depends_on:
      - aiosources_postgres
    image: ghcr.io/theo-bnts/aiosources:stable
    restart: unless-stopped
    environment:
      DATABASE_URL: postgresql://aiosources:aiosources@aiosources_postgres:5432/aiosources
      PUBLIC_URL: http://XXX.XXX.XXX.XXX:7000 # Replace with your IP address or domain.
      ADMIN_PASSWORD: XXX # Password for the administration interface.
    volumes:
      - aiosources_data:/data:rw
    ports:
      - 7000:7000
    networks:
      - aiosources # Also connect AIOStreams to this network.
      - aiosources_postgres
    dns:
      - 1.1.1.1

volumes:
  aiosources_postgres_data:
  aiosources_data:

networks:
  aiosources:
  aiosources_postgres:
```

Start the application:

```sh
docker compose up -d
```

Open `http://YOUR_SERVER_IP:7000` and sign in with `ADMIN_PASSWORD`. For access over the internet, use an HTTPS reverse proxy, restrict access to the administration interface and set `PUBLIC_URL` to that external HTTPS address.

Both Linux `amd64` and `arm64` images are available. If AIOStreams uses another Compose project, attach it to the same Docker network or use the externally reachable address.

### Stable versions and previews

The Compose example uses `ghcr.io/theo-bnts/aiosources:stable`. GHCR is the primary image registry; Codeberg remains a mirror. New builds are published as **GitHub pre-releases** and use the `preview` tag; they do not replace `stable` or `latest` automatically. A newly created GHCR package must be made public before anonymous pulls work.

To try a preview, replace `:stable` with `:preview`, then pull and recreate the application container. Each pre-release also gives the exact image digest, which pins the tested build even when `preview` advances. Keep both data volumes. To follow stable releases again, restore `:stable`. An older image is not guaranteed to understand database changes made by a newer preview; do not use a production database as a disposable test installation.

For maintainers: after testing a pre-release, open **GitHub → Releases → Edit**, clear **Set as a pre-release** and save. The stable-promotion workflow verifies the recorded image and both architectures, then publishes that same image under `latest`, `stable` and the release tag, without rebuilding it. GHCR stable tags become available after the first promotion.

## Environment variables

These are all application environment variables. The image supplies the defaults below; native installations use `.ingest`, `providers` and `127.0.0.1:8080` for the three path/listen settings. Add overrides to the application service's `environment` section or `env_file`.

| Variable | Image default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Required | PostgreSQL connection URL. |
| `BIND` | `0.0.0.0:7000` | Address and port on which the application listens. |
| `PUBLIC_URL` | Unset | Exact external HTTP(S) origin, for example `https://sources.example.com`. Setting this does not enable HTTPS; HTTPS is required for catalog sharing and public access. |
| `ADMIN_PASSWORD` | Generated when omitted | Administration password, 12–72 bytes. An omitted password is saved as `/data/admin-password`; it can be read locally with `/aiosource password`. |
| `DATA_DIR` | `/data` | Private application state, including the encryption key. Preserve this storage when updating. |
| `PROVIDERS_DIR` | `/data/providers` | Private JSON source definitions. |
| `MASTER_KEY` | Generated `vault.key` in `DATA_DIR` | Optional existing 32-byte encryption key, encoded in base64 or hexadecimal. Never change the key of an existing installation. |

TMDB, AllDebrid, TorBox and source credentials belong in **Security → Secrets**. They have no dedicated deployment variables. Import concurrency defaults to **5**, and can be changed from **1 to 32** in Sources settings without deployment changes; existing saved limits are preserved. Each source runs at most one import at a time.

**Upgrading from prefixed variables:** remove `INGEST_` from the six old deployment variable names in your environment and Compose overrides before starting this image. Keep every value, path and volume unchanged. The application refuses obsolete names rather than silently generating a different password or encryption key. `DATABASE_URL` keeps its name. Compose-only options and development/test variables are listed in [Deployment and settings](docs/SETTINGS.md).

## Set up your catalog

1. Open **Sources → Settings → Add source**. Configure your authorized source or import its JSON definition. Add any required credential through the encrypted secret picker, save, then start a preview or import. Automatic timing belongs to that source's **Schedules** section.
2. In **Metadata → Settings**, add your own [TMDB API read access token](https://www.themoviedb.org/settings/api). Information updates and torrent identification run automatically when enabled. Use **Identification** to review uncertain matches.
3. Optionally add your AllDebrid or TorBox key in **Debrid → Settings**. AllDebrid availability checks can create temporary entries in your account; AIOSources only cleans up entries it has confirmed it created.
4. In **Stremio → Settings**, create an access, save its manifest URL, enable it, then install the link in Stremio or AIOStreams. When AIOSources handles debrid playback, leave AIOStreams' **Service Wrap** and **Reconfigure Service** disabled for this addon.

**Search** finds clean film and series titles in your identified catalog. Open a title to compare its source releases, sort by date, size or source name, and select a season and episode for a series. Titles and relevance ranking use your local metadata, without contacting TMDB during a search. Titles prefer your browser language, a matching local alias, then English. Translation language codes become available as metadata updates normally.

Filter titles by type and an optional single year. Films match any locally known release year; series match the span of their locally known release, season and episode years. Year filtering makes no TMDB request.

**Download** starts the file in your browser while keeping the application open. **Add to debrid** explicitly starts preparation for an uncached version; use **View operation** to follow its readiness in **Debrid → History**, including after leaving Search. Private sources need your announce URL for uncached downloads. The history retains the selection and sanitized provider calls for seven days, without API keys, announce URLs or generated download links. Browser downloads use a protected streaming relay: files consume the instance’s bandwidth but are never saved on its disk or loaded fully into memory. Stremio playback continues to use direct provider links.

Versions are largest first. The optional language filter includes your browser language, MULTI and unknown audio by default; disable it to see every language. Stremio installations offer the same filters.

**Private Stremio access:** create as many named installations as you need in **Stremio → Settings**. New accesses include all current and future sources. Source selection and playback options belong to **Filters**. Optional expiration, request and concurrency limits belong to **Advanced**, with no per-access limits by default. Settings are saved on the instance and apply to the same manifest URL without reinstalling. Enable, disable, replace or revoke each access independently. The full manifest URL is shown only when creating or replacing its key; save it securely. The database retains only its key’s fingerprint.

The former global installation token and its Legacy panel have been removed. Replace an old global-token manifest with a named access; existing named installations keep working. Public Stremio and Torznab configurations remain in their connection URLs, with no saved visitor access or profile. Their optional controls are grouped under **Filters**, followed by **Advanced** for personal announce URLs when the administrator permits them.

### Torznab

Open **Connections → Torznab**, create an access key and enable it when ready. Access defaults to all sources without per-access limits. Select media types, sources, cache services and **Cached results only** in **Filters**, and optional expiration or limits in **Advanced**. Saved settings apply immediately to the same key. Enter the Torznab URL and generated API key in your client; Prowlarr uses **Generic Torznab**. A key can still be used with multiple URL filters that narrow its saved settings.

Requests search your published local data without contacting your sources or TMDB. Standard categories select movies, series or other releases. Torznab can include records that are not identified; Stremio uses identified video types, including anime. Optional `sources` and `media` URL parameters can narrow each connection further. Private-source magnets contain no administrator announce URL: the recipient needs their own source access to download them. No debrid account is shared through Torznab. See the [Torznab setup and parameter reference](docs/TORZNAB.md).

**Sources → Torrent search** searches the individual imported records, including title fragments, source IDs and complete or partial info hashes. **Metadata → Identification** accepts the same search syntax when reviewing matches. Each list shows its filtered result count above the rows.

Treat the manifest URL like a password: it grants access to your catalog and permitted playback services. Disable, replace or revoke its named access in Stremio settings to stop an old link. Use your service accounts according to their terms; a catalog or playback link does not grant rights to its content.

Streams use Stremio's standard `description`, `infoHash`, `url` and `behaviorHints` fields. AIOStreams also reads the structured source name, service, cache state, publication date, duration and file/folder sizes when available; the original release and selected filename retain the quality, codec and language information. Missing optional data is omitted. Arbitrary raw fields and administrator secrets are excluded. A configured announce URL can be included when direct torrent playback needs it, so protect installation links. **Cached results only** excludes direct torrent playback and strips announces from debrid preparation; an unavailable cached file never falls back to the administrator’s announce. Private Torznab magnets never contain an announce.

A source marked **Private source** without an announce URL is offered only when cached availability is verified. Unknown or uncached results are hidden, including episodes that cannot be confirmed in a season pack. Without a configured debrid service, these private sources are excluded from direct torrent results. Turning off **Private source** disables announce editing and use while keeping the selected secret for a later reactivation.

Two-step verification, encrypted secrets and session management are available in **Security**. Keep the database and application data volumes together when making backups: the data volume contains the key needed to decrypt your secrets. Completed diagnostic history is automatically retained for seven days; published torrents and recovery data for unfinished imports are preserved.

### Keep storage under control

**Storage → Data retention** uses **Storage saving** by default. New imports keep torrent identifiers, release, language and playback information relevant to AIOSources and AIOStreams, while discarding descriptions and unused source details. **All mapped fields** keeps additional fields selected in your source definitions. In Storage saving, successful per-record processing details are omitted; record errors, previews, run history, committed changes and resume/coverage evidence remain available. Both modes preserve the TMDB titles, aliases, identifiers and season information used for identification; full TMDB responses are never stored.

Saving this setting affects imports started afterwards, including automatic imports. An import already queued or in progress keeps its original mode. Previously stored fields are not removed automatically. Source APIs may still return complete responses; the saving applies to retained data and unnecessary detail-only enrichment.

To start again with the new mode, save your selection and pause any running imports, then optionally use **Reset all source data**. The button explains why it is unavailable when these steps are still needed. This removes imported torrents, remote catalog copies, source history, resume points and manual identifications. Your source definitions, secrets, schedules, sharing permissions, TMDB information and debrid caches are retained. The next import starts from scratch. This reset cannot be undone.

**Connections → Shared catalogs** shares published data. **Sources → Remote catalogs** manages imports from other instances. Sharing keeps only fields its export protocol can use, and does not keep publication snapshots while every share is disabled or expired. Enabling sharing builds a fresh snapshot of the current catalog. Storage measurements include PostgreSQL tables and indexes; ordinary deletion makes space reusable inside PostgreSQL, while a source reset also releases the large source table files.

### Optional public access

Enable **Connections → Public access** to offer a visitor homepage. Visitors supply at least one AllDebrid or TorBox key and receive an encrypted installation URL. Keys are processed without saving them; playback uses each visitor's own account. Administration remains protected. Disabling public access revokes existing public links, while private installations keep working.

Optional **Advanced → Expose source names and filters** publishes source display names and enables source selection for both public connections. Visitors can then supply their own complete announce URLs for uncached playback from those sources. The URLs are encrypted in their installation link and never saved. Your administrator announce URLs remain private. Turning the option off hides source names and filters and restores cached-only access to private sources, including links already issued. Public Torznab is a separate opt-in on the same page: it uses encrypted URL configurations, accepts optional personal announces, and reads the published catalog and known shared cache without contacting sources or debrid services.

Fresh availability can be reused between visitors and the administrator, independently for each debrid service. By default, private or unknown sources are offered only when a matching video is confirmed cached; administrator announce URLs are excluded. Missing AllDebrid observations use the administrator's recoverable probes, so a configured administrator AllDebrid account is needed to fill its cold cache. TorBox can check directly with the visitor's key without creating torrents.

Use an external HTTPS `PUBLIC_URL`. Visitors must trust the host that processes their keys and keep their encrypted link private. The existing histories retain media and safe provider diagnostics for up to seven days, without visitor identities or keys. See [Public instances](docs/PUBLIC_INSTANCES.md) for cache behavior and the AllDebrid cached-playback race limitation.

## Update

```sh
docker compose pull
docker compose up -d
```

Keep both data volumes. Interrupted collections resume from saved progress where their recovery policy allows it; deliberate manual stops remain under your control.

## Help and contributions

- [Source setup](docs/CONFIGURATION.md), [connector compatibility and Prowlarr](docs/CONNECTORS.md), and [complete configuration reference](docs/JSON_REFERENCE.md)
- [Environment settings](docs/SETTINGS.md), [operations](docs/OPERATIONS.md) and [catalog sharing](docs/SHARING.md)
- [Security and private vulnerability reporting](SECURITY.md)
- [Contributing](CONTRIBUTING.md), [development](docs/DEVELOPMENT.md) and [verification](docs/VERIFICATION.md)
- [Content rights, service terms and attribution](LEGAL.md)

## License and acknowledgements

AIOSources is available under the [MIT License](LICENSE). It builds on [Ingest](https://github.com/moodiness/ingest), including its Torznab SDK, with the original copyright and license notices preserved. Included libraries, icons and fonts retain their own licenses; see the [third-party notices](web/public/THIRD-PARTY-NOTICES.txt).

Film and series information is supplied by [TMDB](https://www.themoviedb.org). This product uses the TMDB API but is not endorsed or certified by TMDB. Its API and data have separate [usage and attribution requirements](https://developer.themoviedb.org/docs/faq), including licensing for commercial use. TMDB attribution and licenses are also accessible from **About AIOSources** in the application.

AIOSources is an independent project. Stremio, AIOStreams, AllDebrid, TorBox and TMDB names identify compatible services and do not imply affiliation or endorsement.
