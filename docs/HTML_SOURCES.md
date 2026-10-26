# Configure an HTML source

[Back to README](../README.md) · [Source setup](CONFIGURATION.md)

Use the **HTML** template for a website whose listings are present in the page HTML. You need permission to collect from the site and enough knowledge of its page structure to select the right elements.

## Build the listing

1. In **Sources → Settings → Add source**, choose **HTML** and enter the listing address.
2. Set the selector for one result row. Choose an element that repeats once per record and excludes headers, adverts and navigation.
3. Select the record's title and stable identifier. Read the appropriate text or link attribute. If the identifier comes from a URL, ensure it stays the same when the title changes.
4. Add the fields available on the listing: size, publication date, categories, info hash or magnet link, and other useful details.
5. Configure pagination, including the next-page link or page number, so the importer recognizes when no further page is available.

Selectors are CSS selectors. Text and attribute extraction are configured separately. Avoid relying only on a row's position: the page can change when a new record is added.

## Detail pages

If a listing does not include an info hash or useful details, configure a detail-page link and selectors for those fields. Detail requests increase the number of calls, so choose a suitable request interval.

Keep detail links on the expected site and use the authentication method required by that site. Store credentials in **Secrets**. Reuse **Check configuration** and **Preview** after changing selectors or pagination.

## Check before collecting

Preview several records and more than one listing page. Check that titles, identifiers and sizes belong to the same record, and that the last page stops correctly. Confirm that missing optional values do not become part of another result.

Use unrestricted listings when you want all categories. Some sites expose only recent pages or a fixed number of results; scheduling every 30 minutes cannot guarantee completeness if more records arrive between imports than those pages retain.

## Limits and troubleshooting

- Pages requiring JavaScript rendering cannot be collected by the HTML template alone.
- Login challenges, CAPTCHA and anti-bot checks may prevent collection. AIOSources does not bypass them.
- A site redesign can invalidate selectors. Inspect a failed run, update the definition and preview it again.
- A successful request does not necessarily contain the expected listing: a login or error page may be returned instead.
- Respect the site's access rules and rate limits. Faster polling does not expand the history made available by the site.

For import states, pauses and recovery, see [History and troubleshooting](OPERATIONS.md#history-and-troubleshooting).
