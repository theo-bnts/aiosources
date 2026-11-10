package model

import (
	"slices"
	"strings"
	"unicode"
)

type StorageMode string

const (
	StorageCompact StorageMode = "compact"
	StorageFull    StorageMode = "full"
)

func (mode StorageMode) Valid() bool { return mode == StorageCompact || mode == StorageFull }

type StorageSettings struct {
	Mode     StorageMode     `json:"mode"`
	Revision int64           `json:"revision"`
	Options  *StorageOptions `json:"options,omitempty"`
}

type StorageOptions struct {
	AllFields          bool     `json:"all_fields"`
	AdditionalFields   []string `json:"additional_fields"`
	SuccessfulRecords  bool     `json:"successful_records"`
	RecordFields       bool     `json:"record_fields"`
	PublicationHistory bool     `json:"publication_history"`
}

func (o StorageOptions) Valid() bool {
	if len(o.AdditionalFields) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, field := range o.AdditionalFields {
		if field == "" || len(field) > 256 || strings.TrimSpace(field) != field || seen[field] {
			return false
		}
		for _, r := range field {
			if unicode.IsControl(r) {
				return false
			}
		}
		seen[field] = true
	}
	return true
}

func LegacyStorageOptions(mode StorageMode) StorageOptions {
	return StorageOptions{AllFields: mode == StorageFull, SuccessfulRecords: mode == StorageFull, PublicationHistory: true, AdditionalFields: []string{}}
}

func (s StorageSettings) EffectiveOptions() StorageOptions {
	if s.Options != nil {
		return *s.Options
	}
	return LegacyStorageOptions(s.Mode)
}

func (r Run) EffectiveStorageOptions() StorageOptions {
	if r.StorageOptions != nil {
		return *r.StorageOptions
	}
	return LegacyStorageOptions(r.StorageMode)
}

func (r Run) StoredFields(fields map[string]any) map[string]any {
	fields = WithoutSwarmCounts(fields)
	opts := r.EffectiveStorageOptions()
	if opts.AllFields {
		return StoredTorrentFields(fields, StorageFull)
	}
	result := StoredTorrentFields(fields, StorageCompact)
	for _, key := range opts.AdditionalFields {
		if value, ok := fields[key]; ok && !CompactTorrentField(key) {
			result[key] = value
		} else if strings.Contains(key, ".") {
			retainOptionalField(result, fields, strings.Split(key, "."))
		}
	}
	return result
}

func storageObject(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case map[string][]string:
		result := map[string]any{}
		for k, item := range v {
			result[k] = item
		}
		return result
	case map[string]string:
		result := map[string]any{}
		for k, item := range v {
			result[k] = item
		}
		return result
	}
	return nil
}
func retainOptionalField(target, source map[string]any, path []string) {
	if target == nil || source == nil || len(path) == 0 {
		return
	}
	value, ok := source[path[0]]
	if !ok {
		return
	}
	if len(path) == 1 {
		target[path[0]] = value
		return
	}
	child := storageObject(value)
	if child == nil {
		return
	}
	copy := map[string]any{}
	for k, v := range storageObject(target[path[0]]) {
		copy[k] = v
	}
	retainOptionalField(copy, child, path[1:])
	if len(copy) > 0 {
		target[path[0]] = copy
	}
}

func RequiredTorrentField(path string) bool {
	root, field, nested := strings.Cut(path, ".")
	if !nested {
		return CompactTorrentField(root)
	}
	switch root {
	case "attributes":
		return slices.Contains(compactAttributeKeys, field)
	case "external_ids":
		return slices.Contains(compactIdentifierKeys, field)
	case "metadata":
		return RequiredTorrentField(field)
	}
	return false
}

type StorageIndexStatus struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Ready   bool   `json:"ready"`
	Bytes   int64  `json:"bytes"`
	Error   string `json:"error,omitempty"`
}

type StorageIndexSettings struct {
	Revision int64                `json:"revision"`
	Values   map[string]bool      `json:"values"`
	Status   []StorageIndexStatus `json:"status,omitempty"`
}

type StorageRetentionSettings struct {
	Revision       int64          `json:"revision"`
	Days           map[string]int `json:"days"`
	RequestLimit   int            `json:"request_limit"`
	RequestResults bool           `json:"request_results"`
}

var StorageRetentionGroups = []string{"diagnostics", "changes", "logs", "operations", "requests", "sharing"}

func (s StorageRetentionSettings) Valid() bool {
	if s.Revision < 1 || len(s.Days) != len(StorageRetentionGroups) || s.RequestLimit < 0 || s.RequestLimit > 1000000 {
		return false
	}
	for _, key := range StorageRetentionGroups {
		days, ok := s.Days[key]
		if !ok || days < -1 || days > 3650 {
			return false
		}
	}
	return true
}

type StorageOverview struct {
	Settings        StorageSettings `json:"settings"`
	Running         int             `json:"running"`
	AvailableFields []string        `json:"available_fields"`
	Indexing        bool            `json:"indexing"`
}

// Compact storage is a projection, never normalization. In particular, retain
// conflicting identifier evidence and exact numbers so matching cannot become
// more confident merely because storage saving is enabled.
var compactTorrentKeys = []string{
	"title", "name", "info_hash", "infohash", "hash", "size", "published_at", "duration",
	"isbn", "isbn_10", "isbn_13", "openlibrary", "openlibrary_id", "author", "authors", "book_title", "book_format", "category_name", "subcategory_name",
	"category", "categories", "category_id", "subcategory_id", "type", "media_type", "year",
	"imdb", "imdb_id", "tt_id", "tmdb", "tmdb_id", "tvdb", "tvdb_id", "mal", "mal_id", "igdb", "igdb_id", "platform", "platforms", "game_platform",
	"season", "seasons", "episode", "episodes", "episode_start", "episode_end",
	"audiolangs", "languages", "language", "sublangs", "subtitles",
	"resolution", "quality", "source", "codec", "video_codec", "audio_codec", "audio_channels",
	"hdr", "bitrate", "release_group", "release_type", "edition", "proper", "repack",
	"filename", "file_name", "file_index", "file_size", "folder_name", "folder_size",
	"private", "external_ids", "attributes", "metadata",
}

var compactIdentifierKeys = []string{"isbn", "isbn_10", "isbn_13", "openlibrary", "openlibrary_id", "imdb", "imdb_id", "tmdb", "tmdb_id", "tvdb", "tvdb_id", "mal", "mal_id", "igdb", "igdb_id"}

var compactAttributeKeys = []string{
	"isbn", "isbn10", "isbn13", "isbn_10", "isbn_13", "openlibrary", "openlibrary_id", "author", "authors", "booktitle", "book_title", "book_format",
	"imdb", "imdbid", "tmdb", "tmdbid", "tvdb", "tvdbid", "mal", "malid", "igdb", "igdbid", "igdb_id", "platform", "platforms", "game_platform",
	"season", "seasons", "episode", "episodes", "audiolangs", "languages", "language", "sublangs",
	"category", "categories", "size", "infohash",
	"resolution", "quality", "source", "codec", "videocodec", "audiocodec", "audiochannels", "hdr",
	"year", "duration", "private", "bitrate", "release_group", "edition", "proper", "repack",
	"video_codec", "audio_codec", "audio_channels", "subtitles",
}

func CompactTorrentField(name string) bool { return slices.Contains(compactTorrentKeys, name) }

func CompactTorrentFields() []string { return slices.Clone(compactTorrentKeys) }

func compactObject(value any, keys []string) any {
	switch object := value.(type) {
	case map[string]any:
		if object == nil {
			return value
		}
		result := make(map[string]any)
		for key, field := range object {
			if slices.Contains(keys, key) {
				result[key] = field
			}
		}
		return result
	case map[string][]string:
		if object == nil {
			return value
		}
		// Torznab attributes use typed string slices before JSON persistence.
		result := make(map[string][]string)
		for key, field := range object {
			if slices.Contains(keys, key) {
				result[key] = field
			}
		}
		return result
	case map[string]string:
		if object == nil {
			return value
		}
		result := make(map[string]string)
		for key, field := range object {
			if slices.Contains(keys, key) {
				result[key] = field
			}
		}
		return result
	}
	// Non-object values are not silently repaired: invalid/conflicting evidence
	// must retain its original meaning for consumers and diagnostics.
	return value
}

func StoredTorrentFields(fields map[string]any, mode StorageMode) map[string]any {
	fields = WithoutSwarmCounts(fields)
	if mode != StorageCompact || fields == nil {
		return fields
	}
	return compactTorrentFields(fields)
}

func compactTorrentFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	result := make(map[string]any)
	for key, value := range fields {
		if !CompactTorrentField(key) {
			continue
		}
		switch key {
		case "external_ids":
			value = compactObject(value, compactIdentifierKeys)
		case "attributes":
			value = compactObject(value, compactAttributeKeys)
		case "metadata":
			// Source metadata can nest the same identifier/attribute objects.
			// Apply their projection at every level instead of retaining unused
			// detail payloads merely because they sit inside a known container.
			if object, ok := value.(map[string]any); ok {
				value = compactTorrentFields(object)
			} else {
				value = compactObject(value, compactTorrentKeys)
			}
		}
		result[key] = value
	}
	return result
}

func (r Run) MetadataFields() []string {
	fields := r.Config.MetadataFields()
	opts := r.EffectiveStorageOptions()
	if opts.AllFields {
		return fields
	}
	return slices.DeleteFunc(slices.Clone(fields), func(field string) bool {
		return !CompactTorrentField(field) && !slices.Contains(opts.AdditionalFields, field)
	})
}

// The saved definition stays intact. Detail-only jobs must not request fields
// that compact storage intentionally discards and then retry them forever.
func (r Run) ConnectorConfig() Provider {
	config := r.Config
	if config.Traversal != nil && !r.EffectiveStorageOptions().AllFields {
		traversal := *config.Traversal
		traversal.EnrichFields = r.MetadataFields()
		if len(traversal.EnrichFields) == 0 {
			traversal.MetadataAfterIncremental = false
		}
		config.Traversal = &traversal
	}
	return config
}
