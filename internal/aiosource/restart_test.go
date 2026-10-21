package aiosource

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/vault"
)

// A separate process is killed without Close or deferred checkpoint handlers.
// This exercises the same durable queues and orphan recovery as a container kill.
func TestRestartWorkerProcess(t *testing.T) {
	endpoint := os.Getenv("AIOSOURCES_RESTART_TEST_DSN")
	if endpoint == "" {
		t.Skip("subprocess helper")
	}
	ctx := t.Context()
	db, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := vault.New(ctx, db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, db, v, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	if os.Getenv("AIOSOURCES_RESTART_TEST_MODE") == "metadata" {
		s.TMDBBase = os.Getenv("AIOSOURCES_RESTART_TEST_BASE")
		s.MetadataHTTP = &http.Client{Timeout: 5 * time.Second}
		c, err := db.AIOConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Sync(ctx, c); err != nil {
			t.Fatal(err)
		}
	} else if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
}

func restartWorker(t *testing.T, endpoint, mode, base string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRestartWorkerProcess$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "AIOSOURCES_RESTART_TEST_DSN="+endpoint, "AIOSOURCES_RESTART_TEST_MODE="+mode, "AIOSOURCES_RESTART_TEST_BASE="+base)
	output := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, output
}

func waitForCommittedProgress(t *testing.T, count func() int, total int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if committed := count(); committed > 0 && committed < total {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not expose partial committed progress")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMetadataSurvivesHardRestartWithoutRefetchingCommittedDetails(t *testing.T) {
	ctx, s, conn := testService(t)
	c := prepareMetadataPipeline(t, s)
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 1; i <= 120; i++ {
		ids = append(ids, fmt.Sprint(i))
	}
	if err := s.DB.QueueAIOMetadata(ctx, "movie", ids, true); err != nil {
		t.Fatal(err)
	}
	var resumed atomic.Bool
	var requests atomic.Int32
	var mu sync.Mutex
	afterRestart := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/changes") {
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/movie/")
		if resumed.Load() {
			mu.Lock()
			afterRestart[id] = true
			mu.Unlock()
		} else if requests.Add(1) > 40 {
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, strings.Replace(movieDetail, `"id":101`, `"id":`+id, 1))
	}))
	t.Cleanup(server.Close)
	endpoint := conn.Config().ConnString()
	child, _ := restartWorker(t, endpoint, "metadata", server.URL)
	waitForCommittedProgress(t, func() int {
		var count int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}, len(ids))
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	var committed []string
	var pending int
	if err := conn.QueryRow(ctx, `SELECT ARRAY(SELECT tmdb_id FROM ingest.aio_works), (SELECT count(*) FROM ingest.aio_metadata_jobs)`).Scan(&committed, &pending); err != nil || len(committed)+pending != len(ids) {
		t.Fatalf("hard stop lost metadata jobs: committed=%d pending=%d error=%v", len(committed), pending, err)
	}
	resumed.Store(true)
	child, output := restartWorker(t, endpoint, "metadata", server.URL)
	if err := child.Wait(); err != nil {
		t.Fatalf("restart failed: %v %s", err, output)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range committed {
		if afterRestart[id] {
			t.Fatal("committed metadata was downloaded again")
		}
	}
	var works, interrupted int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_metadata_jobs),(SELECT count(*) FROM ingest.operation_runs WHERE kind='metadata' AND status='interrupted')`).Scan(&works, &pending, &interrupted); err != nil || works != len(ids) || pending != 0 || interrupted != 1 {
		t.Fatalf("restart did not finish durable work: works=%d pending=%d interrupted=%d error=%v", works, pending, interrupted, err)
	}
}

func TestMatchingSurvivesHardRestartWithoutRewritingCommittedResults(t *testing.T) {
	ctx, _, conn := testService(t)
	if _, err := conn.Exec(ctx, `
INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','101','tt1234567',ARRAY[2021]);
INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,'Example Movie','example movie' FROM ingest.aio_works;
INSERT INTO ingest.torrents(provider_id,source_id,fields)
 SELECT 'synthetic',n::text,jsonb_build_object('title','Example.Movie.2021.1080p','imdb_id','tt1234567') FROM generate_series(1,2000) n;
CREATE FUNCTION ingest.slow_test_match() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.004); RETURN NEW; END $$;
CREATE TRIGGER slow_test_match BEFORE INSERT ON ingest.aio_matches FOR EACH ROW EXECUTE FUNCTION ingest.slow_test_match();`); err != nil {
		t.Fatal(err)
	}
	endpoint := conn.Config().ConnString()
	child, _ := restartWorker(t, endpoint, "matching", "")
	waitForCommittedProgress(t, func() int {
		var count int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_matches`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}, 2000)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	var committed, pending int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_matches),(SELECT count(*) FROM ingest.aio_match_queue)`).Scan(&committed, &pending); err != nil || committed == 0 || pending == 0 || committed+pending != 2000 {
		t.Fatalf("hard stop lost matching jobs: committed=%d pending=%d error=%v", committed, pending, err)
	}
	// Remember the actual committed rows after the process and its transactions
	// are gone; the restart must not overwrite their publication timestamps.
	if _, err := conn.Exec(ctx, `CREATE TABLE ingest.committed_test_matches AS SELECT provider_id,source_id,updated_at FROM ingest.aio_matches`); err != nil {
		t.Fatal(err)
	}
	child, output := restartWorker(t, endpoint, "matching", "")
	if err := child.Wait(); err != nil {
		t.Fatalf("restart failed: %v %s", err, output)
	}
	var matched, rewritten, interrupted int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_matches WHERE status='matched'),(SELECT count(*) FROM ingest.aio_match_queue),(SELECT count(*) FROM ingest.committed_test_matches c JOIN ingest.aio_matches m USING(provider_id,source_id) WHERE c.updated_at<>m.updated_at),(SELECT count(*) FROM ingest.operation_runs WHERE kind='matching' AND status='interrupted')`).Scan(&matched, &pending, &rewritten, &interrupted); err != nil || matched != 2000 || pending != 0 || rewritten != 0 || interrupted != 1 {
		t.Fatalf("matching restart changed durable work: matched=%d pending=%d rewritten=%d interrupted=%d error=%v", matched, pending, rewritten, interrupted, err)
	}
}
