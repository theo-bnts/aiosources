# AIOSources

**Your sources. One catalog. Ready for Stremio, AIOStreams and Torznab.**

AIOSources brings your own sources together so you can find a film or series, compare available versions and watch or download it through your preferred app. Imports, identification and availability checks run automatically once configured; the web interface lets you search, review results and keep control of what you share.

AIOSources is open source and self-hosted. It comes without sources or content: connect services you are authorized to use and share only content you have the right to share.

[Get started](#get-started) · [Connect your apps](#connect-your-apps) · [Public access](#offer-public-access) · [Help](#help-and-contributions)

## What you get

- **One place to search.** Find films and series by clean titles, in your browser language when available. Compare releases by size, with filters for year, language and cached availability.
- **Your choice of sources.** Add compatible sources through a visual editor or your own JSON definitions. Preview their results and choose when they update.
- **Better identification.** Link releases to film and series information, including seasons and episodes. Review uncertain matches and correct them yourself.
- **Watch or download.** Use Stremio or AIOStreams, connect Torznab-compatible apps, or download from the web interface with AllDebrid or TorBox.
- **Connections you control.** Create separate app connections with their own filters. Share a selected catalog with another instance, or optionally open a visitor page.
- **Visibility without guesswork.** See what was imported, what was returned to Stremio, which operations need attention and where storage is used.

Already using someone else's public instance? Start with [the visitor guide](docs/PUBLIC_INSTANCES.md#use-a-public-instance). You do not need to host AIOSources or create an administrator account.

## Get started

You need Docker Compose and a source connection you are authorized to use. TMDB information is needed for film and series identification. AllDebrid and TorBox accounts are optional for administrators and are required for debrid playback.

### 1. Start your instance

Save the following as `compose.yaml`. Replace `XXX.XXX.XXX.XXX` with your server address and `XXX` with an administrator password of 12–72 bytes. Change the example database password in **both** `POSTGRES_PASSWORD` and `DATABASE_URL`.

This example is for a trusted local network. For internet access, use HTTPS, set `PUBLIC_URL` to the external HTTPS address and protect access to administration. See [deployment settings](docs/SETTINGS.md) for other setups.

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

Start it:

```sh
docker compose up -d
```

Open `http://YOUR_SERVER_IP:7000` and sign in with your administrator password. Keep both data volumes: they contain your catalog, configuration and encrypted credentials. Images support Linux `amd64` and `arm64`.

### 2. Add your sources

Open **Sources → Settings → Add source**. Use the visual editor or import your own source JSON. Select any required credential through **Secrets**, check the configuration and save.

Enable the source, then use **Run manual import → Preview** to check a sample before publishing records. When it looks right, start an import and choose an automatic update in its **Schedules** section. Saving a definition does not contact the source. Supported connections are Torznab and HTTP/JSON; HTML pages and ordinary RSS feeds need a compatible bridge. See [source setup](docs/CONFIGURATION.md) and [supported connectors](docs/CONNECTORS.md).

### 3. Set up identification and playback

In **Metadata → Settings**, add your own [TMDB API read access token](https://www.themoviedb.org/settings/api). Enable automatic information updates and torrent identification. Review uncertain results in **Identification**; you can search for the correct title and choose a match.

For debrid playback, add your AllDebrid or TorBox key in **Debrid → Settings**. Availability checks show which versions are already cached. AllDebrid checks can create temporary account entries; AIOSources only cleans up entries it has confirmed it created.

If a service needs a different outgoing connection, open **Administration → Network**. Add an existing WARP, VPN or other proxy, test it, then select it for that service in **Routing**. You can also route all outgoing connections or individual sources through it. See [proxy setup](docs/NETWORK.md).

### 4. Search and connect an app

Open **Search**, find a title and compare its releases, or [connect Stremio, AIOStreams or a Torznab client](#connect-your-apps).

## Find the version you want

**Search** shows identified films and series, with local titles and aliases. Searching the catalog does not query your sources or TMDB. For a series, choose a season and episode to see corresponding releases and season packs.

Results are largest first, and **Cached results only** is enabled by default. Turn it off to include versions that need preparation. The language filter includes your browser language, MULTI and unknown audio by default; disable it to see every language. An optional year filter also matches a series spanning that year.

Choose **Download** to start a ready file in your browser. Choose **Add to debrid** to prepare an uncached version, then **View operation** to follow progress in **Debrid → History**. Downloads from the web interface use your instance's bandwidth without saving the file on its disk. Stremio uses direct provider playback links.

**Torrent search** lists individual imported records, including unidentified releases. Use title fragments, source IDs or complete or partial info hashes. **Identification** uses the same search syntax when reviewing matches. These pages help explain why a release appears, is missing or needs a correction.

## Connect your apps

### Stremio and AIOStreams

1. Open **Connections → Stremio → Settings** and create a connection.
2. Choose any optional **Filters**: media types, sources, playback services, languages and cached-only results. Expiration and limits are in **Advanced**.
3. Save the generated manifest URL securely and enable the connection.
4. Install that URL in Stremio or add it as a custom addon in AIOStreams. When AIOSources handles debrid playback, leave AIOStreams' **Service Wrap** and **Reconfigure Service** disabled for this addon.

Create as many connections as you need. Each has independent settings and can be disabled or revoked. Saved filter changes apply to its existing URL without reinstalling. New connections include all sources by default.

Keep installation URLs private: they grant the access you configured. Private-source uncached playback needs an announce URL; without one, those sources are offered only when the requested video is confirmed cached. **Cached results only** excludes direct torrent playback and administrator announces. See [public access and privacy](docs/PUBLIC_INSTANCES.md) for the visitor rules.

### Torznab-compatible apps

Open **Connections → Torznab → Settings**, create an access and select its filters. Enable it, then copy the Torznab URL and generated API key into your client. In Prowlarr, choose **Generic Torznab**.

Torznab searches your imported catalog without contacting your sources or TMDB. Each access can select several sources and media types, and additional URL filters can narrow it for different clients. Private-source magnets do not include the administrator's announce URL; recipients need their own authorized source access. See the [Torznab guide](docs/TORZNAB.md).

## Exchange catalogs with another instance

**Connections → Shared catalogs** lets you give each recipient a separate, revocable, read-only share. Choose the sources and fields they may receive, save the generated sharing password and enable the share.

**Sources → Remote catalogs** imports a catalog shared with you. Add its URL and sharing password, enable the connection and choose an update interval. **Run manual import** also offers **Sync changes** or **Full resync**. Your current copy stays available until a new import completes successfully.

Only permitted torrent information is transferred. Accounts, source credentials, announce URLs, debrid caches and matching decisions stay with their own instance; imported records are identified locally. See [sharing and remote catalogs](docs/SHARING.md).

## Offer public access

An administrator can enable a visitor homepage in **Connections → Public access**. Visitors choose Stremio or, when allowed, Torznab. Administration stays protected.

For Stremio, visitors supply at least one personal AllDebrid or TorBox key. Their encrypted installation URL contains their configuration; AIOSources does not save visitor keys or announce URLs. Fresh cached-availability observations can be reused across users, independently for each debrid service.

The administrator chooses whether to disclose source names and allow visitors to supply their own announce URLs. Administrator keys and announces are never provided to visitors. Public links stop working when public access is disabled.

Visitors must trust the host processing their keys and keep their installation URL private. Activity histories retain media requests and safe diagnostics for seven days. Read the [public instance guide](docs/PUBLIC_INSTANCES.md) before enabling this feature, including provider-specific limitations.

## Keep track of your instance

- **Statistics** show imported and identified records, pending work and service availability.
- **History** follows each import, identification or playback operation. Open a specific run for its details.
- **Logs** help explain errors and interruptions.
- **Storage** shows where space is used. **Storage saving**, enabled by default, keeps the fields used by AIOSources and compatible apps instead of unnecessary source details.
- **Security** manages encrypted secrets, two-step verification and active sessions.

Diagnostic history is cleaned up after seven days. Published records and saved progress for unfinished imports remain available. A cancelled or paused import may therefore retain preparation data; retained history is not the same as the live catalog. See [storage and operations](docs/OPERATIONS.md).

Changing the storage mode affects new imports. If you want to import everything again, **Storage → Settings → Maintenance → Reset all source data** removes imported data and history while retaining source definitions, secrets, schedules, TMDB information and debrid caches. Review the confirmation carefully: this cannot be undone.

## Update or try a preview

The example follows the **stable** channel:

```sh
docker compose pull
docker compose up -d
```

Keep both volumes and their encryption key. Interrupted work resumes from its saved progress when eligible; manually paused work remains under your control.

To test upcoming changes, replace `:stable` with `:preview` in the image name, then run the same commands. Previews may contain bugs. Test against separate data if you need a safe return to an older version: a database upgraded by a preview may not work with an older image. [Releases](https://github.com/theo-bnts/aiosources/releases) include the exact image for each build.

**Coming from V1?** Back up your V1 volumes and keep them separately. V2 needs fresh application and database volumes; V1 data is not migrated automatically. See [first deployment and upgrades](docs/SETTINGS.md#first-aiosources-deployment).

## Optional environment settings

Most settings and credentials are managed from the interface. The complete application environment list is below; deployment and development options are documented in [Deployment and settings](docs/SETTINGS.md).

<details>
<summary>All supported application environment variables</summary>

| Variable | Image default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Required | PostgreSQL connection URL. |
| `BIND` | `0.0.0.0:7000` | Address and port on which the application listens. |
| `PUBLIC_URL` | Unset | Exact external HTTP(S) origin, for example `https://sources.example.com`. Setting this does not enable HTTPS; HTTPS is required for catalog sharing and public access. |
| `ADMIN_PASSWORD` | Generated when omitted | Administration password, 12–72 bytes. An omitted password is saved as `/data/admin-password`; it can be read locally with `/aiosource password`. |
| `DATA_DIR` | `/data` | Private application state, including the encryption key. Preserve this storage when updating. |
| `PROVIDERS_DIR` | `/data/providers` | Private JSON source definitions. |
| `MASTER_KEY` | Generated `vault.key` in `DATA_DIR` | Optional existing 32-byte encryption key, encoded in base64 or hexadecimal. Never change the key of an existing installation. |

Native installations use `.ingest`, `providers` and `127.0.0.1:8080` for the state directory, source directory and listen address. The image uses the defaults above. `PUBLIC_URL` declares an address; it does not enable HTTPS itself.

TMDB, debrid and source keys belong in **Security → Secrets**. They have no dedicated deployment variables. Import concurrency defaults to **5**, adjustable from **1 to 32** in Sources settings.

For an older deployment using prefixed variables, remove `INGEST_` from those names before updating. Preserve every value, path and volume; `DATABASE_URL` keeps its name. Obsolete deployment names are rejected to prevent accidentally creating different credentials.

</details>

## Help and contributions

- [Discord support — French only](https://discord.gg/h3sja5XN8b)
- [Report a bug or suggest an improvement](https://github.com/theo-bnts/aiosources/issues)
- [Source setup](docs/CONFIGURATION.md) · [Connector compatibility](docs/CONNECTORS.md) · [Sharing](docs/SHARING.md) · [Public instances](docs/PUBLIC_INSTANCES.md)
- [Deployment](docs/SETTINGS.md) · [Proxy setup](docs/NETWORK.md) · [Operations and troubleshooting](docs/OPERATIONS.md)
- [Contributing](CONTRIBUTING.md) · [Development](docs/DEVELOPMENT.md) · [JSON reference](docs/JSON_REFERENCE.md)
- [Report a security issue privately](SECURITY.md) · [Content rights and service terms](LEGAL.md)
- [Support AIOSources on GitHub Sponsors](https://github.com/sponsors/theo-bnts)

## License and acknowledgements

AIOSources is available under the [MIT License](LICENSE). It builds on [Ingest](https://github.com/moodiness/ingest), including its Torznab SDK, with the original copyright and license notices preserved. Included libraries, icons and fonts retain their own licenses; see the [third-party notices](web/public/THIRD-PARTY-NOTICES.txt).

Film and series information is supplied by [TMDB](https://www.themoviedb.org). This product uses the TMDB API but is not endorsed or certified by TMDB. Its API and data have separate [usage and attribution requirements](https://developer.themoviedb.org/docs/faq), including licensing for commercial use. TMDB attribution and licenses are also accessible from **About AIOSources** in the application.

AIOSources is an independent project. Stremio, AIOStreams, AllDebrid, TorBox and TMDB names identify compatible services and do not imply affiliation or endorsement.
