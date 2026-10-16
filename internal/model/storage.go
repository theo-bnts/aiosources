package model

import "slices"

type StorageMode string

const (
	StorageCompact StorageMode = "compact"
	StorageFull    StorageMode = "full"
)

func (mode StorageMode) Valid() bool { return mode == StorageCompact || mode == StorageFull }

type StorageSettings struct {
	Mode     StorageMode `json:"mode"`
	Revision int64       `json:"revision"`
}

type StorageOverview struct {
	Settings StorageSettings `json:"settings"`
	Running  int             `json:"running"`
}

// Compact storage is a projection, never normalization. In particular, retain
// conflicting identifier evidence and exact numbers so matching cannot become
// more confident merely because storage saving is enabled.
var compactTorrentKeys = []string{
	"title", "name", "info_hash", "infohash", "hash", "size", "published_at", "duration",
	"category", "categories", "category_id", "subcategory_id", "type", "media_type", "year",
	"imdb", "imdb_id", "tt_id", "tmdb", "tmdb_id", "tvdb", "tvdb_id", "mal", "mal_id",
	"season", "seasons", "episode", "episodes", "episode_start", "episode_end",
	"audiolangs", "languages", "language", "sublangs", "subtitles",
	"resolution", "quality", "source", "codec", "video_codec", "audio_codec", "audio_channels",
	"hdr", "bitrate", "release_group", "release_type", "edition", "proper", "repack",
	"filename", "file_name", "file_index", "file_size", "folder_name", "folder_size",
	"private", "external_ids", "attributes", "metadata",
}

var compactIdentifierKeys = []string{"imdb", "imdb_id", "tmdb", "tmdb_id", "tvdb", "tvdb_id", "mal", "mal_id"}

var compactAttributeKeys = []string{
	"imdb", "imdbid", "tmdb", "tmdbid", "tvdb", "tvdbid", "mal", "malid",
	"season", "seasons", "episode", "episodes", "audiolangs", "languages", "language", "sublangs",
	"category", "categories", "size", "infohash",
	"resolution", "quality", "source", "codec", "videocodec", "audiocodec", "audiochannels", "hdr",
	"year", "duration", "private", "bitrate", "release_group", "edition", "proper", "repack",
	"video_codec", "audio_codec", "audio_channels", "subtitles",
}

func CompactTorrentField(name string) bool { return slices.Contains(compactTorrentKeys, name) }

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
	if r.StorageMode != StorageCompact {
		return fields
	}
	return slices.DeleteFunc(slices.Clone(fields), func(field string) bool { return !CompactTorrentField(field) })
}

// The saved definition stays intact. Detail-only jobs must not request fields
// that compact storage intentionally discards and then retry them forever.
func (r Run) ConnectorConfig() Provider {
	config := r.Config
	if config.Traversal != nil && r.StorageMode == StorageCompact {
		traversal := *config.Traversal
		traversal.EnrichFields = r.MetadataFields()
		if len(traversal.EnrichFields) == 0 {
			traversal.MetadataAfterIncremental = false
		}
		config.Traversal = &traversal
	}
	return config
}
