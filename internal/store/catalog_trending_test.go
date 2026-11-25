package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogTrendingRanksCategoriesByPeriodAndRespectsPublicationRules(t *testing.T) {
	ctx, db := sharingDatabase(t)
	now := time.Now().UTC()

	workSeq := 0
	insertWork := func(kind, title, frTitle, poster, frPoster string, years []int, extraSQL string, extraArgs ...any) int64 {
		t.Helper()
		workSeq++
		var id int64
		var tmdb any
		igdb, ol, mb, sw := "", "", "", ""
		switch kind {
		case "movie", "tv":
			tmdb = fmt.Sprint(10000 + workSeq)
		case "game":
			igdb = fmt.Sprint(20000 + workSeq)
		case "book":
			ol = fmt.Sprintf("OL%dW", 30000+workSeq)
		case "music":
			mb = fmt.Sprintf("00000000-0000-4000-8000-%012d", 40000+workSeq)
		case "software":
			sw = fmt.Sprintf("winget:vendor.%d", 50000+workSeq)
		}
		localizedTitles := "{}"
		if frTitle != "" {
			localizedTitles = fmt.Sprintf(`{"fr":%q,"en":%q}`, frTitle, title)
		}
		localizedPosters := "{}"
		if frPoster != "" {
			localizedPosters = fmt.Sprintf(`{"fr":%q,"en":%q}`, frPoster, poster)
		}
		err := db.pool.QueryRow(ctx, `INSERT INTO aiosources.aio_works(
 media_type,tmdb_id,igdb_id,openlibrary_id,musicbrainz_id,software_key,
 display_title,localized_titles,poster_path,localized_posters,years
) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10::jsonb,$11) RETURNING id`,
			kind, tmdb, igdb, ol, mb, sw, title, localizedTitles, poster, localizedPosters, years,
		).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		if extraSQL != "" {
			args := append([]any{id}, extraArgs...)
			if _, err := db.pool.Exec(ctx, extraSQL, args...); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}

	seq := 0
	addTorrent := func(workID int64, age time.Duration, usePublishedAt bool, status string, staleRevision, queued bool) {
		t.Helper()
		seq++
		sourceID := fmt.Sprintf("t-%d", seq)
		ts := now.Add(-age)
		fields := fmt.Sprintf(`{"title":"Release %d","info_hash":"%040x"}`, seq, seq)
		if usePublishedAt {
			fields = fmt.Sprintf(`{"title":"Release %d","info_hash":"%040x","published_at":%q}`, seq, seq, ts.Format(time.RFC3339))
		}
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields,first_seen_at) VALUES('fixture',$1,$2::jsonb,$3)`, sourceID, fields, ts); err != nil {
			t.Fatal(err)
		}
		revOffset := 0
		if staleRevision {
			revOffset = -1
		}
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,$2,w.revision+$3,$4,'fixture'
 FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id=$2
 WHERE t.provider_id='fixture' AND t.source_id=$1`, sourceID, workID, revOffset, status); err != nil {
			t.Fatal(err)
		}
		if !queued {
			if _, err := db.pool.Exec(ctx, `DELETE FROM aiosources.aio_match_queue WHERE provider_id='fixture' AND source_id=$1`, sourceID); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Videos: combine movies and series.
	movieDay := insertWork("movie", "Daily Hit", "Succes du Jour", "/daily.jpg", "/daily_fr.jpg", []int{2026}, "")
	seriesWeek := insertWork("tv", "Weekly Series", "", "/weekly.jpg", "", []int{2025, 2026}, "")
	movieMonth := insertWork("movie", "Monthly Classic", "", "/monthly.jpg", "", []int{2024}, "")
	movieQuarter := insertWork("movie", "Quarter Blockbuster", "", "/quarter.jpg", "", []int{2023}, "")
	movieYear := insertWork("movie", "Yearly Legend", "", "/year.jpg", "", []int{2022}, "")
	movieTooOld := insertWork("movie", "Ancient Film", "", "/old.jpg", "", []int{2010}, "")
	movieAdult := insertWork("movie", "Hidden Adult", "", "/adult.jpg", "", []int{2026}, `UPDATE aiosources.aio_works SET adult=TRUE WHERE id=$1`)

	// Other 4 categories.
	gameWork := insertWork("game", "Pixel Quest", "", "co1234", "", []int{2026}, "")
	softwareWork := insertWork("software", "MediaPlayer", "", "vlc", "", []int{}, `UPDATE aiosources.aio_works SET software_data='{"vendor":"VideoLAN"}'::jsonb WHERE id=$1`)
	musicWork := insertWork("music", "Midnight Echo", "", "11111111-1111-4111-8111-111111111111", "", []int{2025}, `UPDATE aiosources.aio_works SET music_data='{"artists":["The Synths"],"primary_type":"Album"}'::jsonb WHERE id=$1`)
	bookWork := insertWork("book", "Starlight Atlas", "", "", "", []int{2024}, `UPDATE aiosources.aio_works SET book_data='{"cover":12345,"author_keys":["OL1A"]}'::jsonb WHERE id=$1`)
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.ol_authors(id,name,normalized) VALUES('OL1A','Ada Lovelace','ada lovelace')`); err != nil {
		t.Fatal(err)
	}

	// Daily Hit: 2 torrents in past 6 hours + 1 queued (ignored) + 1 unmatched (ignored) + 1 stale revision (ignored).
	addTorrent(movieDay, 3*time.Hour, true, "matched", false, false)
	addTorrent(movieDay, 5*time.Hour, false, "matched", false, false) // uses first_seen_at fallback
	addTorrent(movieDay, 2*time.Hour, true, "matched", false, true)   // queued -> ignored
	addTorrent(movieDay, 2*time.Hour, true, "unmatched", false, false)
	addTorrent(movieDay, 2*time.Hour, true, "matched", true, false)

	// Adult work with 10 torrents today -> must never appear.
	for range 10 {
		addTorrent(movieAdult, time.Hour, true, "matched", false, false)
	}

	// Weekly Series: 3 torrents 3 days ago (beats Daily Hit in week, absent in day).
	for range 3 {
		addTorrent(seriesWeek, 3*24*time.Hour, true, "matched", false, false)
	}
	// Monthly Classic: 4 torrents 15 days ago.
	for range 4 {
		addTorrent(movieMonth, 15*24*time.Hour, true, "matched", false, false)
	}
	// Quarter Blockbuster: 5 torrents 60 days ago.
	for range 5 {
		addTorrent(movieQuarter, 60*24*time.Hour, true, "matched", false, false)
	}
	// Yearly Legend: 6 torrents 200 days ago.
	for range 6 {
		addTorrent(movieYear, 200*24*time.Hour, true, "matched", false, false)
	}
	// Ancient Film: 20 torrents 400 days ago (outside year window).
	for range 20 {
		addTorrent(movieTooOld, 400*24*time.Hour, true, "matched", false, false)
	}

	// Populate other categories within the past day.
	addTorrent(gameWork, 4*time.Hour, true, "matched", false, false)
	addTorrent(softwareWork, 4*time.Hour, true, "matched", false, false)
	addTorrent(musicWork, 4*time.Hour, true, "matched", false, false)
	addTorrent(bookWork, 4*time.Hour, true, "matched", false, false)

	categoryMap := func(res model.CatalogTrending) map[string][]model.CatalogTrendingItem {
		out := map[string][]model.CatalogTrendingItem{}
		for _, cat := range res.Categories {
			out[cat.ID] = cat.Items
		}
		return out
	}

	// Verify canonical category order and French localization for day.
	dayRes, err := db.CatalogTrending(ctx, model.CatalogTrendingOptions{Period: "day", Language: "fr", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if dayRes.Period != "day" || len(dayRes.Categories) != 5 {
		t.Fatalf("unexpected day response: %+v", dayRes)
	}
	for i, want := range model.TrendingCategories {
		if dayRes.Categories[i].ID != want {
			t.Fatalf("category %d = %q, want %q", i, dayRes.Categories[i].ID, want)
		}
	}
	dayCats := categoryMap(dayRes)
	if len(dayCats["videos"]) != 1 || dayCats["videos"][0].ID != movieDay || dayCats["videos"][0].Torrents != 2 || dayCats["videos"][0].Title != "Succes du Jour" || dayCats["videos"][0].Cover != "https://image.tmdb.org/t/p/w342/daily_fr.jpg" {
		t.Fatalf("unexpected day videos: %+v", dayCats["videos"])
	}
	if len(dayCats["games"]) != 1 || dayCats["games"][0].ID != gameWork || dayCats["games"][0].Cover != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1234.jpg" {
		t.Fatalf("unexpected day games: %+v", dayCats["games"])
	}
	if len(dayCats["software"]) != 1 || dayCats["software"][0].Vendor != "VideoLAN" || dayCats["software"][0].Cover != "https://cdn.jsdelivr.net/gh/selfhst/icons@main/png/vlc.png" {
		t.Fatalf("unexpected day software: %+v", dayCats["software"])
	}
	if len(dayCats["music"]) != 1 || len(dayCats["music"][0].Artists) != 1 || dayCats["music"][0].Artists[0] != "The Synths" || dayCats["music"][0].MusicType != "Album" {
		t.Fatalf("unexpected day music: %+v", dayCats["music"])
	}
	if len(dayCats["books"]) != 1 || len(dayCats["books"][0].Authors) != 1 || dayCats["books"][0].Authors[0] != "Ada Lovelace" || dayCats["books"][0].Cover != "https://covers.openlibrary.org/b/id/12345-M.jpg?default=false" {
		t.Fatalf("unexpected day books: %+v", dayCats["books"])
	}

	// Verify ranking shifts across all 5 periods.
	for _, tc := range []struct {
		period   string
		wantTop  int64
		wantCnt  int64
		wantSize int
	}{
		{"day", movieDay, 2, 1},
		{"week", seriesWeek, 3, 2},
		{"month", movieMonth, 4, 3},
		{"quarter", movieQuarter, 5, 4},
		{"year", movieYear, 6, 5},
	} {
		res, err := db.CatalogTrending(ctx, model.CatalogTrendingOptions{Period: tc.period, Language: "en"})
		if err != nil {
			t.Fatalf("period %s: %v", tc.period, err)
		}
		videos := categoryMap(res)["videos"]
		if len(videos) != tc.wantSize || videos[0].ID != tc.wantTop || videos[0].Torrents != tc.wantCnt {
			t.Fatalf("period %s videos = %+v, want len=%d top=%d cnt=%d", tc.period, videos, tc.wantSize, tc.wantTop, tc.wantCnt)
		}
	}

	// Verify per-category limit cap.
	limited, err := db.CatalogTrending(ctx, model.CatalogTrendingOptions{Period: "year", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := categoryMap(limited)["videos"]; len(got) != 2 || got[0].ID != movieYear || got[1].ID != movieQuarter {
		t.Fatalf("limit cap failed: %+v", got)
	}

	// Verify invalid period and limits fail cleanly.
	for _, opts := range []model.CatalogTrendingOptions{
		{Period: "decade"},
		{Period: "week", Limit: -1},
		{Period: "week", Limit: 51},
	} {
		if _, err := db.CatalogTrending(ctx, opts); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("expected ErrInvalid for %+v, got %v", opts, err)
		}
	}
}
