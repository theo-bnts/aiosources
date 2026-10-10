package connectors

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	sdk "github.com/moodiness/ingest/torznab"
	"github.com/theo-bnts/aiosources/internal/model"
)

type torznabConnector struct {
	provider        model.Provider
	client          *Client
	sdk             *sdk.Client
	scopes          [][]int
	queries         []string
	capsRaw         []byte
	idSource        string
	idPattern       *regexp.Regexp
	localCategories map[int64]struct{}
	advisoryTotals  bool
	checkpoint      func(context.Context, model.Page) error
	knownIDs        func(context.Context, []string) (map[string]bool, error)
}
type torznabCursor struct {
	Version    int     `json:"version"`
	QueryIndex int     `json:"query_index,omitempty"`
	Scope      int     `json:"scope"`
	Scopes     [][]int `json:"scopes"`
	Offset     int     `json:"offset"`
	Total      *int    `json:"total,omitempty"`
	KnownPages int     `json:"known_pages,omitempty"`
	Done       bool    `json:"done,omitempty"`
}

func validateTorznab(p model.Provider) error {
	if p.HTTP.Method != "GET" || p.HTTP.Body != nil {
		return errors.New("Torznab requires GET without a body")
	}
	if p.HTTP.ItemsPath != "" || p.Mapping.ID != "" || len(p.Mapping.Fields) != 0 {
		return errors.New("Torznab defines its own RSS mapping")
	}
	if p.Pagination != (model.Pagination{}) {
		return errors.New("Torznab defines capability-negotiated offset pagination")
	}
	for name := range p.Options {
		switch name {
		case "category_scope", "search_mode", "search_queries", "published_at_unit", "id_source", "id_pattern", "local_categories", "total_mode":
		default:
			return errors.New("unknown Torznab adapter option")
		}
	}
	if _, err := publishedAtUnit(p); err != nil {
		return err
	}
	if _, _, err := torznabIdentity(p); err != nil {
		return err
	}
	if _, err := localCategories(p); err != nil {
		return err
	}
	if _, err := torznabSearchQueries(p); err != nil {
		return err
	}
	totalMode, err := OptionString(p, "total_mode", "strict")
	if err != nil {
		return err
	}
	if totalMode != "strict" && totalMode != "advisory" {
		return errors.New("total_mode must be strict or advisory")
	}
	scope, err := OptionString(p, "category_scope", "combined")
	if err != nil {
		return err
	}
	if scope != "combined" && scope != "each" && scope != "advertised" {
		return errors.New("category_scope must be combined, each, or advertised")
	}
	if scope == "each" && len(p.Search.Categories) == 0 {
		return errors.New("category_scope=each requires search.categories")
	}
	if scope == "advertised" && len(p.Search.Categories) != 0 {
		return errors.New("category_scope=advertised cannot also set search.categories")
	}
	mode, err := OptionString(p, "search_mode", "search")
	if err != nil {
		return err
	}
	switch sdk.SearchMode(mode) {
	case sdk.ModeSearch, sdk.ModeTV, sdk.ModeMovie, sdk.ModeMusic, sdk.ModeBook:
	default:
		return errors.New("unsupported Torznab search_mode")
	}
	for name := range p.HTTP.Query {
		switch strings.ToLower(name) {
		case "t", "apikey", "q", "cat", "offset", "limit", "extended", "o":
			return errors.New("static Torznab query cannot override reserved parameters")
		}
	}
	return nil
}

func torznabSearchQueries(p model.Provider) ([]string, error) {
	value, exists := p.Options["search_queries"]
	if !exists {
		return []string{p.Search.Query}, nil
	}
	if p.Search.Query != "" {
		return nil, errors.New("search_queries cannot also set search.query")
	}
	var queries []string
	switch values := value.(type) {
	case []any:
		queries = make([]string, len(values))
		for i, value := range values {
			query, ok := value.(string)
			if !ok {
				return nil, errors.New("search_queries must contain only strings")
			}
			queries[i] = query
		}
	case []string:
		queries = append([]string(nil), values...)
	default:
		return nil, errors.New("search_queries must be an array of strings")
	}
	if len(queries) == 0 {
		return nil, errors.New("search_queries must not be empty")
	}
	return queries, nil
}

func torznabIdentity(p model.Provider) (string, *regexp.Regexp, error) {
	source, err := OptionString(p, "id_source", "")
	if err != nil {
		return "", nil, err
	}
	if source != "" && source != "guid" && source != "link" {
		return "", nil, errors.New("id_source must be guid or link")
	}
	pattern, err := OptionString(p, "id_pattern", "")
	if err != nil {
		return "", nil, err
	}
	if pattern == "" {
		return source, nil, nil
	}
	if source == "" {
		return "", nil, errors.New("id_pattern requires an explicit id_source")
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil || compiled.NumSubexp() != 1 {
		return "", nil, errors.New("id_pattern must be a regexp with exactly one capture")
	}
	return source, compiled, nil
}

func newTorznab(ctx context.Context, p model.Provider, mode model.RunMode, env Environment) (Connector, error) {
	idSource, idPattern, err := torznabIdentity(p)
	if err != nil {
		return nil, err
	}
	categories, err := localCategories(p)
	if err != nil {
		return nil, err
	}
	queries, err := torznabSearchQueries(p)
	if err != nil {
		return nil, err
	}
	var knownIDs func(context.Context, []string) (map[string]bool, error)
	scope, _ := OptionString(p, "category_scope", "combined")
	if mode == model.ModeIncremental && p.Schedule.KnownPages > 0 && (scope != "combined" || len(queries) > 1) {
		if env.KnownIDs == nil {
			return nil, errors.New("query/category-scoped Torznab incremental requires prior-run source-ID membership")
		}
		knownIDs = env.KnownIDs
	}
	client, err := NewClient(ctx, p, env)
	if err != nil {
		return nil, err
	}
	totalMode, _ := OptionString(p, "total_mode", "strict")
	return &torznabConnector{
		provider: p, client: client, queries: queries, idSource: idSource, idPattern: idPattern,
		localCategories: categories, advisoryTotals: totalMode == "advisory",
		checkpoint: env.Checkpoint, knownIDs: knownIDs,
	}, nil
}
func (c *torznabConnector) Close() error { return c.client.Close() }

func (c *torznabConnector) open(ctx context.Context) error {
	if c.sdk != nil {
		return nil
	}
	c.client.mu.Lock()
	c.client.last = Response{}
	c.client.mu.Unlock()
	target, _ := url.Parse(c.provider.URL)
	parameters := target.Query()
	for name, value := range c.provider.HTTP.Query {
		values, err := queryValues(value)
		if err != nil {
			return err
		}
		parameters[name] = values
	}
	target.RawQuery = parameters.Encode()
	unit, _ := publishedAtUnit(c.provider)
	client, err := sdk.Open(ctx, sdk.Config{URL: target.String(), HTTPClient: c.client.HTTPClient(), PageSize: c.provider.PageSize, RequestInterval: time.Nanosecond, DisableRetries: true, PublishedAtUnit: unit, AdvisoryTotals: c.advisoryTotals})
	if err != nil {
		return err
	}
	c.capsRaw = c.client.lastResponse().Body
	c.scopes = nil
	scope, _ := OptionString(c.provider, "category_scope", "combined")
	switch scope {
	case "combined":
		c.scopes = [][]int{append([]int{}, c.provider.Search.Categories...)}
	case "each":
		seen := make(map[int]bool)
		for _, category := range c.provider.Search.Categories {
			if !seen[category] {
				c.scopes = append(c.scopes, []int{category})
				seen[category] = true
			}
		}
	case "advertised":
		seen := make(map[int]bool)
		var add func([]sdk.Category)
		add = func(categories []sdk.Category) {
			for _, category := range categories {
				// Parent and child searches can expose different provider views.
				// Persist the full negotiated scope list in continuation state.
				if !seen[category.ID] {
					c.scopes = append(c.scopes, []int{category.ID})
					seen[category.ID] = true
				}
				add(category.Subcategories)
			}
		}
		add(client.Capabilities().Categories)
		if len(c.scopes) == 0 {
			return errors.New("Torznab did not advertise categories for category-scoped collection")
		}
	}
	c.sdk = client
	if c.checkpoint != nil {
		response := c.client.lastResponse()
		page := model.Page{Body: response.Body, ContentType: response.Header.Get("Content-Type"),
			Metadata: map[string]any{"auxiliary_response": true},
			Items:    []model.Record{{Raw: c.capsRaw, ContentType: "application/xml", Auxiliary: true, Ignored: true, Fields: map[string]any{"resource": "capabilities"}}}}
		c.capsRaw = nil
		if err := c.checkpoint(ctx, page); err != nil {
			return err
		}
	}
	return nil
}

func (c *torznabConnector) Fetch(ctx context.Context, checkpoint json.RawMessage) (model.Page, error) {
	state := torznabCursor{Version: 1}
	if len(checkpoint) > 0 {
		if err := decodeJSON(checkpoint, &state); err != nil || state.Version != 1 || state.QueryIndex < 0 || state.QueryIndex >= len(c.queries) || state.Scope < 0 || state.Offset < 0 || len(state.Scopes) == 0 || state.Scope >= len(state.Scopes) || (state.Total != nil && *state.Total < 0) {
			return model.Page{}, errors.New("invalid Torznab continuation state")
		}
		if state.KnownPages < 0 || (c.knownIDs != nil &&
			(state.KnownPages > c.provider.Schedule.KnownPages || (!state.Done && state.KnownPages == c.provider.Schedule.KnownPages))) {
			return model.Page{}, errors.New("invalid Torznab known-page continuation")
		}
		for _, scope := range state.Scopes {
			for _, category := range scope {
				if category < 0 {
					return model.Page{}, errors.New("invalid Torznab category continuation")
				}
			}
		}
		if state.Done {
			return model.Page{Done: true, Next: checkpoint}, nil
		}
	}
	if err := c.open(ctx); err != nil {
		response := c.client.lastResponse()
		failure := safeFailure(err, "parse")
		return model.Page{Body: response.Body, ContentType: response.Header.Get("Content-Type"), Next: checkpoint, Error: failure.Error(), Metadata: map[string]any{"failure_code": FailureCode(failure)}}, failure
	}
	if len(state.Scopes) == 0 {
		state.Scopes = c.scopes
	}
	mode, _ := OptionString(c.provider, "search_mode", "search")
	params := make(url.Values)
	for name, value := range c.provider.HTTP.Query {
		values, err := queryValues(value)
		if err != nil {
			return model.Page{}, err
		}
		params[name] = values
	}
	query := sdk.Query{Mode: sdk.SearchMode(mode), Text: c.queries[state.QueryIndex], Categories: state.Scopes[state.Scope], Offset: state.Offset, Limit: c.provider.PageSize, Params: params}
	raw, err := c.sdk.SearchRaw(ctx, query)
	fingerprintScope := strconv.Itoa(state.Scope)
	if len(c.queries) > 1 {
		fingerprintScope = strconv.Itoa(state.QueryIndex) + ":" + fingerprintScope
	}
	page := model.Page{Body: raw.Body, ContentType: "application/rss+xml", Next: checkpoint, Position: fingerprintScope + ":" + strconv.Itoa(state.Offset), Metadata: map[string]any{"offset": state.Offset, "categories": query.Categories, "fingerprint_scope": fingerprintScope}}
	if len(c.queries) > 1 {
		page.Metadata["query_index"], page.Metadata["query_count"] = state.QueryIndex, len(c.queries)
	}
	if status := c.client.lastResponse().StatusCode; status != 0 {
		page.Metadata["http_status"] = status
	}
	if len(c.capsRaw) > 0 {
		page.Items = append(page.Items, model.Record{Raw: c.capsRaw, ContentType: "application/xml", Auxiliary: true, Ignored: true, Fields: map[string]any{"resource": "capabilities"}})
		c.capsRaw = nil
	}
	missingIdentity := false
	for _, source := range raw.Items {
		record := c.record(source)
		record.Ignored = !matchesLocalCategories(c.localCategories, record.Fields)
		missingIdentity = missingIdentity || record.SourceID == ""
		page.Items = append(page.Items, record)
	}
	if missingIdentity {
		digest := sdk.PageFingerprint(raw.Page.Items)
		page.FallbackFingerprint = hex.EncodeToString(digest[:])
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
		return page, failure
	}
	if err != nil {
		// The SDK returns parsed numeric context with its typed pagination
		// sentinel. Do not classify by parsing its error text or raw XML.
		if errors.Is(err, sdk.ErrPaginationStalled) {
			if raw.Page.Offset != state.Offset {
				page.Metadata["expected_position"], page.Metadata["actual_position"] = state.Offset, raw.Page.Offset
				return fail(err, "position_mismatch")
			}
			if len(raw.Items) > int(^uint(0)>>1)-state.Offset {
				return fail(err, "pagination_overflow")
			}
			if !c.advisoryTotals && raw.Page.Total != nil && state.Offset+len(raw.Items) > *raw.Page.Total {
				page.Metadata["expected_total"], page.Metadata["actual_position"] = *raw.Page.Total, state.Offset+len(raw.Items)
				return fail(err, "records_exceed_total")
			}
		}
		return fail(err)
	}
	next := state
	if c.advisoryTotals {
		next.Total = nil
		if raw.Page.Total != nil {
			page.Metadata["advertised_total"] = *raw.Page.Total
		}
	} else if raw.Page.Total != nil {
		total := *raw.Page.Total
		if state.Total != nil && total != *state.Total {
			page.Metadata["expected_total"], page.Metadata["actual_total"] = *state.Total, total
			return fail(fmt.Errorf("%w: Torznab advertised total changed during traversal", model.ErrStalled), "total_changed")
		}
		next.Total = &total
	}
	if next.Total != nil {
		page.Metadata["total"] = *next.Total
	}
	if len(raw.Items) > int(^uint(0)>>1)-state.Offset {
		return fail(errors.New("Torznab pagination offset overflow"), "pagination_overflow")
	}
	next.Offset += len(raw.Items)
	if next.Total != nil {
		if next.Offset > *next.Total {
			page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Offset
			return fail(fmt.Errorf("%w: Torznab items exceed advertised total", model.ErrStalled), "records_exceed_total")
		}
		if len(raw.Items) == 0 && next.Offset < *next.Total {
			page.Metadata["expected_total"], page.Metadata["actual_position"] = *next.Total, next.Offset
			return fail(fmt.Errorf("%w: Torznab empty page before advertised total", model.ErrStalled), "empty_before_total")
		}
	}
	scopeDone := len(raw.Items) == 0 || (next.Total != nil && next.Offset == *next.Total)
	if c.knownIDs != nil {
		page.Metadata["known_pages_managed"] = true
		// Include every primary identity, even locally filtered records. An
		// unidentified or malformed item cannot establish a known-page boundary.
		ids := make([]string, 0, len(raw.Items))
		allKnown := len(raw.Items) > 0
		for _, record := range page.Items {
			if record.Auxiliary {
				continue
			}
			if record.SourceID == "" || record.Error != "" {
				allKnown = false
				break
			}
			ids = append(ids, record.SourceID)
		}
		for start := 0; allKnown && start < len(ids); start += 100 {
			batch := ids[start:min(start+100, len(ids))]
			known, err := c.knownIDs(ctx, batch)
			if err != nil {
				return fail(err)
			}
			for _, id := range batch {
				if !known[id] {
					allKnown = false
					break
				}
			}
		}
		if allKnown {
			next.KnownPages++
		} else {
			next.KnownPages = 0
		}
		if next.KnownPages >= c.provider.Schedule.KnownPages {
			scopeDone = true
			page.Metadata["incremental_stop"] = "prior_run_boundary"
		} else if scopeDone {
			page.Metadata["incremental_stop"] = "end"
		}
	}
	if scopeDone {
		if state.Scope+1 == len(state.Scopes) && state.QueryIndex+1 == len(c.queries) {
			page.Done = true
			next.Done = true
		} else {
			next.Scope++
			if next.Scope == len(state.Scopes) {
				next.QueryIndex++
				next.Scope = 0
			}
			next.Offset = 0
			next.Total = nil
			next.KnownPages = 0
		}
	}
	page.Next, err = json.Marshal(next)
	if err != nil {
		return fail(errors.New("could not encode Torznab continuation"))
	}
	return page, nil
}

func (c *torznabConnector) record(source sdk.RawItem) model.Record {
	item := source.Item
	fields := make(map[string]any)
	for _, pair := range []struct{ name, value string }{{"guid", item.GUID}, {"title", item.Title}, {"link", item.Link}, {"comments", item.Comments}, {"info_hash", item.InfoHash}, {"magnet_url", item.MagnetURL}} {
		if pair.value != "" {
			fields[pair.name] = pair.value
		}
	}
	if item.PublishedAt != nil {
		fields["published_at"] = item.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	if item.Size != nil {
		fields["size"] = *item.Size
	}
	if item.Categories != nil {
		fields["categories"] = item.Categories
	}
	if len(item.Attributes) > 0 {
		fields["attributes"] = item.Attributes
	}
	fields = model.WithoutSwarmCounts(fields)
	id := item.GUID
	if c.idSource != "" {
		id = FieldString(fields[c.idSource])
		if c.idPattern != nil {
			match := c.idPattern.FindStringSubmatch(id)
			id = ""
			if len(match) == 2 {
				id = strings.TrimSpace(match[1])
			}
		}
	} else if id == "" && item.Link != "" {
		// A download URL can rotate passkeys. Only a credential-free link is
		// suitable as a fallback identity; do not hash changing signed URLs.
		link, err := url.Parse(item.Link)
		if err == nil && validHTTPURL(link) {
			private := c.client.containsURLSecret(link)
			for name := range link.Query() {
				if sensitiveName(name) || (c.provider.Auth.Name != "" && strings.EqualFold(name, c.provider.Auth.Name)) {
					private = true
				}
			}
			if !private {
				id = item.Link
			}
		}
	}
	record := model.Record{SourceID: id, Raw: source.Body, ContentType: "application/xml", Fields: fields}
	if source.Error != nil {
		record.Error = "Torznab item contains invalid metadata"
	}
	if id == "" {
		if record.Error != "" {
			record.Error += "; "
		}
		record.Error += "record has no stable source identity"
	}
	return record
}

func torznabTemplate() string {
	return `{
  "version": 1,
  "id": "example_torznab",
  "name": "Example Torznab provider",
  "adapter": "torznab",
  "url": "https://indexer.example/api",
  "enabled": false,
  "auth": {
    "type": "query",
    "name": "apikey",
    "secret_ref": "example_torznab_key"
  },
  "request_interval": "1s",
  "rate_limit_reset": "epoch",
  "page_size": 100,
  "search": {
    "categories": []
  },
  "options": {
    "category_scope": "combined",
    "search_mode": "search"
  },
  "output": {
    "fields": ["title", "size", "info_hash", "attributes"]
  }
}
`
}
