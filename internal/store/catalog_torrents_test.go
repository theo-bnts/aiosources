package store

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDirectCatalogSearchDoesNotRequireMatchesAndFiltersBeforePagination(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, row := range []struct {
		provider, id, hash string
		size               int
	}{
		{"private", "cached", strings.Repeat("a", 40), 100},
		{"private", "unknown", strings.Repeat("b", 40), 300},
		{"public", "unknown", strings.Repeat("c", 40), 200},
	} {
		publishSharingRow(t, ctx, db, row.provider, row.id, `{"title":"Example.Chronicle.EPUB","info_hash":"`+row.hash+`","size":`+strconv.Itoa(row.size)+`,"private_note":"never-return-this"}`, nil)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_shared_availability(service,hash,cached,checked_at,files) VALUES('torbox',$1,true,now(),'[{"path":"Example.epub","size":100}]')`, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	policy := CatalogTorrentPolicy{CacheAfter: time.Now().Add(-time.Hour), Services: []string{"torbox"}}
	for _, q := range []string{"Example Chronicle", "Example.Chronicle"} {
		page, err := db.CatalogTorrentRecords(ctx, model.ListOptions{Query: q}, nil, policy)
		if err != nil || page.Total != 3 || len(page.Items) != 3 || page.Items[0].Size != 300 {
			t.Fatalf("direct search %s: %+v %v", q, page, err)
		}
	}
	policy.Public = true
	page, err := db.CatalogTorrentRecords(ctx, model.ListOptions{Query: "Example"}, nil, policy)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].SourceID != "cached" {
		t.Fatalf("private unknown torrent/count leaked: %+v %v", page, err)
	}
	policy.UncachedSources = []string{"public"}
	for _, offset := range []int{0, 1, 2} {
		page, err = db.CatalogTorrentRecords(ctx, model.ListOptions{Query: "Example", Limit: 1, Offset: offset}, []model.ResultSort{{Field: "cached", Direction: "desc"}}, policy)
		if err != nil || page.Total != 2 || len(page.Items) != func() int {
			if offset < 2 {
				return 1
			}
			return 0
		}() {
			t.Fatalf("visibility/pagination: %+v %v", page, err)
		}
		if offset == 0 && page.Items[0].SourceID != "cached" {
			t.Fatal("cache priority ignored")
		}
	}
	policy.CachedOnly = true
	page, err = db.CatalogTorrentRecords(ctx, model.ListOptions{Query: "Example"}, nil, policy)
	if err != nil || page.Total != 1 {
		t.Fatalf("cached-only: %+v %v", page, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.aio_shared_availability SET checked_at=now()-interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	page, err = db.CatalogTorrentRecords(ctx, model.ListOptions{Query: "Example"}, nil, policy)
	if err != nil || page.Total != 0 {
		t.Fatal("expired cache disclosed", err)
	}
	policy.CachedOnly = false
	policy.UncachedSources = nil
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.aio_shared_availability SET checked_at=now(),files='[{"path":"folder/readme.txt","size":12}]'`); err != nil {
		t.Fatal(err)
	}
	page, err = db.CatalogTorrentRecords(ctx, model.ListOptions{Query: "Example"}, nil, policy)
	if err != nil || page.Total != 0 {
		t.Fatal("unusable private cached file leaked a count", page.Total, err)
	}
}
