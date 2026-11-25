package model

// Trending periods match the rolling windows exposed in the Trending view.
const (
	TrendingPeriodDay     = "day"
	TrendingPeriodWeek    = "week"
	TrendingPeriodMonth   = "month"
	TrendingPeriodQuarter = "quarter"
	TrendingPeriodYear    = "year"

	DefaultTrendingPeriod = TrendingPeriodWeek
	DefaultTrendingLimit  = 20
	MaxTrendingLimit      = 50
)

var trendingPeriodDays = map[string]int{
	TrendingPeriodDay:     1,
	TrendingPeriodWeek:    7,
	TrendingPeriodMonth:   30,
	TrendingPeriodQuarter: 90,
	TrendingPeriodYear:    365,
}

// TrendingCategories defines the canonical top-to-bottom category order.
var TrendingCategories = []string{"videos", "games", "software", "music", "books"}

func TrendingPeriodDays(period string) (int, bool) {
	days, ok := trendingPeriodDays[period]
	return days, ok
}

type CatalogTrendingOptions struct {
	Period   string
	Language string
	Limit    int
}

type CatalogTrending struct {
	Period     string                    `json:"period"`
	Categories []CatalogTrendingCategory `json:"categories"`
}

type CatalogTrendingCategory struct {
	ID    string                `json:"id"`
	Items []CatalogTrendingItem `json:"items"`
}

// CatalogTrendingItem contains only identified public metadata and the matched
// torrent count within the requested period. No source identities or payloads
// cross this boundary.
type CatalogTrendingItem struct {
	ID        int64    `json:"id"`
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Years     []int    `json:"years"`
	Cover     string   `json:"cover,omitempty"`
	Torrents  int64    `json:"torrents"`
	Artists   []string `json:"artists,omitempty"`
	Authors   []string `json:"authors,omitempty"`
	Vendor    string   `json:"vendor,omitempty"`
	MusicType string   `json:"music_type,omitempty"`
}
