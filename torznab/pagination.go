package torznab

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Search fetches one page using the capabilities discovered by Open. It asks
// for extended attributes and records the fields and quotas actually returned.
func (c *Client) Search(ctx context.Context, query Query) (Page, error) {
	params, err := c.searchParameters(query)
	if err != nil {
		return Page{}, err
	}
	return c.searchPage(ctx, params, query.Offset)
}

// Walk visits each nonempty page sequentially, starting at Query.Offset. It
// advances by the actual item count, not the requested page size. A short page
// alone is not considered the end: Walk continues until an advertised total is
// reached or an empty page is received. Callback errors stop traversal unchanged.
// With Config.AdvisoryTotals, only an empty page completes the traversal.
//
// Repeated pages, inconsistent offsets, decreasing totals, and early empty pages
// with an advertised total return ErrPaginationStalled instead of reporting success.
// Results can overlap if an indexer changes while being traversed; applications
// should upsert by a stable release identity. No snapshot or full-history access
// is promised by Torznab. Cancellation stops requests and all quota waits.
func (c *Client) Walk(ctx context.Context, query Query, visit func(Page) error) error {
	if visit == nil {
		return errors.New("torznab: a page callback is required")
	}
	params, err := c.searchParameters(query)
	if err != nil {
		return err
	}
	seen := make(map[[sha256.Size]byte]struct{})
	offset := query.Offset
	var total *int
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		params.Set("offset", strconv.Itoa(offset))
		page, err := c.searchPage(ctx, params, offset)
		if err != nil {
			return err
		}
		if !c.config.AdvisoryTotals && page.Total != nil {
			value := *page.Total
			if total != nil && value < *total {
				return fmt.Errorf("%w: advertised total decreased", ErrPaginationStalled)
			}
			total = &value
		}
		if len(page.Items) == 0 {
			if total != nil && offset < *total {
				return fmt.Errorf("%w: empty page before the advertised total", ErrPaginationStalled)
			}
			return nil
		}
		next := offset + len(page.Items) // searchPage already checked overflow.
		if total != nil && next > *total {
			return fmt.Errorf("%w: items exceed the advertised total", ErrPaginationStalled)
		}
		digest := PageFingerprint(page.Items)
		if _, duplicate := seen[digest]; duplicate {
			return fmt.Errorf("%w: repeated page", ErrPaginationStalled)
		}
		seen[digest] = struct{}{}
		if err := visit(page); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if total != nil && next == *total {
			return nil
		}
		offset = next
	}
}

// Attributes returns the sorted names of extended attributes observed in
// successful searches so far. It is an observation, not an exhaustive schema:
// later releases or categories may introduce additional fields. Values remain
// available in each Item.Attributes without discarding provider-specific data.
func (c *Client) Attributes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.observedAttributes))
	for name := range c.observedAttributes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Client) searchParameters(query Query) (url.Values, error) {
	if query.Offset < 0 || query.Limit < 0 {
		return nil, errors.New("torznab: offset and limit must be nonnegative")
	}
	mode := query.Mode
	if mode == "" {
		mode = ModeSearch
	}
	switch mode {
	case ModeSearch, ModeTV, ModeMovie, ModeMusic, ModeBook:
	default:
		return nil, ErrUnsupportedSearch
	}
	capability, advertised := c.caps.Searches[mode]
	if (advertised && !capability.Available) || (!advertised && mode != ModeSearch) {
		return nil, ErrUnsupportedSearch
	}
	params := make(url.Values, len(query.Params)+7)
	for name, values := range query.Params {
		switch strings.ToLower(name) {
		case "t", "apikey", "q", "cat", "offset", "limit", "extended", "o":
			return nil, errors.New("torznab: Params cannot override a reserved query parameter")
		}
		params[name] = append([]string(nil), values...)
	}
	limit := query.Limit
	if limit == 0 {
		limit = c.config.PageSize
	}
	if limit == 0 {
		limit = c.caps.Limits.Default
	}
	if limit == 0 {
		limit = c.caps.Limits.Max
	}
	if limit == 0 {
		limit = 100
	}
	if max := c.caps.Limits.Max; max > 0 && limit > max {
		limit = max
	}
	params.Set("t", string(mode))
	params.Set("offset", strconv.Itoa(query.Offset))
	params.Set("limit", strconv.Itoa(limit))
	params.Set("extended", "1")
	params.Set("o", "xml")
	if query.Text != "" {
		params.Set("q", query.Text)
	}
	if len(query.Categories) > 0 {
		categories := make([]string, 0, len(query.Categories))
		seen := make(map[int]struct{}, len(query.Categories))
		for _, id := range query.Categories {
			if id < 0 {
				return nil, errors.New("torznab: category IDs must be nonnegative")
			}
			if _, duplicate := seen[id]; !duplicate {
				categories = append(categories, strconv.Itoa(id))
				seen[id] = struct{}{}
			}
		}
		params.Set("cat", strings.Join(categories, ","))
	}
	return params, nil
}

func (c *Client) searchPage(ctx context.Context, params url.Values, offset int) (Page, error) {
	var page Page
	err := c.fetch(ctx, c.endpoint, params, func(data []byte) error {
		var err error
		page, err = parsePage(data, offset, c.config.PublishedAtUnit)
		if err != nil {
			return err
		}
		if page.RateLimit != nil {
			c.observeRate(*page.RateLimit)
		}
		if page.Offset != offset {
			return fmt.Errorf("%w: response offset differs from request", ErrPaginationStalled)
		}
		if len(page.Items) > int(^uint(0)>>1)-offset {
			return fmt.Errorf("%w: offset overflow", ErrPaginationStalled)
		}
		if !c.config.AdvisoryTotals && page.Total != nil && len(page.Items) > 0 && offset+len(page.Items) > *page.Total {
			return fmt.Errorf("%w: items exceed the advertised total", ErrPaginationStalled)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, item := range page.Items {
			for name := range item.Attributes {
				if c.observedAttributes == nil {
					c.observedAttributes = make(map[string]struct{})
				}
				c.observedAttributes[name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

// PageFingerprint detects repeated pages independently of item order and mutable
// counters. It falls back to descriptive fields when GUIDs, links and hashes are
// absent; the digest is pagination evidence, never a release's native identity.
func PageFingerprint(items []Item) [sha256.Size]byte {
	type identity struct {
		kind byte
		key  string
	}
	identities := make([]identity, len(items))
	for i, item := range items {
		switch {
		case item.GUID != "":
			identities[i] = identity{'g', item.GUID}
		case item.Link != "" && !isMagnet(item.Link):
			identities[i] = identity{'l', item.Link}
		case item.InfoHash != "":
			identities[i] = identity{'h', item.InfoHash}
		case item.Link != "":
			identities[i] = identity{'l', item.Link}
		default:
			key := item.Title
			if item.Size != nil {
				key += "\x00" + strconv.FormatInt(*item.Size, 10)
			}
			if item.PublishedAt != nil {
				key += "\x00" + item.PublishedAt.UTC().Format("2006-01-02T15:04:05.999999999Z")
			}
			identities[i] = identity{'t', key}
		}
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].kind != identities[j].kind {
			return identities[i].kind < identities[j].kind
		}
		return identities[i].key < identities[j].key
	})
	hash := sha256.New()
	var prefix [9]byte
	for _, item := range identities {
		prefix[0] = item.kind
		binary.LittleEndian.PutUint64(prefix[1:], uint64(len(item.key)))
		hash.Write(prefix[:])
		hash.Write([]byte(item.key))
	}
	var digest [sha256.Size]byte
	hash.Sum(digest[:0])
	return digest
}
