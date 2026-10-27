package model

import (
	"encoding/json"
	"strconv"
	"strings"
)

// BookSourceHints reads only bibliographic fields understood by the matcher.
// The same projection is used for queued identification and private audits.
func BookSourceHints(fields map[string]any) BookHints {
	text := func(v any) string {
		switch v := v.(type) {
		case string:
			return v
		case json.Number:
			return string(v)
		case int:
			return strconv.Itoa(v)
		case int64:
			return strconv.FormatInt(v, 10)
		case []string:
			if len(v) > 0 {
				return v[0]
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case []any:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					return s
				}
			}
		}
		return ""
	}
	first := func(names ...string) string {
		for _, name := range names {
			if s := text(fields[name]); s != "" {
				return s
			}
		}
		return ""
	}
	h := BookHints{OpenLibrary: first("openlibrary_id", "openlibrary"), Title: first("book_title"), Kind: first("media_type", "type"), ISBN: first("isbn", "isbn_13", "isbn_10"), Author: first("author", "authors"), Format: first("book_format"), CategoryNames: []string{first("category_name"), first("subcategory_name")}}
	var identifiers func(any)
	identifiers = func(value any) {
		if len(h.ISBNs) >= 100 {
			return
		}
		if list, ok := value.([]string); ok {
			for _, v := range list {
				identifiers(v)
			}
			return
		}
		if list, ok := value.([]any); ok {
			for _, v := range list {
				identifiers(v)
			}
			return
		}
		if s := text(value); s != "" {
			h.ISBNs = append(h.ISBNs, s)
		}
	}
	for _, key := range []string{"isbn", "isbn_10", "isbn_13"} {
		identifiers(fields[key])
	}
	if attrs := bookHintObject(fields["attributes"]); attrs != nil {
		for _, key := range []string{"isbn", "isbn10", "isbn13", "isbn_10", "isbn_13"} {
			identifiers(attrs[key])
		}
		if h.ISBN == "" {
			h.ISBN = text(attrs["isbn"])
		}
		if h.Author == "" {
			h.Author = text(attrs["author"])
			if h.Author == "" {
				h.Author = text(attrs["authors"])
			}
		}
		if h.Title == "" {
			h.Title = text(attrs["booktitle"])
			if h.Title == "" {
				h.Title = text(attrs["book_title"])
			}
		}
		if h.OpenLibrary == "" {
			h.OpenLibrary = text(attrs["openlibrary_id"])
			if h.OpenLibrary == "" {
				h.OpenLibrary = text(attrs["openlibrary"])
			}
		}
		if h.Format == "" {
			h.Format = text(attrs["book_format"])
		}
	}
	if ext := bookHintObject(fields["external_ids"]); ext != nil {
		for _, key := range []string{"isbn", "isbn_10", "isbn_13"} {
			identifiers(ext[key])
		}
		if h.OpenLibrary == "" {
			h.OpenLibrary = text(ext["openlibrary"])
			if h.OpenLibrary == "" {
				h.OpenLibrary = text(ext["openlibrary_id"])
			}
		}
	}
	var categories func(any)
	categories = func(value any) {
		switch v := value.(type) {
		case []int:
			h.Categories = append(h.Categories, v...)
		case []string:
			for _, c := range v {
				categories(c)
			}
		case []any:
			for _, c := range v {
				categories(c)
			}
		default:
			for _, raw := range strings.FieldsFunc(text(value), func(c rune) bool { return c == ',' || c == ';' }) {
				if n, e := strconv.Atoi(strings.TrimSpace(raw)); e == nil {
					h.Categories = append(h.Categories, n)
				}
			}
		}
	}
	categories(fields["categories"])
	categories(fields["category"])
	return h
}

func bookHintObject(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case map[string]string:
		out := map[string]any{}
		for k, s := range v {
			out[k] = s
		}
		return out
	case map[string][]string:
		out := map[string]any{}
		for k, s := range v {
			out[k] = s
		}
		return out
	}
	return nil
}
