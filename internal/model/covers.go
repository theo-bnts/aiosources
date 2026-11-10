package model

import (
	"regexp"
	"strconv"
)

var tmdbPoster = regexp.MustCompile(`^/[A-Za-z0-9_-]{1,128}\.(?:jpg|png|webp)$`)
var igdbImage = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Construct image URLs from validated provider identifiers, never raw remote
// URLs. Browsers fetch these directly; the instance stores no image bytes.
func CoverURL(kind, poster string, cover int64) string {
	if kind == "game" && igdbImage.MatchString(poster) {
		return "https://images.igdb.com/igdb/image/upload/t_cover_big/" + poster + ".jpg"
	}
	if kind == "book" {
		if cover > 0 {
			return "https://covers.openlibrary.org/b/id/" + strconv.FormatInt(cover, 10) + "-M.jpg?default=false"
		}
		return ""
	}
	if (kind == "movie" || kind == "tv") && tmdbPoster.MatchString(poster) {
		return "https://image.tmdb.org/t/p/w342" + poster
	}
	return ""
}
