// Package torznab discovers and consumes Torznab APIs without provider-specific
// paths or XML mappings in the calling application.
//
// # Custom provider profiles
//
// LoadProvider reads the project's own strict JSON format.
// Profiles contain access details, known exceptions and collection preferences.
// The API still supplies category definitions and extended RSS metadata. Example:
//
//	{
//	  "version": 1,
//	  "id": "example",
//	  "name": "Private indexer",
//	  "url": "https://indexer.example/",
//	  "api_path": "/api",
//	  "auth": {"api_key_env": "INDEXER_API_KEY"},
//	  "request_interval": "2s",
//	  "page_size": 100,
//	  "search": {"categories": [2030, 5070]},
//	  "output": {"fields": ["guid", "title", "size", "seeders", "attributes.imdbid"]}
//	}
//
// Omit api_path to discover from the site URL. auth also accepts cookie_env,
// username_env and password_env; these contain environment variable NAMES.
// Provider.Config(nil) resolves them from the environment, or a supplied lookup
// function can resolve them from a secret store. Unknown or duplicate JSON keys,
// multiple values, invalid types and missing referenced credentials are errors.
// The module supplies a generic template at providers/provider.example.json,
// not real service details. Copy it to providers/provider.json or use a different
// filename per endpoint. Keep real profiles private; other JSON files are Git-ignored.
//
// Each invocation selects one file with -provider; filenames are arbitrary.
// Give independent providers distinct IDs and credential environment-variable
// references. A profile describes a Torznab API; it does not adapt non-Torznab
// endpoints or scrape website pages.
//
// With the variables referenced by your private profile exported, run:
//
//	go run ./examples/crawl -provider providers/provider.json -pages 3
//
// The command emits JSON Lines on stdout and diagnostics on stderr. It reads
// metadata only: it does not download payloads or modify the adjacent database.
// Progress reports page offsets and item counts on stderr without changing the
// JSON Lines format. -pages limits nonempty pages, not individual items: every
// selected page is emitted in full before stopping, with no request for another
// page. Zero, the default, traverses the accessible feed; negatives are errors.
// A server may return short pages or end before the requested number of pages.
//
// # Per-provider collection settings
//
// search.categories sends an OR filter using exact category IDs; omitted or empty
// lists leave categories unfiltered. Parent IDs are not expanded into child IDs.
// output.fields selects properties to emit, not which metadata the API returns.
// Omitted or null fields preserve the full Item output; an explicitly empty list
// is rejected rather than silently returning all fields.
//
// Normalized selectors are guid, title, link, comments, published_at, size,
// info_hash, magnet_url, categories, seeders and peers. attributes.NAME selects a
// raw extended attribute by its literal name; attributes selects the entire raw
// attribute map, including names first encountered on later pages. Selected
// attributes remain lists nested under "attributes" in the output.
//
// Missing properties are omitted, not replaced by null or zero. A present numeric
// zero is retained. For partial database updates, callers can distinguish omitted
// properties from supplied values; SQL mapping and update policy remain theirs.
// Include suitable stable identities when persisting or deduplicating results.
//
// Provider.Config configures the client, including publication-date input units.
// Go callers pass Search.Categories to Query.Categories and, if Output.Fields is
// non-nil, compile it with NewProjection.
// Apply Projection.Project to items inside the Walk callback, after traversal
// checks use the original items. The example command applies both settings in
// this order, so excluding identity fields from output does not break pagination.
//
// # Discovery and authentication
//
// Open accepts a website URL or a complete API URL. It checks capabilities on
// the supplied URL, same-origin API links in returned HTML, and a bounded set
// of common API paths. This is discovery, not proof that an API does not exist:
// unlinked custom endpoints and APIs hosted on other origins require their exact
// URL. Probes only use GET and never submit login forms or solve challenges.
//
// Authentication supports API keys, existing Cookie headers, a supplied HTTP
// client's cookie jar, and HTTP Basic username/password. Arbitrary website login
// forms require site-specific authentication outside this package. Credentials
// are restricted to the supplied origin, and errors omit URLs and server error
// descriptions. HTTPS is recommended for private remote indexers. When exposed
// through a server-side service, restrict user-supplied URLs to trusted origins
// yourself: this package intentionally permits locally hosted indexer endpoints.
//
// # Metadata and traversal
//
// Capabilities reports advertised search modes, parameters, categories and page
// sizes. There is no universal advertised schema for every release attribute.
// Search and Walk retain all extended values in Item.Attributes, and Attributes
// reports names observed so far. Missing optional numeric values remain nil.
// Returned download URLs and attributes may contain private keys: protect stored
// data and do not publish it unintentionally.
//
// Publication dates normalize to UTC. Config.PublishedAtUnit optionally accepts
// integer Unix dates in "seconds" or "milliseconds"; the unit is never inferred.
// Textual RFC 3339 and RFC 5322 dates remain accepted. Dates retain subsecond
// precision and must fall in UTC years 1 through 9999. With a unit configured,
// invalid dates fail Search and are marked on the affected RawItem in SearchRaw.
// Lossless raw responses and item fragments remain unchanged.
//
// SDK provider JSON uses a top-level published_at_unit field. The example
// command also accepts -published-at-unit; a nonempty flag overrides the
// provider setting. These are input settings, not numeric output formats:
// PublishedAt remains a *time.Time whose JSON representation is RFC 3339.
//
// RateLimit reports observed quotas, not page sizes. The client respects common
// quota headers, Retry-After, and the optional newznab:apilimits extension. Local
// pacing defaults to one second between requests when no stronger restriction
// applies. An unknown quota remains unknown; an exhausted quota without a reset
// stops with ErrRateLimited. HTTP 429/503 with Retry-After have bounded retries.
// Requests and waits honor context cancellation.
//
// Walk streams one page at a time, automatically negotiating limit and advancing
// offset by the actual number of records. It detects repeated pages and refuses
// to silently call inconsistent pagination complete. A successful walk means
// the accessible result set ended, not that the full site history was indexed.
// Some indexers expose only recent results or require search text. Moving feeds
// can contain overlapping pages: persist using stable identities and upserts.
// Callback errors stop immediately; persistence and scheduling belong to callers.
//
// Example usage:
//
//	client, err := torznab.Open(ctx, torznab.Config{
//		URL: "https://indexer.example",
//		APIKey: os.Getenv("TORZNAB_API_KEY"),
//	})
//	if err != nil {
//		return err
//	}
//	return client.Walk(ctx, torznab.Query{}, func(page torznab.Page) error {
//		for _, item := range page.Items {
//			if err := store(item); err != nil {
//				return err
//			}
//		}
//		return nil
//	})
//
// A runnable JSON Lines example lives in examples/crawl. This module is
// independent of the adjacent ingestion service and has no database dependency.
package torznab
