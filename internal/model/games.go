package model

import (
	"strconv"
	"strings"
	"time"
)

// Provider IDs retain their own namespace. Game releases never supply video
// numbering or borrow a film identifier, even when their titles are identical.
type GamePlatform struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Abbreviation string `json:"abbreviation,omitempty"`
}
type GameInformation struct {
	Edition   string         `json:"edition,omitempty"`
	Platforms []GamePlatform `json:"platforms,omitempty"`
	Kind      string         `json:"kind,omitempty"`
	Parent    string         `json:"parent,omitempty"`
}
type GameHints struct {
	Kind          string
	IGDB          string
	Platforms     []string
	Categories    []int
	CategoryNames []string
}
type GameStatus struct {
	Pending            bool       `json:"pending"`
	Running            bool       `json:"running"`
	Complete           bool       `json:"complete"`
	Works              int64      `json:"works"`
	Processed          int64      `json:"processed"`
	Total              int64      `json:"total"`
	Phase              string     `json:"phase"`
	LastSync           *time.Time `json:"last_sync"`
	ChangesThrough     *time.Time `json:"changes_through"`
	LastReconciliation *time.Time `json:"last_reconciliation"`
	NextUpdate         *time.Time `json:"next_update"`
	RetryAt            *time.Time `json:"retry_at"`
	Error              string     `json:"error,omitempty"`
	After              int64      `json:"-"`
	StartedAt          *time.Time `json:"-"`
	Generation         int64      `json:"-"`
}

func GameSourceHints(fields map[string]any) GameHints {
	b := BookSourceHints(fields)
	h := GameHints{Kind: b.Kind, Categories: b.Categories, CategoryNames: b.CategoryNames}
	var names func(any)
	names = func(value any) {
		if len(h.CategoryNames) >= 64 {
			return
		}
		switch v := value.(type) {
		case string:
			if _, err := strconv.Atoi(strings.TrimSpace(v)); err != nil && len(v) <= 200 {
				h.CategoryNames = append(h.CategoryNames, v)
			}
		case []any:
			for _, item := range v[:min(64, len(v))] {
				names(item)
			}
		case []string:
			for _, item := range v[:min(64, len(v))] {
				names(item)
			}
		}
	}
	names(fields["category"])
	names(fields["categories"])
	if attrs := storageObject(fields["attributes"]); attrs != nil {
		names(attrs["category"])
		names(attrs["categories"])
		extra := BookSourceHints(map[string]any{"category": attrs["category"], "categories": attrs["categories"]})
		h.Categories = append(h.Categories, extra.Categories...)
	}
	var appendPlatform func(any)
	appendPlatform = func(value any) {
		if len(h.Platforms) >= 32 {
			return
		}
		switch value := value.(type) {
		case string:
			if len(value) <= 160 && len(h.Platforms) < 32 {
				h.Platforms = append(h.Platforms, value)
			}
		case []any:
			for _, v := range value[:min(32, len(value))] {
				appendPlatform(v)
			}
		case []string:
			for _, v := range value[:min(32, len(value))] {
				appendPlatform(v)
			}
		}
	}
	for _, key := range []string{"platform", "platforms", "game_platform"} {
		appendPlatform(fields[key])
		if attrs := storageObject(fields["attributes"]); attrs != nil {
			appendPlatform(attrs[key])
		}
	}
	return h
}
