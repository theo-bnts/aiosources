package store

import (
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestSavedViewRetirementPreservesPublishedRecordsAndSearch(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.migrateThrough(ctx, 71); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "fixture", "record", `{"title":"Example.Documentary.2024","size":9007199254740993}`, nil)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.saved_views(id,name,filters) VALUES('old-view','Documentaries','{"q":"documentary"}')`); err != nil {
		t.Fatal(err)
	}
	before, err := db.ListTorrents(ctx, model.ListOptions{Query: "docum"})
	if err != nil || before.Total != 1 {
		t.Fatalf("legacy search: %+v %v", before, err)
	}
	for range 2 {
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var removed bool
	if err := db.pool.QueryRow(ctx, `SELECT to_regclass('ingest.saved_views') IS NULL`).Scan(&removed); err != nil || !removed {
		t.Fatalf("saved views still retained: %v %v", removed, err)
	}
	after, err := db.ListTorrents(ctx, model.ListOptions{Query: "docum"})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("retiring views changed published search: %+v %v", after, err)
	}
}
