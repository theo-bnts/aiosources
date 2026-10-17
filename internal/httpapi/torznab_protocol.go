package httpapi

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
)

const torznabNamespace = "http://torznab.com/schemas/2015/feed"

func torznabQuery(raw string) (model.TorznabQuery, url.Values, error) {
	q := model.TorznabQuery{Limit: 100}
	if len(raw) > 8192 {
		return q, nil, model.ErrInvalid
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return q, nil, model.ErrInvalid
	}
	v := url.Values{}
	for key, items := range values {
		key = strings.ToLower(key)
		if len(items) != 1 || v.Has(key) {
			return q, nil, model.ErrInvalid
		}
		switch key {
		case "t", "apikey", "q", "cat", "sources", "media", "imdbid", "tmdbid", "tvdbid", "season", "ep", "year", "limit", "offset", "extended", "attrs", "o", "minage", "maxage", "minsize", "maxsize", "services", "cached_only":
		default:
			return q, nil, model.ErrInvalid
		}
		v[key] = items
	}
	q.Mode = strings.ToLower(v.Get("t"))
	if v.Has("media") {
		q.MediaTypes = strings.Split(v.Get("media"), ",")
		for _, typ := range q.MediaTypes {
			if !slices.Contains([]string{"movie", "tv", "other"}, typ) {
				return q, v, model.ErrInvalid
			}
		}
	}
	if v.Has("services") {
		q.Services = strings.Split(v.Get("services"), ",")
		for _, service := range q.Services {
			if service != "alldebrid" && service != "torbox" {
				return q, v, model.ErrInvalid
			}
		}
	}
	if v.Has("cached_only") {
		q.CachedOnly, err = strconv.ParseBool(v.Get("cached_only"))
		if err != nil {
			return q, v, model.ErrInvalid
		}
	}
	q.Query = v.Get("q")
	if !slices.Contains([]string{"caps", "search", "movie", "tvsearch"}, q.Mode) {
		return q, v, model.ErrNotFound
	}
	if v.Has("o") && !strings.EqualFold(v.Get("o"), "xml") {
		return q, v, model.ErrInvalid
	}
	if v.Has("extended") && !slices.Contains([]string{"0", "1", "true", "false", "yes", "no"}, strings.ToLower(v.Get("extended"))) {
		return q, v, model.ErrInvalid
	}
	if len(v.Get("attrs")) > 500 {
		return q, v, model.ErrInvalid
	}
	for _, r := range v.Get("attrs") {
		if !unicode.IsLetter(r) && r != ',' {
			return q, v, model.ErrInvalid
		}
	}
	integer := func(key string, max int) (int, error) {
		raw := v.Get(key)
		if raw == "" || len(raw) > 10 {
			return 0, model.ErrInvalid
		}
		for _, r := range raw {
			if r < '0' || r > '9' {
				return 0, model.ErrInvalid
			}
		}
		n, e := strconv.ParseUint(raw, 10, 31)
		if e != nil || n > uint64(max) {
			return 0, model.ErrInvalid
		}
		return int(n), nil
	}
	for _, f := range []struct {
		key string
		dst *int
		max int
	}{{"limit", &q.Limit, 2147483647}, {"offset", &q.Offset, 2147483647}, {"year", &q.Year, 9999}} {
		if v.Has(f.key) {
			*f.dst, err = integer(f.key, f.max)
			if err != nil {
				return q, v, err
			}
		}
	}
	q.Limit = min(q.Limit, 200)
	if v.Has("year") && q.Year < 1000 {
		return q, v, model.ErrInvalid
	}
	for _, f := range []struct {
		key string
		dst **int
		max int
	}{{"season", &q.Season, 999}, {"ep", &q.Episode, 99999}} {
		if v.Has(f.key) {
			n, e := integer(f.key, f.max)
			if e != nil {
				return q, v, e
			}
			*f.dst = &n
		}
	}
	if (q.Season != nil || q.Episode != nil) && (q.Mode != "tvsearch" || (q.Episode != nil && q.Season == nil)) {
		return q, v, model.ErrInvalid
	}
	if v.Has("cat") {
		q.Categories = []int{}
		for _, cat := range strings.Split(v.Get("cat"), ",") {
			if cat == "" || len(cat) > 10 {
				return q, v, model.ErrInvalid
			}
			n, e := strconv.ParseUint(cat, 10, 31)
			if e != nil {
				return q, v, model.ErrInvalid
			}
			if !slices.Contains(q.Categories, int(n)) {
				q.Categories = append(q.Categories, int(n))
			}
		}
	}
	if v.Has("sources") {
		q.Providers = strings.Split(v.Get("sources"), ",")
		for _, id := range q.Providers {
			if !providers.ValidID(id) {
				return q, v, model.ErrInvalid
			}
		}
	}
	for _, f := range []struct {
		key string
		dst *string
	}{{"imdbid", &q.IMDb}, {"tmdbid", &q.TMDB}, {"tvdbid", &q.TVDB}} {
		if v.Has(f.key) {
			raw := v.Get(f.key)
			if f.key == "imdbid" {
				raw = strings.TrimPrefix(strings.ToLower(raw), "tt")
			}
			if len(raw) < 1 || len(raw) > 16 {
				return q, v, model.ErrInvalid
			}
			for _, r := range raw {
				if r < '0' || r > '9' {
					return q, v, model.ErrInvalid
				}
			}
			n, e := strconv.ParseUint(raw, 10, 53)
			if e != nil || n == 0 {
				return q, v, model.ErrInvalid
			}
			if f.key == "imdbid" {
				*f.dst = "tt" + raw
			} else {
				*f.dst = strconv.FormatUint(n, 10)
			}
		}
	}
	for _, f := range []struct {
		key string
		dst *string
		age bool
	}{{"minsize", &q.MinSize, false}, {"maxsize", &q.MaxSize, false}, {"minage", &q.PublishedBefore, true}, {"maxage", &q.PublishedAfter, true}} {
		if v.Has(f.key) {
			maximum := 2147483647
			if f.age {
				maximum = 365000
			}
			n, e := integer(f.key, maximum)
			if e != nil {
				return q, v, e
			}
			if f.age {
				*f.dst = time.Now().UTC().AddDate(0, 0, -n).Format(time.RFC3339Nano)
			} else {
				*f.dst = strconv.FormatInt(int64(n)*1024*1024, 10)
			}
		}
	}
	q.SearchFilters, err = store.NormalizeSearchFilters(q.SearchFilters)
	if (q.Season != nil || q.Episode != nil) && q.Query == "" && q.IMDb == "" && q.TMDB == "" && q.TVDB == "" {
		return q, v, model.ErrInvalid
	}
	return q, v, err
}

func torznabError(w http.ResponseWriter, status, code int, message string) {
	value := struct {
		XMLName     xml.Name `xml:"error"`
		Code        int      `xml:"code,attr"`
		Description string   `xml:"description,attr"`
	}{Code: code, Description: message}
	body, _ := xml.Marshal(value)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append([]byte(xml.Header), body...))
}

type torznabCapability struct {
	XMLName   xml.Name
	Available string `xml:"available,attr"`
	Params    string `xml:"supportedParams,attr,omitempty"`
}
type torznabCategory struct {
	ID   int    `xml:"id,attr"`
	Name string `xml:"name,attr"`
}
type torznabCapabilities struct {
	XMLName xml.Name `xml:"caps"`
	Server  struct {
		Version string `xml:"version,attr"`
		Title   string `xml:"title,attr"`
		URL     string `xml:"url,attr"`
	} `xml:"server"`
	Limits struct {
		Max     int `xml:"max,attr"`
		Default int `xml:"default,attr"`
	} `xml:"limits"`
	Modes      []torznabCapability `xml:"searching>mode"`
	Categories []torznabCategory   `xml:"categories>category"`
}

func torznabCaps(share model.Share, origin string) torznabCapabilities {
	v := torznabCapabilities{}
	v.Server.Version = "1.3"
	v.Server.Title = "AIOSources"
	v.Server.URL = origin
	v.Limits.Max = 200
	v.Limits.Default = 100
	allowed := func(typ string) bool { return len(share.MediaTypes) == 0 || slices.Contains(share.MediaTypes, typ) }
	for _, mode := range []struct{ name, params, typ string }{{"search", "q", ""}, {"tv-search", "q,imdbid,tmdbid,tvdbid,season,ep", "tv"}, {"movie-search", "q,imdbid,tmdbid,year", "movie"}, {"audio-search", "", "unsupported"}, {"book-search", "", "unsupported"}} {
		available := "no"
		if mode.typ != "unsupported" && (mode.typ == "" || allowed(mode.typ)) {
			available = "yes"
		}
		v.Modes = append(v.Modes, torznabCapability{XMLName: xml.Name{Local: mode.name}, Available: available, Params: mode.params})
	}
	for _, cat := range []struct {
		typ, name string
		id        int
	}{{"movie", "Movies", 2000}, {"tv", "TV", 5000}, {"other", "Other", 8000}} {
		if allowed(cat.typ) {
			v.Categories = append(v.Categories, torznabCategory{ID: cat.id, Name: cat.name})
		}
	}
	return v
}

type torznabAttribute struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
type torznabItem struct {
	Title string `xml:"title"`
	GUID  struct {
		Value     string `xml:",chardata"`
		Permalink bool   `xml:"isPermaLink,attr"`
	} `xml:"guid"`
	Link      string `xml:"link"`
	Date      string `xml:"pubDate,omitempty"`
	Size      int64  `xml:"size"`
	Category  int    `xml:"category"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	Attributes []torznabAttribute `xml:"torznab:attr"`
}
type torznabRSS struct {
	XMLName   xml.Name `xml:"rss"`
	Version   string   `xml:"version,attr"`
	Namespace string   `xml:"xmlns:torznab,attr"`
	Channel   struct {
		Title       string `xml:"title"`
		Description string `xml:"description"`
		Link        string `xml:"link"`
		Response    struct {
			Offset int   `xml:"offset,attr"`
			Total  int64 `xml:"total,attr"`
		} `xml:"torznab:response"`
		Items []torznabItem `xml:"item"`
	} `xml:"channel"`
}

func torznabResults(result model.List[model.TorznabRecord], names map[string]string, origin string, visitorAnnounces ...map[string]string) torznabRSS {
	v := torznabRSS{Version: "2.0", Namespace: torznabNamespace}
	v.Channel.Title = "AIOSources"
	v.Channel.Description = "Published torrents"
	v.Channel.Link = origin
	v.Channel.Response.Total = result.Total
	v.Channel.Response.Offset = result.Offset
	for _, rec := range result.Items {
		item := torznabItem{Title: rec.Title, Size: rec.Size, Category: rec.Category}
		item.GUID.Value = "urn:aiosources:" + rec.OccurrenceID
		// Rebuild from a validated hash. Never forward native magnets or URLs:
		// those can contain a source API key or an administrator's announce URL.
		magnet := "magnet:?xt=urn:btih:" + rec.Hash + "&dn=" + strings.ReplaceAll(url.QueryEscape(rec.Title), "+", "%20")
		if len(visitorAnnounces) > 0 {
			if announce := visitorAnnounces[0][rec.Provider]; announce != "" {
				magnet += "&tr=" + strings.ReplaceAll(url.QueryEscape(announce), "+", "%20")
			}
		}
		item.Link = magnet
		item.Enclosure.URL = magnet
		item.Enclosure.Length = rec.Size
		item.Enclosure.Type = "application/x-bittorrent"
		if rec.PublishedAt != nil {
			item.Date = rec.PublishedAt.UTC().Format(time.RFC1123Z)
		}
		attr := func(name, value string) {
			if value != "" {
				item.Attributes = append(item.Attributes, torznabAttribute{Name: name, Value: value})
			}
		}
		attr("size", strconv.FormatInt(rec.Size, 10))
		attr("category", strconv.Itoa(rec.Category))
		attr("infohash", rec.Hash)
		attr("magneturl", magnet)
		attr("imdb", strings.TrimPrefix(rec.IMDb, "tt"))
		attr("tmdbid", rec.TMDB)
		attr("tvdbid", rec.TVDB)
		if rec.Category == 5000 {
			season, episode := rec.Season, rec.Episode
			if !rec.ReviewedEpisode {
				parsed := aiosource.ParseRelease(rec.Title)
				if !parsed.InvalidNumbering {
					if season == nil && len(parsed.Seasons) == 1 {
						season = &parsed.Seasons[0]
					}
					if episode == nil && len(parsed.Episodes) == 1 {
						episode = &parsed.Episodes[0]
					}
				}
			}
			if season != nil {
				attr("season", strconv.Itoa(*season))
			}
			if episode != nil {
				attr("episode", strconv.Itoa(*episode))
			}
		}
		for _, language := range aiosource.ReleaseLanguageCodes(rec.AIORecord) {
			attr("language", language)
		}
		attr("indexer", names[rec.Provider])
		v.Channel.Items = append(v.Channel.Items, item)
	}
	return v
}
