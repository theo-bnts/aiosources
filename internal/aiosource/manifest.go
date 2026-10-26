package aiosource

const Version = "2.2.0"

// Client image loaders may not reach a private Docker hostname or pass an
// instance's access gateway. Keep the public brand asset independent of both
// the instance origin and its installation credentials.
const LogoURL = "https://raw.githubusercontent.com/theo-bnts/aiosources/main/web/public/logo.png"

func Manifest() map[string]any {
	return map[string]any{
		"id":          "org.aiosource.v2",
		"version":     Version,
		"name":        "AIOSources",
		"description": "Search your source catalogs with verified film and series identification, optional debrid playback, and Stremio integration",
		"logo":        LogoURL,
		"resources": []any{map[string]any{
			"name":       "stream",
			"types":      []string{"movie", "series", "anime"},
			"idPrefixes": []string{"tt", "imdb:", "tmdb:", "tvdb:", "mal:"},
		}},
		"types":         []string{"movie", "series", "anime"},
		"catalogs":      []any{},
		"behaviorHints": map[string]bool{"configurable": true},
	}
}
