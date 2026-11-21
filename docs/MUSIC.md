# Music

[Back to README](../README.md) · [Search and identification](OPERATIONS.md) · [Torznab](TORZNAB.md)

AIOSources can identify music albums, EPs and singles already present in your imported sources using **MusicBrainz** and **Cover Art Archive**, then offer them through Search and Torznab (including Lidarr and Prowlarr). Music has its own **Statistics**, **Settings** and **History** under **Metadata → Catalogs → Music**. Search, matching and review run entirely against the local PostgreSQL catalog; your torrent titles and search terms are never sent to MusicBrainz.

## Set up

1. Open **Metadata → Catalogs → Music → Settings**.
2. Optionally adjust **Catalog filters**:
   - **Official releases only** (enabled by default): keeps release groups with at least one official release and excludes bootleg-only or promo-only entries.
   - **Exclude audiobooks and spoken word** (enabled by default): skips Audiobook, Audio drama and Spokenword entries so audiobooks remain handled by [Books (Open Library)](BOOKS.md).
   - **Import Cover Art Archive index** (enabled by default): imports the lightweight Cover Art Archive metadata dump to record front-cover availability. Cover images are never downloaded to disk and stay remote.
   - **Release types**: defaults to **Albums**, **EPs** and **Singles**.
   - **Filter by release language**: optional, disabled by default.
3. Choose your update mode:
   - **Without a token (default — CC0 public domain):** AIOSources automatically downloads MusicBrainz's twice-weekly public full exports (`fullexport/LATEST`: `mbdump.tar.bz2` and optionally `mbdump-cover-art-archive.tar.bz2`). No account or API key is required.
   - **With an optional MetaBrainz Live Data Feed token (hourly updates — CC BY-NC-SA 3.0):** create a free personal token in your [MetaBrainz profile](https://metabrainz.org/profile), save it via **Add MetaBrainz token** (encrypted in Secrets), and save. After the initial dump completes, AIOSources downloads hourly global `dbmirror2` replication packets (`replication-<SEQ>-v2.tar.bz2`) and re-projects only the affected release groups. Because replication packets are global, **nothing about your local torrents is ever sent to MetaBrainz**.
4. Enable **Automatically update music information** and save, or run **Manual actions → Update music information**.

Only core catalog tables are imported (artists, artist credits, aliases, release groups, releases, labels, catalog numbers, barcodes, formats, Discogs/Wikidata URLs, redirects, and Cover Art Archive front-cover flags). Individual tracklists and recordings (~100M rows) and genres are deliberately skipped to keep PostgreSQL storage compact.

## Long pauses and automatic recovery

If music updates are paused or disabled for weeks or months (for example 3 months):

- Hourly replication packets older than MetaBrainz's retention window return `404`/`410`, or fall more than 7 days / 168 packets behind.
- Twice a year (May and November), MusicBrainz increments `SCHEMA_SEQUENCE` for database schema changes.

When continuity is lost or a schema upgrade occurs, AIOSources automatically switches to **full export recovery** from `fullexport/LATEST`, keeping your existing searchable music catalog intact while rebuilding the mirror, and then resumes hourly Live Data Feed replication from the snapshot's new sequence. If MusicBrainz introduces a future `SCHEMA_SEQUENCE` that your installed version of AIOSources does not yet support, updates pause safely (**fail-closed**) and preserve your existing music catalog until you upgrade AIOSources.

## Find and identify music

Use **Search → Filters → Type → Music** to find identified releases, compare Lossless/MP3 versions, inspect MusicBrainz / Discogs / Wikidata links, and select individual audio files or full archives through your debrid service.

For Torznab clients such as **Lidarr** or **Prowlarr**, enable the **Music** media filter on a private or public Torznab connection. AIOSources advertises `audio-search` (`t=music`) with `artist`, `album`, `label`, `track`, `year` and `mbid` parameters under category `3000 Audio` (`3010 MP3`, `3040 Lossless`, `3050 Other`), while keeping `3030 Audiobook` separated for Open Library.

Automatic identification matches verified identifiers (`mbid`, `discogs_id`, `wikidata_id`, `barcode`, `catno`) or exact artist + album (+ year) evidence. Discographies, box sets and multi-album collections are kept for manual review in **Metadata → Identification**.

## Optional settings and maintenance

- **Automatic updates:** defaults to every **60 minutes** (minimum 15 minutes). Without a Live Data Feed token, full export checks are throttled to at most once every 6 hours.
- **Network → Outgoing → Routing → MusicBrainz:** route full exports and Live Data Feed packets through a configured proxy if needed.
- **Maintenance → Reset music information:** removes MusicBrainz/Cover Art Archive data, related automatic and manual music identifications, cache observations and music operation history. Videos, games, books, source records, settings and debrid files remain untouched.

## Licensing

- **MusicBrainz core full exports (`mbdump.tar.bz2`):** dedicated to the public domain under **CC0 1.0**.
- **MetaBrainz Live Data Feed & Cover Art Archive metadata (`mbdump-cover-art-archive.tar.bz2`):** provided for non-commercial use under **CC BY-NC-SA 3.0** (commercial deployments require a [MetaBrainz supporter license](https://metabrainz.org/supporters)).
- **Cover art images:** loaded directly from `https://coverartarchive.org` (hosted by the Internet Archive) in the user's browser; AIOSources stores no cover images. See [Usage and service terms](../LEGAL.md).
