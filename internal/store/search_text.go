package store

import (
	"encoding/base32"
	"encoding/hex"
	"strings"
	"unicode"
)

const torrentSearchTextSQL = "ingest.torrent_search_text(provider_id,source_id,fields)"

type torrentSearchTerm struct {
	text     string
	phrase   bool
	excluded bool
}

// Whitespace combines fragments; OR separates alternatives. Quotes retain the
// existing whole-word phrase semantics, and a leading minus excludes either
// kind of term. Unclosed quotes are accepted like PostgreSQL web search.
func torrentSearchGroups(query string) [][]torrentSearchTerm {
	groups := [][]torrentSearchTerm{nil}
	for query != "" {
		query = strings.TrimLeftFunc(query, unicode.IsSpace)
		term := torrentSearchTerm{}
		if strings.HasPrefix(query, "-") {
			term.excluded = true
			query = strings.TrimLeftFunc(strings.TrimLeft(query, "-"), unicode.IsSpace)
		}
		if strings.HasPrefix(query, `"`) {
			term.phrase = true
			query = query[1:]
			end := strings.IndexByte(query, '"')
			if end < 0 {
				term.text, query = query, ""
			} else {
				term.text, query = query[:end], query[end+1:]
			}
		} else {
			end := strings.IndexFunc(query, func(r rune) bool { return unicode.IsSpace(r) || r == '"' })
			if end < 0 {
				term.text, query = query, ""
			} else {
				term.text, query = query[:end], query[end:]
			}
		}
		if !term.phrase {
			// Punctuation around a word is formatting, as in web search. Keep
			// SQL pattern characters literal rather than turning them into syntax.
			term.text = strings.TrimFunc(term.text, func(r rune) bool {
				return unicode.IsPunct(r) && r != '%' && r != '_' && r != '\\'
			})
		}
		if strings.TrimSpace(term.text) == "" {
			continue
		}
		if term.phrase && strings.IndexFunc(term.text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) < 0 {
			continue
		}
		last := len(groups) - 1
		if !term.excluded && !term.phrase && strings.EqualFold(term.text, "OR") && len(groups[last]) > 0 && strings.TrimSpace(query) != "" {
			groups = append(groups, nil)
			continue
		}
		groups[last] = append(groups[last], term)
	}
	if len(groups[len(groups)-1]) == 0 {
		groups = groups[:len(groups)-1]
	}
	return groups
}

var escapeSearchFragment = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func torrentTextSearchSQL(query string, bind func(any) string) string {
	var alternatives []string
	for _, group := range torrentSearchGroups(query) {
		var terms []string
		for _, term := range group {
			var predicate string
			if term.phrase {
				query := "websearch_to_tsquery('simple'::regconfig," + bind(`"`+term.text+`"`) + ")"
				if term.excluded {
					query = "!!" + query
				}
				predicate = "search_document @@ " + query
			} else {
				predicate = torrentSearchTextSQL + " ILIKE " + bind("%"+escapeSearchFragment.Replace(term.text)+"%")
				// The merged search box retains complete base32 hash support while
				// preserving literal title/ID matches and the query's operators.
				if len(term.text) == 32 {
					if decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(term.text)); err == nil {
						predicate = "(" + predicate + " OR (search_info_hash IS NOT NULL AND search_info_hash=" + bind(hex.EncodeToString(decoded)) + "))"
					}
				}
				if term.excluded {
					predicate = "NOT (" + predicate + ")"
				}
			}
			terms = append(terms, predicate)
		}
		alternatives = append(alternatives, "("+strings.Join(terms, " AND ")+")")
	}
	if len(alternatives) == 0 {
		return "FALSE"
	}
	return "(" + strings.Join(alternatives, " OR ") + ")"
}
