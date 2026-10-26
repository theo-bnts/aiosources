# Shared and remote catalogs

[Back to README](../README.md) · [Sources](CONFIGURATION.md)

Share selected catalog information with another AIOSources instance, or keep a searchable local copy of a catalog shared with you. Each side chooses which optional data it allows.

## Share a catalog

1. Configure an external HTTPS address in `PUBLIC_URL`.
2. Open **Connections → Shared catalogs → Settings → Create share** and give the recipient a recognizable name.
3. In **Filters**, keep all sources or select specific ones.
4. Choose the published torrent fields and optional groups under **Shared data**. Expand **Advanced** if you need expiration or request limits.
5. Create the share, save its generated password and enable it. Send its URL and password privately to the recipient.

New shares start disabled. Replacing a password or disabling a share stops future access; it cannot erase copies a recipient has already downloaded. Give separate recipients separate shares.

## Import a shared catalog

1. Open **Sources → Remote catalogs → Settings → Add remote catalog**.
2. Enter the HTTPS URL supplied by the owner and add its sharing password through **Secrets**.
3. Select the optional **Shared data** you want to receive and choose an automatic update interval.
4. Save and enable the connection, then choose **Run manual import** for an immediate sync.

The first sync imports the available catalog; later syncs fetch changes. **Full resync** is available in the same manual-import menu when needed. Your existing published copy stays available until the new import succeeds.

## What can be shared?

| Data | What you receive |
| --- | --- |
| Published torrent fields | The owner's selected titles, hashes, sizes, categories and publication dates. |
| Identifiers and episodes | Available media identifiers, years and season/episode information. |
| Languages | Available audio and subtitle language labels. |
| Audio and video information | Available quality tags, duration, bitrate and related details. |
| File information | Available relative filenames, folders, sizes and file positions. |
| Film and series information | Titles, translations, identifiers and years for the permitted catalog. |
| Debrid cache observations | Recent shareable cached-availability information for permitted torrents, without personal download links or account details. |

Optional groups are off by default and must be allowed by both sender and receiver. The connection's **Data received** shows what the last successful sync included. An older instance may provide only the basic torrent catalog.

Remote identification decisions and manual reviews are not imported: identification runs locally. Film and series information is combined by its identifiers without duplicating the same work; directly acquired local information takes priority. Cached observations expire normally and do not provide service access without your own credentials. Import extra information only from an owner you trust.

**Never shared:** API keys, passwords, announce URLs, personal playback links, source definitions, account settings and remote history. Receiving a catalog does not give its owner access to your administration.

## Follow or remove a connection

**Statistics** shows imported and pending records. **History** contains remote imports separately from native source imports. **View imported data** opens **Torrents** filtered to that catalog.

**Delete** removes the remote connection, its local torrent copy, import progress, history and local identifications. Reusable Secrets, metadata and debrid caches remain available. Finish or pause active work before deleting.

Shared information remains subject to its owner's permissions and terms. Only share data you are authorized to redistribute.
