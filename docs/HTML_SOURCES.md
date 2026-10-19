# Read a website directly

[Source setup](CONFIGURATION.md) · [Source compatibility](CONNECTORS.md) · [JSON reference](JSON_REFERENCE.md)

Choose the **HTML** template in **Sources → Settings → Add source** to import a website without an API or bridge. You configure how to read its pages; AIOSources then uses the usual imports, schedules, histories, identification and app connections.

No website definitions are bundled. Use a source you are authorized to access.

## Set up a source

1. **Identity:** choose its display name and a stable source identifier.
2. **Connection:** enter the listing URL. If needed, select an authentication secret. A session cookie belongs in **Secrets**, using **Cookie** authentication; never paste it into the definition. Existing proxy routing and request limits apply.
3. **Pagination:** choose **Follow next-page links**, or page/offset parameters if the website uses them. Specify the enabled next-link selector. For empty listings, configure a marker displayed only when no records exist.
4. **Source fields:** identify the listing container, each record, and its stable ID. Map the title and any available hash, size, date, categories or other useful fields.
5. Select **Check configuration**, save and enable the source, then use **Run manual import → Preview**. Inspect the records before importing or configuring its schedule.

**Check configuration** validates the selectors and settings without contacting the website. **Preview** reads real pages without publishing them. Check more than the first page before rebuilding an existing catalog.

## Find the right selectors

Use your browser’s page inspector to find the listing container, one record and its fields. A CSS selector can use a class (`.title`), ID (`#catalog`), attribute (`a[rel=next]`) or structure (`tbody > tr[data-id]`).

- **Listing container selector:** exactly one element unique to a valid listing. A shared header or generic `body` cannot distinguish a sign-in page from real results.
- **Record selector:** one element per torrent, relative to that container. Exclude headings, advertisements and pagination.
- **Native ID:** an attribute such as `data-id`, or an ID extracted from a detail link. Keep it stable between imports. An empty selector reads the current row itself.
- **Field selector:** relative to one row, or to the detail container for detail fields. An empty attribute reads text; `href`, `datetime` and `data-*` read those attributes instead.

Extraction patterns are optional. For a detail link `/items/123`, `/items/([0-9]+)` extracts `123`. A pattern may have one capture, or none to use its complete match. No match omits an optional field; a missing required field stops the page. A title is required for every imported record, from the listing or details.

## Read hashes, sizes and dates

Choose **Info hash or magnet link** for a hash or magnet. Only the hash is retained; magnet announce addresses are discarded. Choose **Read hash from a torrent file** when a link serves `.torrent` metadata. AIOSources reads the exact info-dictionary hash, not the content described by the torrent. Neither that file nor its announce addresses are saved as fields.

**File size** accepts byte counts and units such as `1.5 GB` or `1,5 GiB`, including `Go` / `Gio` and `octets`. `GB` / `Go` use decimal units; `GiB` / `Gio` use binary units. Prefer an exact byte attribute when available. Approximate displayed sizes are rounded once to bytes.

**Date** accepts ISO and email-style dates. For other displayed dates, enter a custom Go date layout, such as `02/01/2006 15:04`, and a timezone when absent from the page. A `datetime` attribute usually avoids custom formatting. Unix dates can use `options.published_at_unit`, as for other source adapters.

Use **multiple values** for categories or languages. Do not map seeders, peers or leechers: their transient counts are not retained by the application.

## Detail pages

Expand **Source fields → Detail pages** when the listing does not provide all needed fields. Configure its row-relative detail link, one valid detail container and the extra field mappings.

AIOSources requests a detail page only when one of its mapped fields is absent from the listing. It keeps values already read from the listing. A missing required detail field or failed detail request stops that listing page; it does not publish a partial page. The requests use the source’s authentication, proxy, pacing and quotas.

Detail and torrent-file requests stay on the source’s origin. They do not follow a site link to another host or downgrade HTTPS. Only explicitly selected listing, detail and torrent-file requests are made: scripts, images, styles and other embedded resources are not fetched or executed.

## Pagination and recovery

For next-link pagination, no matching enabled next link means the valid listing has ended. Select a real next link, not the last-page link. Duplicate top/bottom links to the same next page are supported. Conflicting links, repeated pages and unsafe continuations fail rather than looping.

For numbered pages, configure the parameter and starting page/offset. Leave the next-link selector empty to continue until a confirmed empty page. A short page alone is not the end. The page-size parameter is optional: many websites have a fixed size and ignore it. Explicit next links are authoritative GET URLs; static query or form values are not reapplied to them.

The empty-result selector must match an actual empty-state marker when no rows exist. Missing rows without that marker are an error. This prevents a changed layout, sign-in page or temporary challenge from being treated as an empty catalog. Optional **Response checks** can also verify the current position and total; configured totals must stay consistent during the run.

Complete listing pages are committed with their continuation. Details are part of that same atomic page: an interruption retries the current listing and its details, while earlier committed pages remain saved. **Preview**, **Update**, **Rebuild source catalog**, page budgets, quotas and known-page stopping use the ordinary import behavior. A failed or paused rebuild leaves the previous published catalog available.

The connector reads HTML returned by the server. It does not run JavaScript, sign in through interactive forms, solve challenges or import Cardigann definitions. For a supported HTML search form, choose POST and provide its non-secret form values as a JSON object in Connection; the request is sent as a form. Cookies and other credentials still use Secrets.

A successful traversal covers only the listing the website exposes. A recent-only feed, capped search window or omitted next link cannot prove that all historical records are available.
