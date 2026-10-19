package connectors

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/theo-bnts/aiosources/internal/model"
)

type htmlField struct {
	config   model.HTMLField
	selector cascadia.Matcher
	pattern  *regexp.Regexp
	location *time.Location
}

func htmlSelector(value string) (cascadia.Matcher, error) {
	if strings.TrimSpace(value) == "" || len(value) > 2048 {
		return nil, errors.New("HTML selectors must contain 1–2048 bytes")
	}
	selector, err := cascadia.ParseGroup(value)
	if err != nil {
		// Parser errors may quote arbitrary input from a private definition.
		return nil, errors.New("HTML selector is not supported CSS")
	}
	return selector, nil
}

func compileHTMLField(config model.HTMLField) (htmlField, error) {
	field := htmlField{config: config, location: time.UTC}
	var err error
	if config.Selector != "" {
		field.selector, err = htmlSelector(config.Selector)
		if err != nil {
			return field, err
		}
	}
	if config.Attribute != "" && (!safeField.MatchString(config.Attribute) || strings.ContainsAny(config.Attribute, ".")) {
		return field, errors.New("HTML attribute names must be safe names such as href, datetime or data-id")
	}
	if config.Pattern != "" {
		if len(config.Pattern) > 2048 {
			return field, errors.New("HTML extraction patterns are limited to 2048 bytes")
		}
		field.pattern, err = regexp.Compile(config.Pattern)
		if err != nil || field.pattern.NumSubexp() > 1 {
			return field, errors.New("HTML extraction patterns must be valid Go regular expressions with at most one capture")
		}
	}
	switch config.Format {
	case "", "text", "integer", "size", "date", "url", "info_hash", "torrent":
	default:
		return field, errors.New("unknown HTML field format")
	}
	if (config.DateLayout != "" || config.Timezone != "") && config.Format != "date" {
		return field, errors.New("HTML date layouts and timezones require the date format")
	}
	if len(config.DateLayout) > 256 {
		return field, errors.New("HTML date layouts are limited to 256 bytes")
	}
	if config.Timezone != "" {
		field.location, err = time.LoadLocation(config.Timezone)
		if err != nil {
			return field, errors.New("HTML date timezone must be an IANA timezone")
		}
	}
	if (config.Format == "torrent" || config.Format == "info_hash") && config.All {
		return field, errors.New("HTML hash fields must select a single value")
	}
	return field, nil
}

func validateHTML(p model.Provider) error {
	config := p.HTML
	if config == nil {
		return errors.New("HTML sources require html settings")
	}
	if p.HTTP.ItemsPath != "" || p.Mapping.ID != "" || len(p.Mapping.Fields) > 0 || p.Traversal != nil {
		return errors.New("HTML sources use CSS selectors in html, not JSON mappings or traversal")
	}
	if p.Search.Query != "" || len(p.Search.Categories) > 0 {
		return errors.New("HTML search parameters belong in http.query; search settings are Torznab-only")
	}
	for name := range p.Options {
		if name != "published_at_unit" && name != "local_categories" {
			return errors.New("unknown HTML adapter option")
		}
	}
	if _, err := publishedAtUnit(p); err != nil {
		return err
	}
	if categories, err := localCategories(p); err != nil {
		return err
	} else if len(categories) > 0 {
		_, listing := config.Fields["categories"]
		detail := false
		if config.Details != nil {
			_, detail = config.Details.Fields["categories"]
		}
		if !listing && !detail {
			return errors.New("local_categories requires an HTML categories mapping")
		}
	}
	for _, selector := range []string{config.PageSelector, config.RowsSelector} {
		if _, err := htmlSelector(selector); err != nil {
			return err
		}
	}
	for _, selector := range []string{config.NextSelector, config.EmptySelector} {
		if selector != "" {
			if _, err := htmlSelector(selector); err != nil {
				return err
			}
		}
	}
	if config.ID.All || config.ID.Format != "" && config.ID.Format != "text" && config.ID.Format != "url" && config.ID.Format != "integer" {
		return errors.New("HTML native identity must be a single text, URL or integer value")
	}
	if _, err := compileHTMLField(config.ID); err != nil {
		return fmt.Errorf("HTML native identity: %w", err)
	}
	validateFields := func(fields map[string]model.HTMLField) error {
		if len(fields) == 0 || len(fields) > model.MaxHTMLFields {
			return errors.New("HTML mappings require 1–64 fields")
		}
		for name, config := range fields {
			if !safeField.MatchString(name) {
				return errors.New("HTML field names must be safe identifiers")
			}
			if _, err := compileHTMLField(config); err != nil {
				return fmt.Errorf("HTML field configuration: %w", err)
			}
			if config.Format == "torrent" && name != "info_hash" {
				return errors.New("HTML torrent-file extraction is supported only for info_hash")
			}
			if config.All && (name == "title" || name == "size" || name == "published_at" || name == "info_hash") {
				return errors.New("HTML title, size, publication date and hash must be single values")
			}
			allowed := true
			switch name {
			case "title":
				allowed = config.Format == "" || config.Format == "text"
			case "size":
				allowed = config.Format == "" || config.Format == "text" || config.Format == "integer" || config.Format == "size"
			case "info_hash":
				allowed = config.Format == "" || config.Format == "text" || config.Format == "info_hash" || config.Format == "torrent"
			}
			if !allowed {
				return errors.New("HTML field format conflicts with the catalog field type")
			}
		}
		return nil
	}
	if err := validateFields(config.Fields); err != nil {
		return err
	}
	_, listingTitle := config.Fields["title"]
	if config.Details != nil {
		detail := config.Details
		if detail.URL.All || detail.URL.Format != "" && detail.URL.Format != "url" && detail.URL.Format != "text" {
			return errors.New("HTML detail links require a single URL or text value")
		}
		if _, err := compileHTMLField(detail.URL); err != nil {
			return err
		}
		if _, err := htmlSelector(detail.PageSelector); err != nil {
			return err
		}
		if err := validateFields(detail.Fields); err != nil {
			return err
		}
		_, detailTitle := detail.Fields["title"]
		listingTitle = listingTitle || detailTitle
	}
	if !listingTitle {
		return errors.New("HTML sources require a title field in the listing or detail mapping")
	}
	for _, check := range []*model.HTMLField{config.Total, config.Current} {
		if check == nil {
			continue
		}
		if check.All || check.Format != "" && check.Format != "text" && check.Format != "integer" {
			return errors.New("HTML pagination checks require a single integer value")
		}
		if _, err := compileHTMLField(*check); err != nil {
			return err
		}
	}
	if p.HTTP.Method != http.MethodGet && p.HTTP.Method != http.MethodPost {
		return errors.New("HTML sources support GET or form POST")
	}
	if p.HTTP.Method == http.MethodGet && p.HTTP.Body != nil {
		return errors.New("HTML GET requests cannot include a body")
	}
	if p.HTTP.Body != nil {
		body, ok := p.HTTP.Body.(map[string]any)
		if !ok {
			return errors.New("HTML form bodies must be JSON objects of form values")
		}
		for _, value := range body {
			if _, err := queryValues(value); err != nil {
				return errors.New("HTML form values must be scalars or arrays of scalars")
			}
		}
	}
	if p.Pagination.In != "query" && p.Pagination.In != "body" || p.Pagination.In == "body" && p.HTTP.Method != http.MethodPost {
		return errors.New("HTML pagination requires query parameters or a POST form body")
	}
	if p.Pagination.NextPath != "" || p.Pagination.CurrentPath != "" || p.Pagination.TotalPath != "" || p.Pagination.CursorParam != "" {
		return errors.New("HTML pagination uses CSS selectors, not JSON response pointers or cursor parameters")
	}
	switch p.Pagination.Type {
	case "next":
		if config.NextSelector == "" || p.Pagination.In == "body" {
			return errors.New("HTML next-link pagination requires html.next_selector and query parameters")
		}
	case "page", "offset", "none":
	default:
		return errors.New("HTML pagination must be next, page, offset or none")
	}
	if p.Pagination.Start < 0 {
		return errors.New("HTML starting position must be nonnegative")
	}
	parameter := p.Pagination.PageParam
	if p.Pagination.Type == "offset" {
		parameter = p.Pagination.OffsetParam
	}
	for _, name := range []string{parameter, p.Pagination.SizeParam} {
		if name != "" && (!safeField.MatchString(name) || sensitiveName(name) || p.Auth.Name != "" && strings.EqualFold(name, p.Auth.Name)) {
			return errors.New("HTML pagination parameter names must be safe non-credential names")
		}
	}
	if parameter == p.Pagination.SizeParam && parameter != "" {
		return errors.New("HTML pagination position and size parameters must differ")
	}
	return nil
}

func htmlTemplate() string {
	return `{
  "version": 1,
  "id": "example_html",
  "name": "Website catalog",
  "adapter": "html",
  "url": "https://archive.example/catalog",
  "enabled": false,
  "auth": { "type": "none" },
  "request_interval": "2s",
  "request_timeout": "30s",
  "page_size": 100,
  "http": { "method": "GET" },
  "pagination": { "type": "next", "start": 1 },
  "html": {
    "page_selector": "#catalog",
    "rows_selector": "tbody > tr[data-id]",
    "empty_selector": ".empty-results",
    "next_selector": "a[rel=next]:not([aria-disabled=true])",
    "id": { "attribute": "data-id" },
    "fields": {
      "title": { "selector": ".title", "required": true },
      "info_hash": { "selector": "a[href^='magnet:']", "attribute": "href", "format": "info_hash" },
      "size": { "selector": ".size", "format": "size" },
      "published_at": { "selector": "time[datetime]", "attribute": "datetime", "format": "date" }
    }
  },
  "schedule": { "known_pages": 0 }
}
`
}
