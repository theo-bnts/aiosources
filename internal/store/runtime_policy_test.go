package store

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestExtendedSettingsMigrationPreservesPoliciesAndSavedImports(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.migrateThrough(ctx, 73); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.runtime_settings SET values=jsonb_set(values,'{days}','14'),revision=8 WHERE section='history'`); err != nil {
		t.Fatal(err)
	}
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "migration-source", Adapter: "json"}, model.ModeFull)
	run, err = db.SavePage(ctx, run, model.Page{Next: []byte(`{"page":7}`)}, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	history, err := db.RuntimeSection(ctx, "history")
	if err != nil || history.Values["days"] != 14 || history.Values["batch_size"] != 5000 || history.Revision != 9 {
		t.Fatal("saved retention was overwritten", history, err)
	}
	after, err := db.GetRun(ctx, run.ID)
	if err != nil || string(after.Cursor) != string(run.Cursor) || after.Status != run.Status {
		t.Fatal("migration changed an import checkpoint", err)
	}
	for _, section := range model.RuntimeSections() {
		if value, err := db.RuntimeSection(ctx, section); err != nil || !value.Valid(section) {
			t.Fatal("missing operational policy", section, err)
		}
	}
}

func TestMatchingPolicyRechecksAutomaticResultsAndKeepsManualReviews(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, source := range []string{"automatic", "reviewed"} {
		publishSharingRow(t, ctx, db, source, "item", `{"title":"Example Film 2021","info_hash":"`+strings.Repeat("a", 40)+`"}`, nil)
	}
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Film"}, Years: []int{2021}}, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	works, err := db.AIOReviewCandidates(ctx, "tt1234567", "tt1234567", "")
	if err != nil || len(works) != 1 {
		t.Fatal(err)
	}
	w := works[0]
	items, _ := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{ProviderID: "reviewed"})
	detail, err := db.AIOReviewDetail(ctx, items.Items[0].OccurrenceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAIOReview(ctx, items.Items[0].OccurrenceID, model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, WorkID: w.ID, WorkRevision: w.Revision}); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.AIOMatchBatch(ctx, 10)
	if _, err := db.MatchAIOBatch(ctx, rows, func(r model.AIORecord, _ AIOCandidateSearch) (model.AIOMatchResult, error) {
		return model.AIOMatchResult{Record: r, Status: "matched", Reason: "test correspondence", Work: &w}, nil
	}); err != nil {
		t.Fatal(err)
	}
	policy, _ := db.RuntimeSection(ctx, "matching")
	policy.Values["minimum_similarity"] = 100
	if err := db.UpdateRuntimeSection(ctx, "matching", policy); err != nil {
		t.Fatal(err)
	}
	streams, err := db.AIOStreams(ctx, "imdb", "tt1234567", "movie")
	if err != nil || len(streams) != 1 || streams[0].Provider != "reviewed" {
		t.Fatal("policy change lost a manual review or retained an old automatic result", err)
	}
	rows, err = db.AIOMatchBatch(ctx, 10)
	if err != nil || len(rows) != 1 || rows[0].Provider != "automatic" {
		t.Fatal("automatic result was not queued", err)
	}
	generation, _ := db.AIOCandidateGeneration(ctx)
	policy, _ = db.RuntimeSection(ctx, "matching")
	if err := db.UpdateRuntimeSection(ctx, "matching", policy); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.AIOCandidateGeneration(ctx); got != generation {
		t.Fatal("saving unchanged thresholds requeued the catalog")
	}
}

func TestMatchingPolicyIsFencedWithPublicationAndBypassesStaleRuntimeSnapshots(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "fenced", "item", `{"title":"Example Film"}`, nil)
	peer, err := Open(ctx, db.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if _, err := db.RuntimeValues(ctx, "matching"); err != nil {
		t.Fatal(err)
	}
	// Model a different replica's old snapshot. Decisions must use the policy
	// under the same publication fence, independently of this ordinary cache.
	db.runtime.mu.Lock()
	db.runtime.until = time.Now().Add(time.Hour)
	db.runtime.mu.Unlock()
	rows, _ := db.AIOMatchBatch(ctx, 10)
	entered, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	finished := make(chan error, 1)
	go func() {
		_, err := db.MatchAIOBatch(ctx, rows, func(r model.AIORecord, _ AIOCandidateSearch) (model.AIOMatchResult, error) {
			close(entered)
			<-release
			return model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "test"}, nil
		})
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	policy, _ := peer.RuntimeSection(ctx, "matching")
	policy.Values["minimum_similarity"] = 100
	saved := make(chan error, 1)
	go func() { saved <- peer.UpdateRuntimeSection(ctx, "matching", policy) }()
	select {
	case err := <-saved:
		t.Fatal("policy bypassed publication fence", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	released = true
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	rows, _ = db.AIOMatchBatch(ctx, 10)
	if _, err := db.MatchAIOBatch(ctx, rows, func(r model.AIORecord, _ AIOCandidateSearch) (model.AIOMatchResult, error) {
		if r.MatchingPolicy.MinimumSimilarity != 100 {
			t.Error("decision used an old policy")
		}
		return model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "test"}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataQueueAndDeferredRetryUseSavedBounds(t *testing.T) {
	ctx, db := sharingDatabase(t)
	policy, _ := db.RuntimeSection(ctx, "metadata")
	policy.Values["queue_batch"], policy.Values["publication_batch"] = 2, 1
	if err := db.UpdateRuntimeSection(ctx, "metadata", policy); err != nil {
		t.Fatal(err)
	}
	if err := db.QueueAIOMetadata(ctx, "movie", []string{"101", "102", "103"}, true); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 2 {
		t.Fatal("queue batch limit ignored", err)
	}
	retries, _ := db.RuntimeSection(ctx, "metadata_retries")
	retries.Values["detail_retry_seconds"], retries.Values["detail_retry_max_seconds"] = 120, 300
	if err := db.UpdateRuntimeSection(ctx, "metadata_retries", retries); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishAIOMetadata(ctx, jobs[0], false); err != nil {
		t.Fatal(err)
	}
	var due time.Time
	if err := db.pool.QueryRow(ctx, `SELECT next_attempt FROM ingest.aio_metadata_jobs WHERE media_type=$1 AND tmdb_id=$2`, jobs[0].Type, jobs[0].ID).Scan(&due); err != nil || time.Until(due) < 118*time.Second || time.Until(due) > 121*time.Second {
		t.Fatal("retry policy ignored", err)
	}
}

func TestSourceRetrySettingsAreCapturedAndRemainStableAfterResume(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	settings, _ := db.RuntimeSection(ctx, "source_retries")
	settings.Values["retries"], settings.Values["initial_seconds"], settings.Values["maximum_seconds"] = 1, 3, 5
	if err := db.UpdateRuntimeSection(ctx, "source_retries", settings); err != nil {
		t.Fatal(err)
	}
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "retry-snapshot", Adapter: "json"}, model.ModeFull)
	if run.EffectivePolicy().SourceRetries != (model.SourceRetryPolicy{Retries: 1, InitialSeconds: 3, MaximumSeconds: 5}) {
		t.Fatal("source retry policy was not captured")
	}
	run, err := db.SavePage(ctx, run, model.Page{Next: []byte(`{"page":2}`)}, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseManual); err != nil {
		t.Fatal(err)
	}
	settings, _ = db.RuntimeSection(ctx, "source_retries")
	settings.Values["retries"] = 0
	if err := db.UpdateRuntimeSection(ctx, "source_retries", settings); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ResumeRun(ctx, run.ID)
	if err != nil || resumed.EffectivePolicy() != run.EffectivePolicy() || string(resumed.Cursor) != string(run.Cursor) {
		t.Fatal("resume changed saved retries or checkpoint", err)
	}
	created, err := db.CreateRun(ctx, model.Run{ProviderID: "new-retry-policy", Config: model.Provider{ID: "new-retry-policy", Adapter: "json"}, Mode: model.ModePreview})
	if err != nil || created.EffectivePolicy().SourceRetries.Retries != 0 {
		t.Fatal("new imports did not receive the changed policy", err)
	}
}

func TestHistoryCleanupUsesConfiguredTableBatch(t *testing.T) {
	ctx, db := sharingDatabase(t)
	policy, err := db.RuntimeSection(ctx, "history")
	if err != nil {
		t.Fatal(err)
	}
	policy.Values["batch_size"] = 100
	if err := db.UpdateRuntimeSection(ctx, "history", policy); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message,created_at)
 SELECT 'test-cleanup-'||n,'info','test.cleanup','Synthetic cleanup record',now()-interval '8 days' FROM generate_series(1,201)n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PruneHistory(ctx); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind='test.cleanup'`).Scan(&remaining); err != nil || remaining != 101 {
		t.Fatal("cleanup ignored the configured table batch", remaining, err)
	}
}
