package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type htmlConnector struct {
	provider                            model.Provider
	client                              *Client
	checkpoint                          func(context.Context, model.Page) error
	incremental                         bool
	page, rows, empty, next, detailPage cascadia.Matcher
	id, detailURL                       htmlField
	fields, detailFields                map[string]htmlField
	total, current                      *htmlField
	unit                                string
	localCategories                     map[int64]struct{}
}

type htmlCursor struct {
	Version int    `json:"version"`
	Page    int    `json:"page"`
	Offset  int    `json:"offset"`
	Count   int    `json:"count"`
	URL     string `json:"url,omitempty"`
	Total   *int   `json:"total,omitempty"`
	Done    bool   `json:"done,omitempty"`
}

// A listing and its optional details form one atomic data page. Intermediate
// responses enter ordinary diagnostics, but only a fully interpreted listing
// advances the durable cursor. Interrupted detail work is safely retried.
type htmlFetch struct {
	connector *htmlConnector
	response  Response
	metadata  map[string]any
	textBytes int
}

func newHTML(ctx context.Context, p model.Provider, mode model.RunMode, env Environment) (Connector, error) {
	client, err := NewClient(ctx, p, env)
	if err != nil {
		return nil, err
	}
	c := &htmlConnector{provider: p, client: client, checkpoint: env.Checkpoint, incremental: mode == model.ModeIncremental}
	// New has already validated all selectors, patterns and formats.
	c.page, _ = htmlSelector(p.HTML.PageSelector)
	c.rows, _ = htmlSelector(p.HTML.RowsSelector)
	if p.HTML.EmptySelector != "" {
		c.empty, _ = htmlSelector(p.HTML.EmptySelector)
	}
	if p.HTML.NextSelector != "" {
		c.next, _ = htmlSelector(p.HTML.NextSelector)
	}
	c.id, _ = compileHTMLField(p.HTML.ID)
	compile := func(config map[string]model.HTMLField) map[string]htmlField {
		fields := make(map[string]htmlField, len(config))
		for name, config := range config {
			if name != "seeders" && name != "peers" && name != "leechers" {
				fields[name], _ = compileHTMLField(config)
			}
		}
		return fields
	}
	c.fields = compile(p.HTML.Fields)
	if detail := p.HTML.Details; detail != nil {
		c.detailPage, _ = htmlSelector(detail.PageSelector)
		c.detailURL, _ = compileHTMLField(detail.URL)
		c.detailFields = compile(detail.Fields)
	}
	if p.HTML.Total != nil {
		field, _ := compileHTMLField(*p.HTML.Total)
		c.total = &field
	}
	if p.HTML.Current != nil {
		field, _ := compileHTMLField(*p.HTML.Current)
		c.current = &field
	}
	c.unit, _ = publishedAtUnit(p)
	c.localCategories, _ = localCategories(p)
	return c, nil
}

func (c *htmlConnector) Close() error { return c.client.Close() }

func (operation *htmlFetch) request(ctx context.Context, request Request, phase string, recordIndex int) (Response, error) {
	c := operation.connector
	if operation.response.StatusCode != 0 && c.checkpoint != nil {
		metadata := map[string]any{"auxiliary_response": true, "html_phase": operation.metadata["html_phase"], "http_status": operation.response.StatusCode}
		if index, ok := operation.metadata["record_index"]; ok {
			metadata["record_index"] = index
		}
		if err := c.checkpoint(ctx, model.Page{Body: operation.response.Body, ContentType: operation.response.Header.Get("Content-Type"), Metadata: metadata}); err != nil {
			return Response{}, err
		}
	}
	operation.metadata = map[string]any{"html_phase": phase}
	if recordIndex >= 0 {
		operation.metadata["record_index"] = recordIndex
	}
	if phase != "listing" {
		operation.metadata["detail_response"] = true
	}
	response, err := c.client.Do(ctx, request)
	operation.response = response
	return response, err
}

func (c *htmlConnector) Fetch(ctx context.Context, checkpoint json.RawMessage) (page model.Page, resultErr error) {
	state := htmlCursor{Version: 1, Page: c.provider.Pagination.Start}
	if c.provider.Pagination.Type == "next" && state.Page == 0 {
		state.Page = 1
	}
	if c.provider.Pagination.Type == "offset" {
		state.Offset = c.provider.Pagination.Start
		state.Count = state.Offset
	}
	page.Next = checkpoint
	page.Metadata = map[string]any{"pagination": c.provider.Pagination.Type}
	operation := htmlFetch{connector: c}
	defer func() {
		page.Body, page.ContentType = operation.response.Body, operation.response.Header.Get("Content-Type")
		if page.ContentType == "" {
			page.ContentType = "text/html"
		}
		for name, value := range operation.metadata {
			page.Metadata[name] = value
		}
		if operation.response.StatusCode != 0 {
			page.Metadata["http_status"] = operation.response.StatusCode
		}
		if resultErr != nil {
			failure := safeFailure(resultErr, "parse")
			page.Error, resultErr = failure.Error(), failure
			page.Metadata["failure_code"] = FailureCode(failure)
			if reason, ok := page.Metadata["failure_reason"].(string); ok && FailureReasonMessage(reason) != "" {
				page.Error = FailureReasonMessage(reason)
			}
			page.Next, page.Done = checkpoint, false
			SanitizeFailureDiagnostics(page.Metadata)
		}
	}()
	fail := func(reason string) (model.Page, error) {
		page.Metadata["failure_reason"] = reason
		return page, &FailureError{code: "parse"}
	}
	if len(checkpoint) > 0 {
		if err := decodeJSON(checkpoint, &state); err != nil || state.Version != 1 || state.Page < 0 || state.Offset < 0 || state.Count < 0 || state.Total != nil && (*state.Total < 0 || state.Count > *state.Total) {
			return page, &FailureError{code: "configuration"}
		}
		if state.URL != "" {
			target, err := htmlURL(c.client.origin, state.URL, false)
			if err != nil || !sameOrigin(c.client.origin, target) || c.containsURLSecret(target) {
				return fail("invalid_continuation")
			}
		}
	}
	if state.Done {
		page.Done = true
		return page, nil
	}
	page.Position = strconv.Itoa(state.Page) + ":" + strconv.Itoa(state.Offset)
	page.Metadata["page"], page.Metadata["offset"] = state.Page, state.Offset
	request, err := c.request(state)
	if err != nil {
		return page, err
	}
	response, err := operation.request(ctx, request, "listing", -1)
	if err != nil {
		return page, err
	}
	root, base, err := c.document(ctx, response, request.URL)
	if err != nil {
		if ctx.Err() != nil {
			return page, ctx.Err()
		}
		return fail("html_listing_missing")
	}
	containers := cascadia.QueryAll(root, c.page)
	if len(containers) != 1 {
		return fail("html_listing_missing")
	}
	rows := cascadia.QueryAll(containers[0], c.rows)
	if len(rows) > 10000 {
		return fail("html_page_limit")
	}
	empty := c.empty != nil && cascadia.Query(root, c.empty) != nil
	if len(rows) == 0 && !empty || len(rows) > 0 && empty {
		return fail("html_empty_unconfirmed")
	}
	if c.current != nil {
		position, err := c.check(*c.current, root)
		expected := state.Page
		if c.provider.Pagination.Type == "offset" {
			expected = state.Offset
		}
		page.Metadata["expected_position"] = expected
		if err != nil {
			return fail("invalid_position")
		}
		page.Metadata["actual_position"] = position
		if position != expected {
			return fail("position_mismatch")
		}
	}
	next := state
	if c.total != nil {
		total, err := c.check(*c.total, root)
		if err != nil {
			return fail("invalid_total")
		}
		page.Metadata["total"] = total
		if state.Total != nil && *state.Total != total {
			page.Metadata["expected_total"], page.Metadata["actual_total"] = *state.Total, total
			return fail("total_changed")
		}
		next.Total = &total
	}
	if len(rows) > int(^uint(0)>>1)-state.Count || len(rows) > int(^uint(0)>>1)-state.Offset || state.Page == int(^uint(0)>>1) {
		return fail("pagination_overflow")
	}
	next.Count += len(rows)
	next.Page++
	if c.provider.Pagination.Type == "offset" {
		next.Offset += len(rows)
	}
	var nextLink string
	if c.next != nil {
		for _, node := range cascadia.QueryAll(root, c.next) {
			// Disabled links are not continuations, even if the site retains href.
			if htmlHasAttribute(node, "disabled") || strings.EqualFold(htmlAttribute(node, "aria-disabled"), "true") {
				continue
			}
			target, err := c.continuationURL(base, htmlAttribute(node, "href"))
			if err != nil || !sameOrigin(c.client.origin, target) || c.containsURLSecret(target) {
				return fail("invalid_continuation")
			}
			if target.String() == base.String() || target.String() == state.URL {
				return fail("continuation_repeated")
			}
			if nextLink != "" && nextLink != target.String() {
				return fail("invalid_continuation")
			}
			nextLink = target.String()
		}
	}
	if len(rows) == 0 && nextLink != "" || c.provider.Pagination.Type == "none" && nextLink != "" {
		return fail("unexpected_continuation")
	}
	atTotal := next.Total != nil && next.Count == *next.Total
	if next.Total != nil {
		page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Count
		if next.Count > *next.Total {
			return fail("records_exceed_total")
		}
		if atTotal && nextLink != "" {
			return fail("unexpected_continuation")
		}
		if !atTotal && (len(rows) == 0 || c.next != nil && nextLink == "" || c.provider.Pagination.Type == "none") {
			return fail("premature_end")
		}
	}
	page.Done = atTotal || len(rows) == 0 || c.provider.Pagination.Type == "none" || c.next != nil && nextLink == ""
	next.URL, next.Done = nextLink, page.Done
	page.Items = make([]model.Record, 0, len(rows))
	page.RequireUniqueIDs = c.total != nil
	identities := make(map[string]bool, len(rows))
	for index, row := range rows {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		page.Metadata["record_index"] = index
		values, err := c.id.read(row)
		if err != nil || len(values) != 1 {
			return fail("html_identity_invalid")
		}
		identity, err := c.id.convert(values[0], base, c.unit)
		id := FieldString(identity)
		if err != nil || id == "" || len(id) > 2048 || c.containsSecret(id) || identities[id] {
			return fail("html_identity_invalid")
		}
		// URL-shaped native identities must be safe to retain and share too.
		if strings.HasPrefix(id, "http:") || strings.HasPrefix(id, "https:") || strings.Contains(id, "?") {
			if target, err := htmlURL(base, id, false); err != nil || c.containsURLSecret(target) {
				return fail("html_identity_invalid")
			}
		}
		identities[id] = true
		fields := make(map[string]any, len(c.fields)+len(c.detailFields))
		if err := operation.fields(ctx, row, base, c.fields, fields, index); err != nil {
			return failHTMLFields(page, err)
		}
		missingDetails := false
		for name := range c.detailFields {
			missingDetails = missingDetails || fields[name] == nil
		}
		if missingDetails {
			values, err := c.detailURL.read(row)
			if err != nil || len(values) != 1 {
				return fail("html_detail_missing")
			}
			target, err := htmlURL(base, values[0], true)
			if err != nil || !sameOrigin(c.client.origin, target) {
				return fail("invalid_continuation")
			}
			response, err := operation.request(ctx, Request{URL: target.String(), Headers: http.Header{"Accept": {"text/html, application/xhtml+xml"}}}, "detail", index)
			if err != nil {
				return page, err
			}
			detailRoot, detailBase, err := c.document(ctx, response, target.String())
			if err != nil {
				if ctx.Err() != nil {
					return page, ctx.Err()
				}
				return fail("html_detail_missing")
			}
			details := cascadia.QueryAll(detailRoot, c.detailPage)
			if len(details) != 1 {
				return fail("html_detail_missing")
			}
			if err := operation.fields(ctx, details[0], detailBase, c.detailFields, fields, index); err != nil {
				return failHTMLFields(page, err)
			}
		}
		if strings.TrimSpace(FieldString(fields["title"])) == "" {
			page.Metadata["field_name"] = "title"
			return fail("html_field_invalid")
		}
		supplementExternalIDs(fields)
		fields = model.WithoutSwarmCounts(fields)
		page.Items = append(page.Items, model.Record{SourceID: id, Fields: fields, ContentType: "text/html", Ignored: !matchesLocalCategories(c.localCategories, fields)})
	}
	delete(page.Metadata, "record_index")
	page.Next, err = json.Marshal(next)
	return page, err
}

// The error category remains finite, even when a field or upstream body contains
// private data. Transport cancellation and provider failures retain their type.
func failHTMLFields(page model.Page, err error) (model.Page, error) {
	if errors.Is(err, errHTMLFieldLimit) {
		page.Metadata["failure_reason"] = "html_fields_limit"
	} else if FailureCode(err) == "unknown" {
		page.Metadata["failure_reason"] = "html_field_invalid"
	}
	return page, err
}

func (operation *htmlFetch) fields(ctx context.Context, root *html.Node, base *url.URL, plan map[string]htmlField, fields map[string]any, index int) (resultErr error) {
	c := operation.connector
	var currentField string
	defer func() {
		if resultErr != nil {
			operation.metadata["field_name"] = currentField
		}
	}()
	names := make([]string, 0, len(plan))
	for name := range plan {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		currentField = name
		if fields[name] != nil {
			continue
		}
		field := plan[name]
		values, err := field.read(root)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			continue
		}
		converted := make([]any, 0, len(values))
		for _, value := range values {
			operation.textBytes += len(value)
			if operation.textBytes > maxResponseBytes {
				return errHTMLFieldLimit
			}
			var result any
			if field.config.Format == "torrent" {
				target, err := htmlURL(base, value, true)
				if err != nil || !sameOrigin(c.client.origin, target) {
					return &FailureError{code: "configuration"}
				}
				response, err := operation.request(ctx, Request{URL: target.String(), Headers: http.Header{"Accept": {"application/x-bittorrent"}}}, "torrent", index)
				if err != nil {
					return err
				}
				result, err = htmlTorrentHash(response.Body)
				if err != nil {
					operation.metadata["failure_reason"] = "html_torrent_invalid"
					return &FailureError{code: "parse"}
				}
			} else {
				if field.config.Format != "info_hash" && c.containsSecret(value) {
					return errors.New("HTML field contains a source credential")
				}
				result, err = field.convert(value, base, c.unit)
				if err != nil {
					return err
				}
				if field.config.Format == "info_hash" && c.containsSecret(FieldString(result)) {
					return errors.New("HTML hash field contains a source credential")
				}
				if field.config.Format == "url" {
					target, _ := url.Parse(FieldString(result))
					if c.containsURLSecret(target) {
						return errors.New("HTML URL field contains a source credential")
					}
				}
			}
			converted = append(converted, result)
		}
		var value any = converted[0]
		if field.config.All {
			value = converted
		}
		value, err = normalizeField(name, value, c.unit)
		if err != nil {
			return err
		}
		fields[name] = value
	}
	return nil
}

func (c *htmlConnector) check(field htmlField, root *html.Node) (int, error) {
	values, err := field.read(root)
	if err != nil || len(values) != 1 || c.containsSecret(values[0]) {
		return 0, errors.New("HTML pagination check is missing")
	}
	count, err := htmlNumber(values[0])
	if err != nil || count > int64(int(^uint(0)>>1)) {
		return 0, errors.New("HTML pagination check exceeds the supported range")
	}
	return int(count), nil
}

func (c *htmlConnector) document(ctx context.Context, response Response, requestURL string) (*html.Node, *url.URL, error) {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if contentType != "" && contentType != "text/html" && contentType != "application/xhtml+xml" {
		return nil, nil, errors.New("source did not return HTML")
	}
	reader, err := charset.NewReader(bytes.NewReader(response.Body), response.Header.Get("Content-Type"))
	if err != nil {
		return nil, nil, err
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil || len(decoded) > maxResponseBytes {
		return nil, nil, errors.New("decoded HTML exceeds the size limit")
	}
	root, err := html.Parse(bytes.NewReader(decoded))
	if err != nil {
		return nil, nil, err
	}
	nodes := 0
	for node := root; node != nil; {
		nodes++
		if nodes > 100000 || ctx.Err() != nil {
			return nil, nil, errors.New("HTML document exceeds the processing limit")
		}
		if node.FirstChild != nil {
			node = node.FirstChild
			continue
		}
		for node != nil && node.NextSibling == nil {
			node = node.Parent
		}
		if node != nil {
			node = node.NextSibling
		}
	}
	if response.URL != "" {
		requestURL = response.URL
	}
	base, err := url.Parse(requestURL)
	if err != nil || !validHTTPURL(base) || !sameOrigin(c.client.origin, base) {
		return nil, nil, errors.New("HTML document has an invalid base URL")
	}
	// Authentication injected by Client is never persisted in continuations or
	// propagated into extracted links. It is reapplied at each request instead.
	query := base.Query()
	for name, values := range query {
		if model.CredentialParameter(name) || c.client.authType == "query" && strings.EqualFold(name, c.client.authName) || slices.ContainsFunc(values, c.containsSecret) {
			delete(query, name)
		}
	}
	base.RawQuery = query.Encode()
	if element := cascadia.Query(root, cascadia.MustCompile("base[href]")); element != nil {
		target, err := htmlURL(base, htmlAttribute(element, "href"), false)
		if err != nil || !sameOrigin(c.client.origin, target) || c.containsURLSecret(target) {
			return nil, nil, errors.New("HTML base element changes the source origin")
		}
		base = target
	}
	return root, base, nil
}

func (c *htmlConnector) request(state htmlCursor) (Request, error) {
	p := c.provider
	request := Request{URL: p.URL, Method: p.HTTP.Method, Query: make(url.Values), Headers: http.Header{"Accept": {"text/html, application/xhtml+xml"}}}
	if state.URL != "" {
		// A site's next link is authoritative and is normally a GET, including
		// links returned by a form POST. Do not reapply stale query/body filters.
		request.URL, request.Method = state.URL, http.MethodGet
		return request, nil
	}
	for name, value := range p.HTTP.Query {
		values, err := queryValues(value)
		if err != nil {
			return request, err
		}
		request.Query[name] = values
	}
	if c.incremental {
		for name, value := range p.HTTP.IncrementalQuery {
			values, err := queryValues(value)
			if err != nil {
				return request, err
			}
			request.Query[name] = values
		}
	}
	form := make(url.Values)
	if body, ok := p.HTTP.Body.(map[string]any); ok {
		for name, value := range body {
			values, err := queryValues(value)
			if err != nil {
				return request, err
			}
			form[name] = values
		}
	}
	parameters := request.Query
	if p.Pagination.In == "body" {
		parameters = form
	}
	switch p.Pagination.Type {
	case "page":
		parameters.Set(p.Pagination.PageParam, strconv.Itoa(state.Page))
	case "offset":
		parameters.Set(p.Pagination.OffsetParam, strconv.Itoa(state.Offset))
	}
	if p.Pagination.SizeParam != "" && p.Pagination.Type != "none" {
		parameters.Set(p.Pagination.SizeParam, strconv.Itoa(p.PageSize))
	}
	if p.HTTP.Method == http.MethodPost {
		request.Body = []byte(form.Encode())
		request.Headers.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return request, nil
}
