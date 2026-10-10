package torznab

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const maxProviderBytes = 64 << 10

var (
	providerIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	errInvalidProviderJSON = errors.New("torznab: invalid provider JSON; check fields, types and duplicate keys")
)

// Provider is the versioned, deliberately small JSON definition of an indexer.
// Its schema does not interpret templates, CSS selectors, arbitrary scripts,
// field transformations, or website login forms.
// Categories, search capabilities, attributes and pagination come from Torznab.
type Provider struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	// APIPath is an optional root-relative endpoint hint, such as /api/torznab.
	// Omit it to discover from URL. A hint cannot move credentials off-origin.
	APIPath string       `json:"api_path"`
	Auth    ProviderAuth `json:"auth"`
	// RequestInterval uses Go duration syntax, e.g. 2s, not a unitless number.
	// Zero uses the client's local pacing default; server waits still win.
	RequestInterval time.Duration `json:"request_interval"`
	PageSize        int           `json:"page_size"`
	// PublishedAtUnit optionally accepts Unix publication dates; see Config.
	PublishedAtUnit string `json:"published_at_unit"`
	// Search and Output are collection settings, not server capabilities.
	// Use Search.Categories with Query and compile Output.Fields with
	// NewProjection when it is non-nil. The example command applies both.
	Search ProviderSearch `json:"search"`
	Output ProviderOutput `json:"output"`
}

// ProviderAuth references credentials without embedding them in provider files.
// Config resolves these environment names at runtime. Multiple mechanisms may
// be combined when an API requires both a key and an authenticated session.
// UsernameEnv and PasswordEnv refer to HTTP Basic, not an HTML login form.
// A resolved username must not contain a colon.
// A resolved CookieEnv value must be a valid Cookie request header.
type ProviderAuth struct {
	APIKeyEnv   string `json:"api_key_env"`
	CookieEnv   string `json:"cookie_env"`
	UsernameEnv string `json:"username_env"`
	PasswordEnv string `json:"password_env"`
}

// ProviderSearch limits the categories requested by a collection. Nil or empty
// Categories means no category filter. IDs are sent as a logical OR without
// guessing parent/subcategory expansion or altering the discovered capabilities.
type ProviderSearch struct {
	Categories []int `json:"categories"`
}

// ProviderOutput selects what a collection emits for storage. Omitted or null
// Fields preserves the complete normalized item. An explicit empty list is an
// error, never a silent fallback that exposes every field.
type ProviderOutput struct {
	Fields []string `json:"fields"`
}

// LoadProvider reads exactly one JSON object, at most 64 KiB. Unknown fields,
// duplicate keys, invalid types, unsupported versions and unsafe endpoint hints are errors.
// Loading does not read credentials or make any network requests.
func LoadProvider(reader io.Reader) (Provider, error) {
	if reader == nil {
		return Provider{}, errors.New("torznab: a provider reader is required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxProviderBytes+1))
	if err != nil {
		return Provider{}, errors.New("torznab: could not read provider JSON")
	}
	if len(data) > maxProviderBytes {
		return Provider{}, errors.New("torznab: provider JSON exceeds 64 KiB")
	}
	var provider Provider
	if err := json.Unmarshal(data, &provider); err != nil {
		// Parse errors can quote raw input values or property names.
		return Provider{}, errors.New("torznab: invalid provider JSON; check fields, types and duplicate keys")
	}
	if _, err := provider.baseConfig(); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

// providerJSON prevents the JSON methods from recursively calling themselves.
type providerJSON Provider

// MarshalJSON keeps request_interval human-readable rather than encoding the
// underlying time.Duration as a unitless nanosecond count.
func (p Provider) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		providerJSON
		RequestInterval string `json:"request_interval"`
	}{
		providerJSON:    providerJSON(p),
		RequestInterval: p.RequestInterval.String(),
	})
}

// UnmarshalJSON accepts strict, case-sensitive JSON fields and duration strings.
// Semantic validation and credential resolution remain in LoadProvider and Config.
func (p *Provider) UnmarshalJSON(data []byte) error {
	invalid := errInvalidProviderJSON
	if len(data) > maxProviderBytes || !utf8.Valid(data) {
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := checkProviderJSONValue(decoder, reflect.TypeOf(providerJSON{})); err != nil {
		return invalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return invalid
	}
	var wire struct {
		providerJSON
		RequestInterval *string `json:"request_interval"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return invalid
	}
	if wire.RequestInterval != nil {
		duration, err := time.ParseDuration(*wire.RequestInterval)
		if err != nil {
			return invalid
		}
		wire.providerJSON.RequestInterval = duration
	}
	*p = Provider(wire.providerJSON)
	return nil
}

// Walk the declared schema before decoding: encoding/json otherwise accepts
// duplicate keys, case-insensitive fields and null for scalar values.
func checkProviderJSONValue(decoder *json.Decoder, expected reflect.Type) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	invalid := errInvalidProviderJSON
	if expected == reflect.TypeOf(time.Duration(0)) {
		if _, ok := token.(string); !ok {
			return invalid
		}
		return nil
	}
	switch expected.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return invalid
		}
		seen := make(map[string]bool, expected.NumField())
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return invalid
			}
			seen[name] = true
			var fieldType reflect.Type
			for i := range expected.NumField() {
				field := expected.Field(i)
				if field.Tag.Get("json") == name {
					fieldType = field.Type
					break
				}
			}
			if fieldType == nil {
				return invalid
			}
			if err := checkProviderJSONValue(decoder, fieldType); err != nil {
				return err
			}
		}
		if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
			return invalid
		}
	case reflect.Slice:
		// Nil collection settings retain their existing public meaning.
		if token == nil {
			return nil
		}
		if token != json.Delim('[') {
			return invalid
		}
		for decoder.More() {
			if err := checkProviderJSONValue(decoder, expected.Elem()); err != nil {
				return err
			}
		}
		if close, err := decoder.Token(); err != nil || close != json.Delim(']') {
			return invalid
		}
	case reflect.String:
		if _, ok := token.(string); !ok {
			return invalid
		}
	case reflect.Int:
		if _, ok := token.(json.Number); !ok {
			return invalid
		}
	default:
		return invalid
	}
	return nil
}

// Config resolves the provider's credential references and returns a client
// configuration. A nil lookup uses os.LookupEnv; pass your own function to use
// an application's secret store instead. Missing or empty referenced secrets
// are errors, never silent anonymous access. The returned Config may then be
// given a custom HTTPClient before passing it to Open. Search and Output are
// applied by the collection layer, not implicitly by the HTTP client.
func (p Provider) Config(lookup func(string) (string, bool)) (Config, error) {
	config, err := p.baseConfig()
	if err != nil {
		return Config{}, err
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	for _, credential := range []struct {
		name  string
		value *string
	}{
		{p.Auth.APIKeyEnv, &config.APIKey},
		{p.Auth.CookieEnv, &config.Cookie},
		{p.Auth.UsernameEnv, &config.Username},
		{p.Auth.PasswordEnv, &config.Password},
	} {
		if credential.name == "" {
			continue
		}
		value, exists := lookup(credential.name)
		if !exists || value == "" {
			return Config{}, fmt.Errorf("torznab: required credential %s is missing or empty", credential.name)
		}
		*credential.value = value
	}
	if _, err := validateConfig(&config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (p Provider) baseConfig() (Config, error) {
	if p.Version != 1 {
		return Config{}, errors.New("torznab: provider version must be 1")
	}
	if !providerIDPattern.MatchString(p.ID) {
		return Config{}, errors.New("torznab: provider id must use lowercase letters, digits, underscores or hyphens")
	}
	for _, category := range p.Search.Categories {
		if category < 0 {
			return Config{}, errors.New("torznab: search.categories must contain nonnegative IDs")
		}
	}
	if p.Output.Fields != nil {
		if _, err := NewProjection(p.Output.Fields); err != nil {
			return Config{}, err
		}
	}
	for _, name := range []string{p.Auth.APIKeyEnv, p.Auth.CookieEnv, p.Auth.UsernameEnv, p.Auth.PasswordEnv} {
		if name != "" && !environmentNamePattern.MatchString(name) {
			return Config{}, errors.New("torznab: auth values must be environment variable names, not credentials or templates")
		}
	}
	if p.Auth.PasswordEnv != "" && p.Auth.UsernameEnv == "" {
		return Config{}, errors.New("torznab: password_env requires username_env for HTTP Basic")
	}
	config := Config{URL: p.URL, RequestInterval: p.RequestInterval, PageSize: p.PageSize, PublishedAtUnit: p.PublishedAtUnit}
	initial, err := validateConfig(&config)
	if err != nil {
		return Config{}, err
	}
	if p.APIPath != "" {
		ref, err := url.Parse(p.APIPath)
		if err != nil || ref.IsAbs() || ref.Host != "" || ref.User != nil ||
			!strings.HasPrefix(ref.Path, "/") || strings.Contains(p.APIPath, "#") {
			return Config{}, errors.New("torznab: api_path must be a root-relative path without a host or fragment")
		}
		query, err := url.ParseQuery(ref.RawQuery)
		if err != nil {
			return Config{}, errors.New("torznab: invalid api_path query")
		}
		target := initial.ResolveReference(ref)
		if !sameOrigin(initial, target) {
			return Config{}, errors.New("torznab: api_path cannot change the provider origin")
		}
		combined := initial.Query()
		for key, values := range query {
			combined[key] = values
		}
		target.RawQuery = combined.Encode()
		config.URL = target.String()
	}
	return config, nil
}
