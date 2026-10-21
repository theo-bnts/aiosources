package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
	"golang.org/x/net/http/httpguts"
	"golang.org/x/net/publicsuffix"
)

const (
	maxRequestBytes  = 1 << 20
	maxResponseBytes = 16 << 20
	maxHTTPRetries   = 3
)

var errQuota = errors.New("provider quota exhausted without a reset time")
var errResponseLimit = errors.New("response exceeds the size limit")

type Client struct {
	origin                                            *url.URL
	transport                                         *http.Transport
	routing                                           *outbound.Transport
	httpClient                                        *http.Client
	jar                                               http.CookieJar
	headers                                           http.Header
	secretHeaders                                     http.Header
	authType, authName, authValue, username, password string
	interval                                          time.Duration
	timeout                                           time.Duration
	relativeQuotaReset                                bool
	returnEveryResponse                               bool
	expectJSON                                        bool
	maxQuotaRetries                                   int
	rateLimited                                       func(context.Context, Response, time.Time, bool) error
	transientFailure                                  func(context.Context, Response, time.Time, int, bool) error
	responseObserved                                  func(Response, time.Time)
	beforeWait                                        func(context.Context, time.Time) error
	beforeRequest                                     func(context.Context) error
	gate                                              chan struct{}
	mu                                                sync.Mutex
	next                                              time.Time
	quotas                                            map[string]clientQuota
	last                                              Response
	closed                                            bool
	closeContext                                      context.Context
	cancel                                            context.CancelFunc
}

type clientQuota struct {
	until     time.Time
	exhausted bool
}
type connectorTransport struct {
	client       *Client
	publicOrigin *url.URL
}

func NewClient(ctx context.Context, p model.Provider, env Environment) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p = providerDefaults(p)
	if err := validateCommon(p); err != nil {
		return nil, err
	}
	origin, _ := url.Parse(p.URL)
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, errors.New("could not initialize session cookies")
	}
	interval, _ := time.ParseDuration(p.RequestInterval)
	timeout, _ := time.ParseDuration(p.RequestTimeout)
	closeContext, cancel := context.WithCancel(context.Background())
	c := &Client{origin: origin, jar: jar, headers: make(http.Header), secretHeaders: make(http.Header), interval: interval, gate: make(chan struct{}, 1), quotas: make(map[string]clientQuota), closeContext: closeContext, cancel: cancel}
	c.timeout = timeout
	c.expectJSON = p.IsJSON()
	c.relativeQuotaReset = p.RateLimitReset == "relative"
	c.rateLimited = env.RateLimited
	c.transientFailure = env.TransientFailure
	c.responseObserved, c.beforeWait = env.ResponseObserved, env.BeforeWait
	c.beforeRequest = env.BeforeRequest
	if p.RequestLimits != nil && c.beforeRequest == nil {
		cancel()
		return nil, errors.New("request_limits require durable request admission")
	}
	c.maxQuotaRetries = maxHTTPRetries
	if env.MaxQuotaRetries != nil {
		if *env.MaxQuotaRetries < 0 || *env.MaxQuotaRetries > 10 {
			cancel()
			return nil, errors.New("max_quota_retries must be between 0 and 10")
		}
		c.maxQuotaRetries = *env.MaxQuotaRetries
	}
	c.transport = &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: timeout, ExpectContinueTimeout: time.Second, MaxResponseHeaderBytes: 1 << 20}
	direct := c.transport.DialContext
	c.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outbound.Dial(ctx, network, address, direct)
	}
	if env.Network != nil {
		c.routing = env.Network.Transport("source:"+p.ID, c.transport)
	}
	resolved := make(map[string]string)
	resolve := func(ref string) (string, error) {
		if value, exists := resolved[ref]; exists {
			return value, nil
		}
		if ref == "" || env.Secrets == nil {
			return "", errors.New("required provider secret is unavailable")
		}
		value, err := env.Secrets(ctx, ref)
		if err != nil || value == "" {
			return "", errors.New("required provider secret is unavailable")
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("provider secret contains invalid control characters")
		}
		resolved[ref] = value
		return value, nil
	}
	for name, value := range p.HTTP.Headers {
		c.headers.Set(name, value)
	}
	for name, ref := range p.HTTP.SecretHeaders {
		value, err := resolve(ref)
		if err != nil {
			cancel()
			return nil, err
		}
		c.secretHeaders.Set(name, value)
	}
	c.authType, c.authName = p.Auth.Type, p.Auth.Name
	if c.authType == "api_key" {
		c.authType = p.Auth.In
	}
	switch c.authType {
	case "query", "header", "bearer", "cookie":
		c.authValue, err = resolve(p.Auth.SecretRef)
	case "basic":
		c.username, err = resolve(p.Auth.UsernameRef)
		if err == nil {
			c.password, err = resolve(p.Auth.PasswordRef)
		}
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if c.authType == "bearer" {
		c.authValue = strings.TrimSpace(c.authValue)
		if len(c.authValue) >= 7 && strings.EqualFold(c.authValue[:7], "Bearer ") {
			c.authValue = strings.TrimSpace(c.authValue[7:])
		}
		if c.authValue == "" {
			cancel()
			return nil, errors.New("bearer secret must contain a token")
		}
	}
	if c.authType == "basic" && strings.Contains(c.username, ":") {
		cancel()
		return nil, errors.New("HTTP Basic username cannot contain a colon")
	}
	if c.authType == "cookie" {
		cookies, cookieErr := http.ParseCookie(c.authValue)
		if cookieErr != nil || len(cookies) == 0 {
			cancel()
			return nil, errors.New("provider cookie secret is not a valid Cookie header")
		}
		for _, cookie := range cookies {
			cookie.Path = "/"
			cookie.Secure = origin.Scheme == "https"
		}
		jar.SetCookies(origin, cookies)
	}
	c.httpClient = &http.Client{Transport: &connectorTransport{client: c}, CheckRedirect: primaryRedirect(origin)}
	return c, nil
}

func (c *Client) HTTPClient() *http.Client { return c.httpClient }

func (c *Client) lastResponse() Response {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	if c.routing != nil {
		c.routing.CloseIdleConnections()
	}
	c.transport.CloseIdleConnections()
	return nil
}

func primaryRedirect(origin *url.URL) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if !sameOrigin(origin, request.URL) {
			return errors.New("cross-origin redirect refused")
		}
		if len(via) >= 10 {
			return errors.New("redirect limit exceeded")
		}
		return nil
	}
}

func (c *Client) Do(ctx context.Context, request Request) (Response, error) {
	target, err := c.target(request.URL)
	if err != nil {
		return Response{}, err
	}
	return c.do(ctx, request, target, c.httpClient)
}

// DoPublic never inherits primary query parameters, headers, cookies or secrets.
// The hostname must be explicitly allowed; redirects additionally stay on the
// original scheme and port so an allowlisted hostname cannot downgrade HTTPS.
func (c *Client) DoPublic(ctx context.Context, request Request, allowedHosts []string) (Response, error) {
	target, err := url.Parse(request.URL)
	if err != nil || !validHTTPURL(target) {
		return Response{}, errors.New("public request requires an absolute HTTP(S) URL")
	}
	allowed := false
	for _, host := range allowedHosts {
		if host != "" && !strings.ContainsAny(host, "/@*?#") && strings.EqualFold(host, target.Hostname()) {
			allowed = true
		}
	}
	if !allowed {
		return Response{}, errors.New("public request host is not allowed")
	}
	for name, values := range request.Headers {
		if sensitiveName(name) || strings.EqualFold(name, "Referer") || strings.EqualFold(name, "Origin") {
			return Response{}, errors.New("public request cannot include private headers")
		}
		for _, value := range values {
			if c.containsSecret(value) {
				return Response{}, errors.New("public request contains a provider secret")
			}
		}
	}
	for name := range target.Query() {
		if sensitiveName(name) {
			return Response{}, errors.New("public request cannot include credential parameters")
		}
	}
	for name := range request.Query {
		if sensitiveName(name) {
			return Response{}, errors.New("public request cannot include credential parameters")
		}
	}
	if c.containsURLSecret(target) || c.containsSecret(request.Query.Encode()) || c.containsSecret(string(request.Body)) {
		return Response{}, errors.New("public request contains a provider secret")
	}
	client := &http.Client{Transport: &connectorTransport{client: c, publicOrigin: target}, CheckRedirect: primaryRedirect(target)}
	return c.do(ctx, request, target, client)
}

func (c *Client) containsSecret(text string) bool {
	if text == "" {
		return false
	}
	values := []string{c.authValue, c.username, c.password}
	for _, entries := range c.secretHeaders {
		values = append(values, entries...)
	}
	for _, cookie := range c.jar.Cookies(c.origin) {
		values = append(values, cookie.Value)
	}
	for _, value := range values {
		if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
			token := strings.TrimSpace(value[7:])
			if token != "" && (strings.Contains(text, token) || strings.Contains(text, url.QueryEscape(token))) {
				return true
			}
		}
		if value != "" && (strings.Contains(text, value) || strings.Contains(text, url.QueryEscape(value))) {
			return true
		}
	}
	return false
}

func (c *Client) containsURLSecret(target *url.URL) bool {
	if c.containsSecret(target.String()) || c.containsSecret(target.Path) {
		return true
	}
	for _, values := range target.Query() {
		for _, value := range values {
			if c.containsSecret(value) {
				return true
			}
		}
	}
	return false
}

func (c *Client) target(value string) (*url.URL, error) {
	if value == "" {
		target := *c.origin
		return &target, nil
	}
	target, err := url.Parse(value)
	if err != nil {
		return nil, errors.New("invalid request URL")
	}
	if !target.IsAbs() {
		if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
			return nil, errors.New("request URL must be absolute or root-relative")
		}
		target = c.origin.ResolveReference(target)
	}
	if !sameOrigin(c.origin, target) {
		return nil, errors.New("cross-origin request refused")
	}
	return target, nil
}

func (c *Client) do(ctx context.Context, input Request, target *url.URL, client *http.Client) (Response, error) {
	if len(input.Body) > maxRequestBytes {
		return Response{}, errors.New("request body exceeds the size limit")
	}
	method := input.Method
	if method == "" {
		method = http.MethodGet
	}
	query := target.Query()
	for name, values := range input.Query {
		query[name] = append([]string(nil), values...)
	}
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(input.Body))
	if err != nil {
		return Response{}, errors.New("could not construct HTTP request")
	}
	request.Header = input.Headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if input.DisableRedirects {
		copyClient := *client
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copyClient
	}
	response, err := client.Do(request)
	if response == nil {
		return Response{}, safeHTTPError(ctx, err)
	}
	defer response.Body.Close()
	var body []byte
	var readErr error
	if retained, ok := response.Body.(*retainedBody); ok {
		body, readErr = retained.data, retained.err
	} else {
		body, readErr = io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	}
	result := Response{Body: body, StatusCode: response.StatusCode, Header: response.Header.Clone()}
	if response.Request != nil && response.Request.URL != nil {
		result.URL = response.Request.URL.String()
	}
	if len(body) > maxResponseBytes {
		result.Body = body[:maxResponseBytes]
		return result, errors.New("response exceeds the size limit")
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return result, httpFailure(response.StatusCode)
	}
	if err != nil {
		return result, safeHTTPError(ctx, err)
	}
	if readErr != nil {
		return result, safeHTTPError(ctx, readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if input.DisableRedirects && response.StatusCode >= 300 && response.StatusCode < 400 {
			return result, nil
		}
		return result, httpFailure(response.StatusCode)
	}
	return result, nil
}

// RoundTrip owns pacing and authentication so SDK-issued requests cannot bypass
// either. It buffers a bounded response once and returns any read error after
// those bytes, allowing the caller to archive partial response data honestly.
func (t *connectorTransport) RoundTrip(original *http.Request) (*http.Response, error) {
	c := t.client
	ctx, cancel := context.WithCancel(original.Context())
	stop := context.AfterFunc(c.closeContext, cancel)
	defer stop()
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	origin := c.origin
	if t.publicOrigin != nil {
		origin = t.publicOrigin
	}
	if !sameOrigin(origin, original.URL) {
		return nil, errors.New("cross-origin request refused")
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.gate }()
	var body []byte
	if original.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(original.Body, maxRequestBytes+1))
		original.Body.Close()
		if err != nil {
			return nil, safeHTTPError(ctx, err)
		}
		if len(body) > maxRequestBytes {
			return nil, errors.New("request body exceeds the size limit")
		}
	}
	quotaKey := originKey(origin)
	var previous *http.Response
	var rateLimited bool
	for attempt := 0; ; attempt++ {
		if err := c.wait(ctx, quotaKey); err != nil {
			if previous != nil {
				previous.Body.(*retainedBody).err = err
				return previous, nil
			}
			return nil, err
		}
		request := original.Clone(ctx)
		u := *original.URL
		request.URL = &u
		request.Header = original.Header.Clone()
		if request.Header == nil {
			request.Header = make(http.Header)
		}
		request.Body = nil
		if len(body) > 0 {
			request.Body = io.NopCloser(bytes.NewReader(body))
		}
		if t.publicOrigin == nil {
			for name, values := range c.headers {
				if _, exists := request.Header[name]; !exists {
					request.Header[name] = append([]string(nil), values...)
				}
			}
			for name, values := range c.secretHeaders {
				request.Header[name] = append([]string(nil), values...)
			}
			switch c.authType {
			case "query":
				query := request.URL.Query()
				for name := range query {
					if strings.EqualFold(name, c.authName) {
						delete(query, name)
					}
				}
				query.Set(c.authName, c.authValue)
				request.URL.RawQuery = query.Encode()
			case "header":
				request.Header.Set(c.authName, c.authValue)
			case "bearer":
				request.Header.Set("Authorization", "Bearer "+c.authValue)
			case "basic":
				request.SetBasicAuth(c.username, c.password)
			}
			request.Header.Del("Cookie")
			for _, cookie := range c.jar.Cookies(request.URL) {
				request.AddCookie(cookie)
			}
		} else {
			for name := range request.Header {
				if sensitiveName(name) || strings.EqualFold(name, "Referer") || strings.EqualFold(name, "Origin") {
					request.Header.Del(name)
				}
			}
			for name := range request.URL.Query() {
				if sensitiveName(name) {
					return nil, errors.New("public redirect contains credential parameters")
				}
			}
			if c.containsURLSecret(request.URL) {
				return nil, errors.New("public redirect contains a provider secret")
			}
		}
		if _, exists := request.Header["User-Agent"]; !exists {
			request.Header["User-Agent"] = []string{""}
		}
		for name, values := range request.Header {
			if !httpguts.ValidHeaderFieldName(name) {
				return nil, errors.New("invalid HTTP header name")
			}
			for _, value := range values {
				if !httpguts.ValidHeaderFieldValue(value) {
					return nil, errors.New("invalid HTTP header value")
				}
			}
		}
		if t.publicOrigin == nil && c.beforeRequest != nil {
			if err := c.beforeRequest(ctx); err != nil {
				if previous != nil {
					previous.Body.(*retainedBody).err = err
					return previous, nil
				}
				return nil, err
			}
			// The serialized admission may have waited beyond the old pacing
			// deadline. Anchor the next interval to this actual attempt.
			c.mu.Lock()
			c.next = time.Now().Add(c.interval)
			c.mu.Unlock()
		}
		timeout := c.timeout
		if t.publicOrigin != nil {
			timeout = 30 * time.Second
		}
		networkContext, networkCancel := context.WithTimeout(ctx, timeout)
		request = request.WithContext(networkContext)
		var response *http.Response
		var err error
		if c.routing != nil {
			response, err = c.routing.RoundTrip(request)
		} else {
			response, err = c.transport.RoundTrip(request)
		}
		if err != nil {
			safeErr := safeHTTPError(networkContext, err)
			networkCancel()
			if previous != nil {
				previous.Body.(*retainedBody).err = safeErr
				return previous, nil
			}
			return nil, safeErr
		}
		if t.publicOrigin == nil {
			c.jar.SetCookies(request.URL, response.Cookies())
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		response.Body.Close()
		if len(data) > maxResponseBytes {
			data = data[:maxResponseBytes]
			readErr = errResponseLimit
		}
		if readErr != nil {
			readErr = safeHTTPError(networkContext, readErr)
		}
		networkCancel()
		response.Body = &retainedBody{Reader: bytes.NewReader(data), data: data, err: readErr}
		response.ContentLength = int64(len(data))
		retry := c.observe(quotaKey, response.Header, response.StatusCode)
		if response.StatusCode == http.StatusTooManyRequests {
			rateLimited = true
		}
		// A quota response can be followed by a temporary HTML page carrying
		// HTTP 200. Replay only this observed JSON-read failure, within the
		// existing attempt limit; ordinary parse failures remain terminal.
		unexpectedHTML := readErr == nil && rateLimited && c.expectJSON && t.publicOrigin == nil &&
			request.Method == http.MethodGet && response.StatusCode == http.StatusOK &&
			invalidHTMLResponse(response.Header, data)
		transient := readErr == nil && (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
			(transientHTTPStatus(response.StatusCode) || unexpectedHTML)
		c.mu.Lock()
		quota := c.quotas[quotaKey]
		retryAt := quota.until
		if transient {
			now := time.Now()
			deadline := now.Add(transientRetryDelay(response.Header, now, attempt+1))
			if deadline.After(c.next) {
				c.next = deadline
			}
			if c.next.After(retryAt) {
				retryAt = c.next
			}
			retry = !quota.exhausted || !quota.until.IsZero()
			if !retry {
				// Local backoff must never invent a missing server quota reset.
				retryAt = time.Time{}
			}
		}
		retained := Response{Body: data, StatusCode: response.StatusCode, Header: response.Header.Clone(), URL: request.URL.String(), UnexpectedHTML: unexpectedHTML}
		if t.publicOrigin == nil {
			c.last = retained
		}
		c.mu.Unlock()
		if c.responseObserved != nil {
			c.responseObserved(retained, retryAt)
		}
		archiveQuota := response.StatusCode == http.StatusTooManyRequests && c.rateLimited != nil
		archiveTransient := transient && c.transientFailure != nil
		limit := maxHTTPRetries
		if response.StatusCode == http.StatusTooManyRequests {
			limit = c.maxQuotaRetries
		}
		retry = (!c.returnEveryResponse || archiveQuota || archiveTransient) && readErr == nil && retry && attempt < limit &&
			(request.Method == http.MethodGet || request.Method == http.MethodHead || response.StatusCode == http.StatusTooManyRequests)
		if archiveQuota || archiveTransient {
			// The callback owns this response boundary, including archive/control
			// failures. An interruption must not replay its bytes as another page.
			previous = nil
			c.mu.Lock()
			c.last = Response{}
			c.mu.Unlock()
			if archiveQuota {
				if err := c.rateLimited(ctx, retained, retryAt, retry); err != nil {
					return nil, err
				}
			} else if err := c.transientFailure(ctx, retained, retryAt, attempt+1, retry); err != nil {
				return nil, err
			}
		} else {
			previous = response
		}
		if retry {
			continue
		}
		if transient && attempt >= maxHTTPRetries {
			response.Body.(*retainedBody).err = retained.TransientError(attempt + 1)
		}
		return response, nil
	}
}

func invalidHTMLResponse(header http.Header, body []byte) bool {
	mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	return err == nil && (mediaType == "text/html" || mediaType == "application/xhtml+xml") && !json.Valid(body)
}

func transientHTTPStatus(status int) bool {
	switch status {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func transientRetryDelay(header http.Header, now time.Time, attempt int) time.Duration {
	if delay, valid := retryDelay(header.Get("Retry-After"), now); valid {
		return delay
	}
	// Three automatic retries use 2s, 4s, 8s. Keep the final cooldown bounded
	// at 8s as well, so an explicit resume cannot immediately hammer the source.
	return (2 * time.Second) << min(attempt-1, maxHTTPRetries-1)
}

type transientHTTPFailure struct {
	failure         FailureError
	status          int
	attempt         int
	unexpectedHTML  bool
	invalidDocument bool
}

func (e *transientHTTPFailure) Error() string {
	if e.invalidDocument {
		return fmt.Sprintf("Source HTML detail page was unavailable after %d attempts", e.attempt)
	}
	if e.unexpectedHTML {
		return fmt.Sprintf("source returned HTML instead of JSON after a rate limit (%d attempts)", e.attempt)
	}
	return fmt.Sprintf("source returned HTTP %d after %d attempts", e.status, e.attempt)
}

// Parsed HTML has one additional transient failure: an HTTP 200 intermediary
// page can omit the detail container. Use the same pacing, response ledger,
// backoff and attempt controls as HTTP retries. Never skip a missing record.
func (c *Client) retryHTMLDocument(ctx context.Context, response Response, attempt int) error {
	response.InvalidDocument = true
	c.mu.Lock()
	retryAt := time.Now().Add(transientRetryDelay(response.Header, time.Now(), attempt))
	if retryAt.After(c.next) {
		c.next = retryAt
	}
	retryAt = c.next
	c.mu.Unlock()
	retry := attempt <= maxHTTPRetries
	if c.transientFailure != nil {
		if err := c.transientFailure(ctx, response, retryAt, attempt, retry); err != nil {
			return err
		}
	}
	if !retry {
		return response.TransientError(attempt)
	}
	// The next Do performs the actual wait and still honors persisted quotas.
	return ctx.Err()
}

func (e *transientHTTPFailure) Unwrap() error { return &e.failure }

type retainedBody struct {
	*bytes.Reader
	data []byte
	err  error
}

func (r *retainedBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.err != nil {
		return n, r.err
	}
	return n, err
}
func (r *retainedBody) Close() error { return nil }

func (c *Client) wait(ctx context.Context, key string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return errors.New("HTTP client is closed")
		}
		quota := c.quotas[key]
		now := time.Now()
		if !quota.until.IsZero() && !quota.until.After(now) {
			quota = clientQuota{}
			delete(c.quotas, key)
		}
		if quota.exhausted && quota.until.IsZero() {
			c.mu.Unlock()
			return errQuota
		}
		until := c.next
		if quota.until.After(until) {
			until = quota.until
		}
		if !until.After(now) {
			c.next = now.Add(c.interval)
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()
		if c.beforeWait != nil {
			if err := c.beforeWait(ctx, until); err != nil {
				return err
			}
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

func (c *Client) observe(key string, header http.Header, status int) bool {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	quota := c.quotas[key]
	remaining, hasRemaining := headerInteger(header, "RateLimit-Remaining", "X-RateLimit-Remaining")
	if hasRemaining {
		quota.exhausted = remaining == 0
		if remaining > 0 {
			quota.until = time.Time{}
		}
	}
	// A 429 itself proves exhaustion, even when Remaining is absent or stale.
	if status == http.StatusTooManyRequests {
		quota.exhausted = true
	}
	var reset time.Time
	if seconds, ok := headerInteger(header, "RateLimit-Reset"); ok && seconds <= int64((1<<63-1)/time.Second) {
		reset = now.Add(time.Duration(seconds) * time.Second)
	} else if seconds, ok := headerInteger(header, "X-RateLimit-Reset"); ok {
		if !c.relativeQuotaReset {
			reset = time.Unix(seconds, 0)
		} else if seconds <= int64((1<<63-1)/time.Second) {
			reset = now.Add(time.Duration(seconds) * time.Second)
		}
	}
	if quota.exhausted && !reset.IsZero() {
		quota.until = reset
	}
	delay, retryValid := retryDelay(header.Get("Retry-After"), now)
	if retryValid && now.Add(delay).After(quota.until) {
		quota.until = now.Add(delay)
	}
	if status == http.StatusTooManyRequests && !retryValid && quota.until.IsZero() {
		quota.exhausted = true
	}
	c.quotas[key] = quota
	return status == http.StatusTooManyRequests && (retryValid || (!quota.until.IsZero() && quota.exhausted))
}

func headerInteger(header http.Header, names ...string) (int64, bool) {
	for _, name := range names {
		value, err := strconv.ParseInt(strings.TrimSpace(header.Get(name)), 10, 64)
		if err == nil && value >= 0 {
			return value, true
		}
	}
	return 0, false
}
func retryDelay(value string, now time.Time) (time.Duration, bool) {
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
	return max(0, when.Sub(now)), true
}
func safeHTTPError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, errQuota) {
		return errQuota
	}
	if errors.Is(err, errResponseLimit) {
		return errResponseLimit
	}
	var transient *transientHTTPFailure
	if errors.As(err, &transient) {
		return transient
	}
	return safeFailure(err, "network")
}
func validHTTPURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		return err == nil && n > 0 && n <= 65535
	}
	return !strings.HasSuffix(u.Host, ":")
}
func originKey(u *url.URL) string {
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else {
		number, _ := strconv.Atoi(port)
		port = strconv.Itoa(number)
	}
	return strings.ToLower(u.Scheme) + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}
func sameOrigin(a, b *url.URL) bool {
	return validHTTPURL(a) && validHTTPURL(b) && originKey(a) == originKey(b)
}
func sensitiveName(name string) bool {
	name = strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(name))
	switch name {
	case "authorization", "proxyauthorization", "cookie", "setcookie", "apikey", "key", "token", "password", "pass", "secret", "username", "credential", "credentials", "passkey", "auth", "signature", "sig", "accesskey":
		return true
	}
	return strings.HasSuffix(name, "token") || strings.HasSuffix(name, "password") || strings.HasSuffix(name, "secret") || strings.HasSuffix(name, "apikey")
}
