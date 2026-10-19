package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Additional entry points share the source's fields, transport and identity
// namespace. Each listing owns its pagination and Incremental stopping boundary.
type listingsConnector struct {
	inner       Connector
	urls        []string
	selectURL   func(string)
	knownIDs    func(context.Context, []string) (map[string]bool, error)
	knownPages  int
	incremental bool
}

type listingsCursor struct {
	Version    int             `json:"version"`
	Listing    int             `json:"listing"`
	Cursor     json.RawMessage `json:"cursor,omitempty"`
	KnownPages int             `json:"known_pages,omitempty"`
	Done       bool            `json:"done,omitempty"`
}

func validateListings(p model.Provider) error {
	if len(p.HTTP.ListingPaths) == 0 {
		return nil
	}
	if len(p.HTTP.ListingPaths) > 1000 || p.HTTP.Catalog || p.Traversal != nil || p.Adapter != "html" && !p.IsJSON() {
		return errors.New("additional listings require an ordinary JSON or HTML source and at most 1000 paths")
	}
	base, err := url.Parse(p.URL)
	if err != nil || !validHTTPURL(base) {
		return errors.New("additional listings require a valid source URL")
	}
	seen := map[string]bool{base.String(): true}
	for _, path := range p.HTTP.ListingPaths {
		u, err := url.Parse(path)
		if err != nil || len(path) > 2048 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || u.IsAbs() || u.Host != "" || u.Fragment != "" || u.ForceQuery {
			return errors.New("additional listings must be distinct absolute paths on the source origin, without fragments")
		}
		target := base.ResolveReference(u)
		if !validHTTPURL(target) || !sameOrigin(base, target) || seen[target.String()] {
			return errors.New("additional listings must be distinct paths on the source origin")
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return errors.New("invalid additional listing query")
		}
		for name := range q {
			if sensitiveName(name) || p.Auth.Name != "" && strings.EqualFold(name, p.Auth.Name) {
				return errors.New("additional listings cannot contain credentials; use secret references")
			}
		}
		seen[target.String()] = true
	}
	return nil
}

func newListings(inner Connector, p model.Provider, mode model.RunMode, env Environment) Connector {
	c := &listingsConnector{inner: inner, urls: []string{p.URL}, knownIDs: env.KnownIDs, knownPages: p.Schedule.KnownPages, incremental: mode == model.ModeIncremental}
	base, _ := url.Parse(p.URL)
	for _, path := range p.HTTP.ListingPaths {
		u, _ := url.Parse(path)
		c.urls = append(c.urls, base.ResolveReference(u).String())
	}
	switch inner := inner.(type) {
	case *htmlConnector:
		c.selectURL = func(value string) { inner.provider.URL = value }
	case *httpJSONConnector:
		c.selectURL = func(value string) { inner.provider.URL = value }
	}
	return c
}

func (c *listingsConnector) Close() error { return c.inner.Close() }

func (c *listingsConnector) Fetch(ctx context.Context, checkpoint json.RawMessage) (model.Page, error) {
	state := listingsCursor{Version: 1}
	invalid := func() (model.Page, error) {
		return model.Page{Next: checkpoint}, &FailureError{code: "configuration"}
	}
	if len(checkpoint) > 0 {
		if err := decodeJSON(checkpoint, &state); err != nil || state.Version != 1 || state.Listing < 0 || state.Listing > len(c.urls) || state.Done != (state.Listing == len(c.urls)) || state.KnownPages < 0 || state.KnownPages > c.knownPages || state.KnownPages > 0 && (c.knownPages == 0 || !c.incremental) {
			return invalid()
		}
	}
	if state.Done {
		return model.Page{Done: true, Next: checkpoint}, nil
	}
	if c.selectURL == nil {
		return invalid()
	}
	c.selectURL(c.urls[state.Listing])
	page, err := c.inner.Fetch(ctx, state.Cursor)
	if page.Metadata == nil {
		page.Metadata = make(map[string]any)
	}
	page.Position = "listing:" + strconv.Itoa(state.Listing) + ":" + page.Position
	page.Metadata["listing_index"], page.Metadata["listing_count"] = state.Listing, len(c.urls)
	page.Metadata["known_pages_managed"] = true
	page.Metadata["fingerprint_scope"] = "listing:" + strconv.Itoa(state.Listing)
	if err != nil || page.Error != "" {
		page.Next, page.Done = checkpoint, false
		return page, err
	}
	next := state
	next.Cursor = page.Next
	if c.incremental && c.knownPages > 0 && c.knownIDs != nil && !page.Done {
		ids, complete := listingIDs(page.Items)
		allKnown := complete && len(ids) > 0
		if allKnown {
			known, err := c.knownIDs(ctx, ids)
			if err != nil {
				page.Next, page.Done = checkpoint, false
				return page, err
			}
			for _, id := range ids {
				allKnown = allKnown && known[id]
			}
		}
		if allKnown {
			next.KnownPages++
		} else {
			next.KnownPages = 0
		}
		page.Done = next.KnownPages >= c.knownPages
	}
	if page.Done {
		next.Listing++
		next.Cursor, next.KnownPages = nil, 0
	}
	next.Done = next.Listing == len(c.urls)
	page.Done = next.Done
	page.Next, err = json.Marshal(next)
	return page, err
}

func listingIDs(items []model.Record) ([]string, bool) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Auxiliary {
			continue
		}
		if item.SourceID == "" || item.Error != "" {
			return nil, false
		}
		ids = append(ids, item.SourceID)
	}
	return ids, true
}
