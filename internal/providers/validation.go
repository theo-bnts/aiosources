package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

// MaxDocumentBytes bounds submitted definitions, including whitespace.
const MaxDocumentBytes = 128 * 1024

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidID applies the same safe provider identifier rule used by the registry.
func ValidID(id string) bool {
	return model.ValidSourceID(id)
}

// Defaults are applied only to absent fields, never to explicitly supplied zeroes.
// In particular enabled must be an explicit JSON boolean. An absent schedule is
// manual; if later enabled, its default mode is incremental.
func defaults() model.Provider {
	return model.Provider{
		Auth:            model.Auth{Type: "none"},
		RequestInterval: "1s",
		RateLimitReset:  "epoch",
		PageSize:        100,
		HTTP:            model.HTTPConfig{Method: "GET"},
		Schedule:        model.Schedule{Mode: model.ModeIncremental},
	}
}

// Validate never reads the network or resolves secrets. It checks definitions
// independently of their filename and never rewrites the caller's JSON.
func (r *Registry) Validate(raw string) model.Validation {
	p := defaults()
	result := model.Validation{Issues: []string{}}
	if len(raw) > MaxDocumentBytes {
		result.Issues = append(result.Issues, "definition exceeds the 128 KiB limit")
		return result
	}
	if !utf8.ValidString(raw) {
		result.Issues = append(result.Issues, "definition must be valid UTF-8")
		return result
	}
	root, err := parseDocument(raw)
	if err != nil {
		result.Issues = append(result.Issues, err.Error())
		return result
	}
	result.ID, _ = root["id"].(string)
	checkSchemaValue(root, reflect.TypeOf(model.Provider{}), &result.Issues)
	if len(result.Issues) != 0 {
		return result
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		result.Issues = append(result.Issues, "invalid JSON value type or integer outside the supported range")
		return result
	}
	result.ID = p.ID
	result.Provider = &p
	_, explicitEnabled := root["enabled"].(bool)
	if !explicitEnabled {
		result.Issues = append(result.Issues, "enabled must be an explicit boolean")
	}
	result.Issues = append(result.Issues, validateProvider(p)...)
	if r.validate != nil && len(result.Issues) == 0 {
		if err := r.validate(p); err != nil {
			result.Issues = append(result.Issues, err.Error())
		}
	}
	result.Valid = len(result.Issues) == 0
	return result
}

// parseDocument checks tokens before decoding into model types: encoding/json
// otherwise silently accepts duplicate fields and null scalar values.
func parseDocument(raw string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	value, err := readJSONValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("definition must contain exactly one JSON object")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("definition must be a JSON object")
	}
	return root, nil
}

func readJSONValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 1000 {
		return nil, fmt.Errorf("JSON nesting exceeds the supported limit")
	}
	token, err := decoder.Token()
	if err != nil {
		// Decoder errors can quote credentials. Only expose a byte location.
		return nil, fmt.Errorf("invalid JSON near byte %d", decoder.InputOffset())
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return nil, fmt.Errorf("invalid JSON object near byte %d", decoder.InputOffset())
				}
				if strings.ContainsRune(key, '\x00') {
					return nil, fmt.Errorf("JSON property names must not contain NUL characters")
				}
				if _, exists := object[key]; exists {
					return nil, fmt.Errorf("duplicate JSON property near byte %d", decoder.InputOffset())
				}
				value, err := readJSONValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				object[key] = value
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("invalid JSON object near byte %d", decoder.InputOffset())
			}
			return object, nil
		case '[':
			array := []any{}
			for decoder.More() {
				value, err := readJSONValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("invalid JSON array near byte %d", decoder.InputOffset())
			}
			return array, nil
		}
	case string:
		if strings.ContainsRune(token, '\x00') {
			return nil, fmt.Errorf("JSON strings must not contain NUL characters")
		}
		return token, nil
	default:
		return token, nil
	}
	return nil, fmt.Errorf("invalid JSON near byte %d", decoder.InputOffset())
}

// Keep absent/default and null semantics identical to the previous contract:
// maps, slices and open JSON values may be null; scalar, struct and pointer
// fields may not. Match field names exactly, unlike encoding/json's folding.
func checkSchemaValue(value any, expected reflect.Type, issues *[]string) {
	if len(*issues) >= 32 || expected.Kind() == reflect.Interface {
		return
	}
	mismatch := func() { *issues = append(*issues, "incorrect JSON value type for provider field") }
	switch expected.Kind() {
	case reflect.Pointer:
		checkSchemaValue(value, expected.Elem(), issues)
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			mismatch()
			return
		}
		for _, name := range sortedKeys(object) {
			found := false
			for index := range expected.NumField() {
				field := expected.Field(index)
				fieldName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
				if name == fieldName {
					checkSchemaValue(object[name], field.Type, issues)
					found = true
					break
				}
			}
			if !found {
				*issues = append(*issues, "unknown provider field")
			}
		}
	case reflect.Map:
		if value == nil {
			return
		}
		object, ok := value.(map[string]any)
		if !ok {
			mismatch()
			return
		}
		for _, name := range sortedKeys(object) {
			checkSchemaValue(object[name], expected.Elem(), issues)
		}
	case reflect.Slice:
		if value == nil {
			return
		}
		array, ok := value.([]any)
		if !ok {
			mismatch()
			return
		}
		for _, child := range array {
			checkSchemaValue(child, expected.Elem(), issues)
		}
	case reflect.String:
		if _, ok := value.(string); !ok {
			mismatch()
		}
	case reflect.Bool:
		if _, ok := value.(bool); !ok {
			mismatch()
		}
	case reflect.Int:
		number, ok := value.(json.Number)
		if !ok {
			mismatch()
		} else if _, err := strconv.ParseInt(string(number), 10, strconv.IntSize); err != nil {
			mismatch()
		}
	}
}

func validateProvider(p model.Provider) []string {
	issues := []string{}
	add := func(condition bool, message string) {
		if condition {
			issues = append(issues, message)
		}
	}
	add(p.Version != 1, "version must be 1")
	add(!ValidID(p.ID), "id must be 1–128 ASCII letters, digits, underscores or hyphens, starting with a letter or digit")
	add(strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || hasControl(p.Name), "name must be nonempty, at most 200 bytes, without control characters")
	add(!identifier.MatchString(p.Adapter), "adapter must be a safe identifier")
	if err := validateURL(p.URL); err != nil {
		issues = append(issues, "url: "+err.Error())
	}
	interval, err := time.ParseDuration(p.RequestInterval)
	add(err != nil || interval <= 0, "request_interval must be a positive Go duration")
	if p.RequestTimeout != "" {
		timeout, err := time.ParseDuration(p.RequestTimeout)
		add(err != nil || timeout <= 0 || timeout > 15*time.Minute, "request_timeout must be a positive Go duration no longer than 15m")
	}
	if limits := p.RequestLimits; limits != nil {
		add(limits.PerMinute == 0 && limits.PerHour == 0 && limits.PerDay == 0, "request_limits must enable at least one positive limit")
		for _, count := range []int{limits.PerMinute, limits.PerHour, limits.PerDay} {
			add(count < 0 || count > 1_000_000, "request_limits counts must be between 0 and 1000000")
		}
	}
	add(p.RateLimitReset != "epoch" && p.RateLimitReset != "relative", "rate_limit_reset must be epoch or relative")
	add(p.PageSize < 1 || p.PageSize > 10000, "page_size must be between 1 and 10000")
	settings, err := scheduling.Settings(p)
	if err != nil {
		issues = append(issues, err.Error())
	} else if _, err := scheduling.Parse(settings); err != nil {
		issues = append(issues, err.Error())
	}
	add(!p.IsRemoteCatalog() && p.Schedule.Mode == model.ModeMetadata && !p.SupportsMetadata(), "schedule.mode metadata requires a native JSON source with configured detail fields")
	add(p.Traversal != nil && p.Traversal.MetadataAfterIncremental && !p.SupportsMetadata(), "traversal.metadata_after_incremental requires a native JSON source with configured detail fields")
	for _, category := range p.Search.Categories {
		if category < 0 {
			issues = append(issues, "search.categories must contain nonnegative integers")
			break
		}
	}
	add(p.HTTP.Method != "GET" && p.HTTP.Method != "POST", "http.method must be GET or POST")
	issues = append(issues, validateAuth(p.Auth)...)
	headerNames := make(map[string]bool, len(p.HTTP.Headers)+len(p.HTTP.SecretHeaders))
	for _, name := range sortedKeys(p.HTTP.Headers) {
		value := p.HTTP.Headers[name]
		normalized := strings.ToLower(name)
		add(!headerName(name) || managedHeader(name), "http.headers contains an invalid or transport-managed header name")
		add(headerNames[normalized], "HTTP header names must be unique ignoring case")
		headerNames[normalized] = true
		add(credentialName(name), "sensitive literal HTTP headers are forbidden; use http.secret_headers")
		add(strings.ContainsAny(value, "\r\n\x00") || hasInvalidHeaderByte(value), "HTTP header values must not contain control characters")
	}
	for _, name := range sortedKeys(p.HTTP.SecretHeaders) {
		ref := p.HTTP.SecretHeaders[name]
		normalized := strings.ToLower(name)
		add(!headerName(name) || managedHeader(name), "http.secret_headers contains an invalid or transport-managed header name")
		add(headerNames[normalized], "HTTP header names must be unique ignoring case")
		headerNames[normalized] = true
		add(!identifier.MatchString(ref), "http.secret_headers values must be nonempty safe secret references")
	}
	for _, query := range []struct {
		path   string
		values map[string]any
	}{{"http.query", p.HTTP.Query}, {"http.incremental_query", p.HTTP.IncrementalQuery}} {
		for _, name := range sortedKeys(query.values) {
			add(name == "" || hasControl(name), query.path+" contains an invalid parameter name")
			add(credentialName(name), "credential query parameters are forbidden; configure auth with a secret reference")
		}
		inspectData(query.values, query.path, &issues)
		if _, err := json.Marshal(query.values); err != nil {
			issues = append(issues, query.path+" must contain finite JSON-compatible values")
		}
	}
	inspectData(p.HTTP.Body, "http.body", &issues)
	inspectData(p.Options, "options", &issues)
	if p.Traversal != nil {
		for _, scope := range p.Traversal.Scopes {
			inspectData(scope.Query, "traversal.scopes.query", &issues)
			inspectData(scope.Match, "traversal.scopes.match", &issues)
		}
		for _, query := range p.Traversal.QueryVariants {
			inspectData(query, "traversal.query_variants", &issues)
		}
		if p.Traversal.IDRecovery != nil {
			inspectData(p.Traversal.IDRecovery.DiscoveryQuery, "traversal.id_recovery.discovery_query", &issues)
		}
		if _, err := json.Marshal(p.Traversal); err != nil {
			issues = append(issues, "traversal must contain finite JSON-compatible values")
		}
	}
	if _, err := json.Marshal(p.Options); err != nil {
		issues = append(issues, "options must contain finite JSON-compatible values")
	}
	if _, err := json.Marshal(p.HTTP.Body); err != nil {
		issues = append(issues, "http.body must contain finite JSON-compatible values")
	}
	if p.Auth.Type != "none" {
		authName := p.Auth.Name
		if p.Auth.Type == "bearer" || p.Auth.Type == "basic" {
			authName = "Authorization"
		}
		if p.Auth.Type == "cookie" {
			authName = "Cookie"
		}
		if p.Auth.Type == "header" || p.Auth.Type == "bearer" || p.Auth.Type == "basic" || p.Auth.Type == "cookie" || p.Auth.Type == "api_key" && p.Auth.In == "header" {
			add(headerNames[strings.ToLower(authName)], "auth and HTTP headers must not configure the same header")
		}
		if p.Auth.Type == "query" || p.Auth.Type == "api_key" && p.Auth.In == "query" {
			for _, values := range []map[string]any{p.HTTP.Query, p.HTTP.IncrementalQuery} {
				for name := range values {
					add(strings.EqualFold(name, authName), "auth and HTTP query parameters must not configure the same parameter")
				}
			}
		}
	}
	add(p.Pagination.Type != "" && p.Pagination.Type != "none" && p.Pagination.Type != "offset" && p.Pagination.Type != "page" && p.Pagination.Type != "cursor" && p.Pagination.Type != "next", "pagination.type must be none, offset, page, cursor or next")
	add(p.Pagination.In != "" && p.Pagination.In != "query" && p.Pagination.In != "body", "pagination.in must be query or body")
	add(p.Pagination.In == "body" && p.HTTP.Method != "POST", "body pagination requires http.method POST")
	add(p.Pagination.Start < 0, "pagination.start must be nonnegative")
	for _, param := range []string{p.Pagination.SizeParam, p.Pagination.PageParam, p.Pagination.OffsetParam, p.Pagination.CursorParam} {
		add(hasControl(param) || credentialName(param), "pagination parameter names must not contain controls or be credential names")
	}
	for _, pointer := range []string{p.HTTP.ItemsPath, p.Mapping.ID, p.Pagination.NextPath, p.Pagination.TotalPath, p.Pagination.CurrentPath} {
		add(!jsonPointer(pointer), "mapping and pagination paths must be RFC 6901 JSON Pointers")
	}
	for _, name := range sortedKeys(p.Mapping.Fields) {
		add(!identifier.MatchString(name), "mapping.fields names must be safe identifiers")
		add(!jsonPointer(p.Mapping.Fields[name]), "mapping.fields values must be RFC 6901 JSON Pointers")
	}
	seenFields := make(map[string]bool, len(p.Output.Fields))
	for _, field := range p.Output.Fields {
		name := strings.TrimPrefix(field, "attributes.")
		add(!identifier.MatchString(name) || seenFields[field], "output.fields must contain unique safe identifiers or attributes.NAME selectors")
		seenFields[field] = true
	}
	return issues
}

func validateAuth(auth model.Auth) []string {
	issues := []string{}
	if auth.UsernameRef != "" && !identifier.MatchString(auth.UsernameRef) || auth.PasswordRef != "" && !identifier.MatchString(auth.PasswordRef) || auth.SecretRef != "" && !identifier.MatchString(auth.SecretRef) {
		issues = append(issues, "auth references must be safe identifiers")
	}
	switch auth.Type {
	case "none":
		if auth.SecretRef != "" || auth.UsernameRef != "" || auth.PasswordRef != "" || auth.In != "" || auth.Name != "" {
			issues = append(issues, "auth.type none cannot contain authentication parameters")
		}
	case "basic":
		if auth.UsernameRef == "" || auth.PasswordRef == "" || auth.SecretRef != "" || auth.In != "" || auth.Name != "" {
			issues = append(issues, "basic auth requires only username_ref and password_ref")
		}
	case "bearer", "cookie", "query", "header", "api_key":
		if auth.SecretRef == "" || auth.UsernameRef != "" || auth.PasswordRef != "" {
			issues = append(issues, "this authentication type requires only secret_ref")
		}
		if auth.Type == "api_key" {
			if auth.In != "header" && auth.In != "query" {
				issues = append(issues, "api_key auth.in must be header or query")
			}
		} else if auth.In != "" && auth.In != auth.Type {
			issues = append(issues, "auth.in must agree with the authentication type")
		}
		if auth.Type == "header" || auth.Type == "query" || auth.Type == "api_key" {
			if auth.Name == "" || hasControl(auth.Name) {
				issues = append(issues, "header/query authentication requires a parameter name")
			}
			if auth.Type == "header" || auth.In == "header" {
				if !headerName(auth.Name) || managedHeader(auth.Name) {
					issues = append(issues, "auth.name must be a valid non-managed HTTP header name")
				}
			}
		} else if auth.Name != "" {
			issues = append(issues, "bearer and cookie authentication do not accept auth.name")
		}
	default:
		issues = append(issues, "unsupported authentication type")
	}
	return issues
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || hasControl(raw) || strings.ContainsAny(raw, "\\ ") || u == nil || u.Opaque != "" || u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be an absolute http or https URL")
	}
	if u.User != nil || u.Fragment != "" {
		return fmt.Errorf("userinfo and fragments are forbidden")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return fmt.Errorf("port must be between 1 and 65535")
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("contains an invalid query string")
	}
	for name := range query {
		if credentialName(name) {
			return fmt.Errorf("credential query parameters are forbidden; configure auth with a secret reference")
		}
	}
	return nil
}

func inspectData(value any, location string, issues *[]string) {
	switch value := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(value) {
			child := value[key]
			normalized := normalizeName(key)
			if strings.HasSuffix(normalized, "ref") && credentialName(strings.TrimSuffix(normalized, "ref")) {
				ref, ok := child.(string)
				if !ok || !identifier.MatchString(ref) {
					*issues = append(*issues, location+" contains an invalid secret reference")
				}
			} else if credentialName(key) {
				*issues = append(*issues, location+" contains a literal credential field; use a secret reference")
			}
			inspectData(child, location, issues)
		}
	case []any:
		for _, child := range value {
			inspectData(child, location, issues)
		}
	case string:
		if len(value) >= 7 && strings.EqualFold(value[:7], "http://") || len(value) >= 8 && strings.EqualFold(value[:8], "https://") {
			if err := validateURL(value); err != nil {
				*issues = append(*issues, location+" contains an unsafe URL")
			}
		}
	case nil, bool, int, int64, uint64, float64, json.Number:
	default:
		*issues = append(*issues, location+" must contain JSON-compatible values")
	}
}

func normalizeName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' || r == '[' || r == ']' {
			return -1
		}
		return unicode.ToLower(r)
	}, name)
}

func credentialName(name string) bool {
	return model.CredentialParameter(name)
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func hasInvalidHeaderByte(value string) bool {
	for i := range len(value) {
		if value[i] == 127 || value[i] < 32 && value[i] != '\t' {
			return true
		}
	}
	return false
}

func headerName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0 {
			continue
		}
		return false
	}
	return true
}

func managedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "content-length", "transfer-encoding", "connection", "trailer", "upgrade", "proxy-connection":
		return true
	}
	return false
}

func jsonPointer(pointer string) bool {
	if pointer == "" {
		return true
	}
	if pointer[0] != '/' || hasControl(pointer) {
		return false
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] == '~' {
			if i+1 == len(pointer) || pointer[i+1] != '0' && pointer[i+1] != '1' {
				return false
			}
			i++
		}
	}
	return true
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
