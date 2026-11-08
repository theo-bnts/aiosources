package model

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Only explicit video kinds and standard Torznab category ranges establish a
// type. Native numeric category IDs and the word Anime cannot distinguish a
// film from a series. Conflicting declarations cannot corroborate a type.
func VideoTypeEvidence(fields map[string]any) string {
	var established string
	add := func(kind string) {
		if kind != "" && established != kind {
			if established == "" {
				established = kind
			} else {
				established = "conflict"
			}
		}
	}
	var category func(any)
	category = func(value any) {
		switch v := value.(type) {
		case []int:
			for _, item := range v {
				category(strconv.Itoa(item))
			}
		case int:
			category(strconv.Itoa(v))
		case []any:
			for _, item := range v {
				category(item)
			}
		case []string:
			for _, item := range v {
				category(item)
			}
		case json.Number:
			category(string(v))
		case string:
			for _, item := range strings.Split(v, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(item))
				if err == nil && n >= 2000 && n < 3000 {
					add("movie")
				}
				if err == nil && n >= 5000 && n < 6000 {
					add("tv")
				}
			}
		}
	}
	for _, key := range []string{"type", "media_type"} {
		if value, ok := fields[key].(string); ok {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "movie", "film":
				add("movie")
			case "tv", "series", "tvshow":
				add("tv")
			}
		}
	}
	category(fields["categories"])
	if attributes, ok := fields["attributes"].(map[string]any); ok {
		category(attributes["category"])
		category(attributes["categories"])
	}
	if attributes, ok := fields["attributes"].(map[string][]string); ok {
		category(attributes["category"])
		category(attributes["categories"])
	}
	return established
}
