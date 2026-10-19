package providers

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

var providerSchema = buildJSONSchema()

// JSONSchema describes the editable source contract. Connector-specific and
// credential checks still run in Validate; callers must treat this as read-only.
func JSONSchema() json.RawMessage { return providerSchema }

func buildJSONSchema() json.RawMessage {
	schema := schemaFor(reflect.TypeOf(model.Provider{}))
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = "Source configuration"
	schema["description"] = "One strict JSON object, at most 128 KiB. Server validation also checks connector settings and secret references."
	schema["required"] = []string{"version", "id", "name", "adapter", "url", "enabled"}
	field := func(path string) map[string]any {
		value := schema
		for _, name := range strings.Split(path, ".") {
			value = value["properties"].(map[string]any)[name].(map[string]any)
		}
		return value
	}
	set := func(path, key string, value any) { field(path)[key] = value }
	set("version", "const", 1)
	for _, path := range []string{"id", "adapter"} {
		set(path, "pattern", identifier.String())
	}
	set("name", "minLength", 1)
	set("name", "maxLength", 200)
	set("name", "description", "Nonblank name, no control characters, at most 200 UTF-8 bytes.")
	set("url", "format", "uri")
	set("url", "pattern", "^https?://")
	set("url", "description", "Absolute HTTP(S) URL without userinfo, fragments or credential query parameters.")
	set("auth.type", "enum", []string{"none", "basic", "bearer", "cookie", "query", "header", "api_key"})
	set("auth.type", "default", "none")
	set("auth.in", "enum", []string{"", "header", "query", "bearer", "cookie", "basic", "none"})
	for _, path := range []string{"auth.secret_ref", "auth.username_ref", "auth.password_ref"} {
		set(path, "pattern", "^(?:[A-Za-z0-9][A-Za-z0-9_-]{0,127})?$")
		set(path, "description", "Name of a separately stored secret; never paste a credential here.")
	}
	set("request_interval", "default", "1s")
	set("request_interval", "description", "Positive Go duration, for example 1s or 500ms; connectors limit it to 24h.")
	set("request_timeout", "description", "Positive Go duration no longer than 15m, or omitted for the connector default.")
	set("request_limits", "description", "Optional persistent request budgets shared by all runs of this source. Rolling 60-second, 3600-second and 86400-second windows; zero or omission disables a window. At least one limit must be positive.")
	positiveLimits := make([]any, 0, 3)
	for _, name := range []string{"per_minute", "per_hour", "per_day"} {
		set("request_limits."+name, "minimum", 0)
		set("request_limits."+name, "maximum", 1_000_000)
		positiveLimits = append(positiveLimits, map[string]any{
			"required":   []string{name},
			"properties": map[string]any{name: map[string]any{"minimum": 1}},
		})
	}
	set("request_limits", "anyOf", positiveLimits)
	set("rate_limit_reset", "enum", []string{"epoch", "relative"})
	set("rate_limit_reset", "default", "epoch")
	set("page_size", "minimum", 1)
	set("page_size", "maximum", 10000)
	set("page_size", "default", 100)
	field("search.categories")["items"].(map[string]any)["minimum"] = 0
	set("http.method", "enum", []string{"GET", "POST"})
	set("http.method", "default", "GET")
	set("http.secret_headers", "additionalProperties", map[string]any{"type": "string", "pattern": identifier.String()})
	set("http.incremental_query", "description", "Query overrides applied only to Incremental collection. Credential values belong in secret references.")
	set("pagination.type", "enum", []string{"", "none", "offset", "page", "cursor", "next"})
	set("pagination.in", "enum", []string{"", "query", "body"})
	set("pagination.start", "minimum", 0)
	for _, path := range []string{"http.items_path", "mapping.id", "pagination.next_path", "pagination.total_path", "pagination.current_path"} {
		set(path, "description", "RFC 6901 JSON Pointer; empty means the whole response.")
	}
	set("output.fields", "uniqueItems", true)
	set("schedule.mode", "enum", []string{"", "incremental", "full", "metadata"})
	set("schedule.mode", "default", "incremental")
	for _, path := range []string{"schedule.max_pages", "schedule.known_pages"} {
		set(path, "minimum", 0)
		set(path, "maximum", 10000)
	}
	set("schedule.every", "description", "Go duration of at least 1m, mutually exclusive with cron.")
	set("schedule.full_every", "description", "Optional Go duration of at least 1m; requires an incremental every/cron schedule. Full reconciliation replaces a fresh Incremental on the first eligible base tick after its deadline.")
	set("schedule.cron", "description", "Five-field cron expression, mutually exclusive with every.")
	set("schedule.timezone", "description", "IANA time zone; defaults to UTC.")
	set("catalog_sync", "description", "Remote catalog synchronization only. Uses a simple interval and the durable catalog checkpoint. Replaces legacy schedule settings for remote catalogs.")
	set("catalog_sync", "required", []string{"interval_minutes"})
	set("catalog_sync.enabled", "default", false)
	set("catalog_sync.interval_minutes", "minimum", 1)
	set("catalog_sync.interval_minutes", "maximum", model.MaxCatalogSyncMinutes)
	set("traversal.total_mode", "enum", []string{"", "strict", "at_least"})
	set("traversal.incremental_order", "enum", []string{"", "id", "published_at"})
	set("traversal.metadata_after_incremental", "default", false)
	set("traversal.metadata_after_incremental", "description", "After a successful Incremental, queue Metadata only for newly added native IDs with missing enrich_fields. Requires Metadata support; disabled by default. Existing run snapshots are unchanged.")
	set("html", "description", "HTML adapter only. CSS selectors read server-rendered pages; no scripts, styles, images or embedded resources are executed or fetched. Empty pages require an explicit empty_selector match.")
	set("html", "required", []string{"page_selector", "rows_selector", "id", "fields"})
	for _, path := range []string{"html.page_selector", "html.rows_selector", "html.empty_selector", "html.next_selector"} {
		set(path, "maxLength", 2048)
	}
	set("html.id.format", "enum", []string{"", "text", "integer", "url"})
	set("html.details.url.format", "enum", []string{"", "text", "url"})
	for _, path := range []string{"html.total", "html.current"} {
		set(path+".format", "enum", []string{"", "text", "integer"})
	}
	for _, path := range []string{"html.id", "html.total", "html.current", "html.details.url"} {
		set(path+".all", "const", false)
	}
	for _, path := range []string{"html.fields", "html.details.fields"} {
		set(path, "minProperties", 1)
		set(path, "maxProperties", model.MaxHTMLFields)
		properties := field(path)["additionalProperties"].(map[string]any)["properties"].(map[string]any)
		properties["format"].(map[string]any)["enum"] = []string{"", "text", "integer", "size", "date", "url", "info_hash", "torrent"}
	}
	set("html.details", "required", []string{"url", "page_selector", "fields"})
	data, err := json.Marshal(schema)
	if err != nil {
		panic("cannot generate source JSON schema")
	}
	return data
}

func schemaFor(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		// Present pointer fields still require their concrete type, not null.
		return schemaFor(t.Elem())
	case reflect.Struct:
		properties := make(map[string]any, t.NumField())
		for index := range t.NumField() {
			field := t.Field(index)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name != "" && name != "-" {
				properties[name] = schemaFor(field.Type)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	case reflect.Map:
		return map[string]any{"type": []string{"object", "null"}, "additionalProperties": schemaFor(t.Elem())}
	case reflect.Slice:
		return map[string]any{"type": []string{"array", "null"}, "items": schemaFor(t.Elem())}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Interface:
		return map[string]any{}
	default:
		panic("unsupported source schema type")
	}
}
