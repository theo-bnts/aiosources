package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
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

func TestTorrentEpisodeKeyExtraction(t *testing.T) {
	ctx, db := sharingDatabase(t)
	cases := []struct {
		name   string
		fields string
		want   string
	}{
		{"standard SxxExx", `{"title":"Severance.S02E05.1080p.WEB-DL.H264"}`, "e:2:5"},
		{"dotted Sxx.Exx", `{"title":"Severance.S02.E05.2160p"}`, "e:2:5"},
		{"underscored Sxx_Exx", `{"title":"Severance_S02_E05_720p"}`, "e:2:5"},
		{"dashed Sxx-Exx", `{"title":"Severance S02-E05 1080p"}`, "e:2:5"},
		{"multi-episode SxxExxExx", `{"title":"Severance.S02E05E06.1080p"}`, "e:2:5"},
		{"ranged SxxExx-Exx", `{"title":"Severance.S02E05-E06.1080p"}`, "e:2:5"},
		{"versioned SxxExxv2", `{"title":"Severance.S02E05v2.1080p"}`, "e:2:5"},
		{"ending marker SxxExxEND", `{"title":"Drama.S01E12END.1080p"}`, "e:1:12"},
		{"Sxx Ep yy", `{"title":"Show.S01.Ep.04.FRENCH.1080p"}`, "e:1:4"},
		{"Sxx Episode yy accented", `{"title":"Show.S01.Épisode.04.FRENCH.1080p"}`, "e:1:4"},
		{"special season 0", `{"title":"Show.S00E01.Special.1080p"}`, "e:0:1"},
		{"dual relative and absolute", `{"title":"Anime.S02E03.EP1158.1080p"}`, "e:2:3"},
		{"spelled Season Episode", `{"title":"Show Season 2 Episode 05 1080p"}`, "e:2:5"},
		{"spelled French Saison Episode", `{"title":"Show Saison 02 Épisode 05 FRENCH"}`, "e:2:5"},
		{"ordinal 2nd Season Episode", `{"title":"Anime 2nd Season Episode 03 1080p"}`, "e:2:3"},
		{"separated S01 - 04", `{"title":"Anime S01 - 04 1080p"}`, "e:1:4"},
		{"separated Season 1 - 04", `{"title":"Anime Season 1 - 04 1080p"}`, "e:1:4"},
		{"separated year is not episode", `{"title":"Show S01 - 2024 1080p"}`, "s:1"},
		{"pack range with COMPLETE", `{"title":"Show S01 - 03 COMPLETE 1080p"}`, "s:1-3"},
		{"cross format 1x02", `{"title":"Show.1x02.720p.HDTV"}`, "e:1:2"},
		{"cross format zero padded", `{"title":"Show.02x05.1080p"}`, "e:2:5"},
		{"resolution 1920x1080 ignored", `{"title":"Movie.2026.1920x1080.BluRay"}`, ""},
		{"resolution 640x480 ignored", `{"title":"Clip.640x480.x264"}`, ""},
		{"daily YYYY.MM.DD", `{"title":"The.Daily.Show.2026.03.15.1080p.WEB"}`, "e:d:2026-03-15"},
		{"daily YYYY-MM-DD", `{"title":"The Daily Show 2026-03-15 720p"}`, "e:d:2026-03-15"},
		{"daily DD.MM.YYYY", `{"title":"Quotidien.15.03.2026.FRENCH.1080p"}`, "e:d:2026-03-15"},
		{"daily with season prefix", `{"title":"Quotidien.S09.15.03.2026.1080p"}`, "e:d:2026-03-15"},
		{"5.1 audio after year not date", `{"title":"Film.2024.5.1.1080p.DDP5.1"}`, ""},
		{"anime absolute dash", `{"title":"[SubsPlease] One Piece - 1122 (1080p).mkv"}`, "e:a:1122"},
		{"anime multi-tag versioned dash", `{"title":"[FR][Team] Dandadan - 04v2 [1080p].mkv"}`, "e:a:4"},
		{"dash followed by year ignored", `{"title":"Movie - 2024 (1080p)"}`, ""},
		{"dash followed by 1080p ignored", `{"title":"Movie - 1080p BluRay"}`, ""},
		{"standalone Ep", `{"title":"Show Ep 14 1080p"}`, "e:a:14"},
		{"standalone E14", `{"title":"Show E14 1080p"}`, "e:a:14"},
		{"season and separated Ep", `{"title":"Show.S02.FRENCH.Ep.04.1080p"}`, "e:2:4"},
		{"Formula E year ignored", `{"title":"Formula E 2026 Highlights 1080p"}`, ""},
		{"word ending in e+digit ignored", `{"title":"Village2.2020.1080p"}`, ""},
		{"E-AC3 codec ignored", `{"title":"Movie.2026.E-AC3.5.1.1080p"}`, ""},
		{"single season pack S02", `{"title":"Show.S02.1080p.NF.WEB-DL"}`, "s:2"},
		{"multi-season pack S01-S05", `{"title":"Show.S01-S05.COMPLETE.1080p"}`, "s:1-5"},
		{"French season pack", `{"title":"Show.Saison.3.Intégrale.1080p"}`, "s:3"},
		{"ordinal season pack", `{"title":"Anime 2nd Season 1080p"}`, "s:2"},
		{"structured season and episode", `{"title":"Pilot 1080p","season":1,"episode":1}`, "e:1:1"},
		{"structured attributes array after null", `{"title":"Pilot","season":null,"episode":null,"attributes":{"season":["2"],"episode":["5"]}}`, "e:2:5"},
		{"title beats structured numbering", `{"title":"Show.S02E05.1080p","season":9,"episode":9}`, "e:2:5"},
		{"explicit pack ignores structured episode", `{"title":"Show.S01.COMPLETE.1080p","season":1,"episode":1}`, "s:1"},
		{"structured invalid values safe", `{"title":"Plain Movie 2026","season":"bad","episode":"0"}`, ""},
		{"fallback to name field", `{"title":"","name":"Fallback.S03E07.1080p"}`, "e:3:7"},
		{"empty object safe", `{}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			if err := db.pool.QueryRow(ctx, `SELECT aiosources.torrent_episode_key($1::jsonb)`, tc.fields).Scan(&got); err != nil {
				t.Fatalf("torrent_episode_key(%s): %v", tc.fields, err)
			}
			if got != tc.want {
				t.Fatalf("torrent_episode_key(%s) = %q, want %q", tc.fields, got, tc.want)
			}
		})
	}
}

func TestCatalogTrendingRanksTVSeriesByAverageTorrentsPerEpisode(t *testing.T) {
	ctx, db := sharingDatabase(t)
	now := time.Now().UTC()

	workSeq := 0
	insertWork := func(kind, title string, year int) int64 {
		t.Helper()
		workSeq++
		var id int64
		if err := db.pool.QueryRow(ctx, `INSERT INTO aiosources.aio_works(
 media_type,tmdb_id,display_title,years
) VALUES($1,$2,$3,$4) RETURNING id`, kind, fmt.Sprint(80000+workSeq), title, []int{year}).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	seq := 0
	addNamedTorrent := func(workID int64, title string, age time.Duration) string {
		t.Helper()
		seq++
		sourceID := fmt.Sprintf("tv-%d", seq)
		ts := now.Add(-age)
		payload, err := json.Marshal(map[string]any{
			"title":        title,
			"info_hash":    fmt.Sprintf("%040x", seq),
			"published_at": ts.Format(time.RFC3339),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields,first_seen_at) VALUES('fixture',$1,$2::jsonb,$3)`, sourceID, string(payload), ts); err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,$2,w.revision,'matched','fixture'
 FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id=$2
 WHERE t.provider_id='fixture' AND t.source_id=$1`, sourceID, workID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `DELETE FROM aiosources.aio_match_queue WHERE provider_id='fixture' AND source_id=$1`, sourceID); err != nil {
			t.Fatal(err)
		}
		return sourceID
	}

	movieBlockbuster := insertWork("movie", "Blockbuster Movie", 2026)
	movieMid := insertWork("movie", "Mid Movie", 2026)
	weeklyHit := insertWork("tv", "Weekly Hit", 2026)
	ongoingWithOldPacks := insertWork("tv", "Ongoing Hit", 2025)
	multiSeasonPackShow := insertWork("tv", "Archive Packs", 2020)
	dailySoap := insertWork("tv", "Daily Soap", 2024)
	reviewedSeries := insertWork("tv", "Reviewed Series", 2026)

	// Blockbuster movie: 8 torrents (score 8).
	for i := 1; i <= 8; i++ {
		addNamedTorrent(movieBlockbuster, fmt.Sprintf("Blockbuster.Movie.2026.Release%d.1080p", i), 2*24*time.Hour)
	}
	// Mid movie: 4 torrents (score 4).
	for i := 1; i <= 4; i++ {
		addNamedTorrent(movieMid, fmt.Sprintf("Mid.Movie.2026.Release%d.1080p", i), 2*24*time.Hour)
	}
	// Weekly Hit: 2 active episodes, 6 torrents each (12 total -> 6/ep), mixing naming formats for the same episodes.
	ep1Names := []string{
		"Weekly.Hit.S02E01.1080p",
		"Weekly.Hit.S02.E01.2160p",
		"Weekly.Hit.2x01.720p",
		"Weekly Hit Season 2 Episode 1 WEB-DL",
		"Weekly.Hit.S02.Ep.01.FRENCH",
		"Weekly.Hit.S02E01v2.REMUX",
	}
	for _, name := range ep1Names {
		addNamedTorrent(weeklyHit, name, 2*24*time.Hour)
	}
	for i := 1; i <= 6; i++ {
		addNamedTorrent(weeklyHit, fmt.Sprintf("Weekly.Hit.S02E02.Release%d.1080p", i), 24*time.Hour)
	}
	// Ongoing Hit: 2 new episodes x 5 torrents (10) + 4 old season packs (S01..S04) = 14 total -> ep_avg = 5/ep beats 14/4 = 3.5.
	for ep := 1; ep <= 2; ep++ {
		for r := 1; r <= 5; r++ {
			addNamedTorrent(ongoingWithOldPacks, fmt.Sprintf("Ongoing.Hit.S05E%02d.R%d.1080p", ep, r), 2*24*time.Hour)
		}
	}
	for s := 1; s <= 4; s++ {
		addNamedTorrent(ongoingWithOldPacks, fmt.Sprintf("Ongoing.Hit.S%02d.COMPLETE.1080p", s), 3*24*time.Hour)
	}
	// Archive Packs: 5 seasons x 2 packs (10) + 1 stray episode (1) = 11 total -> 11/5 = 2.2/season (no spike to 11/ep).
	for s := 1; s <= 5; s++ {
		addNamedTorrent(multiSeasonPackShow, fmt.Sprintf("Archive.Packs.S%02d.1080p.BluRay", s), 2*24*time.Hour)
		addNamedTorrent(multiSeasonPackShow, fmt.Sprintf("Archive.Packs.S%02d.720p.WEB", s), 2*24*time.Hour)
	}
	addNamedTorrent(multiSeasonPackShow, "Archive.Packs.S03E07.1080p", 2*24*time.Hour)

	// Daily Soap: 10 recent daily episodes x 2 torrents = 20 total (2/ep) + 5 older episodes 200 days ago (10 total, still 2/ep in year).
	for d := 1; d <= 10; d++ {
		addNamedTorrent(dailySoap, fmt.Sprintf("Daily.Soap.2026.03.%02d.720p", d), 2*24*time.Hour)
		addNamedTorrent(dailySoap, fmt.Sprintf("Daily.Soap.%02d.03.2026.1080p", d), 2*24*time.Hour)
	}
	for d := 1; d <= 5; d++ {
		addNamedTorrent(dailySoap, fmt.Sprintf("Daily.Soap.2025.10.%02d.720p", d), 200*24*time.Hour)
		addNamedTorrent(dailySoap, fmt.Sprintf("Daily.Soap.2025-10-%02d.1080p", d), 200*24*time.Hour)
	}

	// Reviewed Series: unnumbered titles with manual review overrides for S01E01 and S01E02 -> 2 torrents across 2 episodes (1/ep).
	rev1 := addNamedTorrent(reviewedSeries, "Reviewed Special Broadcast A", 2*24*time.Hour)
	rev2 := addNamedTorrent(reviewedSeries, "Reviewed Special Broadcast B", 2*24*time.Hour)
	for i, sid := range []string{rev1, rev2} {
		if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_match_reviews(
 provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,override_episode,season,episode
) SELECT t.provider_id,t.source_id,t.fields_fingerprint,md5(aiosources.aio_review_evidence(t.fields)::text),'tv',w.tmdb_id,TRUE,1,$3
 FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id=$2
 WHERE t.provider_id='fixture' AND t.source_id=$1`, sid, reviewedSeries, i+1); err != nil {
			t.Fatal(err)
		}
	}

	weekRes, err := db.CatalogTrending(ctx, model.CatalogTrendingOptions{Period: "week", Language: "en", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	videos := weekRes.Categories[0].Items
	wantOrder := []struct {
		id         int64
		torrents   int64
		episodes   int64
		seasons    int64
		perEpisode float64
		perSeason  float64
	}{
		{movieBlockbuster, 8, 0, 0, 0, 0},
		{weeklyHit, 12, 2, 0, 6, 0},
		{ongoingWithOldPacks, 14, 2, 4, 5, 0},
		{movieMid, 4, 0, 0, 0, 0},
		{multiSeasonPackShow, 11, 1, 5, 0, 2.2},
		{dailySoap, 20, 10, 0, 2, 0},
		{reviewedSeries, 2, 2, 0, 1, 0},
	}
	if len(videos) != len(wantOrder) {
		t.Fatalf("week videos count = %d, want %d: %+v", len(videos), len(wantOrder), videos)
	}
	for i, want := range wantOrder {
		got := videos[i]
		if got.ID != want.id || got.Torrents != want.torrents || got.Episodes != want.episodes || got.Seasons != want.seasons ||
			math.Abs(got.TorrentsPerEpisode-want.perEpisode) > 0.001 || math.Abs(got.TorrentsPerSeason-want.perSeason) > 0.001 {
			t.Fatalf("week videos[%d] = %+v, want %+v", i, got, want)
		}
	}

	// Year window counts the daily soap's 15 episodes active across the year (still 2/ep, still below movies & weekly hit).
	yearRes, err := db.CatalogTrending(ctx, model.CatalogTrendingOptions{Period: "year", Language: "en", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var soapYear *model.CatalogTrendingItem
	for i := range yearRes.Categories[0].Items {
		if yearRes.Categories[0].Items[i].ID == dailySoap {
			soapYear = &yearRes.Categories[0].Items[i]
			break
		}
	}
	if soapYear == nil || soapYear.Torrents != 30 || soapYear.Episodes != 15 || math.Abs(soapYear.TorrentsPerEpisode-2) > 0.001 {
		t.Fatalf("unexpected year daily soap stats: %+v", soapYear)
	}
}

func TestCatalogTrendingUsesIndexOnlyScanOnTrendingIndex(t *testing.T) {
	ctx, db := sharingDatabase(t)
	now := time.Now().UTC()

	// Seed enough rows across old and recent periods so the planner prefers the index.
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,display_title,years)
 SELECT CASE WHEN g%2=0 THEN 'tv' ELSE 'movie' END, (900000+g)::text, 'Perf Title ' || g, ARRAY[2026]
 FROM generate_series(1, 20) g`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields,first_seen_at)
 SELECT 'perf', 'p-' || g,
  jsonb_build_object(
   'title', CASE WHEN g%2=0 THEN format('Perf.Title.S01E%02s.1080p', (g%24)+1) ELSE format('Perf.Movie.%s.1080p', g) END,
   'info_hash', lpad(to_hex(g), 40, '0'),
   'published_at', to_char(($1::timestamptz - ((g%500) || ' days')::interval) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
  ),
  $1::timestamptz - ((g%500) || ' days')::interval
 FROM generate_series(1, 3000) g`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id, t.source_id, t.fields_fingerprint, w.id, w.revision, 'matched', 'perf'
 FROM aiosources.torrents t
 JOIN aiosources.aio_works w ON w.tmdb_id = (900001 + (abs(hashtext(t.source_id)) % 20))::text
 WHERE t.provider_id = 'perf'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM aiosources.aio_match_queue WHERE provider_id='perf'`); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`VACUUM ANALYZE aiosources.torrents`,
		`VACUUM ANALYZE aiosources.torrent_episode_keys`,
		`ANALYZE aiosources.aio_matches`,
		`ANALYZE aiosources.aio_works`,
		`ANALYZE aiosources.aio_match_queue`,
		`ANALYZE aiosources.aio_match_reviews`,
	} {
		if _, err := db.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	var planJSON string
	explain := `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
WITH matched_rows AS (
 SELECT m.work_id,m.revision,
  COALESCE(t.search_published_at,t.first_seen_at) AS published_at,
  CASE
   WHEN rev.provider_id IS NOT NULL THEN
    CASE
     WHEN rev.season IS NOT NULL AND rev.episode IS NOT NULL THEN 'e:' || rev.season::text || ':' || rev.episode::text
     WHEN rev.season IS NOT NULL THEN 's:' || rev.season::text
     ELSE ''
    END
   ELSE COALESCE(ek.episode_key,'')
  END AS ep_key
 FROM aiosources.torrents t
 JOIN aiosources.aio_matches m
  ON m.provider_id=t.provider_id
  AND m.source_id=t.source_id
  AND m.status='matched'
  AND m.work_id IS NOT NULL
  AND COALESCE(m.fingerprint=t.fields_fingerprint,false)
 LEFT JOIN aiosources.aio_match_queue q
  ON q.provider_id=t.provider_id
  AND q.source_id=t.source_id
 LEFT JOIN aiosources.torrent_episode_keys ek
  ON ek.provider_id=t.provider_id
  AND ek.source_id=t.source_id
 LEFT JOIN aiosources.aio_match_reviews rev
  ON rev.provider_id=t.provider_id
  AND rev.source_id=t.source_id
  AND rev.active
  AND rev.override_episode
 WHERE COALESCE(t.search_published_at,t.first_seen_at)>=now()-make_interval(days=>$1)
  AND q.provider_id IS NULL
),
per_key AS (
 SELECT work_id,revision,ep_key,count(*)::bigint AS torrents,max(published_at) AS latest_at
 FROM matched_rows
 GROUP BY work_id,revision,ep_key
),
period_counts AS (
 SELECT work_id,revision,
  sum(torrents)::bigint AS torrents,
  COALESCE(sum(torrents) FILTER (WHERE ep_key LIKE 'e:%'),0)::bigint AS episode_torrents,
  count(*) FILTER (WHERE ep_key LIKE 'e:%')::bigint AS episode_count,
  count(*) FILTER (WHERE ep_key LIKE 's:%')::bigint AS season_count,
  max(latest_at) AS latest_at
 FROM per_key
 GROUP BY work_id,revision
)
SELECT * FROM period_counts`
	if err := db.pool.QueryRow(ctx, explain, 7).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	// Verify specifically that the Index Only Scan node for torrents_trending_recent had 0 heap fetches.
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(planJSON), &plans); err != nil {
		t.Fatal(err)
	}
	foundTorrentsIOS := false
	var inspect func(node map[string]any)
	inspect = func(node map[string]any) {
		if node["Node Type"] == "Index Only Scan" && node["Index Name"] == "torrents_trending_recent" {
			foundTorrentsIOS = true
			if fetches, ok := node["Heap Fetches"].(float64); !ok || fetches != 0 {
				t.Fatalf("torrents_trending_recent Index Only Scan had Heap Fetches = %v, want 0:\n%s", node["Heap Fetches"], planJSON)
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				if m, ok := child.(map[string]any); ok {
					inspect(m)
				}
			}
		}
	}
	inspect(plans[0].Plan)
	if !foundTorrentsIOS {
		t.Fatalf("did not find Index Only Scan on torrents_trending_recent:\n%s", planJSON)
	}

	// Verify trigger updates and clears torrent_episode_keys cleanly when fields change or row is deleted.
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.torrents SET fields = jsonb_set(fields, '{title}', '"Perf.Title.S02E09.1080p"'::jsonb) WHERE provider_id='perf' AND source_id='p-2'`); err != nil {
		t.Fatal(err)
	}
	var updatedKey string
	if err := db.pool.QueryRow(ctx, `SELECT episode_key FROM aiosources.torrent_episode_keys WHERE provider_id='perf' AND source_id='p-2'`).Scan(&updatedKey); err != nil || updatedKey != "e:2:9" {
		t.Fatalf("trigger update key = %q, err=%v, want e:2:9", updatedKey, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.torrents SET fields = '{"title":"Plain Movie 2026"}'::jsonb WHERE provider_id='perf' AND source_id='p-2'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.torrent_episode_keys WHERE provider_id='perf' AND source_id='p-2'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("expected non-TV update to remove episode key row, got count=%d err=%v", remaining, err)
	}
	// Verify ResetSourceData truncates torrent_episode_keys cleanly alongside torrents.
	overview, err := db.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResetSourceData(ctx, overview.Settings.Revision); err != nil {
		t.Fatalf("ResetSourceData failed with torrent_episode_keys: %v", err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.torrent_episode_keys`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("expected ResetSourceData to empty torrent_episode_keys, got count=%d err=%v", remaining, err)
	}
}

func TestMigration106BackfillsExistingTorrentEpisodeKeys(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.migrateThrough(ctx, 105); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES
 ('pre','ep1','{"title":"PreUpgrade.Show.S03E07.1080p"}'),
 ('pre','movie1','{"title":"PreUpgrade.Movie.2026.1080p"}'),
 ('pre','pack1','{"title":"PreUpgrade.Show.S01-S02.COMPLETE.1080p"}')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.pool.Query(ctx, `SELECT source_id, episode_key FROM aiosources.torrent_episode_keys WHERE provider_id='pre' ORDER BY source_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			t.Fatal(err)
		}
		got[id] = key
	}
	if len(got) != 2 || got["ep1"] != "e:3:7" || got["pack1"] != "s:1-2" {
		t.Fatalf("unexpected migration 106 backfill keys: %+v", got)
	}
}
