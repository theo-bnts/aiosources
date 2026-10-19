package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
	"golang.org/x/net/http/httpguts"
)

var safeIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var safeField = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

func providerDefaults(p model.Provider) model.Provider {
	if p.PageSize == 0 {
		p.PageSize = 100
	}
	if p.RequestInterval == "" {
		p.RequestInterval = "1s"
	}
	if p.RequestTimeout == "" {
		p.RequestTimeout = "30s"
	}
	if p.RateLimitReset == "" {
		p.RateLimitReset = "epoch"
	}
	if p.Auth.Type == "" {
		p.Auth.Type = "none"
	}
	if p.HTTP.Method == "" {
		p.HTTP.Method = http.MethodGet
	}
	if p.Adapter == "html" {
		if p.Pagination.Type == "" {
			p.Pagination.Type = "next"
		}
		if p.Pagination.In == "" {
			p.Pagination.In = "query"
		}
		if p.Pagination.PageParam == "" {
			p.Pagination.PageParam = "page"
		}
		if p.Pagination.OffsetParam == "" {
			p.Pagination.OffsetParam = "offset"
		}
	}
	if p.Adapter == "http_json" {
		if p.HTTP.Catalog {
			if p.HTTP.ItemsPath == "" {
				p.HTTP.ItemsPath = "/items"
			}
			if p.Pagination.Type == "" {
				p.Pagination.Type = "cursor"
			}
			if p.Pagination.NextPath == "" {
				p.Pagination.NextPath = "/next_cursor"
			}
		}
		if p.Pagination.Type == "" {
			p.Pagination.Type = "offset"
		}
		if p.Pagination.In == "" {
			p.Pagination.In = "query"
		}
		if p.Pagination.SizeParam == "" {
			p.Pagination.SizeParam = "limit"
		}
		if p.Pagination.PageParam == "" {
			p.Pagination.PageParam = "page"
		}
		if p.Pagination.OffsetParam == "" {
			p.Pagination.OffsetParam = "offset"
		}
		if p.Pagination.CursorParam == "" {
			p.Pagination.CursorParam = "cursor"
		}
		if p.Mapping.ID == "" {
			p.Mapping.ID = "/id"
		}
	}
	return p
}

func New(ctx context.Context, p model.Provider, mode model.RunMode, env Environment) (Connector, error) {
	p = providerDefaults(p)
	if err := Validate(p); err != nil {
		return nil, err
	}
	if mode != model.ModePreview && mode != model.ModeFull && mode != model.ModeIncremental && mode != model.ModeMetadata {
		return nil, fmt.Errorf("%w: invalid collection mode", model.ErrInvalid)
	}
	if mode == model.ModeMetadata && !p.SupportsMetadata() {
		return nil, fmt.Errorf("%w: metadata collection requires a native HTTP JSON detail mapping and enrich_fields", model.ErrInvalid)
	}
	switch p.Adapter {
	case "http_json":
		if p.Traversal != nil {
			return newWindowJSON(ctx, p, mode, env)
		}
		return newHTTPJSON(ctx, p, mode, env)
	case "torznab":
		return newTorznab(ctx, p, mode, env)
	case "html":
		return newHTML(ctx, p, mode, env)
	default:
		return nil, fmt.Errorf("%w: unknown adapter", model.ErrInvalid)
	}
}

func Validate(p model.Provider) error {
	p = providerDefaults(p)
	if err := validateCommon(p); err != nil {
		return fmt.Errorf("%w: %w", model.ErrInvalid, err)
	}
	if p.Traversal != nil && (p.Adapter != "http_json" || p.HTTP.Catalog) {
		return fmt.Errorf("%w: traversal requires a native HTTP JSON source", model.ErrInvalid)
	}
	if p.Traversal != nil && p.Traversal.MetadataAfterIncremental && !p.SupportsMetadata() {
		return fmt.Errorf("%w: automatic metadata requires configured ID recovery and enrich_fields", model.ErrInvalid)
	}
	var err error
	switch p.Adapter {
	case "http_json":
		err = validateHTTPJSON(p)
	case "torznab":
		err = validateTorznab(p)
	case "html":
		err = validateHTML(p)
	default:
		err = errors.New("unknown adapter")
	}
	if err != nil {
		return fmt.Errorf("%w: %w", model.ErrInvalid, err)
	}
	return nil
}

func validateCommon(p model.Provider) error {
	if p.HTML != nil && p.Adapter != "html" {
		return errors.New("html settings require the HTML adapter")
	}
	if p.HTTP.Catalog && p.Adapter != "http_json" {
		return errors.New("catalogue mode requires the http_json adapter")
	}
	if len(p.HTTP.IncrementalQuery) > 0 && ((p.Adapter != "http_json" && p.Adapter != "html") || p.HTTP.Catalog || p.Traversal != nil) {
		return errors.New("http.incremental_query requires ordinary native JSON or HTML pagination")
	}
	if p.Version != 1 {
		return errors.New("provider version must be 1")
	}
	if !safeIdentifier.MatchString(p.ID) {
		return errors.New("provider ID must be a safe identifier")
	}
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 {
		return errors.New("provider name is required and limited to 200 bytes")
	}
	u, err := url.Parse(p.URL)
	if err != nil || !validHTTPURL(u) {
		return errors.New("provider URL must be absolute HTTP(S) without userinfo or fragment")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("invalid provider URL query")
	}
	for name := range query {
		if sensitiveName(name) {
			return errors.New("provider URL must not contain credentials; use secret refs")
		}
	}
	interval, err := time.ParseDuration(p.RequestInterval)
	if err != nil || interval <= 0 || interval > 24*time.Hour {
		return errors.New("request_interval must be a positive duration no longer than 24h")
	}
	timeout, err := time.ParseDuration(p.RequestTimeout)
	if err != nil || timeout <= 0 || timeout > 15*time.Minute {
		return errors.New("request_timeout must be a positive duration no longer than 15m")
	}
	if limits := p.RequestLimits; limits != nil {
		if limits.PerMinute == 0 && limits.PerHour == 0 && limits.PerDay == 0 {
			return errors.New("request_limits must enable at least one positive limit")
		}
		for _, count := range []int{limits.PerMinute, limits.PerHour, limits.PerDay} {
			if count < 0 || count > 1_000_000 {
				return errors.New("request_limits counts must be between 0 and 1000000")
			}
		}
	}
	if p.RateLimitReset != "epoch" && p.RateLimitReset != "relative" {
		return errors.New("rate_limit_reset must be epoch or relative")
	}
	if p.PageSize < 1 || p.PageSize > 10000 {
		return errors.New("page_size must be between 1 and 10000")
	}
	for _, category := range p.Search.Categories {
		if category < 0 {
			return errors.New("search categories must be nonnegative IDs")
		}
	}
	schedule, err := scheduling.Settings(p)
	if err != nil {
		return err
	}
	if _, err := scheduling.Parse(schedule); err != nil {
		return err
	}
	if _, err := pointerTokens(p.Mapping.ID); err != nil {
		return err
	}
	for name, path := range p.Mapping.Fields {
		if !safeField.MatchString(name) {
			return errors.New("mapping field names must be safe identifiers")
		}
		if _, err := pointerTokens(path); err != nil {
			return err
		}
	}
	for _, name := range p.Output.Fields {
		if !safeField.MatchString(name) {
			return errors.New("output field names must be safe identifiers")
		}
	}
	seenHeaders := make(map[string]bool, len(p.HTTP.Headers)+len(p.HTTP.SecretHeaders))
	for name, value := range p.HTTP.Headers {
		canonical := http.CanonicalHeaderKey(name)
		if seenHeaders[canonical] {
			return errors.New("duplicate HTTP header name")
		}
		seenHeaders[canonical] = true
		if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
			return errors.New("invalid static HTTP header")
		}
		if sensitiveName(name) {
			return errors.New("sensitive HTTP headers require secret refs")
		}
		if strings.EqualFold(name, "Host") || strings.EqualFold(name, "Content-Length") || strings.EqualFold(name, "Transfer-Encoding") || strings.EqualFold(name, "Connection") {
			return errors.New("HTTP transport headers cannot be configured")
		}
	}
	for name, ref := range p.HTTP.SecretHeaders {
		canonical := http.CanonicalHeaderKey(name)
		if seenHeaders[canonical] {
			return errors.New("duplicate or conflicting secret HTTP header name")
		}
		seenHeaders[canonical] = true
		if !httpguts.ValidHeaderFieldName(name) || !safeIdentifier.MatchString(ref) {
			return errors.New("invalid secret HTTP header or ref")
		}
		if strings.EqualFold(name, "Cookie") || strings.EqualFold(name, "Set-Cookie") {
			return errors.New("session cookies must use auth.type=cookie")
		}
		if strings.EqualFold(name, "Host") || strings.EqualFold(name, "Content-Length") || strings.EqualFold(name, "Transfer-Encoding") || strings.EqualFold(name, "Connection") {
			return errors.New("HTTP transport headers cannot be configured")
		}
	}
	for _, values := range []map[string]any{p.HTTP.Query, p.HTTP.IncrementalQuery} {
		for name, value := range values {
			if name == "" || sensitiveName(name) {
				return errors.New("HTTP query credentials require secret refs")
			}
			if _, err := queryValues(value); err != nil {
				return err
			}
		}
	}
	if body, err := json.Marshal(p.HTTP.Body); err != nil || len(body) > maxRequestBytes {
		return errors.New("HTTP body must be JSON within the request size limit")
	}
	if err := validateStaticBody(p.HTTP.Body, 0); err != nil {
		return err
	}
	authType := p.Auth.Type
	if authType == "api_key" {
		if p.Auth.In != "query" && p.Auth.In != "header" {
			return errors.New("api_key auth.in must be query or header")
		}
		authType = p.Auth.In
	} else if p.Auth.In != "" && p.Auth.In != authType {
		return errors.New("auth.in conflicts with auth.type")
	}
	switch authType {
	case "none":
		if p.Auth.SecretRef != "" || p.Auth.UsernameRef != "" || p.Auth.PasswordRef != "" || p.Auth.Name != "" {
			return errors.New("auth.none must not include credential refs")
		}
	case "query", "header", "bearer", "cookie":
		if !safeIdentifier.MatchString(p.Auth.SecretRef) || p.Auth.UsernameRef != "" || p.Auth.PasswordRef != "" {
			return errors.New("authentication requires one valid secret_ref")
		}
		if authType == "query" || authType == "header" {
			if !httpguts.ValidHeaderFieldName(p.Auth.Name) {
				return errors.New("authentication parameter name is required and must be a token")
			}
			if authType == "header" && (strings.EqualFold(p.Auth.Name, "Cookie") || strings.EqualFold(p.Auth.Name, "Host") || strings.EqualFold(p.Auth.Name, "Content-Length") || strings.EqualFold(p.Auth.Name, "Transfer-Encoding")) {
				return errors.New("invalid authentication header; cookies require auth.type=cookie")
			}
		} else if p.Auth.Name != "" {
			return errors.New("bearer and cookie authentication do not accept a name")
		}
	case "basic":
		if !safeIdentifier.MatchString(p.Auth.UsernameRef) || !safeIdentifier.MatchString(p.Auth.PasswordRef) || p.Auth.SecretRef != "" || p.Auth.Name != "" {
			return errors.New("basic authentication requires username_ref and password_ref only")
		}
	default:
		return errors.New("unknown authentication type")
	}
	if authType == "header" && seenHeaders[http.CanonicalHeaderKey(p.Auth.Name)] {
		return errors.New("authentication header conflicts with HTTP headers")
	}
	if (authType == "bearer" || authType == "basic") && seenHeaders["Authorization"] {
		return errors.New("Authorization secret header conflicts with authentication")
	}
	if authType == "query" {
		for _, values := range []map[string]any{p.HTTP.Query, p.HTTP.IncrementalQuery} {
			for name := range values {
				if strings.EqualFold(name, p.Auth.Name) {
					return errors.New("authentication query parameter conflicts with HTTP query")
				}
			}
		}
		for name := range query {
			if strings.EqualFold(name, p.Auth.Name) {
				return errors.New("authentication query parameter must not be present in provider URL")
			}
		}
	}
	return nil
}

func validateStaticBody(value any, depth int) error {
	if depth > 128 {
		return errors.New("HTTP body nesting exceeds the limit")
	}
	switch value := value.(type) {
	case map[string]any:
		for name, child := range value {
			if sensitiveName(name) {
				return errors.New("HTTP body cannot include literal credentials")
			}
			if err := validateStaticBody(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := validateStaticBody(child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func Adapters() []model.AdapterInfo {
	return []model.AdapterInfo{
		{Type: "http_json", Name: "HTTP JSON", Description: "Configurable JSON Pointer mappings and explicit pagination.", Template: httpJSONTemplate()},
		{Type: "torznab", Name: "Torznab", Description: "Discovered capabilities, raw RSS and category-scoped collection.", Template: torznabTemplate()},
		{Type: "html", Name: "HTML", Description: "Read website listings and optional detail pages with CSS selectors and resumable pagination.", Template: htmlTemplate()},
		{Type: "http_json", Profile: "unit3d", Name: "UNIT3D", Description: "UNIT3D torrent API with native IDs, site-specific filters and resumable pagination.", Template: unit3dTemplate()},
	}
}
