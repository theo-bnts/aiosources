package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

func validateRemoteCatalog(p model.Provider) error {
	for name := range p.HTTP.Headers {
		if strings.EqualFold(name, model.CatalogEntitiesHeader) {
			return errors.New("catalog data permissions must use catalog_entities, not a custom header")
		}
	}
	u, err := url.Parse(p.URL)
	if err != nil || !validHTTPURL(u) || u.Scheme != "https" || len(p.URL) > 8192 || strings.TrimSpace(p.URL) != p.URL || u.RawQuery != "" || u.ForceQuery || strings.ContainsAny(u.Host, "\\%") {
		return errors.New("remote catalogue URL must be HTTPS without credentials, query parameters or fragments")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !catalogAddressAllowed(ip) {
		return errors.New("remote catalogue destination is not permitted")
	}
	if p.Auth.Type != "bearer" || !safeIdentifier.MatchString(p.Auth.SecretRef) {
		return errors.New("remote catalogue authentication requires a bearer secret_ref")
	}
	if p.HTTP.Method != http.MethodGet || p.HTTP.Body != nil || len(p.HTTP.Query) != 0 || len(p.HTTP.SecretHeaders) != 0 || len(p.Options) != 0 {
		return errors.New("remote catalogue requests require GET with no body, query overrides, additional credentials or adapter options")
	}
	if p.PageSize > 1000 {
		return errors.New("remote catalogue page_size must not exceed 1000")
	}
	if p.HTTP.ItemsPath != "/items" || p.Mapping.ID != "/id" || len(p.Mapping.Fields) != 0 || p.Pagination.Type != "cursor" || p.Pagination.In != "query" || p.Pagination.CursorParam != "cursor" || p.Pagination.SizeParam != "limit" || p.Pagination.NextPath != "/next_cursor" || p.Pagination.Start != 0 || p.Pagination.TotalPath != "" || p.Pagination.CurrentPath != "" {
		return errors.New("remote catalogue mode requires the fixed versioned catalogue envelope and cursor pagination")
	}
	return nil
}

// Private LAN and loopback HTTPS are intentional. DNS is checked again for every
// connection and only the validated IP is dialed, including through a selected
// proxy. Certificate verification still uses the original URL hostname.
func catalogAddressAllowed(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.Equal(net.IPv4bcast) && !ip.Equal(net.IP{100, 100, 100, 200})
}

func catalogDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid catalogue destination")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("catalogue destination could not be resolved")
	}
	for _, address := range addresses {
		if address.Zone != "" || !catalogAddressAllowed(address.IP) {
			return nil, errors.New("catalogue destination address is not permitted")
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	for _, address := range addresses {
		connection, err := outbound.Dial(ctx, network, net.JoinHostPort(address.IP.String(), port), dialer.DialContext)
		if err == nil {
			return connection, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("catalogue destination connection failed")
}

func catalogText(value string) bool {
	return value != "" && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func catalogFields(fields map[string]any) bool {
	for name, value := range fields {
		switch name {
		case "title", "size", "info_hash", "seeders", "leechers", "published_at", "category", "categories":
		default:
			return false
		}
		if values, ok := value.([]any); ok {
			for _, item := range values {
				if !catalogScalar(item) {
					return false
				}
			}
		} else if !catalogScalar(value) {
			return false
		}
	}
	return true
}

func catalogScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number:
		return true
	default:
		return false
	}
}

func (c *httpJSONConnector) fetchCatalog(ctx context.Context, checkpoint json.RawMessage) (model.Page, error) {
	state := model.CatalogCursor{Version: 1}
	if len(checkpoint) != 0 {
		if err := decodeJSON(checkpoint, &state); err != nil || state.Version != 1 || (state.Mode != "" && state.Mode != "full" && state.Mode != "incremental") || (state.Cursor != "" && state.Checkpoint != "") || (state.Mode == "" && (state.Cursor != "" || state.InstanceID != "" || state.Done)) || (state.Mode != "" && state.InstanceID == "") || (state.Done && state.Checkpoint == "") {
			return model.Page{}, errors.New("invalid catalogue continuation state")
		}
	}
	if state.Done {
		return model.Page{Done: true, Next: checkpoint}, nil
	}
	query := url.Values{"limit": {strconv.Itoa(c.provider.PageSize)}}
	if state.Cursor != "" {
		query.Set("cursor", state.Cursor)
	} else if state.Checkpoint != "" {
		query.Set("checkpoint", state.Checkpoint)
	}
	headers := http.Header{"Accept": {"application/json"}}
	if selection := c.provider.CatalogEntities.Header(); selection != "" {
		headers.Set(model.CatalogEntitiesHeader, selection)
	}
	response, err := c.client.Do(ctx, Request{Method: http.MethodGet, Query: query, DisableRedirects: true, Headers: headers})
	restarted := false
	if (err == nil || FailureCode(err) == "http") && response.StatusCode == http.StatusGone && state.Cursor != "" {
		var expired struct {
			Code string `json:"code"`
		}
		if decodeJSON(response.Body, &expired) == nil && expired.Code == model.CatalogSnapshotExpiredCode {
			// Expired snapshots cannot mix with their old staging. Request one new
			// authoritative Full and reset staging only after validating its page.
			// Keep the original endpoint/checkpoint fence and expected publisher.
			query.Del("cursor")
			response, err = c.client.Do(ctx, Request{Method: http.MethodGet, Query: query, DisableRedirects: true, Headers: headers})
			state.Mode, state.Cursor, state.Checkpoint = "", "", ""
			restarted = true
		}
	}
	page := model.Page{Body: response.Body, ContentType: "application/json", Next: checkpoint, RequireUniqueIDs: true, Metadata: map[string]any{"catalog_version": 1, "known_pages_managed": true}}
	fail := func(message string) (model.Page, error) {
		page.Error = message
		page.Metadata["failure_code"] = "parse"
		return page, &FailureError{code: "parse"}
	}
	if err != nil {
		failure := safeFailure(err, "unknown")
		page.Error = failure.Error()
		page.Metadata["failure_code"] = FailureCode(failure)
		return page, failure
	}
	if response.StatusCode != http.StatusOK {
		failure := httpFailure(response.StatusCode)
		page.Error = failure.Error()
		page.Metadata["failure_code"] = FailureCode(failure)
		return page, failure
	}
	// Decode original item fragments separately so raw archives retain wire bytes,
	// including unsupported/malformed items, rather than re-marshaled copies.
	var envelope struct {
		Version    int                   `json:"version"`
		Entities   model.CatalogEntities `json:"entities"`
		Supplement json.RawMessage       `json:"supplement"`
		InstanceID string                `json:"instance_id"`
		Mode       string                `json:"mode"`
		Items      []json.RawMessage     `json:"items"`
		NextCursor string                `json:"next_cursor"`
		Checkpoint string                `json:"checkpoint"`
	}
	if !utf8.Valid(response.Body) || decodeJSON(response.Body, &envelope) != nil {
		return fail("invalid remote catalogue envelope")
	}
	if envelope.Version == 2 {
		if c.provider.CatalogEntities.Empty() || envelope.Entities.Intersect(c.provider.CatalogEntities) != envelope.Entities {
			return fail("Remote catalog returned unrequested data permissions")
		}
		if len(envelope.Supplement) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(envelope.Supplement))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&page.CatalogSupplement) != nil || decoder.Decode(new(any)) != io.EOF {
				return fail("Invalid shared catalog data")
			}
		}
		if len(envelope.Items)+len(page.CatalogSupplement.Metadata)+len(page.CatalogSupplement.Availability) > c.provider.PageSize || !page.CatalogSupplement.Valid(envelope.Entities, time.Now()) {
			return fail("Invalid shared metadata or cache observations")
		}
	} else if !envelope.Entities.Empty() || len(envelope.Supplement) > 0 {
		return fail("Shared data requires catalog protocol version 2")
	}
	if state.Protocol != 0 && (state.Protocol != envelope.Version || state.Entities != envelope.Entities) {
		return fail("Remote catalog data permissions changed during traversal")
	}
	page.Metadata["catalog_version"] = envelope.Version
	page.Metadata["shared_metadata"] = len(page.CatalogSupplement.Metadata)
	page.Metadata["shared_cache_observations"] = len(page.CatalogSupplement.Availability)
	if len(envelope.Items) == 0 {
		page.Metadata["auxiliary_response"] = true
	}
	page.Items = make([]model.Record, 0, len(envelope.Items))
	invalidItem := false
	for _, raw := range envelope.Items {
		record := model.Record{Raw: raw, ContentType: "application/json"}
		var item model.CatalogItem
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || decodeJSON(raw, &item) != nil || !catalogText(item.Origin.InstanceID) || !catalogText(item.Origin.ProviderID) || !catalogText(item.Origin.SourceID) || item.ID != model.CatalogItemID(item.Origin) || !catalogFields(item.Fields) || (item.Deleted && (envelope.Mode != "incremental" || len(item.Fields) != 0)) {
			record.Error = "invalid remote catalogue identity or fields"
			invalidItem = true
		} else {
			record.SourceID, record.Fields, record.Deleted = item.ID, item.Fields, item.Deleted
			record.Origin = &item.Origin
			record.Ignored = item.Origin.InstanceID == c.localInstanceID
		}
		page.Items = append(page.Items, record)
	}
	if (envelope.Version != 1 && envelope.Version != 2) || !catalogText(envelope.InstanceID) || (envelope.Mode != "full" && envelope.Mode != "incremental") || envelope.Items == nil || len(envelope.Items) > c.provider.PageSize || (envelope.NextCursor == "") == (envelope.Checkpoint == "") || (envelope.NextCursor != "" && len(envelope.Items) == 0 && envelope.Version != 2) || len(envelope.NextCursor) > 16384 || len(envelope.Checkpoint) > 16384 {
		return fail("unsupported or inconsistent remote catalogue envelope")
	}
	if (state.InstanceID != "" && envelope.InstanceID != state.InstanceID) || (state.Mode != "" && envelope.Mode != state.Mode) || (state.Mode == "" && state.Checkpoint == "" && envelope.Mode != "full") || (envelope.NextCursor != "" && envelope.NextCursor == state.Cursor) {
		return fail("remote catalogue traversal changed identity or scope")
	}
	if invalidItem {
		return fail("remote catalogue contains invalid records")
	}
	page.Done = envelope.Checkpoint != ""
	page.ResetStaging = state.Mode == "" && envelope.Mode == "full"
	if restarted {
		page.Metadata["catalog_snapshot_restarted"] = true
	}
	page.Next, err = json.Marshal(model.CatalogCursor{Version: 1, Protocol: envelope.Version, Entities: envelope.Entities, CacheGeneration: state.CacheGeneration, InstanceID: envelope.InstanceID, Mode: envelope.Mode, Cursor: envelope.NextCursor, Checkpoint: envelope.Checkpoint, Done: page.Done, BaseEndpoint: state.BaseEndpoint, BaseCheckpoint: state.BaseCheckpoint})
	if err != nil {
		return fail("could not encode catalogue continuation")
	}
	return page, nil
}
