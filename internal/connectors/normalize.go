package connectors

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
)

func OptionString(p model.Provider, name, fallback string) (string, error) {
	value, exists := p.Options[name]
	if !exists {
		return fallback, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("option %s must be a string", name)
	}
	return text, nil
}

func publishedAtUnit(p model.Provider) (string, error) {
	if _, exists := p.Options["published_at_unit"]; !exists {
		return "", nil
	}
	unit, err := OptionString(p, "published_at_unit", "")
	if err != nil {
		return "", err
	}
	if unit != "seconds" && unit != "milliseconds" {
		return "", errors.New("published_at_unit must be seconds or milliseconds")
	}
	return unit, nil
}

// localCategories is separate from remote search categories: some APIs expose
// only an unfiltered feed. Excluded observations still belong in the archive.
func localCategories(p model.Provider) (map[int64]struct{}, error) {
	value, exists := p.Options["local_categories"]
	if !exists {
		return nil, nil
	}
	result := make(map[int64]struct{})
	add := func(value any) error {
		category, err := integerValue(value)
		if err != nil || category < 0 {
			return errors.New("local_categories must contain nonnegative integer IDs")
		}
		result[category] = struct{}{}
		return nil
	}
	switch values := value.(type) {
	case []any:
		for _, value := range values {
			if err := add(value); err != nil {
				return nil, err
			}
		}
	case []int:
		for _, value := range values {
			if err := add(value); err != nil {
				return nil, err
			}
		}
	default:
		return nil, errors.New("local_categories must be an array of integer IDs")
	}
	if len(result) == 0 {
		return nil, errors.New("local_categories must not be empty")
	}
	return result, nil
}

func matchesLocalCategories(allowed map[int64]struct{}, fields map[string]any) bool {
	if len(allowed) == 0 {
		return true
	}
	switch categories := fields["categories"].(type) {
	case []int64:
		for _, category := range categories {
			if _, found := allowed[category]; found {
				return true
			}
		}
	case []int:
		for _, category := range categories {
			if _, found := allowed[int64(category)]; found {
				return true
			}
		}
	}
	return false
}

// FieldString accepts scalar identifiers only; objects and floating-point IDs
// must not accidentally become unstable, formatted source identities.
func FieldString(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		text := string(value)
		if len(text) > 0 && (text[0] == '-' || (text[0] >= '0' && text[0] <= '9')) && json.Valid([]byte(text)) && !strings.ContainsAny(text, ".eE") {
			return text
		}
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float64:
		if !math.IsNaN(value) && !math.IsInf(value, 0) && math.Trunc(value) == value && math.Abs(value) <= 9007199254740991 {
			return strconv.FormatFloat(value, 'f', 0, 64)
		}
	}
	return ""
}

func integerValue(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case int32:
		return int64(v), nil
	case uint64:
		if v <= math.MaxInt64 {
			return int64(v), nil
		}
	case json.Number:
		return v.Int64()
	case string:
		if strings.TrimSpace(v) != "" {
			return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v == math.Trunc(v) && v >= math.MinInt64 && v < 9223372036854775808 {
			return int64(v), nil
		}
	}
	return 0, errors.New("value must be an integer")
}

func decodeJSON(raw []byte, destination any) error {
	if !utf8.Valid(raw) {
		return errors.New("JSON response must contain valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid JSON response")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("JSON response must contain one value")
	}
	return nil
}

func pointerTokens(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, errors.New("mapping paths must be RFC 6901 JSON Pointers")
	}
	tokens := strings.Split(pointer[1:], "/")
	for i, token := range tokens {
		for j := 0; j < len(token); j++ {
			if token[j] == '~' {
				j++
				if j == len(token) || (token[j] != '0' && token[j] != '1') {
					return nil, errors.New("invalid JSON Pointer escape")
				}
			}
		}
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
	}
	return tokens, nil
}

func pointerValue(root any, pointer string) (any, bool, error) {
	tokens, err := pointerTokens(pointer)
	if err != nil {
		return nil, false, err
	}
	value := root
	for _, token := range tokens {
		switch node := value.(type) {
		case map[string]any:
			var found bool
			value, found = node[token]
			if !found {
				return nil, false, nil
			}
		case []any:
			if token == "" || (len(token) > 1 && token[0] == '0') {
				return nil, false, errors.New("invalid JSON Pointer array index")
			}
			index, parseErr := strconv.ParseUint(token, 10, 63)
			if parseErr != nil {
				return nil, false, errors.New("invalid JSON Pointer array index")
			}
			if index >= uint64(len(node)) {
				return nil, false, nil
			}
			value = node[index]
		default:
			return nil, false, nil
		}
	}
	return value, true, nil
}

// rawPointer descends using RawMessage so the selected object and each item
// retain their original member ordering, whitespace and numeric lexemes.
func rawPointer(raw json.RawMessage, pointer string) (json.RawMessage, bool, error) {
	tokens, err := pointerTokens(pointer)
	if err != nil {
		return nil, false, err
	}
	for _, token := range tokens {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			return nil, false, errors.New("invalid JSON response")
		}
		switch trimmed[0] {
		case '{':
			var node map[string]json.RawMessage
			if err := decodeJSON(raw, &node); err != nil {
				return nil, false, err
			}
			var found bool
			raw, found = node[token]
			if !found {
				return nil, false, nil
			}
		case '[':
			var node []json.RawMessage
			if err := decodeJSON(raw, &node); err != nil {
				return nil, false, err
			}
			if token == "" || (len(token) > 1 && token[0] == '0') {
				return nil, false, errors.New("invalid JSON Pointer array index")
			}
			index, err := strconv.ParseUint(token, 10, 63)
			if err != nil {
				return nil, false, errors.New("invalid JSON Pointer array index")
			}
			if index >= uint64(len(node)) {
				return nil, false, nil
			}
			raw = node[index]
		default:
			return nil, false, nil
		}
	}
	return raw, true, nil
}

func FieldsFromJSON(raw []byte, mapping model.Mapping, publishedAtUnit string) (string, map[string]any, error) {
	var root any
	if err := decodeJSON(raw, &root); err != nil {
		return "", nil, err
	}
	idPath := mapping.ID
	if idPath == "" {
		idPath = "/id"
	}
	idValue, found, err := pointerValue(root, idPath)
	if err != nil {
		return "", nil, err
	}
	id := FieldString(idValue)
	var issues []error
	if !found || id == "" || strings.IndexByte(id, 0) >= 0 || !utf8.ValidString(id) {
		id = ""
		issues = append(issues, errors.New("record has no stable source identity"))
	}
	fields := make(map[string]any, len(mapping.Fields))
	for field, pointer := range mapping.Fields {
		if field == "seeders" || field == "peers" {
			continue
		}
		value, found, err := pointerValue(root, pointer)
		if err != nil {
			issues = append(issues, fmt.Errorf("field %s: invalid mapping path", field))
			continue
		}
		if !found || value == nil {
			continue
		}
		value, err = normalizeField(field, value, publishedAtUnit)
		if err != nil {
			issues = append(issues, fmt.Errorf("field %s: %w", field, err))
			continue
		}
		fields[field] = value
	}
	supplementExternalIDs(fields)
	return id, model.WithoutSwarmCounts(fields), errors.Join(issues...)
}

// Keep the authoritative array (including unknown kinds) intact. Attributes are
// only a compatible projection for output selectors, not a replacement source.
func supplementExternalIDs(fields map[string]any) {
	entries, _ := fields["external_ids"].([]any)
	attributes, _ := fields["attributes"].(map[string]any)
	if fields["attributes"] != nil && attributes == nil {
		return
	}
	for _, entry := range entries {
		external, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		var name string
		switch FieldString(external["kind"]) {
		case "imdb":
			name = "imdbid"
		case "tmdb_movie", "tmdb_tv":
			name = "tmdbid"
		case "tvdb", "tvdb_series", "tvdb_movie":
			name = "tvdbid"
		default:
			continue
		}
		value := FieldString(external["value"])
		if value == "" {
			continue
		}
		if attributes == nil {
			attributes = make(map[string]any)
			fields["attributes"] = attributes
		}
		var values []string
		add := func(value any) {
			if text := FieldString(value); text != "" && !slices.Contains(values, text) {
				values = append(values, text)
			}
		}
		switch existing := attributes[name].(type) {
		case []string:
			values = existing
		case []any:
			for _, item := range existing {
				add(item)
			}
		default:
			add(existing)
		}
		add(value)
		attributes[name] = values
	}
}

func normalizeField(name string, value any, publishedAtUnit string) (any, error) {
	switch name {
	case "size":
		number, err := integerValue(value)
		if err != nil || number < 0 {
			return nil, errors.New("must be a nonnegative integer")
		}
		return number, nil
	case "title":
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("must be a string")
		}
		return text, nil
	case "info_hash":
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("must be a BitTorrent info hash")
		}
		text = strings.TrimSpace(text)
		// Some JSON APIs hex-encode the ASCII SHA-1 rather than its 20 bytes.
		// Decode exactly one such layer; the normal hexadecimal check below
		// still rejects arbitrary decoded text or malformed inner hashes.
		if len(text) == 80 {
			if decoded, err := hex.DecodeString(text); err == nil {
				text = string(decoded)
			}
		}
		if len(text) == 40 || len(text) == 64 {
			if _, err := hex.DecodeString(text); err == nil {
				return strings.ToLower(text), nil
			}
		}
		if len(text) == 32 {
			if decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(text)); err == nil {
				return hex.EncodeToString(decoded), nil
			}
		}
		return nil, errors.New("must be a hexadecimal or base32 BitTorrent info hash")
	case "published_at":
		return normalizePublishedAt(value, publishedAtUnit)
	case "categories":
		var entries []any
		switch v := value.(type) {
		case []any:
			entries = v
		case []int:
			entries = make([]any, len(v))
			for i := range v {
				entries[i] = v[i]
			}
		default:
			entries = []any{value}
		}
		result := make([]int64, 0, len(entries))
		for _, entry := range entries {
			n, err := integerValue(entry)
			if err != nil || n < 0 {
				return nil, errors.New("must contain nonnegative category IDs")
			}
			result = append(result, n)
		}
		return result, nil
	case "metadata":
		if metadata, ok := value.(map[string]any); ok {
			if nfo, ok := metadata["nfoContent"].(string); ok && strings.IndexByte(nfo, 0) >= 0 {
				// JSONB cannot represent NUL text. Preserve the received NFO
				// reversibly without mutating other mappings of this object.
				normalized := maps.Clone(metadata)
				normalized["nfoContent"] = map[string]any{
					"encoding": "base64",
					"data":     base64.StdEncoding.EncodeToString([]byte(nfo)),
				}
				return normalized, nil
			}
		}
		return value, nil
	default:
		return value, nil
	}
}

func normalizePublishedAt(value any, unit string) (string, error) {
	if unit != "" && unit != "seconds" && unit != "milliseconds" {
		return "", errors.New("published_at_unit must be seconds or milliseconds")
	}
	var date time.Time
	textDate := false
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		var err error
		date, err = time.Parse(time.RFC3339Nano, text)
		if err != nil {
			date, err = mail.ParseDate(text)
		}
		textDate = err == nil
	}
	if !textDate {
		if unit == "" {
			return "", errors.New("must be an RFC 3339 or RFC 5322 date")
		}
		epoch, err := integerValue(value)
		if err != nil {
			return "", errors.New("must be an RFC 3339 or RFC 5322 date or an integer Unix timestamp")
		}
		const minSeconds int64 = -62135596800
		const maxSeconds int64 = 253402300799
		if unit == "milliseconds" {
			if epoch < minSeconds*1000 || epoch > maxSeconds*1000+999 {
				return "", errors.New("publication date must have a UTC year between 1 and 9999")
			}
			date = time.UnixMilli(epoch)
		} else {
			if epoch < minSeconds || epoch > maxSeconds {
				return "", errors.New("publication date must have a UTC year between 1 and 9999")
			}
			date = time.Unix(epoch, 0)
		}
	}
	date = date.UTC()
	if date.Year() < 1 || date.Year() > 9999 {
		return "", errors.New("publication date must have a UTC year between 1 and 9999")
	}
	return date.Format(time.RFC3339Nano), nil
}
