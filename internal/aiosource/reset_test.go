package aiosource

import (
	"context"
	"errors"
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
