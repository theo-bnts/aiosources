package store

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestLocalCoversUseBrowserLanguageEnglishThenExistingImage(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Example film"}, PosterPath: "/default.jpg", LocalizedPosters: map[string]string{"fr": "/french.jpg", "en": "/english.jpg"}}, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "example", "movie", `{"title":"Example film","info_hash":"`+strings.Repeat("a", 40)+`"}`, nil)
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,openlibrary_id,display_title,book_normalized,years,book_data) VALUES('book','OL100W','Example book','example book',ARRAY[2001],'{"cover":999}');
 INSERT INTO aiosources.ol_editions(id,work_keys,title,normalized,languages,cover) VALUES
 ('OL100M',ARRAY['OL100W'],'Example book','example book',ARRAY['de'],100),
 ('OL200M',ARRAY['OL100W'],'Example book','example book',ARRAY['en'],200),
 ('OL300M',ARRAY['OL100W'],'Example book','example book',ARRAY['fr'],300);`); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "example", "book", `{"title":"Example book.epub","info_hash":"`+strings.Repeat("b", 40)+`"}`, nil)
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','Example match' FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.media_type=CASE t.source_id WHEN 'movie' THEN 'movie' ELSE 'book' END;
 DELETE FROM aiosources.aio_match_queue;`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ language, movie, book string }{{"fr", "/french.jpg", "300"}, {"fr-CA", "/french.jpg", "300"}, {"ja", "/english.jpg", "200"}, {"de", "/english.jpg", "100"}} {
		page, err := db.CatalogTitles(ctx, model.ListOptions{Query: "example"}, "example", model.CatalogSearchFilters{Language: tc.language})
		if err != nil || len(page.Items) != 2 {
			t.Fatal("local cover search", err)
		}
		for _, item := range page.Items {
			wanted := model.CoverURL("movie", tc.movie, 0)
			if item.Type == "book" {
				wanted = "https://covers.openlibrary.org/b/id/" + tc.book + "-M.jpg?default=false"
			}
			if item.Cover != wanted {
				t.Fatalf("%s %s image: %q", tc.language, item.Type, item.Cover)
			}
			detail, err := db.CatalogTitle(ctx, item.ID, tc.language)
			if err != nil || detail.Cover != item.Cover {
				t.Fatal("search and detail images differ", err)
			}
		}
	}
	var id, revision int64
	if err := db.pool.QueryRow(ctx, `SELECT id,revision FROM aiosources.aio_works WHERE media_type='movie'`).Scan(&id, &revision); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Example film"}, PosterPath: "/new.jpg", LocalizedPosters: map[string]string{"fr": "/newFrench.jpg"}}, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	detail, err := db.CatalogTitle(ctx, id, "fr")
	if err != nil || detail.Cover != model.CoverURL("movie", "/newFrench.jpg", 0) {
		t.Fatal("poster refresh withdrew working matches", err)
	}
	var after int64
	if err := db.pool.QueryRow(ctx, `SELECT revision FROM aiosources.aio_works WHERE id=$1`, id).Scan(&after); err != nil || after != revision {
		t.Fatal("image change invalidated identification", err)
	}
}

func TestPosterUpgradeKeepsMetadataRetryProgressAndOnlyQueuesVisibleLocalVideos(t *testing.T) {
	ctx, db := released23Database(t, 93, applicationSchema)
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,display_title,catalog_import_source,years) VALUES('movie','101','Example','',ARRAY[2020]),('tv','202','Example series','',ARRAY[2020]),('movie','303','Shared example','remote',ARRAY[2020]),('movie','404','Unused example','',ARRAY[2020]);
 INSERT INTO aiosources.torrents(provider_id,source_id,fields) SELECT 'example',tmdb_id,'{"title":"Example"}'::jsonb FROM aiosources.aio_works WHERE tmdb_id<>'404';
 INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason) SELECT 'example',tmdb_id,'fingerprint',id,revision,'matched','Fixture' FROM aiosources.aio_works WHERE tmdb_id<>'404';
 INSERT INTO aiosources.aio_metadata_jobs(media_type,tmdb_id,attempts,next_attempt,version) VALUES('movie','101',3,now()+interval '1 hour',7);`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var attempts, version, count int
	var due time.Time
	if err := db.pool.QueryRow(ctx, `SELECT attempts,version,next_attempt FROM aiosources.aio_metadata_jobs WHERE tmdb_id='101'`).Scan(&attempts, &version, &due); err != nil || attempts != 3 || version != 7 || time.Until(due) < 59*time.Minute {
		t.Fatal("image backfill reset a retry", err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.aio_metadata_jobs`).Scan(&count); err != nil || count != 2 {
		t.Fatal("image update queued unused or remote metadata", count, err)
	}
}
