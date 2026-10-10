package torznab

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// RawItem keeps the exact XML fragment separately from normalization. Namespace
// declarations may be inherited from RawPage.Body. Error never includes source
// text. Existing Item and Search JSON representations remain unchanged.
type RawItem struct {
	Body  []byte
	Item  Item
	Error error
}

// RawPage is an archival search response. Body is retained on protocol and HTTP
// failures as well. Items includes every complete item fragment recoverable from
// the response; fields of malformed records must not be treated as authoritative.
type RawPage struct {
	Page  Page
	Body  []byte
	Items []RawItem
}

// SearchRaw is the archival counterpart of Search. Unlike Search, invalid item
// values become individual RawItem errors, rather than discarding sibling items.
// Invalid XML or inconsistent pagination still fails the page, retaining bytes.
func (c *Client) SearchRaw(ctx context.Context, query Query) (RawPage, error) {
	params, err := c.searchParameters(query)
	if err != nil {
		return RawPage{}, err
	}
	var result RawPage
	err = c.fetchResponseObserved(ctx, c.endpoint, params, func(data []byte, _ *url.URL) error {
		var parseErr error
		result, parseErr = parseRawPage(data, query.Offset, c.config.PublishedAtUnit)
		if parseErr != nil {
			return parseErr
		}
		page := result.Page
		if page.RateLimit != nil {
			c.observeRate(*page.RateLimit)
		}
		if page.Offset != query.Offset {
			return fmt.Errorf("%w: response offset differs from request", ErrPaginationStalled)
		}
		if len(result.Items) > int(^uint(0)>>1)-query.Offset {
			return fmt.Errorf("%w: offset overflow", ErrPaginationStalled)
		}
		if !c.config.AdvisoryTotals && page.Total != nil && query.Offset+len(result.Items) > *page.Total {
			return fmt.Errorf("%w: items exceed the advertised total", ErrPaginationStalled)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, item := range result.Items {
			for name := range item.Item.Attributes {
				if c.observedAttributes == nil {
					c.observedAttributes = make(map[string]struct{})
				}
				c.observedAttributes[name] = struct{}{}
			}
		}
		return nil
	}, func(data []byte) { result.Body = data })
	if err != nil && len(result.Items) == 0 && len(result.Body) > 0 {
		parsed, _ := parseRawPage(result.Body, query.Offset, c.config.PublishedAtUnit)
		result = parsed
	}
	return result, err
}

func parseRawPage(data []byte, offset int, publishedAtUnit string) (RawPage, error) {
	result := RawPage{Body: data, Page: Page{Offset: offset}}
	fragments, fragmentErr := rawItemFragments(data)
	result.Items = make([]RawItem, len(fragments))
	for index, fragment := range fragments {
		result.Items[index] = RawItem{Body: fragment, Error: errProtocol}
	}
	if fragmentErr != nil {
		return result, fragmentErr
	}
	if err := parseAPIError(data); err != nil {
		return result, err
	}
	var raw struct {
		Channels []struct {
			Items     []rssItem     `xml:"item"`
			Responses []rssResponse `xml:"response"`
			Quotas    []rssQuota    `xml:"apilimits"`
		} `xml:"channel"`
	}
	if err := decodeProtocol(data, "rss", &raw); err != nil {
		return result, err
	}
	if len(raw.Channels) != 1 {
		return result, errProtocol
	}
	channel := raw.Channels[0]
	if len(channel.Items) != len(result.Items) || len(channel.Quotas) > 1 {
		return result, errProtocol
	}
	for index, source := range channel.Items {
		item, err := normalizeItem(source, publishedAtUnit)
		if err == nil && strings.TrimSpace(source.Date) != "" && item.PublishedAt == nil {
			err = errProtocol
		}
		if err == nil && item.InfoHash != "" {
			hash := item.InfoHash
			switch len(hash) {
			case 40, 64:
				if _, decodeErr := hex.DecodeString(hash); decodeErr != nil {
					err = errProtocol
				}
			case 32:
				decoded, decodeErr := base32.StdEncoding.DecodeString(strings.ToUpper(hash))
				if decodeErr != nil {
					err = errProtocol
				} else {
					item.InfoHash = hex.EncodeToString(decoded)
				}
			default:
				err = errProtocol
			}
		}
		result.Items[index].Item, result.Items[index].Error = item, err
		result.Page.Items = append(result.Page.Items, item)
	}
	pageOffset, total, err := parsePagination(channel.Responses, offset)
	if err != nil {
		return result, err
	}
	result.Page.Offset, result.Page.Total = pageOffset, total
	if len(channel.Quotas) == 1 {
		result.Page.RateLimit, err = parseQuota(channel.Quotas[0])
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func rawItemFragments(data []byte) ([][]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var path []string
	var result [][]byte
	start := int64(-1)
	for {
		before := decoder.InputOffset()
		token, err := decoder.Token()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return result, errProtocol
		}
		switch token := token.(type) {
		case xml.Directive:
			return result, errProtocol
		case xml.StartElement:
			path = append(path, token.Name.Local)
			if len(path) > 128 {
				return result, errors.New("torznab: XML nesting exceeds the limit")
			}
			if len(path) == 3 && path[0] == "rss" && path[1] == "channel" && path[2] == "item" {
				start = before
			}
		case xml.EndElement:
			if len(path) == 3 && start >= 0 {
				result = append(result, data[start:decoder.InputOffset()])
				start = -1
			}
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		}
	}
}
