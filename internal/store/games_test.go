package store

import (
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestGameCandidatesUseIndexesWithReusablePlansAndApplyYearBeforeTheLimit(t *testing.T) {
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,years)
 SELECT 'game',n::text,'Synthetic Game '||n,'{2020}' FROM generate_series(1,15000) n;
 INSERT INTO aiosources.aio_titles SELECT id,display_title,lower(display_title) FROM aiosources.aio_works;
 INSERT INTO aiosources.aio_titles SELECT id,'Shared Game','shared game' FROM aiosources.aio_works WHERE id<=150;
 UPDATE aiosources.aio_works SET years='{2024}' WHERE igdb_id='150';
 ANALYZE aiosources.aio_works,aiosources.aio_titles;`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SET LOCAL plan_cache_mode=force_generic_plan`); err != nil {
		t.Fatal(err)
	}
	year := 2024
	candidates, overflow, err := searchAIOCandidates(ctx, tx, model.AIOCandidateFilter{Type: "game", Title: "shared game", Year: &year, ExactOnly: true})
	if err != nil || overflow || len(candidates) != 1 || candidates[0].IGDB != "150" {
		t.Fatal("year-compatible game was hidden by unrelated homonyms", err)
	}
	for _, native := range []string{"", "150"} {
		rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF) "+gameCandidatesQuery, native, "shared game", &year)
		if err != nil {
			t.Fatal(err)
		}
		plan := []string{}
		for rows.Next() {
			var line string
			if err = rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(plan, "\n"), "Seq Scan on aio_works") {
			t.Fatalf("game discovery scanned the whole metadata catalog:\n%s", strings.Join(plan, "\n"))
		}
	}
}
