# Search, history and maintenance

[Back to README](../README.md) · [Sources](CONFIGURATION.md) · [Debrid](DEBRID.md)

## Find the right page

| Page | Use it to |
| --- | --- |
| Search | Find identified films, series and books, choose an episode or file and compare releases. |
| Torrents | Search records directly as imported from your sources or remote catalogs. |
| Metadata → Identification | Review uncertain records, choose the correct film, series or book, and manage automatic identification and its history. |
| Sources | Add sources, schedule imports and inspect their history. |
| Metadata → Videos / Books | Each has its own statistics, update settings and history. |
| Debrid | Configure accounts and follow preparation, availability checks and cleanup. |
| Connections | Create Stremio or Torznab access and share catalogs. |
| Administration | Manage security, network, public access, storage and Logs. |

The settings search stays available on desktop and mobile. Select a result to open the relevant control, even inside a collapsed section.

Each settings block has its own **Save**. Saving the main settings does not apply pending changes in optional sections. Folding a section keeps its unsaved edits. Use the small **i** beside a heading or label for explanations; hover on desktop or tap on mobile.

## Search and choose a release

Search uses your imported information, including local titles and aliases. It does not contact TMDB, Open Library or your source websites for the search itself. Titles use your browser language when available, with fallbacks for missing translations.

Optional **Filters** and **Sorting** start closed. New searches include all results unless you choose a restriction. Use type and year filters to narrow titles; a series spanning the selected year can match that year. Open a title, choose a season and episode when applicable, then use release **Filters** and **Sorting** to compare versions.

Sorting can prioritize cached status, language, size, publication date, age, name or source. Several priorities apply in order. Optional language and cached-only filters restrict what is shown; leave them off to see all permitted results. Cache checks and downloads may still contact your debrid service.

**Search torrents directly**, below the title results, also finds releases without a metadata match. It is available before entering a search and keeps your query when you use it after searching. Results use the same release presentation, debrid download actions and file selection as a title page. Public access follows the same private-source restrictions.

Posters and book covers load directly from their official image services. Your instance does not save the image files.

**Download** opens a ready file in your browser. For a book with several formats or audio tracks, use **Choose file**, then select the file you want. **Add to debrid** starts preparation, with **View operation** linking to its progress in Debrid history.

## Review identification

In **Identification**, search for a record or filter its status. Open **Review**, check the suggested title and year, then choose the correct result. You can edit the title and year search if the release name is unusual.

Manual decisions take priority over automatic suggestions. An uncertain match is kept for review instead of being silently accepted. Season and episode information affects which releases appear for a selected episode.

## History and troubleshooting

Histories show recent activity first. Open a run or operation to see its related events and details. Use **First**, **Previous**, **Next** and **Latest** to navigate longer lists.

| What you see | What to do |
| --- | --- |
| Queued | Wait for an available import slot. |
| Running | Follow its progress; another scheduled run cannot use the same source at once. |
| Paused by a page or time budget | Resume it, adjust the limit or wait for an eligible scheduled continuation. |
| Rate limit / HTTP 429 | Respect the retry time and review request spacing or quotas. |
| Authentication error | Check the selected secret and the account's permissions. |
| Response could not be processed | Check whether the source returned the expected listing, rather than an error or login page. |
| Advertised total changed | Review the source's pagination policy. A changing catalog may need a less strict total policy; do not assume a rejected page completed the import. |

Historical error counts include earlier attempts and do not necessarily mean the current attempt is unhealthy. Saving a changed definition does not alter a running import; a new run uses the new settings.

**Logs** groups application activity and failures. Request histories show what the addon returned and the calls made during an operation without saving visitor credentials. When reporting a problem, include the version and relevant error text, with private data removed.

## Storage and retention

**Administration → Storage** shows which data groups occupy space, largest first. Per-source figures are estimates; they include more than the torrent fields, such as indexes, import work and retained history. Film, series and book information and shared caches are counted separately.

In **Storage → Settings**, choose what to retain in **Torrent data**. Required search, identification and playback information is always kept. Extra source fields can be selected individually; successful processed records, their field copies and published-change history are separate choices. Each option shows its relative storage and performance impact.

Field and processed-record choices apply to newly started imports. Existing imports retain their saved choices. Enabling a previously omitted field cannot fill older data automatically: reset source data and import again if you need it throughout the catalog. Disabling a field does not erase older torrent data. Published-change history can be stopped immediately without affecting imports or sharing.

**Search indexes** shows the actual allocated space of optional search accelerators. Turning one off preserves data and search behavior but can greatly slow the relevant queries. Index changes run in the background; rebuilding needs time and temporary free space. Identity, recovery and matching indexes remain protected.

**Retention overrides** lets each history group use a different age limit. Leave a limit blank to use **History retention**, or enter 0 to remove eligible history progressively. Stremio request limits and returned-result details can also be adjusted independently. Shorter retention permanently removes older history; increasing it cannot restore deleted details.

History retention defaults to seven days and is configurable. It cleans up eligible diagnostic history, not your published catalog. Some saved work for resumable imports can remain longer. Removing rows can free reusable space without immediately shrinking the PostgreSQL volume.

**Maintenance → Reset all source data** clears collected source data so it can be imported again. Read the confirmation carefully and expect sources to need another import. Source definitions and reusable Secrets are kept. Other resets are scoped to the page where they appear, such as matching or debrid caches.

Back up before resetting or deleting data you may need. See [installation and backups](SETTINGS.md#preserve-your-data).
