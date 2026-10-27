# Connect a Torznab application

[Back to README](../README.md) · [Public instances](PUBLIC_INSTANCES.md)

Torznab lets applications such as Prowlarr search the torrent records already imported into AIOSources. Your download client handles downloading; this connection does not provide a ready debrid video link.

## Create a private connection

1. Open **Connections → Torznab** and choose **Create access**.
2. Give the connection a name. Leave **Filters → Sources** on all sources, or select the sources and media types you want.
3. Optionally set cache-service, language and result filters or sorting priorities. **Advanced** contains request limits and expiration.
4. Create the access and save the generated API key when shown. Enable the connection.
5. Add a generic Torznab indexer in your client, using the supplied Torznab URL and API key.

The client must be able to reach that address. Trusted local networks can use HTTP; use HTTPS over the internet. Create separate connections when applications need different filters or permissions. You can disable, replace or delete each access independently.

## Choose what is returned

The saved connection defines the sources and media types it can access. Standard category filters in a client's search can narrow those results further. Omitted source filters include sources added later.

Availability filters use the selected cache services. Direct torrent-only connections skip debrid availability checks. Filtering to cached results does not turn Torznab into a debrid download service: the receiving client still needs a usable torrent or magnet and its own download setup.

Books are available alongside films and series. Choose **Books**, **Ebooks** or **Audiobooks** in the media filters. Compatible clients can search by title, author or ISBN and use standard Books, EBook, Comics and Audio/Audiobook categories. Book identification must be set up in [Metadata](BOOKS.md).

Private-source downloads need an authorized announce URL. Treat a private Torznab key as a credential and give it only to clients you trust. Catalog-sharing passwords and Stremio links are separate and cannot replace it.

## Public Torznab

If an instance offers public Torznab, open **Connections → Torznab** on its visitor page and generate your configuration. The connection options are carried in the generated access; visitor API keys are not saved in the server database.

When the administrator allows source exposure, optional private-source announce URLs are entered under **Advanced**. The administrator's announces are never supplied to visitors. Public Torznab accepts no debrid account keys; see the [public-instance guide](PUBLIC_INSTANCES.md).

## Troubleshooting

- **Client cannot connect:** check the address from the client's network, enable the access and confirm the correct API key.
- **No results:** check the saved source/media filters, client categories and whether the source has completed its first import.
- **Request limit reached:** wait for the retry time or adjust that connection's limits.
- **Expired or replaced key:** copy the current details or create a new access.
