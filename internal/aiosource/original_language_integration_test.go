package aiosource

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestOriginalLanguageTMDBSyncAndVOSTFRStreamFilteringAndPlayback(t *testing.T) {
	ctx, s, conn := testService(t)

	// 1. Sync a Japanese movie from TMDB and confirm original_language is stored without bumping revision on refresh.
	var body map[string]any
	if err := json.Unmarshal([]byte(movieDetail), &body); err != nil {
		t.Fatal(err)
	}
	body["original_language"] = "ja"
	body["title"] = "Spirited Example"
	body["original_title"] = "Spirited Example"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.DB.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected 1 metadata job, got %d (%v)", len(jobs), err)
	}
	if ok, err := s.details(ctx, "synthetic-key", jobs[0]); !ok || err != nil {
		t.Fatalf("metadata sync failed: ok=%v err=%v", ok, err)
	}
	var storedLang string
	var revisionBefore int64
	if err := conn.QueryRow(ctx, `SELECT original_language,revision FROM aiosources.aio_works WHERE media_type='movie' AND tmdb_id='101'`).Scan(&storedLang, &revisionBefore); err != nil || storedLang != "ja" {
		t.Fatalf("expected stored original_language='ja', got %q (err=%v)", storedLang, err)
	}

	// Updating original_language alone must not advance work revision.
	if _, err := conn.Exec(ctx, `UPDATE aiosources.aio_works SET original_language='' WHERE media_type='movie' AND tmdb_id='101'`); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	jobs, err = s.DB.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected refresh job, got %d (%v)", len(jobs), err)
	}
	if ok, err := s.details(ctx, "synthetic-key", jobs[0]); !ok || err != nil {
		t.Fatalf("metadata refresh failed: ok=%v err=%v", ok, err)
	}
	var revisionAfter int64
	if err := conn.QueryRow(ctx, `SELECT original_language,revision FROM aiosources.aio_works WHERE media_type='movie' AND tmdb_id='101'`).Scan(&storedLang, &revisionAfter); err != nil || storedLang != "ja" || revisionAfter != revisionBefore {
		t.Fatalf("original_language refresh changed revision or lost value: lang=%q rev=%d->%d err=%v", storedLang, revisionBefore, revisionAfter, err)
	}

	// 2. Also insert a French movie to verify ENGSUB/VOSTA infers French audio.
	if err := s.DB.SaveAIOWork(ctx, model.AIOWork{
		Type:             "movie",
		TMDB:             "202",
		IMDb:             "tt2020202",
		DisplayTitle:     "Atelier Example",
		Titles:           []string{"Atelier Example"},
		Years:            []int{2024},
		OriginalLanguage: "fr",
	}, []string{"atelier example"}); err != nil {
		t.Fatal(err)
	}

	insert := func(provider, sourceID string, fields map[string]any) {
		t.Helper()
		b, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES($1,$2,$3)`, provider, sourceID, b); err != nil {
			t.Fatal(err)
		}
	}

	// Japanese movie in VOSTFR (no audio tag) + bare untagged torrent (must remain unknown).
	insert("src", "ja-vostfr", map[string]any{
		"title":     "Spirited.Example.2021.VOSTFR.1080p.BluRay.x264-GRP",
		"info_hash": strings.Repeat("a", 40),
		"size":      int64(4_123_456_789),
		"tmdb_id":   "101",
	})
	insert("src", "ja-bare", map[string]any{
		"title":     "Spirited.Example.2021.1080p.BluRay.x264-GRP",
		"info_hash": strings.Repeat("b", 40),
		"size":      int64(4_987_654_321),
		"tmdb_id":   "101",
	})
	// French movie in ENGSUB (no audio tag).
	insert("src", "fr-engsub", map[string]any{
		"title":     "Atelier.Example.2024.1080p.BluRay.EngSub.x264-GRP",
		"info_hash": strings.Repeat("c", 40),
		"size":      int64(3_654_321_987),
		"tmdb_id":   "202",
	})

	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify strict Japanese preference finds ja-vostfr (not ja-bare) and renders Japanese audio + French subtitles.
	tokenJA, err := testPrivateInstallation(t, ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	setTestInstallationPreferences(t, ctx, s, tokenJA, model.StreamPreferences{Language: "ja", Mode: "preferred"})
	jaStreams, err := s.Streams(ctx, "http://localhost", tokenJA, "movie", "tmdb:101")
	if err != nil {
		t.Fatal(err)
	}
	if len(jaStreams) != 1 || jaStreams[0].InfoHash != strings.Repeat("a", 40) {
		t.Fatalf("expected only ja-vostfr for preferred=ja, got %+v", jaStreams)
	}
	if !strings.Contains(jaStreams[0].Name, "Japanese") || !strings.Contains(jaStreams[0].Name, "🇯🇵") || !strings.Contains(jaStreams[0].Name, "French subtitles") {
		t.Fatalf("unexpected stream presentation for VOSTFR Japanese movie: %q", jaStreams[0].Name)
	}

	// Verify AIOTorrent also loads OriginalLanguage so Stremio Playback authorization matches Streams!
	playbackRec, err := s.DB.AIOTorrent(ctx, "src", "ja-vostfr")
	if err != nil {
		t.Fatal(err)
	}
	if playbackRec.OriginalLanguage != "ja" || !AllowsLanguage(playbackRec, model.StreamPreferences{Language: "ja", Mode: "preferred"}) {
		t.Fatalf("AIOTorrent missing OriginalLanguage for playback authorization: %+v", playbackRec)
	}

	// Verify strict French preference finds fr-engsub (French film with English subs)
	// and does NOT return ja-vostfr even in 'unknown' mode because ja-vostfr's audio is known as Japanese.
	tokenFR, err := testPrivateInstallation(t, ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	setTestInstallationPreferences(t, ctx, s, tokenFR, model.StreamPreferences{Language: "fr", Mode: "preferred"})
	frStreams, err := s.Streams(ctx, "http://localhost", tokenFR, "movie", "tt2020202")
	if err != nil {
		t.Fatal(err)
	}
	if len(frStreams) != 1 || !strings.Contains(frStreams[0].Name, "French") || !strings.Contains(frStreams[0].Name, "English subtitles") {
		t.Fatalf("expected French audio + English subtitles for French film ENGSUB, got %+v", frStreams)
	}

	tokenFRUnknown, err := testPrivateInstallation(t, ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	setTestInstallationPreferences(t, ctx, s, tokenFRUnknown, model.StreamPreferences{Language: "fr", Mode: "unknown"})
	jaUnderFRUnknown, err := s.Streams(ctx, "http://localhost", tokenFRUnknown, "movie", "tmdb:101")
	if err != nil {
		t.Fatal(err)
	}
	if len(jaUnderFRUnknown) != 1 || jaUnderFRUnknown[0].InfoHash != strings.Repeat("b", 40) {
		t.Fatalf("expected only bare unknown torrent under fr+unknown (not ja-vostfr), got %+v", jaUnderFRUnknown)
	}

	// Verify CatalogRecords and direct CatalogTorrent reflect OriginalLanguage.
	var jaWorkID int64
	if err := conn.QueryRow(ctx, `SELECT id FROM aiosources.aio_works WHERE media_type='movie' AND tmdb_id='101'`).Scan(&jaWorkID); err != nil {
		t.Fatal(err)
	}
	catRecords, err := s.DB.CatalogRecords(ctx, jaWorkID)
	if err != nil || len(catRecords) != 2 {
		t.Fatalf("CatalogRecords: len=%d err=%v", len(catRecords), err)
	}
	for _, rec := range catRecords {
		if rec.SourceID == "ja-vostfr" && !slices.Equal(ReleaseLanguageCodes(rec.AIORecord), []string{"ja"}) {
			t.Fatalf("CatalogRecords missing inferred ja audio: %v", ReleaseLanguageCodes(rec.AIORecord))
		}
	}
	directRec, err := s.DB.CatalogSourceTorrent(ctx, "src", "ja-vostfr")
	if err != nil || !slices.Equal(ReleaseLanguageCodes(directRec.AIORecord), []string{"ja"}) {
		t.Fatalf("CatalogSourceTorrent missing inferred ja audio: %v (err=%v)", ReleaseLanguageCodes(directRec.AIORecord), err)
	}

	// Verify CatalogWork / Work round-trip preserves original_language for shared catalogs.
	meta := model.CatalogWork(model.AIOWork{
		Type: "movie", TMDB: "101", DisplayTitle: "Spirited Example",
		Titles: []string{"Spirited Example"}, Years: []int{2024}, OriginalLanguage: "ja",
	}, time.Now().UTC())
	if !meta.Valid(time.Now().UTC()) || meta.Work().OriginalLanguage != "ja" {
		t.Fatalf("CatalogMetadata did not preserve valid original_language: %+v", meta)
	}
}
