<p align="center"><img src="web/public/logo.png" alt="AIOSources logo" width="112" height="112"></p>

# AIOSources

**Your sources, together. Find a title, choose a version and watch or download it.**

AIOSources brings your own source catalogs into one searchable library. Connect it to Stremio, Nuvio, AIOStreams or a Torznab-compatible application, or use its web interface. Once configured, imports, film and series identification, and availability checks can run automatically.

AIOSources is open source and self-hosted. It includes no sources or media: connect services you are authorized to use and share only information you have the right to share.

[Install](#install-your-instance) · [First steps](#first-steps) · [Connect your apps](#connect-your-apps) · [Documentation](#documentation) · [Help](#help-and-support)

## What you can do

- **Find the right version.** Search films and series by their familiar titles, choose an episode and compare releases. Filter by year, language, source or cached availability.
- **Connect your own sources.** Use Torznab, HTML, JSON or UNIT3D templates. Configure them visually or import your own JSON, preview the results and schedule updates.
- **Watch and download.** Connect your player, download a ready file from the browser or add a release to your debrid account and follow its preparation.
- **Choose your debrid services.** AllDebrid, Debrid-Link, Deepbrid, Premiumize, Real-Debrid and TorBox are supported. Availability features vary by service.
- **Tailor each connection.** Create several Stremio or Torznab connections with different sources, media types, services, filters and sorting priorities.
- **Share a catalog or open a public instance.** Exchange catalogs with other instances, or let visitors use their own accounts through Search, Stremio and Torznab.
- **Keep control.** Review uncertain identifications, see request history, understand storage use and find any setting from the search bar on desktop or mobile.

Using someone else's public instance? Follow the [visitor guide](docs/PUBLIC_INSTANCES.md#use-a-public-instance). You do not need Docker or an AIOSources administrator account.

## Install your instance

### Requirements

- A server or computer running Docker with Docker Compose. Linux `amd64` and `arm64` images are available.
- Access to compatible sources you are authorized to use. Source configurations are supplied by you.
- A [TMDB API read access token](https://www.themoviedb.org/settings/api) for film and series identification.
- Optionally, an account with a supported debrid service for debrid playback and downloads.
- An HTTPS address for use over the internet. HTTP is suitable for a trusted local network.

### Start with Docker Compose

Save this as `compose.yaml`. Replace `XXX.XXX.XXX.XXX` with your server address and `XXX` with an administrator password of 12–72 bytes. Change the database password in **both** `POSTGRES_PASSWORD` and `DATABASE_URL`.

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
      - aiosources # Also connect AIOStreams to this network if needed.
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

Open `http://YOUR_SERVER_IP:7000` and sign in with your administrator password. Keep both volumes: they contain your settings, catalog and credentials. For internet access, configure HTTPS and set `PUBLIC_URL` to that external address; changing this value does not enable HTTPS by itself. See [installation settings](docs/SETTINGS.md).

## First steps

1. **Add a source** in **Sources → Settings**. Choose a template or import your JSON, select its credential from Secrets, and save. Enable the source and use **Run manual import → Preview** to check a sample, then start an import. Choose automatic updates in the source's **Schedules** section.
2. **Set up film and series information** in **Metadata → Settings** with your TMDB token. Enable automatic information updates and identification. Use **Identification** to review uncertain matches or choose the correct title yourself.
3. **Add your debrid keys** in **Debrid → Settings** if you want debrid playback or browser downloads. Only configured services are offered for your private connections.
4. **Open Search** to find a title, or create a connection for your preferred application below.

A new source becomes available after its first successful complete import. A paused import can usually resume; its history explains whether it is waiting for a limit, a source response or an action from you.

## Connect your apps

### Stremio, Nuvio and AIOStreams

1. Open **Connections → Stremio → Settings** and choose **Create access**.
2. Give the connection a name. Optionally choose **Filters**, up to three **Sorting** priorities, or an expiration date and request limits under **Advanced**.
3. Save the installation link when shown, then enable the connection.
4. Install the manifest URL as a custom addon in Stremio or Nuvio, or add it as a custom addon in AIOStreams. Use an address reachable by that application.

When AIOSources provides debrid playback, leave **Service Wrap** and **Reconfigure Service** disabled for this addon in AIOStreams. Keep installation links private: they grant access without your administrator password. You can disable, replace or delete each connection independently.

### Torznab-compatible applications

Open **Connections → Torznab**, create and enable an access, then copy its URL and API key into your client. Configure the sources, media types and optional filters for that connection. Your download client handles downloading. See the [Torznab guide](docs/TORZNAB.md).

## Search, share and manage

**Search** finds identified films and series. Open a title to compare releases, select a ready download or add one to your debrid service. **Torrents** searches imported source records directly. Both search your local catalog; they do not send your search text to TMDB or your sources.

**Connections → Shared catalogs** lets you grant another instance access to selected data. **Sources → Remote catalogs** imports a catalog shared with you. Optional shared details, film and series information, and reusable cache observations require permission on both sides. Credentials and private announce URLs are never included. See [catalog sharing](docs/SHARING.md).

**Administration → Public access** lets you offer Search, Stremio and Torznab independently. Visitors use their own credentials; they cannot open administration. Their keys and optional personal announce URLs are remembered in their browser, without being saved by AIOSources on the server. See [public instances](docs/PUBLIC_INSTANCES.md) for privacy and hosting choices.

Use **History** and **Logs** to follow work, **Storage** to see what takes space, and **Security** for secrets, two-step verification and sessions. **Network** manages trusted incoming proxies and outgoing WARP or compatible proxies. The settings search takes you directly to a control, including options inside **Advanced**.

## Update or try a preview

The example uses `:stable`, the recommended channel. To update:

```sh
docker compose pull
docker compose up -d
```

Back up both volumes before updating. Keep them when recreating containers; `docker compose down -v` deletes them.

To test a prerelease, replace `:stable` with `:preview` and run the same commands. Preview builds may contain unfinished changes. A `:sha-FULL_COMMIT_SHA` image pins an exact build. Back up before switching channels: an older image may not accept data already upgraded by a newer version.

**Moving from V1?** Back up your V1 volume and start V2 with fresh volumes. V1 data cannot be used directly by V2. Keep the original backup until you are satisfied with the migration.

## Environment settings

Most options are configured in the application. These are the supported AIOSources environment variables; source, TMDB and debrid keys belong in **Secrets**.

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | PostgreSQL connection address; required. |
| `PUBLIC_URL` | Address used by your apps and shared links, including scheme and port when needed. |
| `ADMIN_PASSWORD` | Administrator sign-in password. Set one explicitly for a Compose installation. |
| `DATA_DIR` | Persistent application data; `/data` in the image. |
| `PROVIDERS_DIR` | Source configuration folder; `/data/providers` in the image. |
| `BIND` | Listening address; `0.0.0.0:7000` in the image. |
| `MASTER_KEY` | Optional encryption key for deployments that manage their own key. Preserve the existing key when updating. |

See [installation settings](docs/SETTINGS.md) before changing paths, passwords or encryption keys.

## Documentation

- [Sources and schedules](docs/CONFIGURATION.md) · [HTML source setup](docs/HTML_SOURCES.md)
- [Search, identification, history and storage](docs/OPERATIONS.md)
- [Debrid services and downloads](docs/DEBRID.md)
- [Stremio, Nuvio and public instances](docs/PUBLIC_INSTANCES.md) · [Torznab](docs/TORZNAB.md)
- [Shared and remote catalogs](docs/SHARING.md)
- [Network and proxies](docs/NETWORK.md) · [Installation and updates](docs/SETTINGS.md)

## Help and support

- [GitHub issues](https://github.com/theo-bnts/aiosources/issues) for reproducible bugs and suggestions. Remove keys, private links and personal data from reports.
- [Discord](https://discord.gg/h3sja5XN8b) for community help **in French only**.
- [GitHub Sponsors](https://github.com/sponsors/theo-bnts) to support the project.

Contributions are welcome: see [Contributing](CONTRIBUTING.md). Report security issues [privately](SECURITY.md).

## License and acknowledgements

AIOSources is distributed under the [MIT license](LICENSE). Use it only with content and services you are authorized to access; see [usage and service terms](LEGAL.md).

Built on [Ingest](https://github.com/moodiness/ingest). Original copyrights and [third-party license notices](web/public/THIRD-PARTY-NOTICES.txt) are preserved.

This product uses the TMDB API but is not endorsed or certified by TMDB. TMDB branding and data remain subject to their [terms](https://developer.themoviedb.org/docs/faq).
