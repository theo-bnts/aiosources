package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogCategoryPagesKeepGlobalRankingAndTotals(t *testing.T) {
	ctx, db := sharingDatabase(t)
	// Repeated ranking keys exercise the identifier tie-breaker across categories.
	for i := range 90 {
		kind := []string{"movie", "tv", "game", "book"}[i%4]
		var id int64
		var tmdb any
		igdb, openlibrary := "", ""
		switch kind {
		case "movie", "tv":
			tmdb = fmt.Sprint(1000 + i)
		case "game":
			igdb = fmt.Sprint(2000 + i)
		case "book":
			openlibrary = fmt.Sprintf("OL%dW", 3000+i)
		}
		if err := db.pool.QueryRow(ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,igdb_id,openlibrary_id,display_title,book_normalized,years)
VALUES($1,$2,$3,$4,'Example Journey','example journey',$5) RETURNING id`, kind, tmdb, igdb, openlibrary, []int{2000 + i%7}).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_titles(work_id,title,normalized) VALUES($1,'Example Journey',$2)`, id, []string{"example journey", "example journey home", "example journey"}[i%3]); err != nil {
			t.Fatal(err)
		}
		source := fmt.Sprint(id)
		publishSharingRow(t, ctx, db, "fixture", source, `{"title":"Example.Journey","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, nil)
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason,book_kind)
SELECT provider_id,source_id,fields_fingerprint,$1,1,'matched','fixture',$3 FROM aiosources.torrents WHERE source_id=$2`, id, source, []string{"ebook", "audiobook", "book"}[i%3]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM aiosources.aio_match_queue`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query               string
		year, limit, offset int
		families            []string
	}{
		{"example journey", 0, 25, 0, []string{"movie,tv", "game", "ebook,audiobook,book"}},
		{"example journey", 0, 25, 25, []string{"movie,tv", "game", "ebook,audiobook,book"}},
		{"example journey", 0, 50, 50, []string{"movie,tv", "game", "ebook,audiobook,book"}},
		{"example journey", 2004, 25, 0, []string{"movie", "ebook,audiobook"}},
		{"example journey", 0, 25, 100, []string{"movie,tv", "game", "ebook,audiobook,book"}},
		{"1000", 0, 25, 0, []string{"movie,tv", "game", "ebook,audiobook,book"}},
		{"no result", 0, 25, 0, []string{"movie,tv", "game", "ebook,audiobook,book"}},
	} {
		selection := strings.Join(tc.families, ",")
		global, err := db.CatalogTitles(ctx, model.ListOptions{Query: tc.query, Limit: tc.limit, Offset: tc.offset}, tc.query, model.CatalogSearchFilters{Type: selection, Year: tc.year, Language: "fr"})
		if err != nil {
			t.Fatal(err)
		}
		var items []model.CatalogTitle
		var total int64
		for _, family := range tc.families {
			page, err := db.CatalogTitles(ctx, model.ListOptions{Query: tc.query, Limit: tc.limit + tc.offset}, tc.query, model.CatalogSearchFilters{Type: family, Year: tc.year, Language: "fr"})
			if err != nil {
				t.Fatal(err)
			}
			items = append(items, page.Items...)
			total += page.Total
		}
		slices.SortFunc(items, func(a, b model.CatalogTitle) int {
			if a.Ranking == nil || b.Ranking == nil {
				t.Fatal("search omitted ranking")
			}
			if d := b.Ranking.Precision - a.Ranking.Precision; d != 0 {
				return d
			}
			if a.Ranking.Relevance != b.Ranking.Relevance {
				if a.Ranking.Relevance > b.Ranking.Relevance {
					return -1
				}
				return 1
			}
			if d := b.Ranking.Year - a.Ranking.Year; d != 0 {
				return d
			}
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		})
		items = items[min(tc.offset, len(items)):min(tc.offset+tc.limit, len(items))]
		if total != global.Total || len(items) != len(global.Items) {
			t.Fatal("category totals or page length differ", tc, total, global.Total)
		}
		for i, item := range items {
			if item.ID != global.Items[i].ID || *item.Ranking != *global.Items[i].Ranking {
				t.Fatal("category ordering differs from global query", tc, i)
			}
		}
	}
	var id int64
	if err := db.pool.QueryRow(ctx, `SELECT id FROM aiosources.aio_works WHERE media_type='book' ORDER BY id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.aio_works SET years='{}' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.ol_editions(id,work_keys,title,normalized,year) SELECT 'OL9999M',ARRAY[openlibrary_id],'Example Journey','example journey',2099 FROM aiosources.aio_works WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	page, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example journey"}, "example journey", model.CatalogSearchFilters{Type: "ebook,audiobook,book"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ID == id && (item.Ranking == nil || item.Ranking.Year != 0 || !slices.Equal(item.Years, []int{2099})) {
			t.Fatal("displayed edition year changed the global rank", item)
		}
	}
	detail, err := db.CatalogTitle(ctx, id)
	if err != nil || detail.Ranking != nil {
		t.Fatal("detail should not include search-only ordering keys", err)
	}
}

func TestBookFormatFiltersAreDisjointAndCombineWithoutLosingTitles(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for index, kind := range []string{"book", "ebook", "audiobook"} {
		id := []string{"OL100W", "OL200W", "OL300W"}[index]
		var workID int64
		if err := db.pool.QueryRow(ctx, `INSERT INTO aiosources.aio_works(media_type,openlibrary_id,display_title,book_normalized,years) VALUES('book',$1,'Example format','example format','{}') RETURNING id`, id).Scan(&workID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_titles(work_id,title,normalized) VALUES($1,'Example format','example format')`, workID); err != nil {
			t.Fatal(err)
		}
		publishSharingRow(t, ctx, db, "fixture", kind, `{"title":"Example format","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, nil)
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason,book_kind) SELECT provider_id,source_id,fields_fingerprint,$1,1,'matched','fixture',$2 FROM aiosources.torrents WHERE source_id=$2`, workID, kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM aiosources.aio_match_queue`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		selection string
		count     int64
	}{{"", 3}, {"book", 1}, {"ebook", 1}, {"audiobook", 1}, {"ebook,audiobook", 2}, {"book,ebook,audiobook", 3}} {
		page, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example"}, "example", model.CatalogSearchFilters{Type: tc.selection})
		if err != nil || page.Total != tc.count {
			t.Fatalf("format %q: total=%d: %v", tc.selection, page.Total, err)
		}
		if tc.selection == "book" && page.Items[0].OpenLibrary != "OL100W" {
			t.Fatal("Other books included a known digital or audio format")
		}
		var media []string
		if tc.selection != "" {
			media = strings.Split(tc.selection, ",")
		}
		feed, err := db.PublicTorznabCatalog(ctx, model.TorznabQuery{Limit: 50}, nil, media, TorznabVisibility{Public: true, UncachedSources: []string{"fixture"}, CacheAfter: time.Now().Add(-time.Hour)}, nil)
		if err != nil || feed.Total != tc.count || int64(len(feed.Items)) != tc.count {
			t.Fatalf("Torznab formats %q: %+v %v", tc.selection, feed, err)
		}
	}
}

func TestCatalogSearchOnlyPublishesCurrentIdentifications(t *testing.T) {
	ctx, db := sharingDatabase(t)
	work := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie: New Day", "Un autre titre"}, Years: []int{2026}}
	if err := db.SaveAIOWork(ctx, work, []string{"example movie new day", "un autre titre"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "changed", "revision", "queued"} {
		publishSharingRow(t, ctx, db, "source", id, `{"title":"Example.Movie.New.Day.2026","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","private_note":"synthetic-private-value"}`, nil)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,CASE WHEN source_id='changed' THEN 'old' ELSE fields_fingerprint END,(SELECT id FROM aiosources.aio_works),CASE WHEN source_id='revision' THEN 99 ELSE 1 END,'matched','fixture' FROM aiosources.torrents;
DELETE FROM aiosources.aio_match_queue WHERE source_id<>'queued';`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct{ raw, normalized string }{{"Example Movie New Day", "example movie new day"}, {"Movie", "movie"}, {"autre", "autre"}, {"tt1234567", "tt1234567"}, {"101", "101"}} {
		page, err := db.CatalogTitles(ctx, model.ListOptions{Query: q.raw}, q.normalized, model.CatalogSearchFilters{})
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Title != func() string {
			if q.raw == "autre" {
				return "Un autre titre"
			}
			return work.Titles[0]
		}() || page.Items[0].Releases != 2 {
			t.Fatalf("search %q: %+v %v", q.raw, page, err)
		}
		rows, err := db.CatalogRecords(ctx, page.Items[0].ID)
		if err != nil || len(rows) != 2 {
			t.Fatalf("current releases: %+v %v", rows, err)
		}
		for _, row := range rows {
			if !ValidOccurrenceID(row.OccurrenceID) || (row.SourceID != "one" && row.SourceID != "two") {
				t.Fatal("stale or unaddressable release published")
			}
		}
	}
	page, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example", Limit: 1, Offset: 1}, "example", model.CatalogSearchFilters{})
	if err != nil || page.Total != 1 || len(page.Items) != 0 {
		t.Fatalf("offset changed total: %+v %v", page, err)
	}
	page, err = db.CatalogTitles(ctx, model.ListOptions{Query: "example"}, "example", model.CatalogSearchFilters{Type: "tv"})
	if err != nil || page.Total != 0 {
		t.Fatal("media filter bypassed", err)
	}
	for _, kinds := range []string{"movie,tv", "movie,ebook", "movie,book", "movie,movie"} {
		page, err = db.CatalogTitles(ctx, model.ListOptions{Query: "example"}, "example", model.CatalogSearchFilters{Type: kinds})
		if err != nil || page.Total != 1 || len(page.Items) != 1 {
			t.Fatal("combined media selection lost or duplicated a title", kinds, err)
		}
	}
	page, err = db.CatalogTitles(ctx, model.ListOptions{Query: "private value"}, "private value", model.CatalogSearchFilters{})
	if err != nil || page.Total != 0 {
		t.Fatal("private fields were searchable", err)
	}
	for _, q := range []string{"", "x"} {
		if _, err := db.CatalogTitles(ctx, model.ListOptions{Query: q}, q, model.CatalogSearchFilters{}); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("short query accepted", err)
		}
	}
	var id int64
	if err := db.pool.QueryRow(ctx, `UPDATE aiosources.aio_works SET adult=true RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CatalogTitle(ctx, id); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("excluded work published", err)
	}
}

func TestDisplayTitleDoesNotInvalidateMatching(t *testing.T) {
	ctx, db := sharingDatabase(t)
	w := model.AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Original title", "Alternative title"}, Years: []int{2021}}
	if err := db.SaveAIOWork(ctx, w, []string{"original title", "alternative title"}); err != nil {
		t.Fatal(err)
	}
	w.DisplayTitle = "Alternative title"
	if err := db.SaveAIOWork(ctx, w, []string{"original title", "alternative title"}); err != nil {
		t.Fatal(err)
	}
	var title string
	var revision int
	if err := db.pool.QueryRow(ctx, `SELECT display_title,revision FROM aiosources.aio_works`).Scan(&title, &revision); err != nil || title != "Alternative title" || revision != 1 {
		t.Fatalf("presentation change invalidated matching: %q %d %v", title, revision, err)
	}
}

func TestCatalogSearchYearUsesKnownFilmDatesAndSeriesSpan(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, work := range []model.AIOWork{
		{Type: "movie", TMDB: "101", Titles: []string{"Example Film"}, Years: []int{2020, 2022}},
		{Type: "tv", TMDB: "202", Titles: []string{"Example Series"}, Years: []int{2018}, SeasonYears: map[int]int{2: 2020}, EpisodeYears: map[string]int{"2:8": 2022}},
		{Type: "movie", TMDB: "303", Titles: []string{"Example Undated"}, Years: []int{}},
	} {
		if err := db.SaveAIOWork(ctx, work, []string{"example " + work.Type}); err != nil {
			t.Fatal(err)
		}
		publishSharingRow(t, ctx, db, "source", work.TMDB, `{"title":"Example","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, nil)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture' FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.tmdb_id=t.source_id;DELETE FROM aiosources.aio_match_queue;`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		year int
		kind string
		want int64
	}{
		{0, "", 3}, {2020, "", 2}, {2021, "", 1}, {2022, "", 2},
		{2019, "tv", 1}, {2021, "movie", 0}, {2022, "tv", 1}, {2023, "", 0},
	} {
		page, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example", Limit: 1}, "example", model.CatalogSearchFilters{Type: tc.kind, Year: tc.year})
		if err != nil || page.Total != tc.want || len(page.Items) != min(1, int(tc.want)) {
			t.Fatalf("year %d type %q: %+v %v", tc.year, tc.kind, page, err)
		}
	}
	for _, year := range []int{-1, 999, 10000} {
		if _, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example"}, "example", model.CatalogSearchFilters{Year: year}); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("invalid year accepted", year, err)
		}
	}
}

func TestCatalogLocalTitlesUseBrowserQueryEnglishFallbackAndRelevance(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for i, w := range []model.AIOWork{
		{Type: "movie", TMDB: "101", Titles: []string{"Example Ocean", "Océan Exemple"}, Years: []int{2000}, LocalizedTitles: map[string]string{"en": "Example Ocean", "fr": "Océan Exemple"}},
		{Type: "movie", TMDB: "102", Titles: []string{"Example Ocean", "Océan Exemple"}, Years: []int{2026}, LocalizedTitles: map[string]string{"en": "Example Ocean", "fr": "Océan Exemple"}},
		{Type: "movie", TMDB: "103", Titles: []string{"Example Oceanic Adventure"}, Years: []int{2030}, LocalizedTitles: map[string]string{"en": "Example Oceanic Adventure"}},
		{Type: "movie", TMDB: "104", Titles: []string{"The Example Ocean"}, Years: []int{2040}, LocalizedTitles: map[string]string{"en": "The Example Ocean"}},
	} {
		normalized := []string{"example ocean", "ocean exemple"}
		if i == 2 {
			normalized = []string{"example oceanic adventure"}
		}
		if i == 3 {
			normalized = []string{"the example ocean"}
		}
		if err := db.SaveAIOWork(ctx, w, normalized); err != nil {
			t.Fatal(err)
		}
		publishSharingRow(t, ctx, db, "source", w.TMDB, `{"title":"Example","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, nil)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture' FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.tmdb_id=t.source_id;DELETE FROM aiosources.aio_match_queue;`); err != nil {
		t.Fatal(err)
	}
	page, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example ocean"}, "example ocean", model.CatalogSearchFilters{Language: "fr"})
	if err != nil || page.Total != 4 {
		t.Fatal(page, err)
	}
	if page.Items[0].TMDB != "102" || page.Items[1].TMDB != "101" || page.Items[2].TMDB != "104" || page.Items[3].TMDB != "103" || page.Items[0].Title != "Océan Exemple" {
		t.Fatal("relevance, year or browser title incorrect", page)
	}
	fallback, err := db.CatalogTitles(ctx, model.ListOptions{Query: "océan exemple"}, "ocean exemple", model.CatalogSearchFilters{Language: "ja"})
	if err != nil || fallback.Total != 2 || fallback.Items[0].Title != "Océan Exemple" {
		t.Fatal("query-language alias fallback lost", fallback, err)
	}
	detail, err := db.CatalogTitle(ctx, page.Items[0].ID, "ja")
	if err != nil || detail.Title != "Example Ocean" {
		t.Fatal("English fallback lost", detail, err)
	}
}
