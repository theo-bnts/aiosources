package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func torznabGrant(t *testing.T, ctx context.Context, db *Store, sources ...string) model.ShareCredential {
	t.Helper()
	scope := "all"
	if len(sources) > 0 {
		scope = "selected"
	}
	v, err := db.CreateShare(ctx, model.ShareInput{Format: "torznab", Name: "External client", Enabled: true, Scope: scope, SourceIDs: sources, Fields: CatalogFields()})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTorznabPublishedFiltersTotalsAndStorageIsolation(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, row := range []struct{ provider, id, title string }{{"alpha", "one", "Example.Film.2024"}, {"alpha", "two", "Example.Series.S01E02"}, {"beta", "one", "Example.Other.2024"}} {
		publishSharingRow(t, ctx, db, row.provider, row.id, `{"title":"`+row.title+`","info_hash":"`+strings.Repeat("a", 40)+`","size":100,"download_url":"https://private.invalid/secret","magnet_url":"magnet:?tr=private"}`, nil)
	}
	for _, pair := range []struct{ id, typ string }{{"one", "movie"}, {"two", "tv"}} {
		if _, err := db.pool.Exec(ctx, `WITH work AS(INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES($2,$1,'tt1234567',ARRAY[2024]) RETURNING id,revision)
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason) SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture' FROM ingest.torrents t CROSS JOIN work w WHERE provider_id='alpha' AND source_id=$1`, pair.id, pair.typ); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM ingest.aio_match_queue`); err != nil {
		t.Fatal(err)
	}
	grant := torznabGrant(t, ctx, db)
	read := func(q model.TorznabQuery) model.List[model.TorznabRecord] {
		t.Helper()
		d, err := db.BeginTorznabRequest(ctx, grant.Share.ID, grant.Password)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		v, err := d.Torznab(ctx, q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = d.Validate(ctx); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct {
		q     model.TorznabQuery
		total int64
	}{{model.TorznabQuery{Mode: "search", Limit: 1}, 3}, {model.TorznabQuery{Mode: "search", Limit: 100, Offset: 3}, 3}, {model.TorznabQuery{Mode: "movie", Limit: 100}, 1}, {model.TorznabQuery{Mode: "tvsearch", Limit: 100}, 1}, {model.TorznabQuery{Mode: "search", Categories: []int{2000, 5000}, Limit: 100}, 2}, {model.TorznabQuery{Mode: "search", Categories: []int{1234}, Limit: 100}, 0}, {model.TorznabQuery{Mode: "search", MediaTypes: []string{"other"}, Limit: 100}, 1}, {model.TorznabQuery{Mode: "search", IMDb: "tt1234567", Limit: 100}, 2}, {model.TorznabQuery{Mode: "search", SearchFilters: model.SearchFilters{Query: "Example Film"}, Limit: 100}, 1}, {model.TorznabQuery{Mode: "search", SearchFilters: model.SearchFilters{Providers: []string{"beta"}}, Limit: 100}, 1}} {
		v := read(tc.q)
		if v.Total != tc.total {
			t.Fatalf("filter %+v: %+v", tc.q, v)
		}
		if tc.q.Offset >= int(tc.total) && len(v.Items) != 0 {
			t.Fatal("out of bounds page was not empty")
		}
		for _, item := range v.Items {
			if item.SourceID != "" || item.OccurrenceID == "" || strings.Contains(item.Title, "private") {
				t.Fatal("unsafe projection")
			}
		}
	}
	var tracking bool
	var journal int
	if err := db.pool.QueryRow(ctx, `SELECT enabled,(SELECT count(*) FROM ingest.catalog_journal) FROM ingest.catalog_tracking`).Scan(&tracking, &journal); err != nil || tracking || journal != 0 {
		t.Fatal("Torznab enabled synchronization retention", tracking, journal, err)
	}
	if catalog, err := db.Shares(ctx); err != nil || len(catalog) != 0 {
		t.Fatal("Torznab access leaked into catalog shares")
	}
	if _, err := db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("Torznab key authorized catalog protocol", err)
	}
	selected := torznabGrant(t, ctx, db, "alpha")
	d, err := db.BeginTorznabRequest(ctx, selected.Share.ID, selected.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	v, err := d.Torznab(ctx, model.TorznabQuery{Mode: "search", Limit: 100, SearchFilters: model.SearchFilters{Providers: []string{"alpha", "beta"}}}, nil)
	if err != nil || v.Total != 2 {
		t.Fatal("source filter widened permissions", v.Total, err)
	}
	rotated, err := db.RotateShare(ctx, selected.Share.ID, selected.Share.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Validate(ctx); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("rotation did not revoke an admitted response", err)
	}
	if _, err = db.BeginTorznabRequest(ctx, selected.Share.ID, selected.Password); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("old key accepted", err)
	}
	if _, err = db.BeginTorznabRequest(ctx, selected.Share.ID, rotated.Password); err != nil {
		t.Fatal(err)
	}
}
