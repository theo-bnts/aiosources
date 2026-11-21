package model

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// SoftwareInformation stores compact canonical software catalog metadata
// aggregated from NIST NVD CPE, Microsoft WinGet, Homebrew Cask and optional
// selfh.st/icons references. Remote image bytes are never downloaded or stored.
type SoftwareInformation struct {
	Vendor        string   `json:"vendor,omitempty"`
	Description   string   `json:"description,omitempty"`
	License       string   `json:"license,omitempty"`
	LatestVersion string   `json:"latest_version,omitempty"`
	Part          string   `json:"part,omitempty"` // "a" (application) or "o" (operating system)
	Platforms     []string `json:"platforms,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Languages     []string `json:"languages,omitempty"`
	NVDCPE        string   `json:"nvd_cpe,omitempty"`
	WinGetID      string   `json:"winget_id,omitempty"`
	HomebrewCask  string   `json:"homebrew_cask,omitempty"`
	IconRef       string   `json:"icon_ref,omitempty"`
	HasIcon       bool     `json:"has_icon,omitempty"`
}

// SoftwareHints captures software-specific identifiers and source attributes
// from an imported torrent record without retaining raw source payloads.
type SoftwareHints struct {
	Kind          string   `json:"kind,omitempty"`
	SoftwareKey   string   `json:"software_key,omitempty"`
	WinGetID      string   `json:"winget_id,omitempty"`
	HomebrewCask  string   `json:"homebrew_cask,omitempty"`
	NVDCPE        string   `json:"nvd_cpe,omitempty"`
	Vendor        string   `json:"vendor,omitempty"`
	Version       string   `json:"version,omitempty"`
	Platforms     []string `json:"platforms,omitempty"`
	Architecture  string   `json:"architecture,omitempty"`
	Categories    []int    `json:"categories,omitempty"`
	CategoryNames []string `json:"category_names,omitempty"`
}

// SoftwareStatus reports local Software catalog synchronization progress across
// NVD CPE, Microsoft WinGet, Homebrew Cask and selfh.st/icons.
type SoftwareStatus struct {
	Selection         string     `json:"-"`
	Generation        int64      `json:"-"`
	Running           bool       `json:"running"`
	Complete          bool       `json:"complete"`
	Works             int64      `json:"works"`
	NVDEntries        int64      `json:"nvd_entries"`
	WinGetEntries     int64      `json:"winget_entries"`
	HomebrewEntries   int64      `json:"homebrew_entries"`
	Icons             int64      `json:"icons"`
	IconsIndexed      int64      `json:"icons_indexed"`
	Imported          int64      `json:"imported"`
	Position          int64      `json:"position"`
	Filtered          int64      `json:"filtered"`
	Skipped           int64      `json:"skipped"`
	Phase             string     `json:"phase"`
	NVDSnapshot       string     `json:"nvd_snapshot,omitempty"`
	NVDSnapshotSHA256 string     `json:"-"`
	NVDSnapshotAt     *time.Time `json:"nvd_snapshot_at"`
	NVDChangesThrough *time.Time `json:"nvd_changes_through"`
	WinGetETag        string     `json:"-"`
	WinGetSyncedAt    *time.Time `json:"winget_synced_at"`
	HomebrewETag      string     `json:"-"`
	HomebrewSyncedAt  *time.Time `json:"homebrew_synced_at"`
	IconsETag         string     `json:"-"`
	IconsSyncedAt     *time.Time `json:"icons_synced_at"`
	LastImport        *time.Time `json:"last_import"`
	LastSync          *time.Time `json:"last_sync"`
	NextUpdate        *time.Time `json:"next_update"`
	RetryAt           *time.Time `json:"retry_at"`
	NVDKeyConfigured  bool       `json:"nvd_key_configured"`
	NeedsFullImport   bool       `json:"needs_full_import"`
	RecoveryReason    string     `json:"recovery_reason,omitempty"`
	Error             string     `json:"error,omitempty"`
}

// SoftwareSourceHints reads only software metadata fields understood by the
// matcher and identification review UI.
func SoftwareSourceHints(fields map[string]any) SoftwareHints {
	b := BookSourceHints(fields)
	h := SoftwareHints{
		Categories:    b.Categories,
		CategoryNames: b.CategoryNames,
	}
	text := func(v any) string {
		switch value := v.(type) {
		case string:
			return strings.TrimSpace(value)
		case int:
			return strconv.Itoa(value)
		case int64:
			return strconv.FormatInt(value, 10)
		case float64:
			return strconv.FormatFloat(value, 'f', -1, 64)
		case []string:
			if len(value) > 0 {
				return strings.TrimSpace(value[0])
			}
		case []any:
			if len(value) > 0 {
				if s, ok := value[0].(string); ok {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}
	attrs := storageObject(fields["attributes"])
	ext := storageObject(fields["external_ids"])
	first := func(keys ...string) string {
		for _, key := range keys {
			if s := text(fields[key]); s != "" && len(s) <= 500 {
				return s
			}
		}
		if ext != nil {
			for _, key := range keys {
				if s := text(ext[key]); s != "" && len(s) <= 500 {
					return s
				}
			}
		}
		if attrs != nil {
			for _, key := range keys {
				if s := text(attrs[key]); s != "" && len(s) <= 500 {
					return s
				}
			}
		}
		return ""
	}

	h.SoftwareKey = first("software_key")
	h.WinGetID = first("winget_id", "winget")
	h.HomebrewCask = first("homebrew_cask", "brew_cask", "cask")
	h.NVDCPE = first("nvd_cpe", "cpe")
	h.Vendor = first("vendor", "publisher")
	h.Version = first("version", "software_version")
	h.Architecture = first("architecture", "arch")

	addPlatforms := func(v any) {
		if len(h.Platforms) >= 16 {
			return
		}
		switch value := v.(type) {
		case string:
			for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '/' || r == ';' }) {
				part = strings.TrimSpace(part)
				if part != "" && len(part) <= 64 && !slices.Contains(h.Platforms, part) && len(h.Platforms) < 16 {
					h.Platforms = append(h.Platforms, part)
				}
			}
		case []string:
			for _, item := range value[:min(16, len(value))] {
				item = strings.TrimSpace(item)
				if item != "" && len(item) <= 64 && !slices.Contains(h.Platforms, item) && len(h.Platforms) < 16 {
					h.Platforms = append(h.Platforms, item)
				}
			}
		case []any:
			for _, raw := range value[:min(16, len(value))] {
				if item, ok := raw.(string); ok {
					item = strings.TrimSpace(item)
					if item != "" && len(item) <= 64 && !slices.Contains(h.Platforms, item) && len(h.Platforms) < 16 {
						h.Platforms = append(h.Platforms, item)
					}
				}
			}
		}
	}
	for _, key := range []string{"os", "platform", "platforms"} {
		addPlatforms(fields[key])
		if attrs != nil {
			addPlatforms(attrs[key])
		}
	}

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
	if attrs != nil {
		names(attrs["category"])
		names(attrs["categories"])
		extra := BookSourceHints(map[string]any{"category": attrs["category"], "categories": attrs["categories"]})
		h.Categories = append(h.Categories, extra.Categories...)
	}

	hasExcluded := false
	for _, c := range h.Categories {
		switch c {
		case 4050:
			// PC-Games belongs strictly to Games, never Software.
			hasExcluded = true
		case 4070:
			h.Kind = "android"
		case 4060:
			if h.Kind != "android" {
				h.Kind = "ios"
			}
		case 4040:
			if h.Kind == "" || h.Kind == "pc" {
				h.Kind = "mobile"
			}
		case 4030:
			if h.Kind == "" || h.Kind == "pc" {
				h.Kind = "mac"
			}
		case 4020:
			if h.Kind == "" || h.Kind == "pc" {
				h.Kind = "iso"
			}
		case 4010:
			if h.Kind == "" || h.Kind == "pc" {
				h.Kind = "0day"
			}
		case 4000:
			if h.Kind == "" {
				h.Kind = "pc"
			}
		default:
			if (c >= 1000 && c < 2000) || (c >= 2000 && c < 3000) || (c >= 3000 && c < 4000) || (c >= 5000 && c < 6000) || (c >= 7000 && c < 8000) {
				hasExcluded = true
			}
		}
	}
	if !hasExcluded && h.Kind == "" {
		switch strings.ToLower(first("type", "media_type")) {
		case "software", "app", "application", "apps", "pc":
			h.Kind = "pc"
		case "0day":
			h.Kind = "0day"
		case "iso":
			h.Kind = "iso"
		case "mac", "macos", "osx":
			h.Kind = "mac"
		case "mobile":
			h.Kind = "mobile"
		case "ios":
			h.Kind = "ios"
		case "android", "apk":
			h.Kind = "android"
		}
	}
	if hasExcluded {
		h.Kind = ""
	}
	return h
}
