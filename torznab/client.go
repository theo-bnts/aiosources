package torznab

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

const (
	maxDiscoveryProbes = 16
	maxResponseBytes   = 16 << 20
	maxHTTPRetries     = 3
)

// Client discovers and queries a single origin. Its methods may be used concurrently.
type Client struct {
	config             Config
	endpoint           *url.URL
	caps               Capabilities
	mu                 sync.Mutex
	observedAttributes map[string]struct{}

	httpClient     *http.Client
	origin         *url.URL
	gate           chan struct{}
	nextRequest    time.Time
	quota          RateLimit
	quotaWaitUntil time.Time
}

// Open finds a Torznab endpoint using a bounded set of same-origin GET requests.
// A failed discovery may wrap both ErrNotFound and a typed HTTPError or APIError.
func Open(ctx context.Context, config Config) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	initial, err := validateConfig(&config)
	if err != nil {
		return nil, err
	}
	client := &Client{
		config:             config,
		origin:             initial,
		gate:               make(chan struct{}, 1),
		observedAttributes: make(map[string]struct{}),
	}
	client.httpClient, err = client.makeHTTPClient()
	if err != nil {
		return nil, err
	}
	candidates := discoveryCandidates(initial)
	seen := make(map[string]struct{})
	var failures []error
	for probes := 0; len(candidates) > 0 && probes < maxDiscoveryProbes; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate := candidates[0]
		candidates = candidates[1:]
		key := candidate.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		first := probes == 0
		probes++
		var found bool
		err := client.fetchResponse(ctx, candidate, url.Values{"t": {"caps"}}, func(data []byte, responseURL *url.URL) error {
			caps, parseErr := parseCapabilities(data)
			if parseErr == nil {
				client.caps = caps
				client.endpoint = cleanEndpoint(responseURL)
				found = true
				return nil
			}
			// Inspect only the supplied page: this is endpoint discovery, not a web crawl.
			if first {
				links := advertisedEndpoints(data, responseURL, initial)
				candidates = append(links, candidates...)
			}
			return parseErr
		})
		if found {
			return client, nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			failures = append(failures, err)
			var apiErr *APIError
			var httpErr *HTTPError
			if errors.Is(err, ErrRateLimited) ||
				(errors.As(err, &apiErr) && isAuthenticationError(apiErr)) ||
				(errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403 || httpErr.StatusCode == 429 || httpErr.StatusCode == 503)) {
				break
			}
		}
	}
	return nil, errors.Join(append([]error{ErrNotFound}, failures...)...)
}

// Endpoint returns the discovered URL without credentials or query parameters.
func (c *Client) Endpoint() string {
	u := *c.endpoint
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// Capabilities returns a defensive copy of the advertised capabilities.
func (c *Client) Capabilities() Capabilities {
	result := c.caps
	if result.Searches != nil {
		result.Searches = make(map[SearchMode]SearchCapability, len(c.caps.Searches))
		for mode, capability := range c.caps.Searches {
			capability.SupportedParams = append([]string(nil), capability.SupportedParams...)
			result.Searches[mode] = capability
		}
	}
	result.Categories = cloneCategories(c.caps.Categories)
	return result
}

func cloneCategories(categories []Category) []Category {
	if categories == nil {
		return nil
	}
	result := make([]Category, len(categories))
	for i, category := range categories {
		result[i] = category
		result[i].Subcategories = cloneCategories(category.Subcategories)
	}
	return result
}

// RateLimit returns a defensive snapshot of the last observed server quota.
func (c *Client) RateLimit() RateLimit {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneRateLimit(c.quota)
}

func cloneRateLimit(rate RateLimit) RateLimit {
	if rate.Limit != nil {
		value := *rate.Limit
		rate.Limit = &value
	}
	if rate.Remaining != nil {
		value := *rate.Remaining
		rate.Remaining = &value
	}
	if rate.ResetAt != nil {
		value := *rate.ResetAt
		rate.ResetAt = &value
	}
	return rate
}

func validateConfig(config *Config) (*url.URL, error) {
	switch config.PublishedAtUnit {
	case "", "seconds", "milliseconds":
	default:
		return nil, errors.New("torznab: published_at_unit must be seconds or milliseconds")
	}
	if config.PageSize < 0 || config.RequestInterval < 0 {
		return nil, errors.New("torznab: page size and request interval must not be negative")
	}
	if strings.Contains(config.Username, ":") {
		return nil, errors.New("torznab: HTTP Basic username must not contain a colon")
	}
	if config.Password != "" && config.Username == "" {
		return nil, errors.New("torznab: HTTP Basic password requires a username")
	}
	if config.Cookie != "" {
		if _, err := http.ParseCookie(config.Cookie); err != nil || strings.ContainsAny(config.Cookie, "\r\n") {
			return nil, errors.New("torznab: invalid Cookie header")
		}
	}
	u, err := url.Parse(config.URL)
	if err != nil || !validHTTPURL(u) || strings.Contains(config.URL, "#") {
		return nil, errors.New("torznab: an absolute HTTP(S) URL without userinfo or fragment is required")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("torznab: invalid endpoint query")
	}
	if config.APIKey == "" {
		keys := make([]string, 0, len(query))
		for key := range query {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if strings.EqualFold(key, "apikey") && query.Get(key) != "" {
				config.APIKey = query.Get(key)
				break
			}
		}
	}
	if config.RequestInterval == 0 {
		config.RequestInterval = time.Second
	}
	u.Scheme = strings.ToLower(u.Scheme)
	return cleanEndpoint(u), nil
}

func validHTTPURL(u *url.URL) bool {
	if u == nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) ||
		u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return false
	}
	return true
}

func sameOrigin(a, b *url.URL) bool {
	return validHTTPURL(a) && validHTTPURL(b) && strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		n, _ := strconv.Atoi(port)
		return strconv.Itoa(n)
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func reservedParameter(key string) bool {
	switch strings.ToLower(key) {
	case "t", "apikey", "o", "q", "cat", "tag", "attrs", "extended", "offset", "limit",
		"rid", "tvdbid", "tvmazeid", "imdbid", "imdb", "tmdbid", "traktid", "season", "ep",
		"artist", "album", "label", "track", "year", "genre", "author", "title", "publisher",
		"booktitle", "maxage", "minsize", "maxsize", "id", "password", "del":
		return true
	}
	return false
}

func cleanEndpoint(u *url.URL) *url.URL {
	result := *u
	query := result.Query()
	for key := range query {
		if reservedParameter(key) {
			delete(query, key)
		}
	}
	result.RawQuery = query.Encode()
	result.ForceQuery = false
	result.User = nil
	result.Fragment = ""
	result.RawFragment = ""
	return &result
}

func (c *Client) makeHTTPClient() (*http.Client, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	if c.config.HTTPClient != nil {
		*client = *c.config.HTTPClient
	}
	if client.Jar == nil {
		jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
		if err != nil {
			return nil, errors.New("torznab: could not initialize session cookies")
		}
		client.Jar = jar
	}
	if c.config.Cookie != "" {
		request := &http.Request{Header: http.Header{"Cookie": {c.config.Cookie}}}
		cookies := request.Cookies()
		for _, cookie := range cookies {
			cookie.Path = "/"
			cookie.Secure = strings.EqualFold(c.origin.Scheme, "https")
		}
		client.Jar.SetCookies(c.origin, cookies)
	}
	originalRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !sameOrigin(c.origin, request.URL) {
			return errors.New("torznab: cross-origin redirect refused")
		}
		if len(via) >= 10 {
			return errors.New("torznab: redirect limit exceeded")
		}
		if originalRedirect != nil {
			if err := originalRedirect(request, via); err != nil {
				return err
			}
			if !sameOrigin(c.origin, request.URL) {
				return errors.New("torznab: cross-origin redirect refused")
			}
		}
		// A redirect must not select a different API key from its Location header.
		query := request.URL.Query()
		for key := range query {
			if strings.EqualFold(key, "apikey") {
				delete(query, key)
			}
		}
		if c.config.APIKey != "" {
			query.Set("apikey", c.config.APIKey)
		}
		request.URL.RawQuery = query.Encode()
		if c.config.Username != "" {
			request.SetBasicAuth(c.config.Username, c.config.Password)
		}
		return nil
	}
	return client, nil
}

func discoveryCandidates(initial *url.URL) []*url.URL {
	routes := []string{"/api", "/api/torznab/all", "/api/torznab", "/torznab/api", "/torznab", "/api/v2.0/indexers/all/results/torznab/api"}
	candidates := []*url.URL{initial}
	prefix := strings.TrimRight(initial.Path, "/")
	if path.Ext(prefix) != "" || apiLooking(initial) {
		prefix = path.Dir(prefix)
	}
	if prefix != "" && prefix != "/" && prefix != "." {
		for _, route := range routes {
			u := *initial
			u.Path = prefix + route
			u.RawPath = ""
			candidates = append(candidates, &u)
		}
	}
	for _, route := range routes {
		u := *initial
		u.Path = route
		u.RawPath = ""
		candidates = append(candidates, &u)
	}
	return candidates
}

func apiLooking(u *url.URL) bool {
	lowerPath := strings.ToLower(u.Path)
	if strings.Contains(lowerPath, "torznab") || strings.Contains(lowerPath, "newznab") {
		return true
	}
	for _, segment := range strings.Split(lowerPath, "/") {
		if segment == "api" || strings.HasPrefix(segment, "api.") {
			return true
		}
	}
	for key, values := range u.Query() {
		if strings.EqualFold(key, "t") {
			for _, value := range values {
				switch strings.ToLower(value) {
				case "caps", "search", "tvsearch", "movie", "music", "book":
					return true
				}
			}
		}
	}
	return false
}

func advertisedEndpoints(data []byte, responseURL, initial *url.URL) []*url.URL {
	base := responseURL
	baseSeen := false
	var links []*url.URL
	seen := make(map[string]struct{})
	tokens := html.NewTokenizer(bytes.NewReader(data))
	for len(links) < maxDiscoveryProbes-1 {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokens.Token()
		if token.Data != "a" && token.Data != "link" && token.Data != "base" {
			continue
		}
		var href string
		for _, attribute := range token.Attr {
			if attribute.Key == "href" {
				href = attribute.Val
				break
			}
		}
		ref, err := url.Parse(strings.TrimSpace(href))
		if err != nil || href == "" || ref.User != nil {
			continue
		}
		target := base.ResolveReference(ref)
		if !sameOrigin(initial, target) {
			continue
		}
		if _, err := url.ParseQuery(target.RawQuery); err != nil {
			continue
		}
		if token.Data == "base" {
			if !baseSeen {
				base = target
				baseSeen = true
			}
			continue
		}
		if !apiLooking(target) {
			continue
		}
		target = cleanEndpoint(target)
		query := initial.Query()
		for key, values := range target.Query() {
			query[key] = values
		}
		target.RawQuery = query.Encode()
		key := target.String()
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			links = append(links, target)
		}
	}
	return links
}

func (c *Client) fetch(ctx context.Context, target *url.URL, params url.Values, consume func([]byte) error) error {
	return c.fetchResponse(ctx, target, params, func(data []byte, _ *url.URL) error {
		return consume(data)
	})
}

func (c *Client) fetchResponse(ctx context.Context, target *url.URL, params url.Values, consume func([]byte, *url.URL) error) error {
	return c.fetchResponseObserved(ctx, target, params, consume, nil)
}

func (c *Client) fetchResponseObserved(ctx context.Context, target *url.URL, params url.Values, consume func([]byte, *url.URL) error, observe func([]byte)) error {
	if !sameOrigin(c.origin, target) {
		return errors.New("torznab: cross-origin request refused")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.gate <- struct{}{}:
	}
	defer func() { <-c.gate }()
	u := cleanEndpoint(target)
	query := u.Query()
	for key, values := range params {
		if reservedParameter(key) {
			key = strings.ToLower(key)
		}
		query[key] = append([]string(nil), values...)
	}
	delete(query, "apikey")
	if c.config.APIKey != "" {
		query.Set("apikey", c.config.APIKey)
	}
	u.RawQuery = query.Encode()
	for retries := 0; ; retries++ {
		if err := c.waitForTurn(ctx); err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return errors.New("torznab: could not construct HTTP request")
		}
		request.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, text/html;q=0.5")
		if c.config.Username != "" {
			request.SetBasicAuth(c.config.Username, c.config.Password)
		}
		c.mu.Lock()
		c.nextRequest = time.Now().Add(c.config.RequestInterval)
		c.mu.Unlock()
		response, err := c.httpClient.Do(request)
		if err != nil {
			if response != nil && response.Body != nil {
				if observe != nil {
					data, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
					observe(data)
				}
				response.Body.Close()
			}
			return safeRequestError(ctx, err)
		}
		rate, retryAfter, retryValid := headerRateLimit(response.Header, time.Now())
		c.observeRate(rate)
		if retryValid {
			c.mu.Lock()
			deadline := time.Now().Add(retryAfter)
			if deadline.After(c.quotaWaitUntil) {
				c.quotaWaitUntil = deadline
			}
			c.quota.RetryAfter = retryAfter
			c.mu.Unlock()
		}
		var httpErr error
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			httpErr = &HTTPError{StatusCode: response.StatusCode, RetryAfter: retryAfter}
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		if observe != nil {
			observe(data)
		}
		response.Body.Close()
		if readErr != nil {
			return errors.Join(httpErr, safeRequestError(ctx, readErr))
		}
		if len(data) > maxResponseBytes {
			return errors.Join(httpErr, errors.New("torznab: response exceeds the size limit"))
		}
		apiErr := parseAPIError(data)
		if httpErr != nil {
			var authentication *APIError
			if errors.As(apiErr, &authentication) && isAuthenticationError(authentication) {
				return errors.Join(httpErr, apiErr)
			}
			if !c.config.DisableRetries && (response.StatusCode == 429 || response.StatusCode == 503) && retryValid && retries < maxHTTPRetries {
				continue
			}
			if !retryValid && (response.StatusCode == 429 || (rate.Remaining != nil && *rate.Remaining == 0 && rate.ResetAt == nil)) {
				return errors.Join(httpErr, apiErr, ErrRateLimited)
			}
			return errors.Join(httpErr, apiErr)
		}
		if apiErr != nil {
			return apiErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		responseURL := request.URL
		if response.Request != nil {
			responseURL = response.Request.URL
		}
		return consume(data, responseURL)
	}
}

type requestError struct{ cause error }

func (e *requestError) Error() string { return "torznab: HTTP request failed" }
func (e *requestError) Unwrap() error { return e.cause }

func safeRequestError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	// Preserve transport classifications without formatting causes that may
	// contain URLs, credentials, redirect targets or response data.
	return &requestError{cause: err}
}

func isAuthenticationError(err *APIError) bool {
	return err.Code >= 100 && err.Code <= 102
}

func (c *Client) observeRate(rate RateLimit) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rate = cloneRateLimit(rate)
	if rate.Limit != nil {
		c.quota.Limit = rate.Limit
	}
	if rate.Remaining != nil {
		c.quota.Remaining = rate.Remaining
	}
	if rate.ResetAt != nil {
		c.quota.ResetAt = rate.ResetAt
	}
	if rate.RetryAfter > 0 {
		c.quota.RetryAfter = rate.RetryAfter
		deadline := time.Now().Add(rate.RetryAfter)
		if deadline.After(c.quotaWaitUntil) {
			c.quotaWaitUntil = deadline
		}
	}
	if c.quota.Remaining != nil && *c.quota.Remaining == 0 && c.quota.ResetAt != nil && c.quota.ResetAt.After(c.quotaWaitUntil) {
		c.quotaWaitUntil = *c.quota.ResetAt
	}
}

func (c *Client) waitForTurn(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		now := time.Now()
		if !c.quotaWaitUntil.IsZero() && !c.quotaWaitUntil.After(now) {
			// Reset metadata permits another request, but does not prove a replenished quota.
			if c.quota.Remaining != nil && *c.quota.Remaining == 0 {
				c.quota.Remaining = nil
			}
			if c.quota.ResetAt != nil && !c.quota.ResetAt.After(now) {
				c.quota.ResetAt = nil
			}
			c.quota.RetryAfter = 0
			c.quotaWaitUntil = time.Time{}
		}
		if c.quota.Remaining != nil && *c.quota.Remaining == 0 && c.quotaWaitUntil.IsZero() {
			c.mu.Unlock()
			return ErrRateLimited
		}
		until := c.nextRequest
		if c.quotaWaitUntil.After(until) {
			until = c.quotaWaitUntil
		}
		c.mu.Unlock()
		if !until.After(now) {
			return nil
		}
		timer := time.NewTimer(time.Until(until))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func headerRateLimit(header http.Header, now time.Time) (RateLimit, time.Duration, bool) {
	var rate RateLimit
	rate.Limit = headerNonnegativeInteger(header, "RateLimit-Limit", "X-RateLimit-Limit")
	rate.Remaining = headerNonnegativeInteger(header, "RateLimit-Remaining", "X-RateLimit-Remaining")
	if delta := headerNonnegativeInteger(header, "RateLimit-Reset"); delta != nil && *delta <= int64((1<<63-1)/time.Second) {
		reset := now.Add(time.Duration(*delta) * time.Second)
		rate.ResetAt = &reset
	} else if epoch := headerNonnegativeInteger(header, "X-RateLimit-Reset"); epoch != nil {
		reset := time.Unix(*epoch, 0)
		rate.ResetAt = &reset
	}
	delay, valid := retryAfterDuration(header.Get("Retry-After"), now)
	if valid {
		rate.RetryAfter = delay
	}
	return rate, delay, valid
}

func headerNonnegativeInteger(header http.Header, names ...string) *int64 {
	for _, name := range names {
		value, err := strconv.ParseInt(strings.TrimSpace(header.Get(name)), 10, 64)
		if err == nil && value >= 0 {
			return &value
		}
	}
	return nil
}

func retryAfterDuration(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := when.Sub(now)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}
