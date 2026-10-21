package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// All datasets are synthetic and isolated. Run identical fixtures before and
// after a change, with -benchtime=20x -count=5 and TEST_DATABASE_URL set.
func BenchmarkCollectionPersistence(b *testing.B) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModeIncremental} {
		b.Run(string(mode), func(b *testing.B) {
			ctx, db := sharingDatabase(b)
			p := model.Provider{ID: "synthetic", Name: "Synthetic source", Adapter: "http_json"}
			created, err := db.CreateRun(ctx, model.Run{ProviderID: p.ID, Config: p, Mode: mode})
			if err != nil {
				b.Fatal(err)
			}
			run, err := db.ClaimNext(ctx)
			if err != nil || run == nil || run.ID != created.ID {
				b.Fatal("claim fixture", err)
			}
			b.Cleanup(func() { _, _ = db.FinishRun(ctx, run.ID, model.StatusCancelled, "", "") })
			items := make([]model.Record, 100)
			for i := range items {
				items[i] = model.Record{SourceID: fmt.Sprint(i), Fields: map[string]any{
					"title":     fmt.Sprintf("Synthetic.Film.%d.2026.1080p", i),
					"info_hash": fmt.Sprintf("%040x", i+1), "size": int64(123456789),
					"tmdb_id": fmt.Sprint(i + 1), "categories": []int{2000},
				}}
			}
			b.ResetTimer()
			for b.Loop() {
				updated, err := db.SavePage(ctx, *run, model.Page{Items: items, ContentType: "application/json"}, "")
				if err != nil {
					b.Fatal(err)
				}
				*run = updated
			}
			b.ReportMetric(100, "records/page")
		})
	}
}

func BenchmarkCatalogReadPaths(b *testing.B) {
	ctx, db := sharingDatabase(b)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years)
 SELECT 'movie',n::text,'tt'||(10000000+n)::text,ARRAY[2026] FROM generate_series(1,1000) n;
 INSERT INTO ingest.torrents(provider_id,source_id,fields)
 SELECT 'synthetic-'||(n%3)::text,n::text,jsonb_build_object('title','Synthetic.Documentary.'||n||'.2026.1080p','info_hash',md5(n::text)||'12345678','size',123456789) FROM generate_series(1,120000) n;
 INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','synthetic fixture'
 FROM ingest.torrents t JOIN ingest.aio_works w ON w.tmdb_id=((t.source_id::int%1000)+1)::text;
 DELETE FROM ingest.aio_match_queue;
 INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files)
 SELECT 'torbox',md5(n::text)||'12345678',true,now(),'[]'::jsonb FROM generate_series(1,10000) n;
 ANALYZE ingest.torrents; ANALYZE ingest.aio_matches; ANALYZE ingest.aio_works;
 ANALYZE ingest.aio_match_queue; ANALYZE ingest.aio_shared_availability;`)
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"source-statistics", "metadata-statistics", "metadata-controls", "identification", "identification-filtered", "search", "stremio", "shared-cache", "storage"} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				switch name {
				case "source-statistics":
					_, err = db.SourceTorrentStats(ctx, []string{"synthetic-0", "synthetic-1", "synthetic-2"})
				case "metadata-statistics":
					_, err = db.AIOStatus(ctx)
				case "metadata-controls":
					_, err = db.AIOControlStatus(ctx)
				case "identification", "identification-filtered":
					status := ""
					if name == "identification-filtered" {
						status = "matched"
					}
					_, err = db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: status, Limit: 50})
				case "search":
					_, err = db.ListTorrents(ctx, model.ListOptions{Query: "Documentary.1234", Limit: 50})
				case "stremio":
					_, err = db.AIOStreams(ctx, "imdb", "tt10000001", "movie")
				case "shared-cache":
					_, err = db.SharedAvailability(ctx, "torbox", []string{"c4ca4238a0b923820dcc509a6f75849b12345678"}, time.Now().Add(-time.Hour))
				case "storage":
					_, err = db.MeasureHealth(ctx, model.HealthDisk{})
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAdministrationPaths(b *testing.B) {
	ctx, db := sharingDatabase(b)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
 SELECT 'synthetic',n::text,jsonb_build_object('title','Synthetic.Documentary.'||n||'.2026','size',123456789) FROM generate_series(1,30000) n;
 INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,traversal_done)
 VALUES('synthetic-history','synthetic','Synthetic source','preview','succeeded','','{}',true);
 INSERT INTO ingest.events(run_id,kind,message,data)
 SELECT 'synthetic-history','page_saved','Synthetic event','{}'::jsonb FROM generate_series(1,20000);
 ANALYZE ingest.torrents; ANALYZE ingest.runs; ANALYZE ingest.events; ANALYZE ingest.activity_logs;`)
	if err != nil {
		b.Fatal(err)
	}
	quota := 3600
	grant, err := db.CreateShare(ctx, model.ShareInput{Name: "Synthetic share", Enabled: true, Scope: "all", Fields: CatalogFields(), RequestsPerMinute: &quota})
	if err != nil {
		b.Fatal(err)
	}
	download, err := db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = download.Close() })
	for _, name := range []string{"logs", "history", "catalog-export", "history-cleanup-idle", "startup-migrations"} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				switch name {
				case "logs":
					_, err = db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 50}})
				case "history":
					_, err = db.Events(ctx, "synthetic-history", model.ListOptions{Limit: 50})
				case "catalog-export":
					_, err = download.Page(ctx, CatalogPageRequest{Limit: 100})
				case "history-cleanup-idle":
					_, err = db.PruneHistory(ctx)
				case "startup-migrations":
					err = db.Migrate(ctx)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
