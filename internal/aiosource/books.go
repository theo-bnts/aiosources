package aiosource

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

const openLibraryExport = "https://openlibrary.org/data/ol_dump_latest.txt.gz"
const openLibraryAgent = "AIOSources/2.2 (+https://github.com/theo-bnts/aiosources)"

func trustedBookExport(r *http.Request, via []*http.Request) error {
	host := r.URL.Hostname()
	if len(via) > 5 || r.URL.Scheme != "https" || r.URL.User != nil || !(host == "openlibrary.org" || host == "archive.org" || strings.HasSuffix(host, ".archive.org")) {
		return errors.New("untrusted book export redirect")
	}
	return nil
}

func (s *Service) BookState() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.booksRunning }
func (s *Service) StartBooks(trigger string) error {
	if trigger != "manual" && trigger != "automatic" {
		return model.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.ErrConflict
	}
	if s.booksRunning {
		return model.ErrBusy
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.booksRunning = true
	s.booksCancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		_ = s.SyncBooks(ctx, trigger)
		s.mu.Lock()
		s.booksRunning = false
		s.booksCancel = nil
		s.mu.Unlock()
		s.wakeAutomation()
	}()
	return nil
}
func (s *Service) StopBooks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.booksCancel != nil {
		s.booksCancel()
	}
}

// Bulk export access is intentionally independent of TMDB credentials and API
// quotas. Only compact bibliographic data is retained, never full dump JSON.
func (s *Service) syncBookExport(ctx context.Context, h *operationHistory) error {
	status, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
	if status.DataComplete && !status.Complete {
		return s.finishBookExport(ctx, h)
	}
	prior, etag, _, err := s.DB.OpenLibraryCheckpoint(ctx)
	if err != nil {
		return err
	}
	status, err = s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
	endpoint := s.BookExportURL
	if endpoint == "" {
		endpoint = openLibraryExport
	}
	if prior != "" && !status.Complete {
		endpoint = prior
	}
	requestCtx, requestCancel := context.WithCancel(ctx)
	defer requestCancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if err = s.bookRate.wait(ctx); err != nil {
		return err
	}
	req.Header.Set("User-Agent", openLibraryAgent)
	req.Header.Set("Accept-Encoding", "identity")
	client := s.BookHTTP
	if client == nil {
		client = &http.Client{Transport: s.Network.Transport("metadata:openlibrary", outbound.HTTPTransport()), CheckRedirect: trustedBookExport}
	}
	defer client.CloseIdleConnections()
	headerTimeout, err := s.outgoingTimeout(ctx, "metadata_seconds")
	if err != nil {
		return err
	}
	// Bound connection and header waits with the configured metadata timeout.
	// The large streaming export then uses an idle timeout rather than a total
	// request deadline, so a healthy transfer can run for several hours.
	idle := time.AfterFunc(headerTimeout, requestCancel)
	defer idle.Stop()
	requestStarted := time.Now()
	response, err := client.Do(req)
	idle.Reset(2 * time.Minute)
	httpStatus := 0
	if response != nil {
		httpStatus = response.StatusCode
	}
	_ = s.DB.RecordProviderCall(ctx, h.run.ID, "provider_request", httpStatus, time.Since(requestStarted).Milliseconds(), "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			delay := metadataRetryAfter(response.Header.Get("Retry-After"), 15*time.Minute, time.Now())
			s.bookRate.cooldown(delay)
			return &bookRequestFailure{delay: delay}
		}
		return errors.New("book export request failed")
	}
	snapshot := response.Request.URL.String()
	identity := response.Header.Get("ETag")
	if identity == "" {
		identity = response.Header.Get("Last-Modified") + ":" + response.Header.Get("Content-Length")
	}
	if identity == ":" {
		return errors.New("book export has no stable revision")
	}
	if err = s.DB.BookExportChecked(ctx); err != nil {
		return err
	}
	snapshotAt, dated := books.ExportDate(snapshot)
	if !dated {
		return errors.New("book export has no verifiable snapshot date")
	}
	if snapshotAt.After(time.Now().UTC()) {
		return errors.New("book export snapshot date is in the future")
	}
	if status.SnapshotAt != nil && snapshotAt.Before(*status.SnapshotAt) {
		return errors.New("book export is older than the saved snapshot")
	}
	changes, err := s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	if status.Complete && ((snapshot == prior && identity == etag) || (changes.NeedsDumpAfter != nil && snapshotAt.Before(*changes.NeedsDumpAfter))) {
		return nil
	}
	position, err := s.DB.BeginOpenLibrary(ctx, snapshot, identity)
	if err != nil {
		return err
	}
	h.checkpoint(ctx, "book_import")
	body := &bookExportReader{reader: response.Body, timer: idle}
	compressed, err := gzip.NewReader(body)
	if err != nil {
		return err
	}
	defer compressed.Close()
	limited := &io.LimitedReader{R: compressed, N: (500 << 30) + 1}
	scanner := books.NewExportScanner(limited)
	line, saved := int64(0), position
	batch := make([]books.Record, 0, 250)
	lastHistory := time.Now()
	flush := func() error {
		if line <= saved {
			return nil
		}
		if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, saved, line, batch); err != nil {
			return err
		}
		saved = line
		batch = batch[:0]
		h.run.Processed = saved
		if time.Since(lastHistory) >= 5*time.Second {
			h.checkpoint(ctx, "book_import")
			lastHistory = time.Now()
		}
		return nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line++
		if line <= position {
			continue
		}
		record := books.Record{Skipped: scanner.Skipped()}
		if !record.Skipped {
			decoded, err := books.DecodeLine(scanner.Bytes())
			if err != nil {
				record.Skipped = true
			} else {
				record = decoded
			}
		}
		if record.Skipped {
			h.run.Errors++
		}
		batch = append(batch, record)
		if len(batch) == 250 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if limited.N == 0 {
		return errors.New("book export exceeds the supported size")
	}
	if line < position {
		return errors.New("book export ended before saved progress")
	}
	if line == 0 {
		return errors.New("empty book export")
	}
	if err := flush(); err != nil {
		return err
	}
	completed, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
	if completed.Works == 0 || completed.Editions == 0 || completed.Authors == 0 {
		return errors.New("book export is missing bibliographic record types")
	}
	idle.Stop()
	_ = response.Body.Close()
	if err = s.DB.BookExportLoaded(ctx); err != nil {
		return err
	}
	return s.finishBookExport(ctx, h)
}

func (s *Service) finishBookExport(ctx context.Context, h *operationHistory) error {
	h.checkpoint(ctx, "book_reconciliation")
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := s.DB.PruneBookSnapshot(ctx)
		if err != nil {
			return err
		}
		if done {
			break
		}
	}
	if err := s.DB.FinishOpenLibrary(ctx); err != nil {
		return err
	}
	state, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
	if state.SnapshotAt != nil {
		if err = s.DB.BeginBookCatchup(ctx, *state.SnapshotAt); err != nil {
			return err
		}
	}
	h.checkpoint(ctx, "book_changes")
	return nil
}

type bookExportReader struct {
	reader io.Reader
	timer  *time.Timer
}

func (r *bookExportReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.timer.Reset(2 * time.Minute)
	}
	return n, err
}
