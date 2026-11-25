package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	sdk "github.com/theo-bnts/aiosources/torznab"
	"golang.org/x/text/unicode/norm"
)

// TorznabCapabilityError carries a safe, credential-free error classification
// for interactive Torznab t=caps inspection.
type TorznabCapabilityError struct {
	Status  int
	Code    string
	Message string
}

func (e *TorznabCapabilityError) Error() string { return e.Message }

const (
	torznabProbeDefaultTimeout = 15 * time.Second
	torznabProbeMaxTimeout     = 20 * time.Second
	torznabMaxSuggestedIDLen   = 64
)

var canonicalTorznabModes = []sdk.SearchMode{
	sdk.ModeSearch,
	sdk.ModeTV,
	sdk.ModeMovie,
	sdk.ModeMusic,
	sdk.ModeBook,
}

// InspectTorznabCapabilities queries a Torznab source's t=caps endpoint using
// the draft's connection, authentication, and outbound proxy settings without
// requiring unrelated configuration fields (name, id, schedules, categories)
// to be complete.
func InspectTorznabCapabilities(ctx context.Context, p model.Provider, env Environment) (model.TorznabCapabilitiesResult, error) {
	if err := ctx.Err(); err != nil {
		return model.TorznabCapabilitiesResult{}, err
	}
	if p.Adapter != "torznab" {
		return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "configuration",
			Message: "Capability discovery is only available for Torznab sources.",
		}
	}
	rawURL := strings.TrimSpace(p.URL)
	if rawURL == "" {
		return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "configuration",
			Message: "Enter a Source URL in Connection before fetching capabilities.",
		}
	}
	method := strings.ToUpper(strings.TrimSpace(p.HTTP.Method))
	if (method != "" && method != "GET") || p.HTTP.Body != nil {
		return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "configuration",
			Message: "Torznab capability discovery requires HTTP GET without a request body.",
		}
	}
	var replaceControls bool
	if value, exists := p.Options["replace_invalid_xml_controls"]; exists {
		flag, ok := value.(bool)
		if !ok {
			return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "configuration",
				Message: "replace_invalid_xml_controls must be a boolean.",
			}
		}
		replaceControls = flag
	}
	for name := range p.HTTP.Query {
		if isReservedTorznabParam(name) {
			return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "configuration",
				Message: "Static Torznab query parameters cannot override reserved protocol parameters.",
			}
		}
	}

	probe := buildTorznabProbeProvider(p, rawURL, replaceControls)
	if err := validateCommon(probe); err != nil {
		message := err.Error()
		if strings.Contains(message, "must not contain credentials") || strings.Contains(message, "must not be present in provider URL") {
			message = "Remove credentials from the Source URL and configure them in Authentication using a secret reference."
		}
		return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "configuration",
			Message: message,
		}
	}

	zero := 0
	probeEnv := env
	probeEnv.BeforeRequest = nil
	probeEnv.MaxQuotaRetries = &zero
	probeEnv.SourceRetries = model.SourceRetryPolicy{
		Retries:        0,
		InitialSeconds: 1,
		MaximumSeconds: 1,
	}

	client, err := NewClient(ctx, probe, probeEnv)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return model.TorznabCapabilitiesResult{}, err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
				Status:  http.StatusGatewayTimeout,
				Code:    "timeout",
				Message: "The Torznab server timed out before returning capabilities.",
			}
		}
		if err.Error() == "required provider secret is unavailable" {
			return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "missing_secret",
				Message: "The configured credential secret was not found in Secrets. Create or select a secret in Authentication, or switch Authentication type to No authentication for public indexers.",
			}
		}
		return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "configuration",
			Message: err.Error(),
		}
	}
	defer client.Close()

	originalURL, _ := url.Parse(probe.URL)
	target := *originalURL
	parameters := target.Query()
	for name, value := range probe.HTTP.Query {
		values, err := queryValues(value)
		if err != nil {
			return model.TorznabCapabilitiesResult{}, &TorznabCapabilityError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "configuration",
				Message: err.Error(),
			}
		}
		parameters[name] = values
	}
	target.RawQuery = parameters.Encode()

	sdkClient, err := sdk.Open(ctx, sdk.Config{
		URL:                       target.String(),
		HTTPClient:                client.HTTPClient(),
		PageSize:                  100,
		RequestInterval:           time.Nanosecond,
		DisableRetries:            true,
		ReplaceInvalidXMLControls: replaceControls,
	})
	if err != nil {
		return model.TorznabCapabilitiesResult{}, classifyTorznabCapabilityError(err)
	}

	caps := sdkClient.Capabilities()
	endpoint := resolveDiscoveredEndpoint(originalURL, sdkClient.Endpoint())
	title := sanitizeCapabilityText(caps.Title, 200)
	suggestedID := suggestTorznabSourceID(title, endpoint)
	limits := model.TorznabLimits{
		Default: caps.Limits.Default,
		Max:     caps.Limits.Max,
	}
	searches, recommendedMode := normalizeTorznabSearches(caps.Searches)
	categories := convertTorznabCategories(caps.Categories)
	rateLimit := convertTorznabRateLimit(sdkClient.RateLimit())

	return model.TorznabCapabilitiesResult{
		Endpoint:            endpoint,
		Title:               title,
		SuggestedID:         suggestedID,
		Limits:              limits,
		RecommendedPageSize: recommendedTorznabPageSize(limits),
		Searches:            searches,
		RecommendedMode:     recommendedMode,
		Categories:          categories,
		RateLimit:           rateLimit,
	}, nil
}

func buildTorznabProbeProvider(p model.Provider, rawURL string, replaceControls bool) model.Provider {
	probeID := "torznab-probe"
	if model.ValidSourceID(p.ID) {
		probeID = p.ID
	}
	timeout := torznabProbeDefaultTimeout
	if strings.TrimSpace(p.RequestTimeout) != "" {
		if parsed, err := time.ParseDuration(strings.TrimSpace(p.RequestTimeout)); err == nil && parsed > 0 {
			if parsed > torznabProbeMaxTimeout {
				parsed = torznabProbeMaxTimeout
			}
			timeout = parsed
		}
	}
	rateLimitReset := p.RateLimitReset
	if rateLimitReset == "" {
		rateLimitReset = "epoch"
	}
	auth := p.Auth
	if auth.Type == "" {
		auth.Type = "none"
	}
	var options map[string]any
	if replaceControls {
		options = map[string]any{"replace_invalid_xml_controls": true}
	}
	return model.Provider{
		Version:         1,
		ID:              probeID,
		Name:            "Torznab capability probe",
		Adapter:         "torznab",
		URL:             rawURL,
		Enabled:         false,
		Auth:            auth,
		RequestInterval: "50ms",
		RequestTimeout:  timeout.String(),
		RateLimitReset:  rateLimitReset,
		PageSize:        100,
		HTTP: model.HTTPConfig{
			Method:        "GET",
			Headers:       p.HTTP.Headers,
			SecretHeaders: p.HTTP.SecretHeaders,
			Query:         p.HTTP.Query,
		},
		Options: options,
	}
}

func isReservedTorznabParam(name string) bool {
	switch strings.ToLower(name) {
	case "t", "apikey", "q", "cat", "offset", "limit", "extended", "o":
		return true
	}
	return false
}

func classifyTorznabCapabilityError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	code := FailureCode(err)
	if errors.Is(err, context.DeadlineExceeded) || code == "timeout" {
		return &TorznabCapabilityError{
			Status:  http.StatusGatewayTimeout,
			Code:    "timeout",
			Message: "The Torznab server timed out before returning capabilities.",
		}
	}
	var apiErr *sdk.APIError
	if errors.As(err, &apiErr) && apiErr.Code >= 100 && apiErr.Code <= 102 {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "authentication",
			Message: "The Torznab server rejected the configured credentials. Check Authentication settings and Secrets.",
		}
	}
	var httpErr *sdk.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "authentication",
			Message: "The Torznab server rejected the configured credentials. Check Authentication settings and Secrets.",
		}
	}
	if errors.Is(err, sdk.ErrRateLimited) || errors.Is(err, errQuota) || (errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusTooManyRequests) {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "rate_limited",
			Message: "The Torznab server rate-limited the capabilities request. Try again later.",
		}
	}
	if code == "certificate" {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "certificate",
			Message: "TLS certificate verification failed for the Torznab server.",
		}
	}
	if code == "network" {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "network",
			Message: "Could not reach the Torznab server. Check the Source URL and outbound network routing.",
		}
	}
	if errors.As(err, &apiErr) {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "api_error",
			Message: fmt.Sprintf("The Torznab server returned API error %d while fetching capabilities.", apiErr.Code),
		}
	}
	if errors.As(err, &httpErr) && httpErr.StatusCode >= 500 {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "http",
			Message: fmt.Sprintf("The Torznab server returned HTTP %d while fetching capabilities.", httpErr.StatusCode),
		}
	}
	if errors.Is(err, sdk.ErrNotFound) {
		return &TorznabCapabilityError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "not_found",
			Message: "No valid Torznab capabilities (t=caps) document was found at this URL. Check that the URL points to a Torznab endpoint.",
		}
	}
	if errors.As(err, &httpErr) {
		return &TorznabCapabilityError{
			Status:  http.StatusBadGateway,
			Code:    "http",
			Message: fmt.Sprintf("The Torznab server returned HTTP %d while fetching capabilities.", httpErr.StatusCode),
		}
	}
	return &TorznabCapabilityError{
		Status:  http.StatusBadGateway,
		Code:    "parse",
		Message: "The Torznab server returned an unreadable capabilities document.",
	}
}

func resolveDiscoveredEndpoint(original *url.URL, discovered string) string {
	parsed, err := url.Parse(discovered)
	if err != nil {
		return discovered
	}
	if original != nil && original.RawQuery != "" && strings.EqualFold(original.Host, parsed.Host) && original.Path == parsed.Path {
		kept := url.Values{}
		for name, values := range original.Query() {
			if isReservedTorznabParam(name) || sensitiveName(name) {
				continue
			}
			kept[name] = values
		}
		if len(kept) > 0 {
			parsed.RawQuery = kept.Encode()
		}
	}
	return parsed.String()
}

func sanitizeCapabilityText(raw string, maxBytes int) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	cleaned := strings.TrimSpace(b.String())
	if maxBytes <= 0 || len(cleaned) <= maxBytes {
		return cleaned
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(cleaned[cut]) {
		cut--
	}
	return strings.TrimSpace(cleaned[:cut])
}

func suggestTorznabSourceID(title, endpoint string) string {
	validSuggested := func(candidate string) bool {
		if !model.ValidSourceID(candidate) {
			return false
		}
		lower := strings.ToLower(candidate)
		return lower != "new" && lower != "settings"
	}
	if candidate := slugifySourceID(title); validSuggested(candidate) {
		return candidate
	}
	if parsed, err := url.Parse(endpoint); err == nil {
		host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
		if candidate := slugifySourceID(host); validSuggested(candidate) {
			return candidate
		}
	}
	return ""
}

func slugifySourceID(raw string) string {
	decomposed := norm.NFD.String(raw)
	var b strings.Builder
	prevSep := true
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r = r - 'A' + 'a'
		}
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSep = false
		case r == '-' || r == '_' || unicode.IsSpace(r) || r == '.' || r == '/':
			if !prevSep && b.Len() > 0 {
				b.WriteByte('_')
				prevSep = true
			}
		default:
			if !prevSep && b.Len() > 0 {
				b.WriteByte('_')
				prevSep = true
			}
		}
		if b.Len() >= torznabMaxSuggestedIDLen {
			break
		}
	}
	return strings.Trim(b.String(), "_-")
}

func recommendedTorznabPageSize(limits model.TorznabLimits) int {
	chosen := limits.Max
	if chosen <= 0 {
		chosen = limits.Default
	}
	if chosen <= 0 {
		return 0
	}
	if chosen > 10000 {
		return 10000
	}
	return chosen
}

func normalizeTorznabSearches(searches map[sdk.SearchMode]sdk.SearchCapability) ([]model.TorznabSearchModeCapability, string) {
	items := make([]model.TorznabSearchModeCapability, 0, len(searches))
	recommended := ""
	for _, mode := range canonicalTorznabModes {
		capability, exists := searches[mode]
		if !exists {
			continue
		}
		params := make([]string, 0, len(capability.SupportedParams))
		for _, param := range capability.SupportedParams {
			if cleaned := sanitizeCapabilityText(param, 64); cleaned != "" {
				params = append(params, cleaned)
			}
		}
		items = append(items, model.TorznabSearchModeCapability{
			Mode:            string(mode),
			Available:       capability.Available,
			SupportedParams: params,
		})
		if capability.Available {
			if mode == sdk.ModeSearch || recommended == "" {
				recommended = string(mode)
			}
		}
	}
	return items, recommended
}

func convertTorznabCategories(categories []sdk.Category) []model.TorznabCategoryCapability {
	seen := make(map[int]int)
	return convertTorznabCategoryLevel(categories, seen)
}

func convertTorznabCategoryLevel(categories []sdk.Category, seen map[int]int) []model.TorznabCategoryCapability {
	result := make([]model.TorznabCategoryCapability, 0, len(categories))
	indexByID := make(map[int]int, len(categories))
	for _, category := range categories {
		if category.ID < 0 {
			continue
		}
		if _, duplicate := seen[category.ID]; duplicate {
			// If a duplicate parent appears at the same level, merge any unseen subcategories into it.
			if existingIdx, sameLevel := indexByID[category.ID]; sameLevel && len(category.Subcategories) > 0 {
				merged := convertTorznabCategoryLevel(category.Subcategories, seen)
				if len(merged) > 0 {
					result[existingIdx].Subcategories = append(result[existingIdx].Subcategories, merged...)
				}
			}
			continue
		}
		seen[category.ID] = 1
		name := sanitizeCapabilityText(category.Name, 160)
		if name == "" {
			name = fmt.Sprintf("Category %d", category.ID)
		}
		item := model.TorznabCategoryCapability{
			ID:   category.ID,
			Name: name,
		}
		if len(category.Subcategories) > 0 {
			if subs := convertTorznabCategoryLevel(category.Subcategories, seen); len(subs) > 0 {
				item.Subcategories = subs
			}
		}
		indexByID[category.ID] = len(result)
		result = append(result, item)
	}
	return result
}

func convertTorznabRateLimit(rate sdk.RateLimit) *model.TorznabRateLimitHint {
	if rate.Limit == nil && rate.Remaining == nil && rate.ResetAt == nil {
		return nil
	}
	return &model.TorznabRateLimitHint{
		Limit:     rate.Limit,
		Remaining: rate.Remaining,
		ResetAt:   rate.ResetAt,
	}
}
