package connectors

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/andybalholm/cascadia"
	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/net/html"
)

const maxHTMLValueBytes = 64 << 10

var errHTMLFieldLimit = errors.New("HTML extracted fields exceed the page text limit")

var htmlInteger = regexp.MustCompile(`^(?:[0-9]+|[0-9]{1,3}(?:[ ,.\x{00a0}\x{202f}][0-9]{3})+)$`)

func htmlText(node *html.Node) string {
	var result strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if result.Len() > maxHTMLValueBytes {
			return
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "template", "noscript":
				return
			case "br", "p", "div", "li", "td", "th":
				result.WriteByte(' ')
			}
		}
		if node.Type == html.TextNode {
			result.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "p", "div", "li", "td", "th":
				result.WriteByte(' ')
			}
		}
	}
	visit(node)
	return strings.Join(strings.Fields(result.String()), " ")
}

func htmlAttribute(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func htmlHasAttribute(node *html.Node, name string) bool {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return true
		}
	}
	return false
}

func (field htmlField) read(root *html.Node) ([]string, error) {
	nodes := []*html.Node{root}
	if field.selector != nil {
		nodes = cascadia.QueryAll(root, field.selector)
	}
	var values []string
	var textBytes int
	for _, node := range nodes {
		value := htmlText(node)
		if field.config.Attribute != "" {
			value = htmlAttribute(node, field.config.Attribute)
		}
		if len(value) > maxHTMLValueBytes || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return nil, errors.New("HTML field exceeds its text limit or contains invalid text")
		}
		if field.pattern != nil {
			match := field.pattern.FindStringSubmatch(value)
			if len(match) == 0 {
				value = ""
			} else {
				value = match[len(match)-1]
			}
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		values = append(values, value)
		textBytes += len(value)
		if textBytes > maxResponseBytes {
			return nil, errHTMLFieldLimit
		}
		if !field.config.All {
			break
		}
		if len(values) > 1000 {
			return nil, errors.New("HTML field has too many values")
		}
	}
	if field.config.Required && len(values) == 0 {
		return nil, errors.New("required HTML field is missing")
	}
	return values, nil
}

func (field htmlField) convert(value string, base *url.URL, unit string) (any, error) {
	switch field.config.Format {
	case "", "text":
		return value, nil
	case "integer":
		return htmlNumber(value)
	case "size":
		return htmlBytes(value)
	case "url":
		target, err := htmlURL(base, value, false)
		if err != nil {
			return nil, err
		}
		return target.String(), nil
	case "info_hash":
		return htmlInfoHash(value)
	case "date":
		if field.config.DateLayout == "" {
			return normalizePublishedAt(value, unit)
		}
		date, err := time.ParseInLocation(field.config.DateLayout, value, field.location)
		if err != nil || date.UTC().Year() < 1 || date.UTC().Year() > 9999 {
			return nil, errors.New("HTML publication date does not match its layout")
		}
		return date.UTC().Format(time.RFC3339Nano), nil
	default:
		return nil, errors.New("HTML field format cannot be converted as text")
	}
}

func htmlNumber(value string) (int64, error) {
	if !htmlInteger.MatchString(value) {
		return 0, errors.New("HTML integer must be a nonnegative whole number")
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == ',' || r == '.' {
			return -1
		}
		return r
	}, value)
	return integerValue(value)
}

func htmlBytes(value string) (int64, error) { return model.ParseByteSize(value) }

func htmlInfoHash(value string) (any, error) {
	if !strings.HasPrefix(strings.ToLower(value), "magnet:") {
		return normalizeField("info_hash", value, "")
	}
	magnet, err := url.Parse(value)
	if err != nil || !strings.EqualFold(magnet.Scheme, "magnet") || magnet.Host != "" {
		return nil, errors.New("HTML magnet has an invalid identity")
	}
	query, err := url.ParseQuery(magnet.RawQuery)
	if err != nil {
		return nil, errors.New("HTML magnet query is invalid")
	}
	var identity any
	for _, topic := range query["xt"] {
		if !strings.HasPrefix(strings.ToLower(topic), "urn:btih:") {
			continue
		}
		hash, err := normalizeField("info_hash", topic[len("urn:btih:"):], "")
		if err != nil || len(FieldString(hash)) != 40 || identity != nil && identity != hash {
			return nil, errors.New("HTML magnet has conflicting or invalid identities")
		}
		identity = hash
	}
	if identity == nil {
		return nil, errors.New("HTML magnet has no BitTorrent v1 identity")
	}
	// Only the hash survives: announce addresses and magnet parameters do not.
	return identity, nil
}

func htmlURL(base *url.URL, value string, credentials bool) (*url.URL, error) {
	if value == "" || len(value) > 8192 || strings.ContainsAny(value, "\\\r\n\x00") {
		return nil, errors.New("HTML link is invalid")
	}
	reference, err := url.Parse(value)
	if err != nil {
		return nil, errors.New("HTML link is invalid")
	}
	target := base.ResolveReference(reference)
	target.Fragment, target.RawFragment = "", ""
	if !validHTTPURL(target) {
		return nil, errors.New("HTML links require HTTP(S) without userinfo")
	}
	query, err := url.ParseQuery(target.RawQuery)
	if err != nil {
		return nil, errors.New("HTML link query is invalid")
	}
	if !credentials {
		for name := range query {
			if model.CredentialParameter(name) {
				return nil, errors.New("HTML persistent links must not contain credentials")
			}
		}
	}
	target.RawQuery = query.Encode()
	return target, nil
}

func (c *htmlConnector) containsSecret(text string) bool {
	values := []string{c.client.password}
	if c.client.authType != "cookie" {
		values = append(values, c.client.authValue)
	}
	for _, headers := range c.client.secretHeaders {
		values = append(values, headers...)
	}
	for _, cookie := range c.client.jar.Cookies(c.client.origin) {
		name := strings.ToLower(cookie.Name)
		// Websites also set harmless short preferences such as consent=1.
		// Treating every such value as a secret would reject ordinary IDs,
		// titles and next-page URLs containing that digit.
		if len(cookie.Value) >= 16 || model.CredentialParameter(name) || strings.Contains(name, "session") || strings.Contains(name, "auth") || strings.Contains(name, "sid") {
			values = append(values, cookie.Value)
		}
	}
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(value), "bearer ") {
			value = strings.TrimSpace(value[7:])
		}
		if value != "" && (strings.Contains(text, value) || strings.Contains(text, url.QueryEscape(value))) {
			return true
		}
	}
	return false
}

func (c *htmlConnector) continuationURL(base *url.URL, value string) (*url.URL, error) {
	target, err := htmlURL(base, value, true)
	if err != nil {
		return nil, err
	}
	if c.client.authType == "query" {
		query := target.Query()
		for name, values := range query {
			if !strings.EqualFold(name, c.client.authName) {
				continue
			}
			for _, value := range values {
				if value != c.client.authValue {
					return nil, errors.New("HTML continuation changes source credentials")
				}
			}
			delete(query, name)
		}
		target.RawQuery = query.Encode()
	}
	// Managed query authentication is reapplied by Client, never saved inside
	// a checkpoint. Other credential parameters are still refused.
	return htmlURL(base, target.String(), false)
}

func (c *htmlConnector) containsURLSecret(target *url.URL) bool {
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
