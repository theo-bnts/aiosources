package aiosource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The same fixtures run before and after an optimization. Use -benchtime=1x
// with a disposable PostgreSQL server; no public upstream is contacted.
func BenchmarkMetadataRetrieval(b *testing.B) {
	ctx, s, conn := testService(b)
	var active, peak, requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/changes") {
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
			return
		}
		id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movie/"))
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, strings.Replace(movieDetail, `"id":101`, fmt.Sprintf(`"id":%d`, id), 1))
	}))
	b.Cleanup(server.Close)
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	if err := s.Vault.Put(ctx, "benchmark-token", "synthetic-token"); err != nil {
		b.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		b.Fatal(err)
	}
	c.TMDBSecret = "benchmark-token"
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := s.DB.ResetAIOMetadata(ctx); err != nil {
			b.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `UPDATE ingest.aio_state SET inventory_date=$1,changes_through=now()`, time.Now().UTC().Add(-24*time.Hour).Format("2006-01-02")); err != nil {
			b.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) SELECT 'movie',n::text FROM generate_series(1,100) n`); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.Sync(ctx, c); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(peak.Load()), "max-inflight")
	b.ReportMetric(float64(requests.Load())/b.Elapsed().Seconds(), "requests/s")
}

func BenchmarkMatchingDenseCatalog(b *testing.B) {
	ctx, s, conn := testService(b)
	if _, err := conn.Exec(ctx, `
	INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years)
	SELECT 'movie',n::text,'tt'||(10000000+n)::text,ARRAY[1990] FROM generate_series(1,10000) n;
	INSERT INTO ingest.aio_titles(work_id,title,normalized)
	SELECT id,'Whispers of the Forest '||substring(md5(tmdb_id),1,8),'whispers of the forest '||substring(md5(tmdb_id),1,8) FROM ingest.aio_works;
	INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','10001','tt1234567',ARRAY[2021]);
	INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,'Whispers of the Forest','whispers of the forest' FROM ingest.aio_works WHERE tmdb_id='10001';
	INSERT INTO ingest.torrents(provider_id,source_id,fields)
	SELECT 'synthetic',n::text,jsonb_build_object('title','Whispers.of.the.Forest.2021.1080p','imdb_id','tt1234567','info_hash',md5(n::text)||'12345678') FROM generate_series(1,200) n;
	ANALYZE ingest.aio_works; ANALYZE ingest.aio_titles; ANALYZE ingest.torrents;`); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := s.DB.ResetAIOMapping(ctx); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.Match(ctx); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	var matched int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_matches WHERE status='matched'`).Scan(&matched); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(matched), "matched/200")
}

func BenchmarkMetadataSeasonLookup(b *testing.B) {
	ctx, s, conn := testService(b)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
	SELECT 'synthetic',n::text,jsonb_build_object('title','Synthetic.Serial.S01E01.2021','season',1,'tmdb_id',n::text,'imdb_id','tt'||(10000000+n)::text,'tvdb_id',n::text) FROM generate_series(1,100000) n;
	ANALYZE ingest.torrents;`); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		seasons, err := s.DB.AIORequestedSeasons(ctx, "100000", "tt10100000", "100000")
		if err != nil || len(seasons) != 1 {
			b.Fatalf("season lookup: %v %v", seasons, err)
		}
	}
}

// A restart may reread the same dated export. Measure that idempotent replay,
// not a first inventory download or metadata retrieval from the provider.
func BenchmarkMetadataInventoryReplay(b *testing.B) {
	ctx, s, conn := testService(b)
	date := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")
	ids := make([]string, 50000)
	for i := range ids {
		ids[i] = strconv.Itoa(i + 1)
	}
	replay := func() {
		for start := 0; start < len(ids); start += 1000 {
			if err := s.DB.QueueAIOInventory(ctx, "movie", ids[start:start+1000], date); err != nil {
				b.Fatal(err)
			}
		}
	}
	replay()
	b.ResetTimer()
	for range b.N {
		replay()
	}
	b.StopTimer()
	var inventory, jobs int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_inventory),(SELECT count(*) FROM ingest.aio_metadata_jobs)`).Scan(&inventory, &jobs); err != nil || inventory != len(ids) || jobs != len(ids) {
		b.Fatal("inventory replay lost or duplicated durable jobs", inventory, jobs, err)
	}
}

// Unlike the repeated-title fixture, each pick has a different title and ID.
// This measures the no-reuse path, including episodic and season-pack requests.
func BenchmarkMatchingVariedCatalog(b *testing.B) {
	ctx, s, conn := testService(b)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years)
 SELECT CASE WHEN n%3=0 THEN 'movie' ELSE 'tv' END,n::text,'tt'||(10000000+n)::text,ARRAY[2021] FROM generate_series(1,90) n;
 INSERT INTO ingest.aio_titles(work_id,title,normalized)
 SELECT id,'Synthetic Adventure '||tmdb_id,'synthetic adventure '||tmdb_id FROM ingest.aio_works;
 INSERT INTO ingest.torrents(provider_id,source_id,fields)
 SELECT 'synthetic',n::text,jsonb_build_object('title','Synthetic.Adventure.'||n||CASE n%3 WHEN 0 THEN '.2021.1080p' WHEN 1 THEN '.S01E03.2021.1080p' ELSE '.S01.COMPLETE.2021.1080p' END,'tmdb_id',n::text,'info_hash',md5(n::text)||'12345678') FROM generate_series(1,90) n;
 ANALYZE ingest.aio_works; ANALYZE ingest.aio_titles; ANALYZE ingest.torrents;`); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := s.DB.ResetAIOMapping(ctx); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.Match(ctx); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	var matched int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM ingest.aio_matches WHERE status='matched'").Scan(&matched); err != nil || matched != 90 {
		b.Fatal("varied decisions changed", matched, err)
	}
	b.ReportMetric(float64(matched), "matched/90")
}
