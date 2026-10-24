package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Details use a separate capability header: older peers reject unknown entity
// names, but safely ignore this header and continue serving basic torrents.
func (e CatalogEntities) DetailsHeader() string {
	var names []string
	for _, name := range []string{"identifiers", "languages", "technical", "files"} {
		if e.detailPermission(name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

func ParseCatalogDetails(value string) (CatalogEntities, bool) {
	var result CatalogEntities
	if len(value) > 64 {
		return result, false
	}
	if value == "" {
		return result, true
	}
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if result.detailPermission(name) {
			return result, false
		}
		switch name {
		case "identifiers":
			result.Identifiers = true
		case "languages":
			result.Languages = true
		case "technical":
			result.Technical = true
		case "files":
			result.Files = true
		default:
			return result, false
		}
	}
	return result, true
}

func (e CatalogEntities) detailPermission(group string) bool {
	switch group {
	case "identifiers":
		return e.Identifiers
	case "languages":
		return e.Languages
	case "technical":
		return e.Technical
	case "files":
		return e.Files
	}
	return false
}

// Closed vocabulary of source facts. In particular, attributes and metadata are
// not copied wholesale. No URLs, tracker policy, account IDs, library entries,
// raw payloads, source definitions or local match decisions belong here.
var catalogDetailGroups = map[string]string{
	"imdb": "identifiers", "imdb_id": "identifiers", "imdbid": "identifiers", "tt_id": "identifiers",
	"tmdb": "identifiers", "tmdb_id": "identifiers", "tmdbid": "identifiers",
	"tvdb": "identifiers", "tvdb_id": "identifiers", "tvdbid": "identifiers",
	"mal": "identifiers", "mal_id": "identifiers", "malid": "identifiers", "igdb_id": "identifiers",
	"category_id": "identifiers", "subcategory_id": "identifiers", "category_name": "identifiers",
	"type": "identifiers", "media_type": "identifiers", "year": "identifiers",
	"season": "identifiers", "seasons": "identifiers", "episode": "identifiers", "episodes": "identifiers",
	"episode_start": "identifiers", "episode_end": "identifiers",
	"audiolangs": "languages", "languages": "languages", "language": "languages", "sublangs": "languages",
	"resolution": "technical", "quality": "technical", "source": "technical", "codec": "technical",
	"video_codec": "technical", "videocodec": "technical", "audio_codec": "technical", "audiocodec": "technical",
	"audio_channels": "technical", "audiochannels": "technical", "hdr": "technical", "bitrate": "technical",
	"duration": "technical", "release_group": "technical", "release_type": "technical", "edition": "technical",
	"proper": "technical", "repack": "technical",
	"filename": "files", "file_name": "files", "file_size": "files", "file_index": "files",
	"folder_name": "files", "folder_size": "files",
}

const catalogConflicting = "invalid conflicting values"

var catalogEvidence = regexp.MustCompile(`(?i)^(?:tt)?[0-9]{1,16}$`)
var catalogSecretText = regexp.MustCompile(`(?i)(?:bearer\s|(?:api[_-]?key|passkey|password|token|secret|signature|auth)=|[a-f0-9]{32,}|eyJ[A-Za-z0-9_-]+\.)`)

func safeDetailText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.Contains(value, "://") &&
		!strings.Contains(strings.ToLower(value), "magnet:") && !strings.ContainsAny(value, "?#") &&
		!strings.ContainsFunc(value, unicode.IsControl) && !catalogSecretText.MatchString(value)
}

func catalogEvidenceKey(key string) bool {
	return catalogDetailGroups[key] == "identifiers" && key != "category_name" && key != "type" && key != "media_type"
}

func safeDetailValue(key string, value any) any {
	return safeDetailValueAt(key, value, 0)
}

func safeDetailValueAt(key string, value any, depth int) any {
	if depth > 8 {
		if catalogEvidenceKey(key) {
			return catalogConflicting
		}
		return map[string]any{}
	}
	if list, ok := value.([]any); ok {
		if len(list) > 16 {
			if catalogEvidenceKey(key) {
				return catalogConflicting
			}
			list = list[:16]
		}
		result := make([]any, 0, len(list))
		for _, member := range list {
			// Preserve an uninterpretable member rather than silently removing a
			// conflict and making the remaining ID appear more authoritative.
			result = append(result, safeDetailValueAt(key, member, depth+1))
		}
		return result
	}
	switch v := value.(type) {
	case nil, bool:
		return v
	case json.Number:
		if len(v) <= 64 {
			return v
		}
		if catalogEvidenceKey(key) {
			return catalogConflicting
		}
		return map[string]any{}
	case string:
		if catalogEvidenceKey(key) {
			if v == "" || v == "null" || catalogEvidence.MatchString(v) || v == catalogConflicting {
				return v
			}
			return catalogConflicting
		}
		maximum := 128
		if catalogDetailGroups[key] == "files" {
			maximum = 1024
			if strings.HasPrefix(v, "/") || strings.Contains(v, "\\") || slices.Contains(strings.Split(v, "/"), "..") {
				return ""
			}
		}
		if safeDetailText(v, maximum) {
			return v
		}
		return ""
	default:
		// An object cannot be interpreted as a scalar ID by the local matcher.
		return map[string]any{}
	}
}

func projectCatalogDetails(source map[string]any, permissions CatalogEntities, depth int) map[string]any {
	result := map[string]any{}
	for key, value := range source {
		if group := catalogDetailGroups[key]; permissions.detailPermission(group) {
			result[key] = safeDetailValue(key, value)
		} else if depth == 0 && (key == "attributes" || key == "external_ids" || key == "metadata") {
			if nested, ok := value.(map[string]any); ok {
				if fields := projectCatalogDetails(nested, permissions, depth+1); len(fields) > 0 {
					result[key] = fields
				}
			}
		} else if key == "subtitles" && permissions.Languages {
			if list, ok := value.([]any); ok {
				languages := []any{}
				for _, item := range list[:min(len(list), 16)] {
					if subtitle, ok := item.(map[string]any); ok {
						language, _ := subtitle["language"].(string)
						if language == "" {
							language, _ = subtitle["lang"].(string)
						}
						if language != "" && safeDetailText(language, 64) {
							languages = append(languages, map[string]any{"language": language})
						}
					}
				}
				result[key] = languages
			}
		}
	}
	return result
}

// Decode with UseNumber before calling: numeric source evidence must not pass
// through float64. This projection is also used for receiver validation.
func ProjectCatalogDetails(source map[string]any, permissions CatalogEntities) map[string]any {
	result := projectCatalogDetails(source, permissions, 0)
	// Keep bounded pages even with pathological arrays in free-text fields.
	data, _ := json.Marshal(result)
	if len(data) > 32<<10 {
		result = projectCatalogDetails(source, permissions.Intersect(CatalogEntities{Identifiers: true}), 0)
		data, _ = json.Marshal(result)
		if len(data) > 32<<10 {
			compactCatalogEvidence(result)
		}
	}
	return result
}

func compactCatalogEvidence(fields map[string]any) {
	for key, value := range fields {
		switch value := value.(type) {
		case []any:
			fields[key] = catalogConflicting
		case map[string]any:
			compactCatalogEvidence(value)
		}
	}
}

func ValidCatalogDetails(fields map[string]any, permissions CatalogEntities) bool {
	actual, err := json.Marshal(fields)
	if err != nil || len(actual) > 32<<10 {
		return false
	}
	expected, err := json.Marshal(ProjectCatalogDetails(fields, permissions))
	return err == nil && (len(fields) == 0 || bytes.Equal(actual, expected))
}

// A detail-only delta belongs to one immutable version of the approved basic
// fields. It cannot silently enrich a different title/hash or resurrect a row.
type CatalogTorrentDetails struct {
	ID              string         `json:"id"`
	Origin          CatalogOrigin  `json:"origin"`
	BaseFingerprint string         `json:"base_fingerprint"`
	Fields          map[string]any `json:"fields"`
}

func CatalogFieldsFingerprint(fields map[string]any) string {
	data, _ := json.Marshal(fields)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (d CatalogTorrentDetails) Valid(permissions CatalogEntities) bool {
	return permissions.HasTorrentDetails() && d.ID == CatalogItemID(d.Origin) &&
		catalogLabel(d.Origin.InstanceID, 1024) && catalogLabel(d.Origin.ProviderID, 1024) && catalogLabel(d.Origin.SourceID, 1024) &&
		len(d.BaseFingerprint) == 64 && strings.Trim(d.BaseFingerprint, "0123456789abcdef") == "" && d.Fields != nil &&
		ValidCatalogDetails(d.Fields, permissions)
}

// Replace only the known optional vocabulary. Unknown/local fields and the six
// separately authorized basic fields are never modified by a detail delta.
func ReplaceCatalogDetails(fields, details map[string]any) map[string]any {
	result := make(map[string]any, len(fields)+len(details))
	for key, value := range fields {
		if _, detail := catalogDetailGroups[key]; !detail && key != "attributes" && key != "external_ids" && key != "metadata" && key != "subtitles" {
			result[key] = value
		}
	}
	for key, value := range details {
		result[key] = value
	}
	return result
}
