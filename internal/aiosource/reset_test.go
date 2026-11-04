package aiosource

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

func waitReset(t *testing.T, s *Service) ResetState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		state := s.ResetState()
		if state.Status != "running" {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatal("maintenance did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestResetRunsInBackgroundWithoutHoldingTheServiceMutex(t *testing.T) {
	ctx, s, conn := testService(t)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL100W','Synthetic Book','{}'); UPDATE ingest.ol_import_state SET works=1`); err != nil {
		t.Fatal(err)
	}
	block, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback(context.Background())
	// Hold the transaction's first fence, as a slow catalog writer would. Status
	// and admission must remain responsive even while maintenance waits for it.
	if _, err = block.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,4)`); err != nil {
		t.Fatal(err)
	}
	if err := s.StartReset("books"); err != nil {
		t.Fatal(err)
	}
	responsive := make(chan ResetState, 1)
	go func() {
		s.State()
		s.BookState()
		responsive <- s.ResetState()
	}()
	select {
	case state := <-responsive:
		if state.Status != "running" || state.Scope != "books" {
			t.Fatal(state)
		}
	case <-time.After(time.Second):
		t.Fatal("a waiting reset blocked status reads")
	}
	if err := s.StartBooks("manual"); !errors.Is(err, model.ErrBusy) {
		t.Fatal("book update admitted during reset", err)
	}
	if err := s.StartMatch(); !errors.Is(err, model.ErrBusy) {
		t.Fatal("identification admitted during reset", err)
	}
	if err := s.StartReset("videos"); !errors.Is(err, model.ErrBusy) {
		t.Fatal("second reset admitted", err)
	}
	if err := block.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if state := waitReset(t, s); state.Status != "succeeded" {
		t.Fatal(state)
	}
	var books int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works WHERE media_type='book'`).Scan(&books); err != nil || books != 0 {
		t.Fatal("reset did not remove book information", books, err)
	}
	logs, err := s.DB.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 50}, Service: "openlibrary"})
	if err != nil || len(logs.Items) != 1 || logs.Items[0].Message != "Book information reset completed" {
		t.Fatal("reset completion was not logged", logs, err)
	}
}

func TestResetFailurePreservesDataAndReleasesAdmission(t *testing.T) {
	ctx, s, conn := testService(t)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL100W','Synthetic Book','{}')`); err != nil {
		t.Fatal(err)
	}
	release, locked, err := s.DB.AIOLock(ctx, "openlibrary")
	if err != nil || !locked {
		t.Fatal(err)
	}
	defer release()
	if err := s.StartReset("books"); err != nil {
		t.Fatal(err)
	}
	if state := waitReset(t, s); state.Status != "failed" || state.Error == "" {
		t.Fatal(state)
	}
	var books int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works WHERE media_type='book'`).Scan(&books); err != nil || books != 1 {
		t.Fatal("failed reset changed book data", books, err)
	}
	release()
	if err := s.StartReset("books"); err != nil {
		t.Fatal("failed reset retained its reservation", err)
	}
	if state := waitReset(t, s); state.Status != "succeeded" {
		t.Fatal(state)
	}
}

func TestShutdownCancelsWaitingResetWithoutCommitting(t *testing.T) {
	ctx, s, conn := testService(t)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL100W','Synthetic Book','{}')`); err != nil {
		t.Fatal(err)
	}
	block, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback(context.Background())
	if _, err = block.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,4)`); err != nil {
		t.Fatal(err)
	}
	if err = s.StartReset("books"); err != nil {
		t.Fatal(err)
	}
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = s.Close(closeCtx); err != nil {
		t.Fatal("shutdown did not cancel pending maintenance", err)
	}
	var books int
	if err = block.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works WHERE media_type='book'`).Scan(&books); err != nil || books != 1 {
		t.Fatal("interrupted reset changed book data", books, err)
	}
}

func TestResetStopsAndJoinsActiveInformationWorkers(t *testing.T) {
	for _, scope := range []string{"books", "videos", "metadata", "mapping"} {
		t.Run(scope, func(t *testing.T) {
			ctx, s, conn := testService(t)
			if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL100W','Synthetic Book','{}')`); err != nil {
				t.Fatal(err)
			}
			bookStarted, bookCancelled := make(chan struct{}), make(chan struct{})
			s.BookHTTP = &http.Client{Transport: metadataRoundTrip(func(r *http.Request) (*http.Response, error) {
				close(bookStarted)
				<-r.Context().Done()
				close(bookCancelled)
				return nil, r.Context().Err()
			})}
			if err := s.StartBooks("manual"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-bookStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("book worker did not start")
			}
			finished := make(chan struct{})
			var finishOnce sync.Once
			finish := func() { finishOnce.Do(func() { close(finished) }) }
			defer finish()
			ready, cancelled := make(chan error, 2), make(chan struct{}, 2)
			for _, syncJob := range []bool{true, false} {
				if err := s.startJob(syncJob, func(work context.Context) error {
					name := "matcher"
					if syncJob {
						name = "metadata"
					}
					release, locked, err := s.DB.AIOLock(work, name)
					if err == nil && !locked {
						err = model.ErrBusy
					}
					ready <- err
					if err != nil {
						return err
					}
					defer release()
					<-work.Done()
					cancelled <- struct{}{}
					// Cancellation is not completion: keep the worker lock until
					// its final checkpoint/cleanup would have finished.
					<-finished
					return work.Err()
				}); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				select {
				case err := <-ready:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("information worker did not start")
				}
			}
			if err := s.StartReset(scope); err != nil {
				t.Fatal("active workers prevented reset admission", err)
			}
			for range 2 {
				select {
				case <-cancelled:
				case <-time.After(5 * time.Second):
					t.Fatal("reset did not stop information/identification")
				}
			}
			select {
			case <-bookCancelled:
			case <-time.After(5 * time.Second):
				t.Fatal("reset did not stop the book request")
			}
			if state := s.ResetState(); state.Status != "running" || state.Phase != "stopping" {
				t.Fatal("reset did not expose its stopping phase", state)
			}
			if err := s.StartBooks("automatic"); !errors.Is(err, model.ErrBusy) {
				t.Fatal("automatic book update replaced the stopping worker", err)
			}
			if err := s.startMatch("automatic"); !errors.Is(err, model.ErrBusy) {
				t.Fatal("automatic identification replaced the stopping worker", err)
			}
			if err := s.StartReset("cache"); !errors.Is(err, model.ErrBusy) {
				t.Fatal("another reset replaced pending maintenance", err)
			}
			var count int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works WHERE media_type='book'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("reset changed data before its workers finished", count, err)
			}
			finish()
			if state := waitReset(t, s); state.Status != "succeeded" || state.Phase != "" {
				t.Fatal(state)
			}
			if syncing, matching, _ := s.State(); syncing || matching || s.BookState() {
				t.Fatal("reset completed before every worker released admission")
			}
			want := 1
			if scope == "books" || scope == "metadata" {
				want = 0
			}
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works WHERE media_type='book'`).Scan(&count); err != nil || count != want {
				t.Fatal("reset did not preserve its scope", count, want, err)
			}
		})
	}
}

func TestCancelledResetWaitPreservesDataAndReleasesAdmission(t *testing.T) {
	ctx, s, conn := testService(t)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,display_title,years) VALUES('book','OL100W','Synthetic Book','{}')`); err != nil {
		t.Fatal(err)
	}
	stopping, finished := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(finished) }) }
	defer finish()
	if err := s.startJob(false, func(work context.Context) error {
		<-work.Done()
		close(stopping)
		<-finished
		return work.Err()
	}); err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.Reset(work, "books") }()
	select {
	case <-stopping:
	case <-time.After(5 * time.Second):
		t.Fatal("reset did not begin stopping identification")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("reset wait ignored cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled reset stayed stuck waiting for a worker")
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works WHERE media_type='book'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("cancelled reset removed information", count, err)
	}
	finish()
	if err := s.StartReset("books"); err != nil {
		t.Fatal("cancelled reset retained its reservation", err)
	}
	if state := waitReset(t, s); state.Status != "succeeded" {
		t.Fatal(state)
	}
}

func TestCacheResetDoesNotStopInformationOrIdentification(t *testing.T) {
	ctx, s, _ := testService(t)
	ready := make(chan context.Context, 2)
	for _, syncJob := range []bool{true, false} {
		if err := s.startJob(syncJob, func(work context.Context) error {
			ready <- work
			<-work.Done()
			return work.Err()
		}); err != nil {
			t.Fatal(err)
		}
	}
	var workers []context.Context
	for range 2 {
		select {
		case work := <-ready:
			workers = append(workers, work)
		case <-time.After(5 * time.Second):
			t.Fatal("information worker did not start")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Reset(cancelled, "books"); !errors.Is(err, context.Canceled) {
		t.Fatal("already cancelled reset was admitted", err)
	}
	if err := s.Reset(ctx, "cache"); err != nil {
		t.Fatal(err)
	}
	for _, work := range workers {
		if work.Err() != nil {
			t.Fatal("cache reset cancelled an information worker", work.Err())
		}
	}
	if syncing, matching, _ := s.State(); !syncing || !matching {
		t.Fatal("cache reset interrupted unrelated information workers")
	}
}
