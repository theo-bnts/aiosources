package aiosource

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestIdentificationUsesConfiguredWorkerCountInActualTransactions(t *testing.T) {
	for _, workerCount := range []int{1, 3} {
		t.Run(string(rune('0'+workerCount)), func(t *testing.T) {
			ctx, s, conn := testService(t)
			policy, _ := s.DB.RuntimeSection(ctx, "identification")
			policy.Values["workers"], policy.Values["queue_batch"], policy.Values["transaction_batch"] = workerCount, 20, 1
			if err := s.DB.UpdateRuntimeSection(ctx, "identification", policy); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Film"}, Years: []int{2021}}, []string{"example film"}); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, `
CREATE FUNCTION ingest.test_match_capacity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM pg_advisory_xact_lock_shared(12031203,732);
 PERFORM pg_sleep(0.04); RETURN NEW; END; $$;
CREATE TRIGGER test_match_capacity BEFORE INSERT ON ingest.aio_matches FOR EACH ROW EXECUTE FUNCTION ingest.test_match_capacity();
INSERT INTO ingest.torrents(provider_id,source_id,fields)
 SELECT 'capacity-test',n::text,'{"title":"Example.Film.2021.1080p","imdb_id":"tt1234567"}'::jsonb FROM generate_series(1,20)n;`); err != nil {
				t.Fatal(err)
			}
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- s.Match(work) }()
			peak := 0
			for {
				select {
				case err := <-finished:
					if err != nil || peak != workerCount {
						t.Fatal("effective worker count differs from the saved limit", workerCount, peak, err)
					}
					return
				case <-work.Done():
					t.Fatal(work.Err())
				case <-time.After(5 * time.Millisecond):
					var active int
					if err := conn.QueryRow(work, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND classid=12031203 AND objid=732 AND granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&active); err != nil {
						t.Fatal(err)
					}
					peak = max(peak, active)
					if active > workerCount {
						t.Fatal("worker count exceeded saved limit", active)
					}
				}
			}
		})
	}
}

func TestMatchingReliabilityPolicyCanTightenWithoutWeakeningConflicts(t *testing.T) {
	p := ParseRelease("Example.Documentary.Adventure.2021.1080p")
	c := Candidate{EntryID: "1", WorkID: "1", Source: "tmdb", SourceID: "101", Type: "movie", Titles: []string{"Example Documentary Adventures"}, Years: []int{2021}, IDs: map[string]string{"imdb": "tt1234567"}}
	if score := similarity(p.Normalized, Normalize(c.Titles[0])); score < .92 || score >= 1 {
		t.Fatal("fixture is not a close alias", score)
	}
	ids := SourceIDs{IMDb: "tt1234567"}
	if got := ChooseMatch(p, []Candidate{c}, ids, false, SourceEpisode{}); got.Status != "matched" {
		t.Fatal("recommended policy lost supported match", got)
	}
	strict := model.MatchingPolicy{MinimumSimilarity: 100, AmbiguityMargin: 8}
	if got := ChooseMatchWithPolicy(p, []Candidate{c}, ids, false, SourceEpisode{}, strict); got.Status == "matched" {
		t.Fatal("saved title threshold was ignored")
	}
	for _, policy := range []model.MatchingPolicy{model.DefaultMatchingPolicy(), strict} {
		if got := ChooseMatchWithPolicy(p, []Candidate{c}, SourceIDs{IMDb: "tt2345678"}, false, SourceEpisode{}, policy); got.Status == "matched" {
			t.Fatal("policy weakened contradictory ID rejection")
		}
	}
	c.Titles = []string{"Example Documentary Adventure"}
	other := c
	other.WorkID, other.EntryID, other.SourceID, other.IDs, other.Titles = "2", "2", "102", map[string]string{}, []string{"Example Documentary Adventure Special"}
	if got := ChooseMatch(p, []Candidate{c, other}, ids, false, SourceEpisode{}); got.Status != "matched" {
		t.Fatal("recommended ambiguity policy changed", got)
	}
	if got := ChooseMatchWithPolicy(p, []Candidate{c, other}, ids, false, SourceEpisode{}, model.MatchingPolicy{MinimumSimilarity: 92, AmbiguityMargin: 25}); got.Status != "ambiguous" {
		t.Fatal("wider ambiguity margin was ignored", got)
	}
}

func TestMetadataRetriesUseSavedAttemptCount(t *testing.T) {
	ctx, s, _ := testService(t)
	var calls atomic.Int32
	s.MetadataHTTP.Transport = metadataRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	policy, _ := s.DB.RuntimeSection(ctx, "metadata_retries")
	policy.Values["request_attempts"] = 1
	if err := s.DB.UpdateRuntimeSection(ctx, "metadata_retries", policy); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if _, err := s.metadataGET(ctx, "synthetic-key", "movie/101", &result); err == nil || calls.Load() != 1 {
		t.Fatal("single attempt was not applied", err, calls.Load())
	}
	policy, _ = s.DB.RuntimeSection(ctx, "metadata_retries")
	policy.Values["request_attempts"] = 2
	if err := s.DB.UpdateRuntimeSection(ctx, "metadata_retries", policy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.metadataGET(ctx, "synthetic-key", "movie/101", &result); err == nil || calls.Load() != 3 {
		t.Fatal("updated attempt count was not applied", err, calls.Load())
	}
}

func TestMetadataProcessingHonorsConfiguredConcurrencyAndBatchBounds(t *testing.T) {
	ctx, s, conn := testService(t)
	prepareMetadataPipeline(t, s)
	policy, _ := s.DB.RuntimeSection(ctx, "metadata")
	policy.Values["workers"], policy.Values["queue_batch"], policy.Values["publication_batch"] = 1, 2, 1
	if err := s.DB.UpdateRuntimeSection(ctx, "metadata", policy); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101", "102", "103"}, true); err != nil {
		t.Fatal(err)
	}
	var calls, active, peak atomic.Int32
	s.MetadataHTTP.Transport = metadataRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		time.Sleep(15 * time.Millisecond)
		body := strings.Replace(movieDetail, `"id":101`, `"id":`+path.Base(r.URL.Path), 1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	jobs, err := s.DB.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 2 {
		t.Fatal("configured queue batch ignored", err)
	}
	history, err := s.beginOperation(ctx, "metadata", "tmdb", "manual", "", "metadata")
	if err != nil {
		t.Fatal(err)
	}
	defer history.finish(ctx, nil)
	if err := s.processMetadataJobs(ctx, "synthetic-key", jobs, history, &metadataProgress{}, false); err != nil {
		t.Fatal(err)
	}
	var published, pending int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_metadata_jobs)`).Scan(&published, &pending); err != nil || published != 2 || pending != 1 || calls.Load() != 2 || peak.Load() != 1 {
		t.Fatal("processing limits or remaining queue changed", published, pending, calls.Load(), peak.Load(), err)
	}
}

func TestAvailabilityRetryPolicyUsesSavedDelay(t *testing.T) {
	ctx, s, _ := testService(t)
	const key = "synthetic-availability-key"
	hashes := []string{strings.Repeat("a", 40)}
	generation, err := s.DB.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.DB.SaveAIOResource(ctx, model.AIOResource{Service: "torbox", Account: fingerprint(key), Key: "example-resource", Hashes: hashes, CheckedAt: now, LastAttempt: now.Add(-2 * time.Minute), Retry: 1, Generation: generation}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s.Providers.Client.Transport = metadataRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{}}`)), Request: r}, nil
	})
	c, _ := s.DB.AIOConfig(ctx)
	if err := s.checkResource(ctx, "torbox", key, "example-resource", hashes, c); err != nil || calls.Load() != 0 {
		t.Fatal("recommended retry delay changed", err)
	}
	policy, _ := s.DB.RuntimeSection(ctx, "debrid_retries")
	policy.Values["delay_minutes"] = 1
	if err := s.DB.UpdateRuntimeSection(ctx, "debrid_retries", policy); err != nil {
		t.Fatal(err)
	}
	if err := s.checkResource(ctx, "torbox", key, "example-resource", hashes, c); err != nil || calls.Load() != 1 {
		t.Fatal("saved availability retry ignored", err, calls.Load())
	}
	if err := s.CheckPublicConfigurationSize(ctx, "torznab", strings.Repeat("a", 6001)); !errors.Is(err, model.ErrConfigurationLimit) {
		t.Fatal("configuration ceiling not enforced", err)
	}
}

func TestReducedPublicConfigurationLimitsApplyToNewLinksOnly(t *testing.T) {
	ctx, s, _ := testService(t)
	if err := s.DB.SetPublicAccess(ctx, model.PublicAccess{Revision: 1, Enabled: true, TorznabEnabled: true, ExposeSources: true}); err != nil {
		t.Fatal(err)
	}
	options := model.PublicTorznabOptions{}
	for i := range 40 {
		options.Sources = append(options.Sources, strings.Repeat("x", 25)+strconv.Itoa(i))
	}
	value, err := s.CreatePublicTorznab(ctx, "https://public.example", options, nil)
	if err != nil || len(value) <= 1024 {
		t.Fatal("could not create initial configuration", err)
	}
	policy, _ := s.DB.RuntimeSection(ctx, "payloads")
	policy.Values["public_torznab_bytes"], policy.Values["public_stremio_bytes"] = 1024, 1024
	if err := s.DB.UpdateRuntimeSection(ctx, "payloads", policy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePublicTorznab(ctx, "https://public.example", options, nil); !errors.Is(err, model.ErrConfigurationLimit) {
		t.Fatal("new configuration ignored the smaller limit", err)
	}
	if _, err := s.OpenPublicTorznab(ctx, "https://public.example", value); err != nil {
		t.Fatal("existing authenticated configuration became unreadable", err)
	}
	if err := s.CheckPublicConfigurationSize(ctx, "stremio", strings.Repeat("x", 1025)); !errors.Is(err, model.ErrConfigurationLimit) {
		t.Fatal("Stremio configuration limit ignored", err)
	}
}
