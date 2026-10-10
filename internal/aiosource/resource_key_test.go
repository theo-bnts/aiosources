package aiosource

import (
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStremioCacheKeysShareVerifiedAliasesAndSeparateEpisodes(t *testing.T) {
	rows := []model.AIORecord{{IMDb: "tt1234567", TMDB: "202", TVDB: "303"}, {IMDb: "1234567", TMDB: "202", TVDB: "303"}}
	var key string
	for _, id := range []string{"tt1234567:1:2", "imdb:1234567:1:2", "tmdb:202:1:2", "tvdb:303:1:2"} {
		r, err := ParseID(id, "series")
		if err != nil {
			t.Fatal(err)
		}
		got := streamResourceKey(r, rows)
		if key != "" && key != got {
			t.Fatalf("ID aliases duplicated a cache check: %s != %s", key, got)
		}
		key = got
	}
	for _, id := range []string{"tt1234567:1:3", "tt1234567:2:2", "mal:1234567:2"} {
		r, err := ParseID(id, "series")
		if err != nil || streamResourceKey(r, rows) == key {
			t.Fatal("episode scopes merged", id, err)
		}
	}
	rows[1].IMDb, rows[1].TMDB, rows[1].TVDB = "tt9999999", "909", "invalid conflicting values"
	a, _ := ParseID("tt1234567:1:2", "series")
	b, _ := ParseID("tmdb:202:1:2", "series")
	if streamResourceKey(a, rows) == streamResourceKey(b, rows) {
		t.Fatal("conflicting catalog identifiers merged resources")
	}
}

func TestStremioCacheAliasesUsePublishedIdsWhenSourceSuppliesNone(t *testing.T) {
	ctx, s, conn := testService(t)
	work := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", TVDB: "501", Titles: []string{"Example Movie"}, Years: []int{2021}}
	if err := s.DB.SaveAIOWork(ctx, work, []string{"example movie"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
VALUES('source','1',jsonb_build_object('title','Example.Movie.2021.1080p','info_hash',$1::text))`, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var key string
	for _, id := range []string{"tt1234567", "tmdb:101", "tvdb:501"} {
		r, err := ParseID(id, "movie")
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.DB.AIOStreams(ctx, r.Kind, r.ID, r.Type)
		if err != nil || len(rows) != 1 {
			t.Fatal("verified local mapping was not published", id, rows, err)
		}
		got := streamResourceKey(r, rows)
		if key != "" && key != got {
			t.Fatalf("mapped ID aliases duplicated the cache resource: %s != %s", key, got)
		}
		key = got
	}
	// Published read projections must not become assertions in source storage.
	native, err := s.DB.AIOTorrent(ctx, "source", "1")
	if err != nil || native.IMDb != "" || native.TMDB != "" || native.TVDB != "" {
		t.Fatal("verified read identifiers changed native source fields", native, err)
	}
}
