package aiosource

import (
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Agreeing local identifiers share a cache check across Stremio ID aliases.
// Episode and season scopes never merge, especially absolute anime episodes.
func streamResourceKey(r Resource, rows []model.AIORecord) string {
	index := func(value *int) string {
		if value == nil {
			return "-"
		}
		return strconv.Itoa(*value)
	}
	scope := ":" + index(r.Season) + ":" + index(r.Episode)
	if r.Kind == "mal" {
		return "mal:" + r.ID + scope
	}
	typ := "series"
	if r.Type == "movie" {
		typ = "movie"
	}
	for _, kind := range []string{"imdb", "tmdb", "tvdb"} {
		ids := map[string]bool{}
		for _, row := range rows {
			value := map[string]string{"imdb": row.IMDb, "tmdb": row.TMDB, "tvdb": row.TVDB}[kind]
			if kind == "imdb" {
				value = strings.TrimPrefix(strings.ToLower(value), "tt")
			}
			if value == "" {
				continue
			}
			id, err := strconv.ParseUint(value, 10, 53)
			if err != nil || id == 0 {
				ids["invalid"] = true
				continue
			}
			ids[strconv.FormatUint(id, 10)] = true
		}
		if len(ids) == 1 && !ids["invalid"] {
			for id := range ids {
				return typ + ":" + kind + ":" + id + scope
			}
		}
	}
	return r.Type + ":" + r.Kind + ":" + r.ID + scope
}
