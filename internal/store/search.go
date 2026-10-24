package store

import (
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
)

var searchTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}([.][0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func validSearchText(value string, maximum int) bool {
	if len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func decimalBound(value string) (string, bool) {
	if value == "" {
		return "", true
	}
	// This exceeds practical byte/count values without exposing PostgreSQL's
	// numeric parser to unbounded user-provided query strings.
	if len(value) > 1000 {
		return "", false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return "", false
		}
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		value = "0"
	}
	return value, true
}

func decimalGreater(a, b string) bool {
	return len(a) > len(b) || (len(a) == len(b) && a > b)
}

// NormalizeSearchFilters is shared by torrent searches and Torznab queries.
// Error text is deliberately independent of user-provided field values.
func NormalizeSearchFilters(input model.SearchFilters) (model.SearchFilters, error) {
	input.Query = strings.TrimSpace(input.Query)
	input.Category = strings.TrimSpace(input.Category)
	if !validSearchText(input.Query, 500) || !validSearchText(input.Category, 256) || len(input.Providers) > 100 {
		return input, model.ErrInvalid
	}
	providers := make([]string, 0, len(input.Providers))
	seen := make(map[string]bool, len(input.Providers))
	for _, provider := range input.Providers {
		if provider == "" || !validSearchText(provider, 128) || strings.TrimSpace(provider) != provider {
			return input, model.ErrInvalid
		}
		if !seen[provider] {
			providers = append(providers, provider)
			seen[provider] = true
		}
	}
	input.Providers = providers
	for _, bounds := range [][2]*string{{&input.MinSize, &input.MaxSize}} {
		for _, bound := range bounds {
			value, ok := decimalBound(*bound)
			if !ok {
				return input, model.ErrInvalid
			}
			*bound = value
		}
		if *bounds[0] != "" && *bounds[1] != "" && decimalGreater(*bounds[0], *bounds[1]) {
			return input, model.ErrInvalid
		}
	}
	var lower, upper time.Time
	for _, bound := range []struct {
		value  *string
		parsed *time.Time
	}{{&input.PublishedAfter, &lower}, {&input.PublishedBefore, &upper}} {
		if *bound.value == "" {
			continue
		}
		if len(*bound.value) > 64 || !searchTimestamp.MatchString(*bound.value) {
			return input, model.ErrInvalid
		}
		parsed, err := time.Parse(time.RFC3339Nano, *bound.value)
		if err != nil {
			return input, model.ErrInvalid
		}
		// PostgreSQL accepts a narrower offset range than RFC3339. Bind the
		// same instant in UTC, checking the supported year after conversion.
		parsed = parsed.UTC()
		if parsed.Year() < 1 || parsed.Year() > 9999 {
			return input, model.ErrInvalid
		}
		*bound.parsed = parsed
		*bound.value = parsed.Format(time.RFC3339Nano)
	}
	if input.PublishedAfter != "" && input.PublishedBefore != "" && lower.After(upper) {
		return input, model.ErrInvalid
	}
	if input.InfoHash != "" {
		hash := strings.TrimSpace(input.InfoHash)
		switch len(hash) {
		case 40, 64:
			if _, err := hex.DecodeString(hash); err != nil {
				return input, model.ErrInvalid
			}
			input.InfoHash = strings.ToLower(hash)
		case 32:
			decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(hash))
			if err != nil {
				return input, model.ErrInvalid
			}
			input.InfoHash = hex.EncodeToString(decoded)
		default:
			return input, model.ErrInvalid
		}
	}
	switch input.Sort {
	case "":
		input.Sort = "recent"
	case "recent", "oldest", "size_desc", "size_asc", "name_asc", "name_desc", "relevance", "relevance_asc":
	default:
		return input, model.ErrInvalid
	}
	return input, nil
}

func torrentSearchOptions(options model.ListOptions) (model.ListOptions, error) {
	providers := options.Providers
	if options.ProviderID != "" {
		providers = append(append([]string{}, providers...), options.ProviderID)
	}
	filters, err := NormalizeSearchFilters(model.SearchFilters{Query: options.Query, Providers: providers,
		Category: options.Category, MinSize: options.MinSize, MaxSize: options.MaxSize,
		PublishedAfter: options.PublishedAfter, PublishedBefore: options.PublishedBefore,
		InfoHash: options.InfoHash, Sort: options.Sort})
	if err != nil {
		return options, err
	}
	normalized := filters.ListOptions()
	normalized.Limit = options.Limit
	normalized.Offset = options.Offset
	return pageOptions(normalized), nil
}

// Each fragment is server-defined. Only values become positional parameters;
// no user-supplied field, operator, sort expression or SQL is interpolated.
func torrentSearchSQL(options model.ListOptions) (string, string, []any, []any) {
	clauses := []string{}
	args := []any{}
	bind := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if len(options.Providers) > 0 {
		clauses = append(clauses, "provider_id=ANY("+bind(options.Providers)+"::text[])")
	}
	if options.Query != "" {
		clauses = append(clauses, torrentTextSearchSQL(options.Query, bind))
	}
	if options.Category != "" {
		parameter := bind(options.Category)
		clauses = append(clauses, "search_categories @> ARRAY[encode(sha256(convert_to(lower("+parameter+"::text),'UTF8')),'hex')]")
	}
	for _, filter := range []struct{ column, operator, value string }{
		{"search_size", ">=", options.MinSize}, {"search_size", "<=", options.MaxSize},
	} {
		if filter.value == "" {
			continue
		}
		parameter := bind(filter.value) + "::text::numeric"
		clauses = append(clauses, "(ingest.torrent_integer_key("+filter.column+") COLLATE \"C\") "+filter.operator+" (ingest.torrent_integer_key("+parameter+") COLLATE \"C\") AND "+filter.column+" "+filter.operator+" "+parameter)
	}
	if options.PublishedAfter != "" {
		clauses = append(clauses, "search_published_at >= "+bind(options.PublishedAfter)+"::text::timestamptz")
	}
	if options.PublishedBefore != "" {
		clauses = append(clauses, "search_published_at <= "+bind(options.PublishedBefore)+"::text::timestamptz")
	}
	if options.InfoHash != "" {
		clauses = append(clauses, "search_info_hash="+bind(options.InfoHash))
	}
	filter := " FROM ingest.torrents"
	if len(clauses) > 0 {
		filter += " WHERE " + strings.Join(clauses, " AND ")
	}
	// Ranking can add parameters used only by ORDER BY. The total query needs
	// only the filter parameters, while the page uses both sets.
	filterArgs := args
	order := "last_seen_at DESC"
	switch options.Sort {
	case "oldest":
		order = "last_seen_at ASC"
	case "size_desc":
		order = "search_size DESC NULLS LAST"
	case "size_asc":
		order = "search_size ASC NULLS LAST"
	case "name_asc":
		order = "lower(fields->>'title') ASC"
	case "name_desc":
		order = "lower(fields->>'title') DESC"
	case "relevance", "relevance_asc":
		if options.Query != "" {
			queryParameter := bind(options.Query)
			direction := "DESC"
			if options.Sort == "relevance_asc" {
				direction = "ASC"
			}
			order = "ts_rank_cd(search_document,websearch_to_tsquery('simple'::regconfig," + queryParameter + ")) " + direction + "," +
				"public.word_similarity(" + queryParameter + "," + torrentSearchTextSQL + ") " + direction + ",last_seen_at DESC"
		}
	}
	return filter, " ORDER BY " + order + ",provider_id,source_id", filterArgs, args
}
