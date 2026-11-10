# Games

[Back to README](../README.md) · [Search and identification](OPERATIONS.md)

Connect IGDB to identify games already present in your source catalog. Games have their own **Statistics**, **Settings** and **History** under **Metadata → Games**. Search and review use the information imported on your instance; your search terms are not sent to IGDB.

## Set up

1. Create a [Twitch developer application](https://dev.twitch.tv/console/apps) with the **Confidential** client type. IGDB does not use its OAuth redirect URL; `http://localhost/` is sufficient.
2. Open **Metadata → Games → Settings** and enter its **Twitch client ID**.
3. Use **Add Twitch client secret** to save the secret in the same encrypted Secrets store as your other service credentials.
4. Enable **Automatically update game information** and save. You can also use **Manual actions → Update game information**.
5. Follow progress in **History**. Interrupted imports continue from saved progress; existing source records do not need to be downloaded again.

The initial update imports the available IGDB game catalog, including alternative titles, known release years, platforms and cover references. Cover images load directly from IGDB and are not stored on your disk.

## Find and identify games

Use **Search → Filters → Type → Games** to find identified games, then compare source releases and download with a configured debrid service. Games are also available through private and public Torznab connections, using the **Games** media filter and standard Console / PC Games categories. Public access uses the same private-source and cached-content restrictions as other media. Stremio connections remain dedicated to playable videos.

Automatic identification requires game-specific evidence and an exact title or alternative title. Incompatible platforms, conflicting identifiers, numbered sequels, DLC and ambiguous homonyms are not silently treated as the same game. Use **Metadata → Identification** to choose or correct a result. Source categories and optional `igdb_id` / `platform` fields help establish the correct type; an unknown native category number alone is not enough.

A game may appear in IGDB before a matching source release is available. Search shows identified releases available on your instance. Use **Search torrents directly** or **Sources → Files** to inspect unrecognized source records.

## Optional settings

- **Automatic updates:** the default interval is 15 minutes. A complete reconciliation runs every 7 days to refresh related information and detect records removed from IGDB. Both intervals are adjustable.
- **Request limits:** requests are paced at 3 per second by default, within IGDB's documented maximum of 4 per second per application. Reduce this if another application uses the same Twitch credentials.
- **Network → Outgoing → Routing → IGDB:** choose a proxy for both authentication and information requests if needed.
- **Maintenance → Reset game information:** removes game information and its related identifications, cache observations and operation history. Video and book information, imported source records, settings and debrid account files are preserved. Automatic updates can import the catalog again.

Incremental updates follow IGDB's reported game update timestamps. Changes to related records and deletions are covered by full reconciliation; they may therefore take until the next reconciliation to appear. Titles, dates, platforms and regional covers depend on what IGDB provides. An unavailable or rate-limited API preserves the committed catalog and progress for a later retry.

IGDB's [API documentation](https://api-docs.igdb.com/) explains account requirements, limits and service terms. Use your own application credentials.
