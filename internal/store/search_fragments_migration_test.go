package store

import (
	"strings"
	"testing"
)

func TestTorrentFragmentSearchUsesIndex(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields)
        SELECT 'fixture',n::text,jsonb_build_object('title','Ordinary.Release.'||n) FROM generate_series(1,10000) n;
        INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','special','{"title":"Astronomical.Release"}');
        ANALYZE aiosources.torrents;`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err := db.pool.QueryRow(ctx, `EXPLAIN (ANALYZE,FORMAT JSON) SELECT count(*) FROM aiosources.torrents WHERE aiosources.torrent_search_text(provider_id,source_id,fields) ILIKE $1`, "%ronomi%").Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, `"Index Name": "torrents_fragments"`) {
		t.Fatalf("selective fragment search did not use its index: %s", plan)
	}
}
