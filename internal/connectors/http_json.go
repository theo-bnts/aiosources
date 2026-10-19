package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type httpJSONConnector struct {
	provider        model.Provider
	client          *Client
	localInstanceID string
	localCategories map[int64]struct{}
	preserveQuery   bool
	incremental     bool
}
type jsonCursor struct {
	Version int    `json:"version"`
	Page    int    `json:"page"`
	Offset  int    `json:"offset"`
	Cursor  string `json:"cursor,omitempty"`
	URL     string `json:"url,omitempty"`
	Total   *int   `json:"total,omitempty"`
	Done    bool   `json:"done,omitempty"`
}

func newHTTPJSON(ctx context.Context, p model.Provider, mode model.RunMode, env Environment) (Connector, error) {
	categories, err := localCategories(p)
	if err != nil {
		return nil, err
	}
	preserveQuery, _ := p.Options["preserve_query_on_next"].(bool)
	if mode == model.ModeIncremental && len(p.HTTP.IncrementalQuery) > 0 {
		query := make(map[string]any, len(p.HTTP.Query)+len(p.HTTP.IncrementalQuery))
		maps.Copy(query, p.HTTP.Query)
		maps.Copy(query, p.HTTP.IncrementalQuery)
		p.HTTP.Query = query
		// Continuation URLs must not restore the Full ordering or discard the
		// selected catalogue filters after the first Incremental page.
		preserveQuery = true
	}
	client, err := NewClient(ctx, p, env)
	if err != nil {
		return nil, err
	}
	if p.HTTP.Catalog {
		if env.LocalInstanceID == "" {
			_ = client.Close()
			return nil, errors.New("local catalogue identity is unavailable")
		}
		// Resolve and pin allowed addresses at dial time; proxies could bypass
		// destination checks. TLS retains the standard verified configuration.
		client.transport.Proxy = nil
		client.transport.DialContext = catalogDial
		client.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		// Archive each failed response before a durable run resume can retry it.
		client.returnEveryResponse = true
	}
	return &httpJSONConnector{provider: p, client: client, localInstanceID: env.LocalInstanceID, localCategories: categories, preserveQuery: preserveQuery, incremental: mode == model.ModeIncremental}, nil
}
func (c *httpJSONConnector) Close() error { return c.client.Close() }

func validateHTTPJSON(p model.Provider) error {
	if p.HTTP.Catalog {
		return validateRemoteCatalog(p)
	}
	for name := range p.Options {
		if name != "published_at_unit" && name != "local_categories" && name != "null_items_as_empty" && name != "preserve_query_on_next" && name != "allow_total_growth" {
			return errors.New("unknown JSON adapter option")
		}
	}
	if _, err := publishedAtUnit(p); err != nil {
		return err
	}
	categories, err := localCategories(p)
	if err != nil {
		return err
	}
	if len(categories) > 0 && p.Mapping.Fields["categories"] == "" {
		return errors.New("local_categories requires a categories field mapping")
	}
	for _, name := range []string{"null_items_as_empty", "preserve_query_on_next", "allow_total_growth"} {
		if value, exists := p.Options[name]; exists {
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be a boolean", name)
			}
		}
	}
	nullItems, _ := p.Options["null_items_as_empty"].(bool)
	preserveQuery, _ := p.Options["preserve_query_on_next"].(bool)
	allowGrowth, _ := p.Options["allow_total_growth"].(bool)
	if allowGrowth && p.Pagination.TotalPath == "" {
		return errors.New("allow_total_growth requires total_path")
	}
	if allowGrowth && p.Schedule.KnownPages > 0 && len(p.HTTP.IncrementalQuery) == 0 {
		return errors.New("allow_total_growth with known pages requires http.incremental_query for newest-first ordering")
	}
	if p.Traversal != nil && (len(categories) > 0 || nullItems || preserveQuery || allowGrowth) {
		return errors.New("local category, null-item, next-query, and total-growth options require ordinary JSON pagination")
	}
	if p.HTTP.Method != http.MethodGet && p.HTTP.Method != http.MethodPost {
		return errors.New("JSON supports GET or POST")
	}
	if p.HTTP.Method == http.MethodGet && p.HTTP.Body != nil {
		return errors.New("GET requests cannot include a JSON body")
	}
	pagination := p.Pagination
	switch pagination.Type {
	case "page", "offset", "cursor", "none":
	default:
		return errors.New("unknown pagination type")
	}
	if pagination.In != "query" && pagination.In != "body" {
		return errors.New("pagination.in must be query or body")
	}
	if pagination.In == "body" {
		if p.HTTP.Method != http.MethodPost {
			return errors.New("body pagination requires POST")
		}
		if p.HTTP.Body != nil {
			if _, ok := p.HTTP.Body.(map[string]any); !ok {
				return errors.New("body pagination requires an object body")
			}
		}
	}
	if pagination.Start < 0 {
		return errors.New("pagination.start must be nonnegative")
	}
	for _, pointer := range []string{p.HTTP.ItemsPath, pagination.NextPath, pagination.TotalPath, pagination.CurrentPath} {
		if _, err := pointerTokens(pointer); err != nil {
			return err
		}
	}
	if pagination.Type == "cursor" && pagination.NextPath == "" {
		return errors.New("cursor pagination requires next_path")
	}
	parameter := ""
	switch pagination.Type {
	case "page":
		parameter = pagination.PageParam
	case "offset":
		parameter = pagination.OffsetParam
	case "cursor":
		parameter = pagination.CursorParam
	}
	for _, name := range []string{parameter, pagination.SizeParam} {
		if name != "" && (!safeField.MatchString(name) || sensitiveName(name)) {
			return errors.New("pagination parameter names must be safe non-credential names")
		}
		if name != "" && p.Auth.Name != "" && strings.EqualFold(name, p.Auth.Name) {
			return errors.New("pagination cannot overwrite authentication parameters")
		}
	}
	if parameter != "" && parameter == pagination.SizeParam {
		return errors.New("pagination position and size parameters must differ")
	}
	if p.Traversal != nil {
		return validateWindowJSON(p)
	}
	return nil
}

func queryValues(value any) ([]string, error) {
	if value == nil {
		return nil, errors.New("HTTP query values cannot be null")
	}
	scalar := func(value any) (string, error) {
		switch v := value.(type) {
		case string:
			return v, nil
		case bool:
			return strconv.FormatBool(v), nil
		case json.Number:
			if !json.Valid([]byte(v)) {
				return "", errors.New("invalid numeric query value")
			}
			return string(v), nil
		case int:
			return strconv.Itoa(v), nil
		case int64:
			return strconv.FormatInt(v, 10), nil
		case float64:
			encoded, err := json.Marshal(v)
			if err == nil {
				return string(encoded), nil
			}
		}
		return "", errors.New("HTTP query values must be scalars or scalar arrays")
	}
	var entries []any
	switch values := value.(type) {
	case []any:
		entries = values
	case []string:
		result := append([]string(nil), values...)
		return result, nil
	default:
		entries = []any{value}
	}
	result := make([]string, len(entries))
	for i, entry := range entries {
		text, err := scalar(entry)
		if err != nil {
			return nil, err
		}
		result[i] = text
	}
	return result, nil
}

func (c *httpJSONConnector) Fetch(ctx context.Context, checkpoint json.RawMessage) (model.Page, error) {
	if c.provider.HTTP.Catalog {
		return c.fetchCatalog(ctx, checkpoint)
	}
	p := c.provider
	state := jsonCursor{Version: 1, Page: p.Pagination.Start}
	if p.Pagination.Type == "offset" {
		state.Offset = p.Pagination.Start
	}
	if p.Pagination.Type == "cursor" && p.Pagination.Start > 0 {
		state.Cursor = strconv.Itoa(p.Pagination.Start)
	}
	if len(checkpoint) > 0 {
		if err := decodeJSON(checkpoint, &state); err != nil || state.Version != 1 || state.Page < 0 || state.Offset < 0 || (state.Total != nil && *state.Total < 0) {
			return model.Page{}, errors.New("invalid JSON continuation state")
		}
		if state.URL != "" {
			if _, err := c.client.target(state.URL); err != nil {
				return model.Page{}, errors.New("invalid JSON continuation URL")
			}
		}
	}
	if state.Done {
		return model.Page{Done: true, Next: checkpoint}, nil
	}
	request, err := c.request(state)
	if err != nil {
		return model.Page{}, err
	}
	response, err := c.client.Do(ctx, request)
	page := model.Page{Body: response.Body, ContentType: response.Header.Get("Content-Type"), Next: checkpoint, Position: strconv.Itoa(state.Offset), Metadata: map[string]any{"pagination": p.Pagination.Type, "page": state.Page, "offset": state.Offset}}
	if response.StatusCode != 0 {
		page.Metadata["http_status"] = response.StatusCode
	}
	fail := func(err error, reasons ...string) (model.Page, error) {
		failure := safeFailure(err, "parse")
		page.Error = failure.Error()
		page.Metadata["failure_code"] = FailureCode(failure)
		if len(reasons) > 0 && FailureReasonMessage(reasons[0]) != "" {
			page.Metadata["failure_reason"] = reasons[0]
			page.Error = FailureReasonMessage(reasons[0])
		}
		SanitizeFailureDiagnostics(page.Metadata)
		page.Done = false
		return page, failure
	}
	if page.ContentType == "" {
		page.ContentType = "application/json"
	}
	if err != nil {
		return fail(err)
	}
	var root any
	if err := decodeJSON(response.Body, &root); err != nil {
		return fail(err)
	}
	selected, found, err := rawPointer(response.Body, p.HTTP.ItemsPath)
	if err != nil {
		return fail(err)
	}
	trimmed := bytes.TrimSpace(selected)
	nullItems, _ := p.Options["null_items_as_empty"].(bool)
	if !found || len(trimmed) == 0 || (trimmed[0] != '[' && !(nullItems && bytes.Equal(trimmed, []byte("null")))) {
		return fail(errors.New("items_path must select a JSON array"))
	}
	var items []json.RawMessage
	if err := decodeJSON(selected, &items); err != nil {
		return fail(err)
	}
	unit, _ := publishedAtUnit(p)
	page.Items = make([]model.Record, 0, len(items))
	for _, raw := range items {
		id, fields, err := FieldsFromJSON(raw, p.Mapping, unit)
		record := model.Record{SourceID: id, Raw: raw, Fields: fields, ContentType: "application/json"}
		if err != nil {
			record.Error = err.Error()
		}
		record.Ignored = !matchesLocalCategories(c.localCategories, fields)
		page.Items = append(page.Items, record)
	}
	if p.Pagination.CurrentPath != "" {
		expected := state.Offset
		if p.Pagination.Type == "page" {
			expected = state.Page
		}
		value, err := requiredPointer(root, p.Pagination.CurrentPath)
		if err != nil {
			page.Metadata["expected_position"] = expected
			return fail(err, "invalid_position")
		}
		current, err := boundedCount(value)
		if err != nil {
			page.Metadata["expected_position"] = expected
			return fail(errors.New("current_path must select a nonnegative integer"), "invalid_position")
		}
		if current != expected {
			page.Metadata["expected_position"], page.Metadata["actual_position"] = expected, current
			return fail(fmt.Errorf("%w: returned position differs from requested position", model.ErrStalled), "position_mismatch")
		}
	}
	next := state
	if p.Pagination.TotalPath != "" {
		value, err := requiredPointer(root, p.Pagination.TotalPath)
		if err != nil {
			if state.Total != nil {
				page.Metadata["expected_total"] = *state.Total
			}
			return fail(err, "invalid_total")
		}
		total, err := boundedCount(value)
		if err != nil {
			if state.Total != nil {
				page.Metadata["expected_total"] = *state.Total
			}
			return fail(errors.New("total_path must select a nonnegative integer"), "invalid_total")
		}
		allowGrowth, _ := p.Options["allow_total_growth"].(bool)
		// Incremental merges records, while exhaustive traversal still requires monotonic growth.
		if state.Total != nil && total != *state.Total && (!allowGrowth || total < *state.Total && !c.incremental) {
			page.Metadata["expected_total"], page.Metadata["actual_total"] = *state.Total, total
			return fail(fmt.Errorf("%w: advertised total changed during traversal", model.ErrStalled), "total_changed")
		}
		next.Total = &total
	}
	if len(items) > int(^uint(0)>>1)-state.Offset {
		return fail(errors.New("pagination offset overflow"), "pagination_overflow")
	}
	next.Offset = state.Offset + len(items)
	if next.Total != nil {
		page.Metadata["total"] = *next.Total
		if next.Offset > *next.Total {
			page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Offset
			return fail(fmt.Errorf("%w: records exceed advertised total", model.ErrStalled), "records_exceed_total")
		}
		if len(items) == 0 && next.Offset < *next.Total {
			page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Offset
			return fail(fmt.Errorf("%w: empty page before advertised total", model.ErrStalled), "empty_before_total")
		}
	}
	explicitNext, terminator := false, false
	if p.Pagination.NextPath != "" {
		value, err := requiredPointer(root, p.Pagination.NextPath)
		if err != nil {
			return fail(err, "invalid_continuation")
		}
		terminator = value == nil || value == "" || value == false
		if !terminator {
			explicitNext = true
			text := FieldString(value)
			if text == "" {
				return fail(errors.New("next_path must select a cursor, URL, integer, or null terminator"), "invalid_continuation")
			}
			if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "?") || strings.HasPrefix(text, "http:") || strings.HasPrefix(text, "https:") {
				base, _ := c.client.target(request.URL)
				reference, err := url.Parse(text)
				if err != nil {
					return fail(errors.New("invalid next URL"), "invalid_continuation")
				}
				target := base.ResolveReference(reference)
				if !sameOrigin(c.client.origin, target) {
					return fail(errors.New("cross-origin next URL refused"), "invalid_continuation")
				}
				next.URL = target.String()
				if next.URL == state.URL {
					return fail(fmt.Errorf("%w: repeated next URL", model.ErrStalled), "continuation_repeated")
				}
			} else {
				next.URL = ""
				switch p.Pagination.Type {
				case "cursor":
					if text == state.Cursor {
						return fail(fmt.Errorf("%w: repeated cursor", model.ErrStalled), "continuation_repeated")
					}
					next.Cursor = text
				case "page", "offset":
					position, err := boundedCount(value)
					if err != nil {
						return fail(errors.New("next position must be a nonnegative integer"), "invalid_continuation")
					}
					if p.Pagination.Type == "page" {
						if state.Page == int(^uint(0)>>1) {
							return fail(fmt.Errorf("%w: pagination page overflow", model.ErrStalled), "pagination_overflow")
						}
						if position != state.Page+1 {
							page.Metadata["expected_position"], page.Metadata["actual_position"] = state.Page+1, position
							return fail(fmt.Errorf("%w: next page is not consecutive", model.ErrStalled), "continuation_mismatch")
						}
						next.Page = position
					} else if position != next.Offset {
						page.Metadata["expected_position"], page.Metadata["actual_position"] = next.Offset, position
						return fail(fmt.Errorf("%w: next offset skips or repeats records", model.ErrStalled), "continuation_mismatch")
					}
				case "none":
					return fail(errors.New("response advertises continuation but pagination is disabled"), "unexpected_continuation")
				}
			}
		}
	}
	atTotal := next.Total != nil && next.Offset == *next.Total
	if explicitNext && (atTotal || len(items) == 0 || p.Pagination.Type == "none") {
		if next.Total != nil {
			page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Offset
		}
		return fail(fmt.Errorf("%w: continuation contradicts page completion", model.ErrStalled), "unexpected_continuation")
	}
	if terminator && next.Total != nil && !atTotal {
		page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Offset
		return fail(fmt.Errorf("%w: continuation ended before advertised total", model.ErrStalled), "premature_end")
	}
	if p.Pagination.Type == "none" && next.Total != nil && !atTotal {
		page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Offset
		return fail(errors.New("pagination is disabled before advertised total"), "premature_end")
	}
	page.Done = atTotal || terminator || len(items) == 0 || p.Pagination.Type == "none"
	if !page.Done && !explicitNext {
		next.URL = ""
		if p.Pagination.Type == "page" {
			if state.Page == int(^uint(0)>>1) {
				return fail(errors.New("pagination page overflow"), "pagination_overflow")
			}
			next.Page = state.Page + 1
		}
	}
	// A URL continuation still advances the logical page, which is checked
	// against current_path on the following response.
	if !page.Done && next.URL != "" && p.Pagination.Type == "page" {
		if state.Page == int(^uint(0)>>1) {
			return fail(errors.New("pagination page overflow"), "pagination_overflow")
		}
		next.Page = state.Page + 1
	}
	next.Done = page.Done
	page.Next, err = json.Marshal(next)
	if err != nil {
		return fail(errors.New("could not encode continuation state"))
	}
	return page, nil
}

func (c *httpJSONConnector) request(state jsonCursor) (Request, error) {
	p := c.provider
	request := Request{Method: p.HTTP.Method, URL: state.URL, Query: make(url.Values), Headers: http.Header{"Accept": {"application/json"}}}
	if state.URL == "" {
		request.URL = p.URL
	}
	preserveQuery := c.preserveQuery
	if state.URL == "" || preserveQuery {
		var nextURL *url.URL
		var nextQuery url.Values
		for name, value := range p.HTTP.Query {
			values, err := queryValues(value)
			if err != nil {
				return Request{}, err
			}
			request.Query[name] = values
			if state.URL != "" && strings.HasSuffix(name, "[]") {
				if nextURL == nil {
					nextURL, err = c.client.target(state.URL)
					if err != nil {
						return Request{}, err
					}
					nextQuery = nextURL.Query()
				}
				// Indexed bracket keys are aliases of the configured array,
				// including duplicates already saved in a continuation.
				prefix := strings.TrimSuffix(name, "]")
				for key := range nextQuery {
					if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, "]") {
						continue
					}
					index := key[len(prefix) : len(key)-1]
					if index != "" && strings.Trim(index, "0123456789") == "" {
						delete(nextQuery, key)
					}
				}
			}
		}
		if nextURL != nil {
			nextURL.RawQuery = nextQuery.Encode()
			request.URL = nextURL.String()
		}
	}
	var body any = p.HTTP.Body
	parameters := make(map[string]any)
	if p.Pagination.Type != "none" && p.Pagination.SizeParam != "" && (state.URL == "" || preserveQuery && p.Pagination.In == "query") {
		parameters[p.Pagination.SizeParam] = p.PageSize
	}
	if p.Pagination.Type != "none" && state.URL == "" {
		switch p.Pagination.Type {
		case "page":
			parameters[p.Pagination.PageParam] = state.Page
		case "offset":
			parameters[p.Pagination.OffsetParam] = state.Offset
		case "cursor":
			if state.Cursor != "" {
				parameters[p.Pagination.CursorParam] = state.Cursor
			}
		}
	}
	if p.Pagination.In == "body" && p.Pagination.Type != "none" {
		object := make(map[string]any)
		if source, ok := body.(map[string]any); ok {
			for name, value := range source {
				object[name] = value
			}
		}
		for name, value := range parameters {
			object[name] = value
		}
		body = object
	} else {
		for name, value := range parameters {
			values, _ := queryValues(value)
			request.Query[name] = values
		}
	}
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil || len(encoded) > maxRequestBytes {
			return Request{}, errors.New("invalid or oversized JSON request body")
		}
		request.Body = encoded
		request.Headers.Set("Content-Type", "application/json")
	}
	return request, nil
}

func requiredPointer(root any, pointer string) (any, error) {
	value, found, err := pointerValue(root, pointer)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("configured pagination path is missing")
	}
	return value, nil
}
func boundedCount(value any) (int, error) {
	n, err := integerValue(value)
	if err != nil || n < 0 || n > int64(int(^uint(0)>>1)) {
		return 0, errors.New("invalid nonnegative count")
	}
	return int(n), nil
}
func httpJSONTemplate() string {
	return `{
  "version": 1,
  "id": "example_json",
  "name": "Example JSON provider",
  "adapter": "json",
  "url": "https://indexer.example/api/releases",
  "enabled": false,
  "auth": {
    "type": "bearer",
    "secret_ref": "example_json_token"
  },
  "request_interval": "1s",
  "rate_limit_reset": "epoch",
  "page_size": 100,
  "http": {
    "method": "GET",
    "items_path": "/data/items"
  },
  "pagination": {
    "type": "offset",
    "in": "query",
    "offset_param": "offset",
    "size_param": "limit",
    "start": 0,
    "total_path": "/data/total"
  },
  "mapping": {
    "id": "/id",
    "fields": {
      "title": "/title",
      "size": "/size",
      "info_hash": "/info_hash",
      "categories": "/categories",
      "published_at": "/published_at"
    }
  },
  "output": {
    "fields": ["title", "size", "info_hash"]
  }
}
`
}
