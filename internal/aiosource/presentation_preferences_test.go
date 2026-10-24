package aiosource

import (
	"encoding/json"
	"github.com/theo-bnts/aiosources/internal/model"
	"strings"
	"testing"
)

func TestLanguagePreferencesUseAudioEvidenceAndCanBeDisabled(t *testing.T) {
	for _, tc := range []struct {
		title                     string
		native                    []string
		preferred, multi, unknown bool
	}{
		{"Example.Movie.2024.FRENCH", nil, true, true, true},
		{"Example.Movie.2024.MULTI", nil, false, true, true},
		{"Example.Movie.2024.1080p", nil, false, false, true},
		{"Example.Movie.2024.GERMAN", nil, false, false, false},
		{"Example.Movie.2024.VOSTFR", []string{"eng"}, false, false, false},
		{"Example.Movie.2024.1080p", []string{"fr-FR"}, true, true, true},
		{"Example.Movie.2024.1080p", []string{"und"}, false, false, true},
		{"French.Story.2024.ENGLISH", nil, false, false, false},
		{"Example.Movie.2024.1080p", []string{"not a language"}, false, false, true},
	} {
		rec := model.AIORecord{Title: tc.title, Languages: tc.native}
		for _, mode := range []string{"preferred", "multi", "unknown", "all"} {
			expected := mode == "all" || (mode == "preferred" && tc.preferred) || (mode == "multi" && tc.multi) || (mode == "unknown" && tc.unknown)
			if AllowsLanguage(rec, model.StreamPreferences{Language: "fr", Mode: mode}) != expected {
				t.Fatalf("%s / %v / %s", tc.title, tc.native, mode)
			}
		}
	}
	for _, bad := range []string{"key~fr~invalid", "key~~unknown", "key~fr-FR~preferred", "key~fr~all~other"} {
		if _, _, err := InstallationPreferences(bad); err == nil {
			t.Fatal("accepted invalid preference", bad)
		}
	}
	credential, p, err := InstallationPreferences("key~fr~unknown")
	if err != nil || credential != "key" || p.Language != "fr" || p.Mode != "unknown" {
		t.Fatal(credential, p, err)
	}
}

func TestFormatterPreservesStructuredMetadataAndOmitsMissingFields(t *testing.T) {
	r := model.AIORecord{Title: "Example.Film.2024.2160p.WEB-DL.H265.HDR10.DV.TrueHD.Atmos.7.1.EXTENDED.MULTI", Size: 10_000_000_000, DurationSeconds: 7200, Languages: []string{"fr"}}
	base := baseStream(r, Resource{Type: "movie"}, "Example source")
	yes := true
	out := playbackStream(base, r, "alldebrid", &yes, &model.AIOFile{Path: r.Title + ".mkv", Size: 9_000_000_000}, "https://files.example/video")
	for _, value := range []string{"2160P", "WEB-DL", "H265", "HDR10", "DV", "TRUEHD", "ATMOS", "7.1", "EXTENDED", "Mbps", "Example source"} {
		if !strings.Contains(out.Title, value) {
			t.Fatal("display metadata missing", value, out.Title)
		}
	}
	if !strings.Contains(out.Name, "Cached") || !strings.Contains(out.Name, "French") || out.Hints.Size != 9_000_000_000 || out.Hints.Service != "alldebrid" || out.Hints.FolderName != r.Title {
		t.Fatal(out)
	}
	empty := baseStream(model.AIORecord{Title: "Example"}, Resource{Type: "movie"}, "")
	if strings.Contains(empty.Title, "Unknown") || strings.Contains(empty.Title, "📦") || strings.Contains(empty.Name, "🌐") {
		t.Fatal("invented absent data", empty)
	}
	data, _ := json.Marshal(out)
	if strings.Contains(string(data), "seeders") || strings.Contains(string(data), "library") {
		t.Fatal("unsupported or unqueried metadata invented")
	}
}

func TestCatalogAndStremioShareLanguageFilterAndLargestFirstOffline(t *testing.T) {
	ctx, s, conn := testService(t)
	rows := []struct {
		id, lang string
		size     int64
	}{{"fr", "FRENCH", 300}, {"de", "GERMAN", 900}, {"multi", "MULTI", 500}, {"unknown", "1080p", 100}}
	for i, row := range rows {
		seedMovie(t, ctx, s, conn, "source", row.id, strings.Repeat(string(rune('a'+i)), 40))
		if _, err := conn.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(jsonb_set(fields,'{title}',to_jsonb($1::text)),'{size}',to_jsonb($2::bigint)) WHERE source_id=$3`, "Example.Movie.2021."+row.lang, row.size, row.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example"}, model.CatalogSearchFilters{})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	token, _ := testPrivateInstallation(t, ctx, s)
	for _, mode := range []string{"preferred", "multi", "unknown", "all"} {
		prefs := model.StreamPreferences{Language: "fr", Mode: mode}
		releases, err := s.CatalogReleases(ctx, page.Items[0].ID, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{StreamPreferences: prefs})
		if err != nil {
			t.Fatal(err)
		}
		setTestInstallationPreferences(t, ctx, s, token, prefs)
		streams, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
		if err != nil {
			t.Fatal(err)
		}
		expected := map[string]int{"preferred": 1, "multi": 2, "unknown": 3, "all": 4}[mode]
		if len(streams) != expected || int(releases.Total) != expected {
			t.Fatalf("%s: %d streams/%d releases", mode, len(streams), releases.Total)
		}
		for i := range streams {
			if streams[i].Hints.Size != releases.Items[i].Size {
				t.Fatal("search and stream order diverged", mode)
			}
			if i > 0 && streams[i-1].Hints.Size < streams[i].Hints.Size {
				t.Fatal("not largest first")
			}
		}
	}
	if !s.Authorized(ctx, token) || s.Authorized(ctx, token+"~fr~unknown") || s.Authorized(ctx, "invalid~fr~unknown") || s.Authorized(ctx, token+"~fr~invalid") {
		t.Fatal("installation preference changed authorization")
	}
	history, err := s.DB.ListStremioRequests(ctx, model.ListOptions{})
	if err != nil || history.Total != 4 {
		t.Fatal(history, err)
	}
	for _, item := range history.Items {
		if item.Matched-item.Eligible != item.LanguageExcluded {
			t.Fatal("language exclusions not explained", item)
		}
	}
}

func TestTMDBTranslationsKeepLanguageWithoutChangingAliases(t *testing.T) {
	body := strings.Replace(movieDetail, `"data":{"title":"Exemple Film"}`, `"iso_639_1":"fr","data":{"title":"Exemple Film"}`, 1)
	work, err := decodeWork("movie", "101", []byte(body))
	if err != nil || work.LocalizedTitles["fr"] != "Exemple Film" || work.LocalizedTitles["en"] != "Example Movie" {
		t.Fatal(work.LocalizedTitles, err)
	}
}

func TestNativePresentationUsesWhitelistedFieldsWithoutSubtitleURLs(t *testing.T) {
	ctx, s, conn := testService(t)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one','{"title":"Example.2024","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","resolution":2160,"codec":"HEVC","repack":true,"bitrate":15000000,"sublangs":["fr"],"subtitles":[{"lang":"en","url":"https://secret.invalid/private-subtitle-key"}],"release_group":"ExampleGroup","attributes":{"audiocodec":"TRUEHD","audiochannels":"7.1","hdr":"HDR10+","secret_key":"never-display"}}')`); err != nil {
		t.Fatal(err)
	}
	queued, err := s.DB.AIOMatchBatch(ctx, 1)
	if err != nil || len(queued) != 1 || queued[0].Presentation != nil {
		t.Fatal("presentation work leaked into the matching queue", err)
	}
	rec, err := s.DB.AIOTorrent(ctx, "source", "one")
	if err != nil {
		t.Fatal(err)
	}
	out := baseStream(rec, Resource{Type: "movie"}, "Example source")
	for _, field := range []string{"2160P", "HEVC", "REPACK", "TRUEHD", "7.1", "HDR10+", "FR/EN subtitles", "ExampleGroup"} {
		if !strings.Contains(out.Title, field) {
			t.Fatal("missing native presentation", field, out.Title)
		}
	}
	if strings.Contains(out.Title+out.Name, "private-subtitle-key") || strings.Contains(out.Title+out.Name, "never-display") || strings.Contains(out.Name, "🇫🇷") || strings.Contains(out.Name, "🇬🇧") {
		t.Fatal("private values or subtitle languages leaked into audio metadata")
	}
}
