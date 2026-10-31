package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
	"github.com/theo-bnts/aiosources/internal/store"
)

const bookChangePageSize = 1000
const bookChangeMaxOffset = 10000
const bookChangeOverlap = 25

var errBookContinuity = errors.New("Open Library change continuity could not be verified")
var errBookRequest = errors.New("Open Library information could not be retrieved")

func (s *Service) SyncBooks(ctx context.Context, trigger string) (syncErr error) {
	if trigger != "manual" && trigger != "automatic" {
		return model.ErrInvalid
	}
	release, locked, err := s.DB.AIOLock(ctx, "openlibrary")
	if err != nil {
		return err
	}
	if !locked {
		return model.ErrBusy
	}
	defer release()
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	if trigger == "automatic" {
		if !c.OpenLibraryEnabled {
			return nil
		}
		due, err := s.DB.OpenLibraryDue(ctx, c.OpenLibraryMinutes)
		if err != nil || !due {
			return err
		}
	}
	h, err := s.beginOperation(ctx, "metadata", "openlibrary", trigger, "", "openlibrary")
	if err != nil {
		return err
	}
	if err = s.DB.PrepareBookImport(ctx, c); err != nil {
		h.finish(ctx, err)
		return err
	}
	attempt := 0
	defer func() {
		h.finish(ctx, syncErr)
		final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if syncErr != nil {
			retryAt := time.Now().Add(s.bookSyncRetryDelay(final, max(attempt, 1)))
			var retry *bookRequestFailure
			if errors.As(syncErr, &retry) {
				retryAt = retry.retryAt
			}
			message := "Book information update failed. Saved progress is preserved; retry the update to continue."
			if errors.Is(syncErr, context.Canceled) {
				message = "Book information update interrupted. Saved progress is preserved."
			} else if retry != nil {
				message = "Open Library requested a waiting period. Saved progress is preserved."
			}
			_ = s.DB.FailOpenLibrary(final, message, retryAt)
		}
		_ = s.DB.RecordBookInformationActivity(final, syncErr, h.run.Processed, h.run.Errors)
	}()
	priorChanges, err := s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	if priorChanges.ProviderRetryAt != nil && priorChanges.ProviderRetryAt.After(time.Now()) {
		return &bookRequestFailure{retryAt: *priorChanges.ProviderRetryAt}
	}
	attempt, err = s.DB.BeginBookSync(ctx)
	if err != nil {
		return err
	}
	feedErr := s.collectBookTailIfDue(ctx, h, c.OpenLibraryMinutes, trigger == "manual")
	var retry *bookRequestFailure
	if errors.As(feedErr, &retry) {
		return feedErr
	}
	stop := s.keepBookTail(ctx, h, c.OpenLibraryMinutes)
	defer stop()
	state, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil {
		return err
	}
	changes, err := s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	if !state.Complete || changes.NeedsDumpAfter != nil && (trigger == "manual" || changes.DumpCheckedAt == nil || time.Since(*changes.DumpCheckedAt) >= 24*time.Hour) {
		if err = s.syncBookExport(ctx, h); err != nil {
			return err
		}
		state, err = s.DB.OpenLibraryStatus(ctx)
		if err != nil {
			return err
		}
	}
	if state.SnapshotAt == nil {
		at, ok := books.ExportDate(state.Snapshot)
		if !ok {
			return errors.New("book export has no verifiable snapshot date; a new export is required")
		}
		if err = s.DB.SetBookSnapshotDate(ctx, at); err != nil {
			return err
		}
		state.SnapshotAt = &at
	}
	changes, err = s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	if changes.BootstrapDay == nil {
		if err = s.DB.BeginBookCatchup(ctx, *state.SnapshotAt); err != nil {
			return err
		}
	}
	if feedErr == nil && (changes.RetryAt == nil || !changes.RetryAt.After(time.Now())) {
		h.checkpoint(ctx, "book_changes")
		if err = s.catchUpBookDay(ctx, h); err != nil {
			return err
		}
	}
	if err = s.applyBookChanges(ctx, h); err != nil {
		return err
	}
	stop()
	if feedErr != nil {
		return feedErr
	}
	changes, err = s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	if changes.ProviderRetryAt != nil && changes.ProviderRetryAt.After(time.Now()) {
		return &bookRequestFailure{retryAt: *changes.ProviderRetryAt}
	}
	if changes.Error != "" {
		return errBookRequest
	}
	return s.DB.FinishBookChanges(ctx)
}

type bookRequestFailure struct{ retryAt time.Time }

func (s *Service) bookSyncRetryDelay(ctx context.Context, attempt int) time.Duration {
	policy, err := s.DB.RuntimeValues(ctx, "metadata_retries")
	if err != nil {
		return syncRetryDelay(attempt)
	}
	return model.RetryBackoff(attempt-1, time.Duration(policy["sync_retry_seconds"])*time.Second, time.Duration(policy["sync_retry_max_seconds"])*time.Second)
}

func (s *Service) bookProviderFailure(ctx context.Context, retryAfter string) error {
	delay := metadataRetryAfter(retryAfter, s.bookSyncRetryDelay(ctx, 1), time.Now())
	until := time.Now().Add(delay)
	s.bookRate.cooldown(delay)
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.DB.RetainBookProviderCooldown(final, until); err != nil {
		return err
	}
	return &bookRequestFailure{retryAt: until}
}

func (e *bookRequestFailure) Error() string {
	return "Open Library requested a retry; saved progress is preserved"
}

func (s *Service) bookGET(ctx context.Context, h *operationHistory, path, step string) ([]byte, int, string, error) {
	policy, err := s.DB.RuntimeValues(ctx, "metadata_retries")
	if err != nil {
		return nil, 0, "", err
	}
	for attempt := 0; ; attempt++ {
		body, status, location, err := s.bookGETOnce(ctx, h, path, step)
		if err == nil || !errors.Is(err, errBookRequest) || (status != 0 && status < 500) || attempt+1 >= policy["request_attempts"] {
			return body, status, location, err
		}
		timer := time.NewTimer(time.Duration(policy["request_retry_seconds"]*(1<<attempt)) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, status, "", ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) bookGETOnce(ctx context.Context, h *operationHistory, path, step string) ([]byte, int, string, error) {
	state, err := s.DB.BookChangesState(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	if state.ProviderRetryAt != nil && state.ProviderRetryAt.After(time.Now()) {
		return nil, 0, "", &bookRequestFailure{retryAt: *state.ProviderRetryAt}
	}
	timeout, err := s.outgoingTimeout(ctx, "metadata_seconds")
	if err != nil {
		return nil, 0, "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	base := s.BookAPIURL
	if base == "" {
		base = "https://openlibrary.org"
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return nil, 0, "", errBookRequest
	}
	req.Header.Set("User-Agent", openLibraryAgent)
	req.Header.Set("Accept", "application/json")
	if err = s.bookRate.wait(requestCtx); err != nil {
		if ctx.Err() != nil {
			return nil, 0, "", ctx.Err()
		}
		return nil, 0, "", errBookRequest
	}
	client := s.BookHTTP
	if client == nil {
		client = &http.Client{Transport: s.Network.Transport("metadata:openlibrary", outbound.HTTPTransport())}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout == 0 {
		copyClient.Timeout, err = s.outgoingTimeout(ctx, "metadata_seconds")
		if err != nil {
			return nil, 0, "", err
		}
	}
	started := time.Now()
	response, err := copyClient.Do(req)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	code := ""
	if err != nil {
		code = "provider_error"
		if errors.Is(err, context.DeadlineExceeded) {
			code = "timeout"
		}
	} else if status == 429 {
		code = "rate_limit"
	} else if status >= 500 {
		code = "provider_unavailable"
	} else if status == 404 {
		code = "not_found"
	} else if status >= 400 {
		code = "provider_error"
	}
	_ = s.DB.RecordProviderCall(ctx, h.run.ID, step, status, time.Since(started).Milliseconds(), code)
	if err != nil {
		if ctx.Err() != nil {
			return nil, status, "", ctx.Err()
		}
		return nil, status, "", errBookRequest
	}
	defer response.Body.Close()
	if status == 429 || status == 503 && strings.TrimSpace(response.Header.Get("Retry-After")) != "" {
		return nil, status, "", s.bookProviderFailure(ctx, response.Header.Get("Retry-After"))
	}
	if status == http.StatusNotFound || status >= 300 && status < 400 {
		return nil, status, response.Header.Get("Location"), nil
	}
	if status != 200 {
		return nil, status, "", errBookRequest
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return nil, status, "", errBookRequest
	}
	return body, status, "", nil
}

func (s *Service) bookChangePage(ctx context.Context, h *operationHistory, path string, offset int) ([]books.Change, error) {
	body, status, _, err := s.bookGET(ctx, h, path+"?limit="+strconv.Itoa(bookChangePageSize)+"&offset="+strconv.Itoa(offset), "book_changes")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, errBookRequest
	}
	return books.DecodeChanges(body, time.Now().UTC())
}

func (s *Service) queueBookPage(ctx context.Context, page []books.Change) error {
	byID := map[string]books.ChangeKey{}
	for _, change := range page {
		for _, key := range change.Keys {
			if prior, exists := byID[key.ID]; !exists || key.Revision > prior.Revision {
				byID[key.ID] = key
			}
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for start := 0; start < len(ids); start += 250 {
		batch := make([]books.ChangeKey, 0, 250)
		for _, id := range ids[start:min(start+250, len(ids))] {
			batch = append(batch, byID[id])
		}
		if err := s.DB.QueueBookChanges(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func bookPageOverlaps(previous, next []books.Change) bool {
	if len(previous) == 0 {
		return true
	}
	seen := map[string]bool{}
	for _, row := range next {
		seen[row.ID] = true
	}
	for _, row := range previous[max(0, len(previous)-bookChangeOverlap):] {
		if !seen[row.ID] {
			return false
		}
	}
	// A server ignoring offsets must not make repeated pages look like progress.
	return len(next) < bookChangePageSize || next[len(next)-1].ID != previous[len(previous)-1].ID
}

// A head is acknowledged only after every page leading back to the saved
// anchor has been validated and all its document jobs have committed.
func (s *Service) collectBookTail(ctx context.Context, h *operationHistory) error {
	state, err := s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	page, err := s.bookChangePage(ctx, h, "/recentchanges.json", 0)
	if err != nil {
		return err
	}
	if len(page) == 0 {
		return errBookContinuity
	}
	head := page[0]
	if state.AnchorAt != nil && head.At.Before(*state.AnchorAt) {
		return errBookContinuity
	}
	if state.AnchorID == "" {
		if err = s.queueBookPage(ctx, page); err != nil {
			return err
		}
		return s.DB.AdvanceBookAnchor(ctx, "", head)
	}
	previous := []books.Change(nil)
	for offset := 0; ; {
		if !bookPageOverlaps(previous, page) {
			return s.DB.RestartBookAnchor(ctx, state.AnchorID, head)
		}
		boundary := -1
		for i, row := range page {
			if row.ID == state.AnchorID {
				if state.AnchorAt == nil || !row.At.Equal(*state.AnchorAt) {
					return errBookContinuity
				}
				boundary = i
				break
			}
		}
		newRows := page
		if boundary >= 0 {
			newRows = page[:boundary]
		}
		if err = s.queueBookPage(ctx, newRows); err != nil {
			return err
		}
		if boundary >= 0 {
			return s.DB.AdvanceBookAnchor(ctx, state.AnchorID, head)
		}
		if len(page) < bookChangePageSize || offset == bookChangeMaxOffset {
			return s.DB.RestartBookAnchor(ctx, state.AnchorID, head)
		}
		previous = page
		offset = min(offset+bookChangePageSize-bookChangeOverlap, bookChangeMaxOffset)
		page, err = s.bookChangePage(ctx, h, "/recentchanges.json", offset)
		if err != nil {
			return err
		}
	}
}

func (s *Service) collectBookTailIfDue(ctx context.Context, h *operationHistory, minutes int, force bool) error {
	state, err := s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	if !force && state.RetryAt != nil && state.RetryAt.After(now) {
		return nil
	}
	if !force && state.CheckedAt != nil && state.CheckedAt.Add(time.Duration(minutes)*time.Minute).After(now) {
		return nil
	}
	if err = s.collectBookTail(ctx, h); err != nil {
		retryAt := time.Now().Add(s.bookSyncRetryDelay(ctx, 1))
		var retry *bookRequestFailure
		if errors.As(err, &retry) {
			retryAt = retry.retryAt
		}
		if ctx.Err() == nil {
			_ = s.DB.BookFeedFailed(ctx, retryAt)
		}
	}
	return err
}

// Collect while the large gzip stream or a backlog is being processed. The
// collector stores only compact jobs and never mutates the bibliographic data.
func (s *Service) keepBookTail(ctx context.Context, h *operationHistory, minutes int) func() {
	life, cancel := context.WithCancel(ctx)
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-life.Done():
				return
			case <-ticker.C:
				_ = s.collectBookTailIfDue(life, h, minutes, false)
			}
		}
	}()
	return func() { cancel(); group.Wait() }
}

// Historical days bridge the published snapshot and the beginning of the
// continuous tail. Reaching a provider cap is a gap, never a successful sync.
func (s *Service) catchUpBookDay(ctx context.Context, h *operationHistory) error {
	state, err := s.DB.BookChangesState(ctx)
	if err != nil {
		return err
	}
	if state.BootstrapComplete || state.NeedsDumpAfter != nil {
		return nil
	}
	if state.BootstrapDay == nil || state.TailSince == nil {
		return errBookContinuity
	}
	day := state.BootstrapDay.UTC()
	path := "/recentchanges/" + day.Format("2006/01/02") + ".json"
	needsBoundary := day.Format("2006-01-02") == state.TailSince.UTC().Format("2006-01-02")
	foundBoundary := false
	var previous []books.Change
	for offset := 0; ; {
		page, err := s.bookChangePage(ctx, h, path, offset)
		if err != nil {
			return err
		}
		if !bookPageOverlaps(previous, page) {
			return s.DB.RequireBookExport(ctx, *state.TailSince)
		}
		for _, change := range page {
			if change.At.Before(day) || !change.At.Before(day.AddDate(0, 0, 1)) {
				return books.ErrChanges
			}
			if change.ID == state.TailID && change.At.Equal(*state.TailSince) {
				foundBoundary = true
			}
		}
		if err = s.queueBookPage(ctx, page); err != nil {
			return err
		}
		if len(page) < bookChangePageSize {
			if needsBoundary && !foundBoundary {
				return s.DB.RequireBookExport(ctx, *state.TailSince)
			}
			return s.DB.AdvanceBookCatchup(ctx, day, day.AddDate(0, 0, 1))
		}
		if offset == bookChangeMaxOffset {
			return s.DB.RequireBookExport(ctx, *state.TailSince)
		}
		previous = page
		offset = min(offset+bookChangePageSize-bookChangeOverlap, bookChangeMaxOffset)
	}
}

func (s *Service) readBookDocument(ctx context.Context, h *operationHistory, job store.BookChangeJob) (books.Record, error) {
	body, status, location, err := s.bookGET(ctx, h, books.Path(job.ID)+".json", "book_details")
	if err != nil {
		return books.Record{}, err
	}
	if status == 404 {
		if job.Attempts < 1 || time.Since(job.ChangedAt) < 2*time.Minute {
			return books.Record{}, errBookRequest
		}
		return books.Record{Deleted: job.ID, Revision: job.Revision, Modified: &job.ChangedAt}, nil
	}
	if status >= 300 && status < 400 {
		base := s.BookAPIURL
		if base == "" {
			base = "https://openlibrary.org"
		}
		origin, e := url.Parse(base)
		if e != nil || origin.Host == "" {
			return books.Record{}, books.ErrChanges
		}
		target, e := origin.Parse(location)
		if e != nil || target.User != nil || target.Fragment != "" || target.Host != origin.Host || target.Scheme != origin.Scheme {
			return books.Record{}, books.ErrChanges
		}
		key := books.Key(strings.TrimSuffix(target.Path, ".json"))
		if key == "" || key == job.ID || books.Path(key) != strings.TrimSuffix(target.Path, ".json") {
			return books.Record{}, books.ErrChanges
		}
		return books.Record{Redirect: &books.Redirect{Source: job.ID, Target: key}, Revision: job.Revision, Modified: &job.ChangedAt}, nil
	}
	if status != 200 {
		return books.Record{}, errBookRequest
	}
	return books.DecodeDocument(job.ID, job.Revision, body)
}

// Grouped reads reduce requests without depending on the legacy endpoint for
// correctness. Unsupported envelopes fall back to individual document reads.
func (s *Service) readBookDocuments(ctx context.Context, h *operationHistory, jobs []store.BookChangeJob) (map[string]books.Record, bool, error) {
	keys := make([]string, 0, len(jobs))
	revisions := make(map[string]int64, len(jobs))
	for _, job := range jobs {
		keys = append(keys, books.Path(job.ID))
		revisions[job.ID] = job.Revision
	}
	query, err := json.Marshal(keys)
	if err != nil {
		return nil, true, err
	}
	body, status, _, err := s.bookGET(ctx, h, "/api/get_many?keys="+url.QueryEscape(string(query)), "book_details")
	if status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusGone {
		return nil, false, nil
	}
	if err != nil {
		if errors.Is(err, errBookRequest) && status == http.StatusOK {
			return nil, false, nil
		}
		return nil, true, err
	}
	if status != http.StatusOK {
		return nil, false, nil
	}
	records, err := books.DecodeDocuments(body, revisions)
	if err != nil {
		return nil, false, nil
	}
	return records, true, nil
}

func (s *Service) applyBookChanges(ctx context.Context, h *operationHistory) error {
	policy, err := s.DB.RuntimeValues(ctx, "metadata_retries")
	if err != nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	failures := 0
	batchNumber := 0
	groupedReads := true
	lastHistory := time.Now()
	for work.Err() == nil {
		var jobs []store.BookChangeJob
		var err error
		batchNumber++
		if batchNumber%3 == 0 {
			jobs, err = s.DB.OldestBookChangeJobs(work, 25)
		} else {
			jobs, err = s.DB.BookChangeJobs(work, 25)
		}
		if work.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		pending := jobs[:0]
		for _, job := range jobs {
			known, err := s.DB.AcknowledgeKnownBookChange(work, job)
			if work.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if !known {
				pending = append(pending, job)
			}
		}
		var records map[string]books.Record
		if groupedReads && len(pending) > 1 {
			records, groupedReads, err = s.readBookDocuments(work, h, pending)
			if work.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
		}
		for _, job := range pending {
			if work.Err() != nil {
				return ctx.Err()
			}
			record, exists := records[job.ID]
			var err error
			if !exists {
				record, err = s.readBookDocument(work, h, job)
			}
			if work.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				var retry *bookRequestFailure
				if errors.As(err, &retry) {
					return err
				}
				delay := model.RetryBackoff(job.Attempts, time.Duration(policy["detail_retry_seconds"])*time.Second, time.Duration(policy["detail_retry_max_seconds"])*time.Second)
				if e := s.DB.DeferBookChange(ctx, job, delay); e != nil {
					return e
				}
				h.run.Errors++
				failures++
				if failures >= 3 {
					return err
				}
				continue
			}
			relevant, err := s.DB.BookChangeRelevant(work, record)
			if err != nil {
				return err
			}
			if !relevant {
				if err = s.DB.ExcludeBookChange(work, job, record); err != nil {
					return err
				}
				h.run.Processed++
				failures = 0
				s.queueAutomaticMatching(ctx)
				if time.Since(lastHistory) >= 5*time.Second {
					h.checkpoint(ctx, "book_details")
					lastHistory = time.Now()
				}
				continue
			}
			dependencies, err := s.DB.BookDependencies(work, record)
			if err != nil {
				return err
			}
			keys := make([]books.ChangeKey, 0, len(dependencies))
			for _, id := range dependencies {
				keys = append(keys, books.ChangeKey{ID: id, At: job.ChangedAt})
			}
			if len(keys) > 0 {
				if err = s.DB.QueueBookDependencies(work, keys); err != nil {
					return err
				}
			}
			if err = s.DB.ApplyBookChange(work, job, record); err != nil {
				return err
			}
			h.run.Processed++
			s.queueAutomaticMatching(ctx)
			failures = 0
			if time.Since(lastHistory) >= 5*time.Second {
				h.checkpoint(ctx, "book_details")
				lastHistory = time.Now()
			}
		}
	}
	return ctx.Err()
}
