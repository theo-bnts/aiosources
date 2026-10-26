# Add sources and schedule imports

[Back to README](../README.md) · [Search and history](OPERATIONS.md)

A source is a service or website you are authorized to collect from. AIOSources ships without source definitions. You can create one visually or import a JSON supplied by someone you trust.

## Choose a template

Open **Sources → Settings → Add source**.

| Template | Use it for |
| --- | --- |
| Torznab | A compatible indexer API or a Torznab feed provided by another application. |
| HTML | A website with listings that can be read directly from its HTML. |
| JSON | An API that returns structured JSON. |
| UNIT3D | A compatible UNIT3D API, with a starting configuration to adapt to that site. |

Enter a recognizable display name and the address reachable from AIOSources. Keep the source ID when editing an existing source. Renaming the display name is safe; creating a different ID creates a different source.

## Configure and check the source

1. Choose how it signs in. Select a credential from **Secrets**, or add one from the credential field. Public sources may need no authentication.
2. Configure the listing and the fields to read. Records need a stable source ID and title; an info hash is needed for torrent playback. Keep useful identifiers, categories, size, dates, languages and file details when the source supplies them.
3. Configure pagination so the importer can move beyond the first page and recognize the end. Use the site's documented behavior; an advertised total can change while a catalog is being collected.
4. Set request spacing and a timeout appropriate for the service. Leave categories unrestricted if you want every category the listing exposes.
5. Choose **Check configuration**, then **Save**. This checks the definition without contacting the source.
6. Enable the source and choose **Run manual import → Preview**. Check the returned fields and pagination before starting a complete import.

The HTML template has a [dedicated setup guide](HTML_SOURCES.md). A site can restrict its accessible history even when the configuration is correct; an empty category filter does not remove that restriction.

## Private sources

Enable **Private source** if downloading requires a personal announce URL. Store the announce URL through Secrets. Without an announce URL, uncached private-source torrents are not offered for direct downloading. Cached playback still depends on your debrid service.

Public visitors never receive your announce URL. A visitor can supply their own only if you enable that option in [Public access](PUBLIC_INSTANCES.md).

## Choose automatic updates

In the source's **Schedules** section, choose an interval or a supported schedule and enable it. Request spacing controls how quickly one import contacts the source; the schedule controls when another import starts.

- **Incremental** collects updates. The first import builds a complete starting catalog.
- **Full** rebuilds the source catalog and publishes it when successful. Existing published records stay available while it runs.
- **Source details**, when configured, refreshes additional information for known records.

Use **Run manual import** on a source card for an immediate run. **Preview** does not publish records. Page and duration limits can pause a long import; resuming continues its saved work. A scheduled continuation can pick up eligible work on a later tick. Avoid unnecessary rebuilds when incremental updates meet your needs.

## JSON import and export

The **JSON** editor offers import and export controls. Keep credentials in Secrets, referenced by name, rather than writing keys or private URLs into a file you intend to share. Review every definition before importing it.

JSON sources use `"adapter": "json"`. Older `"adapter": "http_json"` definitions remain supported and are renamed automatically when possible, without resetting imported data or forcing a full download.

Saving a changed source does not alter a run already in progress. Start a new run to use the new configuration. If another administrator saved changes first, compare the server version before replacing it.

## Manage a source

Source cards show its schedule and recent run, with **View imported data** opening its records in **Torrents**. Use **History** to inspect a particular import and its related events.

**Sources → Settings → Advanced** contains less common import and recovery controls. **Delete** removes the source and its associated imported data and history; reusable Secrets remain available. Pause or finish active work before deleting.
