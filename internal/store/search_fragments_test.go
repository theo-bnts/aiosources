package store

import (
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestTorrentSearchFragmentsAndQueryOperators(t *testing.T) {
	ctx, db := sharingDatabase(t)
	const fullHash = "00112233445566778899aabbccddeeff00112233"
	decoded, _ := hex.DecodeString(fullHash)
	base32Hash := base32.StdEncoding.EncodeToString(decoded)
	for _, row := range []struct{ provider, id, title, hash string }{
		{"fixture", "release-8394612", "Matrix.Regained.2024.1080p.MULTi.WEB.H265", "00112233445566778899aabbccddeeff00112233"},
		{"remote-example", "release-8394613", "Matrix.Regained.2024.1080p.CAM", ""},
		{"fixture", "blue", "Blue Ocean 2024", ""},
		{"fixture", "reverse", "Ocean Blue 2024", ""},
		{"fixture", "separated", "Blue Deep Ocean 2024", ""},
		{"fixture", "unicode", "ÉCLAIR.宇宙.2024", ""},
		{"fixture", "literal", `Release.100%_done\cut`, ""},
		{"fixture", "decoy", "Release.100XdoneYcut", ""},
	} {
		fields, err := json.Marshal(map[string]any{"title": row.title, "info_hash": row.hash, "private_note": "hidden-only-text"})
		if err != nil {
			t.Fatal(err)
		}
		publishSharingRow(t, ctx, db, row.provider, row.id, string(fields), nil)
	}
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"ATRIX", []string{"release-8394612", "release-8394613"}},
		{"[ATRIX]: (WEB)", []string{"release-8394612"}},
		{"ATRIX : WEB", []string{"release-8394612"}},
		{"gain WEB", []string{"release-8394612"}},
		{"matrix regained", []string{"release-8394612", "release-8394613"}},
		{"matrix-regained", []string{"release-8394612", "release-8394613"}},
		{"matrix.regained", []string{"release-8394612", "release-8394613"}},
		{"gain H26", []string{"release-8394612"}},
		{"release-839461", []string{"release-8394612", "release-8394613"}},
		{"mote-examp", []string{"release-8394613"}},
		{"778899aabb", []string{"release-8394612"}},
		{fullHash, []string{"release-8394612"}},
		{strings.ToLower(base32Hash), []string{"release-8394612"}},
		{"gain -" + base32Hash, []string{"release-8394613"}},
		{base32Hash + " OR 宇宙", []string{"release-8394612", "unicode"}},
		{"atrix -ca", []string{"release-8394612"}},
		{"gain -WEB", []string{"release-8394613"}},
		{`gain -"%"`, []string{"release-8394612", "release-8394613"}},
		{"atrix -CAM OR 宇宙", []string{"release-8394612", "unicode"}},
		{"atrix -CAM or 宇宙", []string{"release-8394612", "unicode"}},
		{`"blue ocean"`, []string{"blue"}},
		{`"blue ocean`, []string{"blue"}},
		{`blue -"deep ocean"`, []string{"blue", "reverse"}},
		{"Écl", []string{"unicode"}},
		{"宇宙", []string{"unicode"}},
		{`100%_done\cut`, []string{"literal"}},
		{"%", []string{"literal"}},
		{"_", []string{"literal"}},
		{`\`, []string{"literal"}},
		{"26", []string{"release-8394612"}},
		{"hidden-only-text", nil},
		{"private_note", nil},
		{`""`, nil},
		{`-"%"`, nil},
		{"-", nil},
		{`';SELECT`, nil},
	} {
		t.Run(test.query, func(t *testing.T) {
			page, err := db.ListTorrents(ctx, model.ListOptions{Query: test.query})
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, row := range page.Items {
				ids = append(ids, row.SourceID)
			}
			sort.Strings(ids)
			if !reflect.DeepEqual(ids, test.want) || page.Total != int64(len(test.want)) {
				t.Fatalf("query %q: got %v (%d total), want %v", test.query, ids, page.Total, test.want)
			}
			// Identification must accept exactly the same fragments, hash forms,
			// operators and literal SQL pattern characters as source search.
			for _, status := range []string{"", "pending"} {
				results, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Query: test.query, Status: status})
				if err != nil {
					t.Fatal(err)
				}
				var identifiedIDs []string
				for _, item := range results.Items {
					identifiedIDs = append(identifiedIDs, item.SourceID)
				}
				sort.Strings(identifiedIDs)
				if results.Total != page.Total || !reflect.DeepEqual(identifiedIDs, ids) {
					t.Fatalf("identification search diverged for %q / %q: %v, want %v", test.query, status, identifiedIDs, ids)
				}
			}
		})
	}
	filtered, err := db.ListTorrents(ctx, model.ListOptions{Query: "gain OR 宇宙", Providers: []string{"remote-example"}})
	if err != nil || filtered.Total != 1 || filtered.Items[0].ProviderID != "remote-example" {
		t.Fatalf("query alternative bypassed the source filter: %+v %v", filtered, err)
	}
}

func BenchmarkTorrentSearchFragments(b *testing.B) {
	ctx, db := sharingDatabase(b)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
        SELECT 'fixture',n::text,jsonb_build_object('title','Ordinary.Volume.'||n||'.2024.1080p.WEB.H265') FROM generate_series(1,100000) n;
        INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','target','{"title":"Astronomical.Documentary.2024"}');
        ANALYZE ingest.torrents;`); err != nil {
		b.Fatal(err)
	}
	for _, test := range []struct{ name, query string }{
		{"middle", "ronomi"}, {"combined", "ronomi docum"}, {"short", "mi"},
	} {
		b.Run(test.name, func(b *testing.B) {
			for b.Loop() {
				page, err := db.ListTorrents(ctx, model.ListOptions{Query: test.query})
				if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].SourceID != "target" {
					b.Fatalf("fragment benchmark: %+v %v", page, err)
				}
			}
		})
	}
}

func TestTorrentSearchFragmentRelevanceAndPagination(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, row := range []struct{ id, title string }{
		{"exact", "Ocean"}, {"prefix", "Oceanic"}, {"middle", "Transoceanic"},
	} {
		fields, _ := json.Marshal(map[string]string{"title": row.title})
		publishSharingRow(t, ctx, db, "fixture", row.id, string(fields), nil)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET last_seen_at='2026-09-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	for offset, id := range []string{"exact", "prefix", "middle"} {
		page, err := db.ListTorrents(ctx, model.ListOptions{Query: "ocean", Sort: "relevance", Limit: 1, Offset: offset})
		if err != nil || page.Total != 3 || len(page.Items) != 1 || page.Items[0].SourceID != id {
			t.Fatalf("relevance page %d: %+v %v", offset, page, err)
		}
	}
}

func TestTorrentSearchFragmentsUpdateWithoutTruncatingPublishedFields(t *testing.T) {
	ctx, db := sharingDatabase(t)
	title := "UniqueFragment " + strings.Repeat("界", 100000) + " NotIndexedTail"
	fields, err := json.Marshal(map[string]any{"title": title, "size": json.Number("9007199254740993")})
	if err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "fixture", "large", string(fields), nil)
	page, err := db.ListTorrents(ctx, model.ListOptions{Query: "quefrag", MinSize: "9007199254740993", MaxSize: "9007199254740993"})
	if err != nil || page.Total != 1 || page.Items[0].Fields["title"] != title || page.Items[0].Fields["size"] != json.Number("9007199254740993") {
		t.Fatalf("bounded fragment search changed published values: %+v %v", page, err)
	}
	tail, err := db.ListTorrents(ctx, model.ListOptions{Query: "NotIndexedTail"})
	if err != nil || tail.Total != 0 {
		t.Fatalf("fragment search exceeded indexed text bounds: %+v %v", tail, err)
	}
	publishSharingRow(t, ctx, db, "fixture", "large", `{"title":"ChangedInformation"}`, nil)
	for _, test := range []struct {
		query string
		count int64
	}{{"quefrag", 0}, {"ngedinfo", 1}} {
		page, err := db.ListTorrents(ctx, model.ListOptions{Query: test.query})
		if err != nil || page.Total != test.count {
			t.Fatalf("fragment index retained stale text for %q: %+v %v", test.query, page, err)
		}
	}
}
