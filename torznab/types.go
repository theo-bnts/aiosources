package torznab

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Config configures automatic discovery on one site. The library does not own
// HTTPClient. Discovery never sends credentials to another origin.
type Config struct {
	// URL is a site URL or a complete Torznab endpoint. Discovery probes a
	// bounded set of common paths and same-origin links advertised by the site.
	URL string
	// APIKey overrides an apikey already present in URL. It may be empty for
	// public indexers. Use HTTPS when accessing a remote private indexer.
	APIKey string
	// Cookie is a valid existing session's Cookie request header, not Set-Cookie.
	// Malformed headers are rejected; a custom HTTPClient.Jar may supply cookies.
	Cookie string
	// Username and Password use HTTP Basic authentication. Username must not
	// contain a colon. Website login forms are not standardized; authenticate
	// those separately and supply their session using Cookie or HTTPClient.Jar.
	Username string
	Password string
	// HTTPClient defaults to a client with a 30-second timeout. Cross-origin
	// redirects are refused, including when a custom HTTPClient is supplied.
	HTTPClient *http.Client
	// PageSize is the preferred number of results per request. Zero uses the
	// indexer's advertised default. The advertised maximum is always respected.
	PageSize int
	// AdvisoryTotals retains response totals as metadata without using them
	// as bounds or completion signals. Enable only for indexers with capped
	// totals; Walk then requires an empty page. Offset checks still apply.
	AdvisoryTotals bool
	// PublishedAtUnit enables integer Unix publication dates in seconds or
	// milliseconds. Empty accepts textual dates only; units are never guessed.
	// Textual dates remain supported, and normalized dates use UTC.
	PublishedAtUnit string
	// RequestInterval is local pacing, not an inferred server quota. Zero
	// uses one second between requests; a positive duration overrides it.
	// Advertised rate-limit waits take precedence over this minimum.
	RequestInterval time.Duration
	// DisableRetries delegates HTTP retry policy to a caller-supplied
	// transport. The default preserves the library's bounded retry behavior.
	DisableRetries bool
}

// SearchMode selects a Torznab search function.
type SearchMode string

const (
	ModeSearch SearchMode = "search"
	ModeTV     SearchMode = "tvsearch"
	ModeMovie  SearchMode = "movie"
	ModeMusic  SearchMode = "music"
	ModeBook   SearchMode = "book"
)

// Query describes a search. Its zero value requests the indexer's unfiltered
// feed. Some indexers restrict that feed to recent releases or require a query.
type Query struct {
	Mode       SearchMode
	Text       string
	Categories []int
	// Offset is the initial result offset, allowing an application to resume.
	// Offsets are not stable checkpoints if the indexer changes its ordering.
	Offset int
	// Limit overrides Config.PageSize for this query; it is a per-page limit,
	// not a limit on the total number of items returned by Walk.
	Limit int
	// Params adds search-specific parameters, such as imdbid, tvdbid, season,
	// ep, or maxage. It cannot override t, apikey, q, cat, offset, limit,
	// extended, or o. SupportedParams is advisory because implementations vary.
	Params url.Values
}

// Capabilities describes the limits, search modes, and categories of an indexer.
type Capabilities struct {
	Title      string
	Limits     Limits
	Searches   map[SearchMode]SearchCapability
	Categories []Category
}

// Limits contains advertised result counts. Zero means not advertised.
type Limits struct {
	Default int
	Max     int
}

// SearchCapability describes one advertised search function.
type SearchCapability struct {
	Available       bool
	SupportedParams []string
}

// Category is a provider-advertised category, including its subcategories.
type Category struct {
	ID            int
	Name          string
	Subcategories []Category
}

// Item is a normalized RSS release. Missing optional counts remain nil rather
// than becoming zero. Attributes preserves all unique extended values, including
// provider-specific metadata and external IDs. URLs may contain private keys.
type Item struct {
	GUID        string              `json:"guid,omitempty"`
	Title       string              `json:"title"`
	Link        string              `json:"link,omitempty"`
	Comments    string              `json:"comments,omitempty"`
	PublishedAt *time.Time          `json:"published_at,omitempty"`
	Size        *int64              `json:"size,omitempty"`
	InfoHash    string              `json:"info_hash,omitempty"`
	MagnetURL   string              `json:"magnet_url,omitempty"`
	Categories  []int               `json:"categories,omitempty"`
	Seeders     *int64              `json:"seeders,omitempty"`
	Peers       *int64              `json:"peers,omitempty"`
	Attributes  map[string][]string `json:"attributes,omitempty"`
}

// Page is one result page. Offset is the server offset, or the requested offset
// if omitted by the server. Total is nil when no total was advertised. A server's
// total covers its accessible search results, not necessarily its full history.
type Page struct {
	Items     []Item     `json:"items"`
	Offset    int        `json:"offset"`
	Total     *int       `json:"total,omitempty"`
	RateLimit *RateLimit `json:"rate_limit,omitempty"`
}

// RateLimit contains the most recently observed API quota. Nil fields mean
// unknown, not unlimited. Limits in t=caps describe page size, not this quota.
// ResetAt is when another request may become available, not a guarantee that
// the entire quota is replenished. Values are derived from response headers or
// a newznab:apilimits element; discovery does not deliberately exhaust a quota.
type RateLimit struct {
	Limit      *int64        `json:"limit,omitempty"`
	Remaining  *int64        `json:"remaining,omitempty"`
	ResetAt    *time.Time    `json:"reset_at,omitempty"`
	RetryAfter time.Duration `json:"retry_after,omitempty"`
}

// APIError represents a Torznab <error> document, including HTTP 200 responses.
// Description is untrusted server text and is deliberately omitted from Error:
// a server may echo credentials. Inspect it only in a trusted diagnostic context.
type APIError struct {
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("torznab: API error %d", e.Code)
}

// HTTPError represents a non-successful HTTP status. RetryAfter is the delay
// requested by the server, if present. HTTP 429 and 503 are retried at most
// three times when Retry-After is supplied; all waits honor context cancellation.
type HTTPError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("torznab: HTTP status %d", e.StatusCode)
}

var (
	// ErrNotFound means no valid Torznab capabilities document was found at
	// the inspected paths. It does not prove the site has no hidden endpoint.
	ErrNotFound = errors.New("torznab: no Torznab endpoint found at the inspected paths")
	// ErrRateLimited means a quota is exhausted without a usable reset time.
	// The library refuses to guess when further requests would be allowed.
	ErrRateLimited = errors.New("torznab: API quota exhausted without a reset time")
	// ErrPaginationStalled indicates a repeated page, an offset mismatch, or
	// inconsistent pagination metadata. Walk must not report a complete crawl.
	ErrPaginationStalled = errors.New("torznab: pagination did not advance consistently")
	// ErrUnsupportedSearch indicates a search mode explicitly unavailable or
	// a specialized mode not advertised by the indexer.
	ErrUnsupportedSearch = errors.New("torznab: unsupported search mode")
)
