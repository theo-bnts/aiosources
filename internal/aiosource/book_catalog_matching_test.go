package aiosource

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func TestBookDiscoveryDoesNotOverflowOnUncorroboratedTitleFragments(t *testing.T) {
	ctx, s, conn := testService(t)
	seedTestBook(t, ctx, s, conn, "fixture")
	if _, err := conn.Exec(ctx, `UPDATE ingest.ol_authors SET aliases=ARRAY['Amber Writer'] WHERE id='OL200A';
INSERT INTO ingest.ol_authors(id,name,normalized)
SELECT 'OL'||n||'A','Another Writer '||n,'another writer '||n FROM generate_series(1000,1299) n;
INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,book_normalized,years,book_data)
SELECT 'book','OL'||n||'W',CASE WHEN n<1150 THEN 'Orchard' ELSE 'The Amber Orchard' END,
 CASE WHEN n<1150 THEN 'orchard' ELSE 'the amber orchard' END,'{}',
 jsonb_build_object('author_keys',jsonb_build_array('OL'||n||'A')) FROM generate_series(1000,1299) n;`); err != nil {
		t.Fatal(err)
	}
	for id, fields := range map[string]map[string]any{
		"joined": {"title": "Morgan.Vale.The.Amber.Orchard.EPUB"},
		"author": {"title": "The Amber Orchard EPUB", "author": "Morgan Vale"},
		"alias":  {"title": "Amber.Writer.The.Amber.Orchard.EPUB"},
		"wrong":  {"title": "The Amber Orchard Morgan Vale EPUB", "author": "A Different Author"},
	} {
		fields["info_hash"] = strings.Repeat("a", 40)
		encoded, _ := json.Marshal(fields)
		if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture',$1,$2)`, id, encoded); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"joined", "author", "alias", "wrong"} {
		var status string
		var work string
		if err := conn.QueryRow(ctx, `SELECT m.status,COALESCE(w.openlibrary_id,'') FROM ingest.aio_matches m LEFT JOIN ingest.aio_works w ON w.id=m.work_id WHERE m.provider_id='fixture' AND m.source_id=$1`, id).Scan(&status, &work); err != nil {
			t.Fatal(err)
		}
		if id == "wrong" {
			if status == "matched" {
				t.Fatal("a joined filename overrode conflicting author evidence")
			}
		} else if status != "matched" || work != "OL100W" {
			t.Fatal("irrelevant fragments or authors hid the corroborated book", id, status)
		}
	}
	// A close title by the same author remains a genuine competitor.
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,book_normalized,years,book_data)
VALUES('book','OL9000W','The Amber Orchards','the amber orchards','{}','{"author_keys":["OL200A"]}');
INSERT INTO ingest.aio_match_queue(provider_id,source_id) VALUES('fixture','author') ON CONFLICT DO NOTHING;`); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := conn.QueryRow(ctx, `SELECT status FROM ingest.aio_matches WHERE provider_id='fixture' AND source_id='author'`).Scan(&status); err != nil || status != "ambiguous" {
		t.Fatal("a nearby corroborated title lost its review requirement", status, err)
	}
}

func TestBookFormatsStayConsistentBetweenSearchAndTorznab(t *testing.T) {
	ctx, s, conn := testService(t)
	work := seedTestBook(t, ctx, s, conn, "fixture")
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
VALUES('fixture','audio','{"title":"Morgan Vale - The Amber Orchard [MP3.128]","categories":[7020],"info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}');`); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"book", "ebook", "audiobook", "book,ebook,audiobook"} {
		found, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Orchard"}, model.CatalogSearchFilters{Type: kind})
		if err != nil || found.Total != 1 {
			t.Fatal("a supported book format disappeared from Search", kind, found.Total, err)
		}
	}
	for _, tc := range []struct {
		kind     string
		hash     string
		category int
	}{{"ebook", strings.Repeat("b", 40), 7020}, {"audiobook", strings.Repeat("a", 40), 3030}} {
		result, err := s.DB.PublicTorznabCatalog(ctx, model.TorznabQuery{Mode: "search", Limit: 50}, nil, []string{tc.kind}, store.TorznabVisibility{Public: true, UncachedSources: []string{"fixture"}, CacheAfter: time.Now().Add(-time.Hour)}, nil)
		if err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].Hash != tc.hash || result.Items[0].Category != tc.category || result.Items[0].SourceID != "" {
			t.Fatal("Torznab disagrees with the parser's book format", tc.kind, result.Total, err)
		}
	}
	// An explicit manual book choice without a known format remains a book;
	// it must not be presented as an ebook or excluded from the general type.
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
VALUES('fixture','opaque','{"title":"Opaque archive","info_hash":"cccccccccccccccccccccccccccccccccccccccc"}')`); err != nil {
		t.Fatal(err)
	}
	var occurrence string
	if err := conn.QueryRow(ctx, `SELECT occurrence_id FROM ingest.torrents WHERE source_id='opaque'`).Scan(&occurrence); err != nil {
		t.Fatal(err)
	}
	detail, err := s.DB.AIOReviewDetail(ctx, occurrence)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveReview(ctx, occurrence, model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, WorkID: work, WorkRevision: 1}); err != nil {
		t.Fatal(err)
	}
	result, err := s.DB.PublicTorznabCatalog(ctx, model.TorznabQuery{Mode: "search", Limit: 50, MediaTypes: []string{"book"}}, nil, []string{"book"}, store.TorznabVisibility{Public: true, UncachedSources: []string{"fixture"}, CacheAfter: time.Now().Add(-time.Hour)}, nil)
	if err != nil || result.Total != 3 {
		t.Fatal("the generic book type lost a manual choice with an unknown format", result.Total, err)
	}
}
