package aiosource

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
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
	if s.booksRunning || s.reset.Status == "running" {
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
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	current, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
	if current.DataComplete && !current.Complete {
		return s.finishBookExport(ctx, h)
	}
	selection := books.ImportSelection{RelevantOnly: c.OpenLibraryRelevantOnly, Languages: c.OpenLibraryLanguages}
	if current.ExportPhase <= 1 || current.Complete {
		selection, err = s.DB.BookImportSelection(ctx, c)
		if err != nil {
			return err
		}
	}
	for {
		state, err := s.DB.OpenLibraryStatus(ctx)
		if err != nil {
			return err
		}
		if state.DataComplete && !state.Complete {
			return s.finishBookExport(ctx, h)
		}
		phase := state.ExportPhase
		if selection.Filtered() && (phase == 0 || phase == 5) {
			phase = 1
		}
		if err = s.syncBookExportPart(ctx, h, selection, phase); err != nil {
			return err
		}
		state, err = s.DB.OpenLibraryStatus(ctx)
		if err != nil {
			return err
		}
		if state.Complete || phase == 0 {
			return nil
		}
		if phase == 1 {
			selection.Titles = nil
			selection.Identifiers = nil
		}
	}
}

func bookPartEndpoint(snapshot string, phase int) string {
	u, err := url.Parse(snapshot)
	if err != nil {
		return snapshot
	}
	at, ok := books.ExportDate(snapshot)
	if !ok {
		return snapshot
	}
	name := "ol_dump_editions_" + at.Format("2006-01-02") + ".txt.gz"
	if strings.HasSuffix(u.Path, "/"+name) {
		u.Path = strings.TrimSuffix(u.Path, name) + "ol_dump_" + books.ExportPartName(phase) + "_" + at.Format("2006-01-02") + ".txt.gz"
		return u.String()
	}
	return snapshot
}

func (s *Service) syncBookExportPart(ctx context.Context, h *operationHistory, selection books.ImportSelection, phase int) error {
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
		if phase > 0 {
			endpoint = "https://openlibrary.org/data/ol_dump_editions_latest.txt.gz"
		}
	}
	if prior != "" && !status.Complete {
		endpoint = prior
	}
	if phase > 0 {
		if status.PhaseSnapshot != "" {
			endpoint = status.PhaseSnapshot
		} else if phase > 1 {
			endpoint = bookPartEndpoint(status.Snapshot, phase)
		}
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
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable && strings.TrimSpace(response.Header.Get("Retry-After")) != "" {
			return s.bookProviderFailure(ctx, response.Header.Get("Retry-After"))
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
	position := int64(0)
	if phase == 0 {
		position, err = s.DB.BeginOpenLibrary(ctx, snapshot, identity)
	} else {
		err = s.DB.BeginBookExportPart(ctx, phase, snapshot, identity)
		if err == nil {
			status, err = s.DB.OpenLibraryStatus(ctx)
			position = status.PhasePosition
		}
	}
	if err != nil {
		return err
	}
	step := "book_import"
	if phase > 0 {
		step = "book_import_" + books.ExportPartName(phase)
	}
	h.checkpoint(ctx, step)
	body := &bookExportReader{reader: response.Body, timer: idle}
	compressed, err := gzip.NewReader(body)
	if err != nil {
		return err
	}
	defer compressed.Close()
	limited := &io.LimitedReader{R: compressed, N: (500 << 30) + 1}
	scanner := books.NewExportScanner(limited)
	line, saved := int64(0), position
	typedLines := int64(0)
	expectedType := "/type/" + strings.TrimSuffix(books.ExportPartName(phase), "s")
	typePrefix := []byte(expectedType + "\t")
	base := int64(0)
	if phase > 0 {
		base = status.Position - position
	}
	batch := make([]books.Record, 0, 250)
	type rawRecord struct {
		key  string
		data []byte
	}
	rawBatch := []rawRecord{}
	rawBytes := 0
	filtered := int64(0)
	lastHistory := time.Now()
	lastCommit := time.Now()
	flush := func() error {
		if line <= saved {
			return nil
		}
		if len(rawBatch) > 0 {
			keys := make([]string, 0, len(rawBatch))
			for _, row := range rawBatch {
				keys = append(keys, row.key)
			}
			chosen, err := s.DB.SelectBookExportKeys(ctx, keys)
			if err != nil {
				return err
			}
			for _, row := range rawBatch {
				if !chosen[row.key] {
					filtered++
					continue
				}
				r, err := books.DecodeLine(row.data)
				if err != nil {
					r = books.Record{Skipped: true}
				}
				if r.Skipped {
					r.PreserveKey = row.key
				}
				if r.Skipped {
					h.run.Errors++
				}
				batch = append(batch, r)
			}
		}
		if phase == 0 {
			if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, saved, line, batch); err != nil {
				return err
			}
		} else {
			if err := s.DB.SaveBookExportPartBatch(ctx, status.Snapshot, base+saved, base+line, batch, phase, line, filtered); err != nil {
				return err
			}
		}
		saved = line
		batch = batch[:0]
		rawBatch = rawBatch[:0]
		rawBytes = 0
		filtered = 0
		lastCommit = time.Now()
		h.run.Processed = base + saved
		if time.Since(lastHistory) >= 5*time.Second {
			h.checkpoint(ctx, step)
			lastHistory = time.Now()
		}
		return nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line++
		if phase > 0 && !scanner.Skipped() && bytes.HasPrefix(scanner.Bytes(), typePrefix) {
			typedLines++
		}
		if line <= position {
			continue
		}
		if phase > 0 && !scanner.Skipped() {
			columns := bytes.SplitN(scanner.Bytes(), []byte{'\t'}, 5)
			if len(columns) == 5 && string(columns[0]) != expectedType {
				continue
			}
			if phase == 1 {
				e, err := books.ProbeEdition(scanner.Bytes())
				if err == nil {
					if selection.RelevantOnly && !e.Digital {
						e.Normalized = books.Normalize(e.Title)
					}
					if !selection.Allows(e) {
						filtered++
						if line-saved >= 10000 || time.Since(lastCommit) >= 2*time.Second {
							if err = flush(); err != nil {
								return err
							}
						}
						continue
					}
				} else {
					h.run.Errors++
					batch = append(batch, books.Record{Skipped: true})
					if len(batch) >= 250 {
						if err = flush(); err != nil {
							return err
						}
					}
					continue
				}
			} else if len(columns) == 5 && books.Key(string(columns[1])) != "" {
				rawBatch = append(rawBatch, rawRecord{key: books.Key(string(columns[1])), data: bytes.Clone(scanner.Bytes())})
				rawBytes += len(scanner.Bytes())
				if len(rawBatch)+len(batch) >= 1000 || rawBytes >= 4<<20 {
					if err = flush(); err != nil {
						return err
					}
				}
				continue
			}
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
		if phase == 1 && record.Edition != nil && !selection.Allows(*record.Edition) {
			filtered++
			continue
		}
		batch = append(batch, record)
		if len(batch) == 250 || len(rawBatch)+len(batch) >= 1000 || phase > 0 && time.Since(lastCommit) >= 2*time.Second {
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
	if line == 0 && phase != 2 {
		return errors.New("empty book export")
	}
	if phase > 0 && typedLines == 0 && (phase != 2 || line > 0) {
		return errors.New("book export is missing the expected record type")
	}
	if err := flush(); err != nil {
		return err
	}
	completed, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
	if phase == 0 && (completed.Works == 0 || completed.Editions == 0 || completed.Authors == 0) {
		return errors.New("book export is missing bibliographic record types")
	}
	idle.Stop()
	_ = response.Body.Close()
	if phase > 0 {
		if err = s.DB.FinishBookExportPart(ctx, phase, line); err != nil {
			return err
		}
		if phase < 4 {
			return nil
		}
		return s.finishBookExport(ctx, h)
	}
	if err = s.DB.BookExportLoaded(ctx); err != nil {
		return err
	}
	return s.finishBookExport(ctx, h)
}

func (s *Service) finishBookExport(ctx context.Context, h *operationHistory) error {
	state, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
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
	if state.ExportPhase > 0 {
		if err = s.DB.FinishBookImportReferences(ctx); err != nil {
			return err
		}
	}
	if err := s.DB.FinishOpenLibrary(ctx); err != nil {
		return err
	}
	state, err = s.DB.OpenLibraryStatus(ctx)
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
