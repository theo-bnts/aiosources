package store

import (
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogSearchOnlyPublishesCurrentIdentifications(t *testing.T) {
	ctx, db := sharingDatabase(t)
	work := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie: New Day", "Un autre titre"}, Years: []int{2026}}
	if err := db.SaveAIOWork(ctx, work, []string{"example movie new day", "un autre titre"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "changed", "revision", "queued"} {
		publishSharingRow(t, ctx, db, "source", id, `{"title":"Example.Movie.New.Day.2026","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","private_note":"synthetic-private-value"}`, nil)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,CASE WHEN source_id='changed' THEN 'old' ELSE fields_fingerprint END,(SELECT id FROM ingest.aio_works),CASE WHEN source_id='revision' THEN 99 ELSE 1 END,'matched','fixture' FROM ingest.torrents;
DELETE FROM ingest.aio_match_queue WHERE source_id<>'queued';`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct{ raw, normalized string }{{"Example Movie New Day", "example movie new day"}, {"Movie", "movie"}, {"autre", "autre"}, {"tt1234567", "tt1234567"}, {"101", "101"}} {
		page, err := db.CatalogTitles(ctx, model.ListOptions{Query: q.raw}, q.normalized, "")
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Title != work.Titles[0] || page.Items[0].Releases != 2 {
			t.Fatalf("search %q: %+v %v", q.raw, page, err)
		}
		rows, err := db.CatalogRecords(ctx, page.Items[0].ID)
		if err != nil || len(rows) != 2 {
			t.Fatalf("current releases: %+v %v", rows, err)
		}
		for _, row := range rows {
			if !ValidOccurrenceID(row.OccurrenceID) || (row.SourceID != "one" && row.SourceID != "two") {
				t.Fatal("stale or unaddressable release published")
			}
		}
	}
	page, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example", Limit: 1, Offset: 1}, "example", "")
	if err != nil || page.Total != 1 || len(page.Items) != 0 {
		t.Fatalf("offset changed total: %+v %v", page, err)
	}
	page, err = db.CatalogTitles(ctx, model.ListOptions{Query: "example"}, "example", "tv")
	if err != nil || page.Total != 0 {
		t.Fatal("media filter bypassed", err)
	}
	page, err = db.CatalogTitles(ctx, model.ListOptions{Query: "private value"}, "private value", "")
	if err != nil || page.Total != 0 {
		t.Fatal("private fields were searchable", err)
	}
	for _, q := range []string{"", "x"} {
		if _, err := db.CatalogTitles(ctx, model.ListOptions{Query: q}, q, ""); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("short query accepted", err)
		}
	}
	var id int64
	if err := db.pool.QueryRow(ctx, `UPDATE ingest.aio_works SET adult=true RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CatalogTitle(ctx, id); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("excluded work published", err)
	}
}

func TestDisplayTitleDoesNotInvalidateMatching(t *testing.T) {
	ctx, db := sharingDatabase(t)
	w := model.AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Original title", "Alternative title"}, Years: []int{2021}}
	if err := db.SaveAIOWork(ctx, w, []string{"original title", "alternative title"}); err != nil {
		t.Fatal(err)
	}
	w.DisplayTitle = "Alternative title"
	if err := db.SaveAIOWork(ctx, w, []string{"original title", "alternative title"}); err != nil {
		t.Fatal(err)
	}
	var title string
	var revision int
	if err := db.pool.QueryRow(ctx, `SELECT display_title,revision FROM ingest.aio_works`).Scan(&title, &revision); err != nil || title != "Alternative title" || revision != 1 {
		t.Fatalf("presentation change invalidated matching: %q %d %v", title, revision, err)
	}
}
