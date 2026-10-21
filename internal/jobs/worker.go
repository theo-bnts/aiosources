package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
)

const cleanupTimeout = 15 * time.Second

const coverageFailureMessage = "Source-ID coverage could not be verified against refreshed source totals; review scope counts and the minimum total in run events"

func sourceFailureMessage(page model.Page) string {
	if incomplete, _ := page.Metadata["coverage_incomplete"].(bool); incomplete {
		return coverageFailureMessage
	}
	if reason, ok := page.Metadata["failure_reason"].(string); ok {
		if message := connectors.FailureReasonMessage(reason); message != "" {
			return message + "; checkpoint unchanged; review run events for details"
		}
	}
	// Only connector-owned reason codes become public text, never source
	// messages, record values, URLs or credentials.
	switch page.Metadata["recovery_discovery_error"] {
	case "empty":
		return "ID recovery discovery returned no source IDs"
	case "noncanonical_id":
		return "ID recovery discovery returned an invalid numeric source ID"
	default:
		return "Source response could not be processed"
	}
}

func (m *Manager) worker() {
	defer m.workers.Done()
	for {
		if m.ctx.Err() != nil {
			return
		}
		// Capture the generation before claiming, so an enqueue between the
		// empty claim and waiting cannot be lost.
		m.mu.Lock()
		wake, connected := m.work, m.connected
		m.mu.Unlock()
		if connected {
			run, err := m.db.ClaimNext(m.ctx)
			if err != nil {
				if m.ctx.Err() == nil {
					m.fail(errPersistence)
				}
				return
			}
			if run != nil {
				m.workers.Add(1)
				go func() {
					defer m.workers.Done()
					m.runClaim(*run)
				}()
				continue
			}
		}
		select {
		case <-m.ctx.Done():
			return
		case <-wake:
		}
	}
}

func (m *Manager) runClaim(run model.Run) {
	ctx, cancel := context.WithCancelCause(m.ctx)
	attempt := &activeAttempt{cancel: cancel}
	m.mu.Lock()
	m.active[run.ID] = attempt
	if !m.connected {
		cancel(errNotifications)
	}
	m.mu.Unlock()
	defer func() {
		cancel(nil)
		m.mu.Lock()
		if m.active[run.ID] == attempt {
			delete(m.active, run.ID)
		}
		m.mu.Unlock()
	}()

	status, message, reason := func() (status model.RunStatus, message string, reason model.PauseReason) {
		// Even an unexpected adapter panic must release the provider lock.
		defer func() {
			if recover() != nil {
				status, message, reason = model.StatusFailed, "Collection adapter failed unexpectedly", ""
			}
		}()
		return m.collect(ctx, run)
	}()
	if ctx.Err() != nil {
		status, message, reason = interrupted(ctx)
	}
	var err error
	if status == model.StatusSucceeded {
		// Publishing a full catalogue is collection work, not bounded cleanup.
		// Keep it cancellable without imposing the shutdown cleanup deadline.
		_, err = m.db.FinishRun(ctx, run.ID, status, message, reason)
		if err != nil && ctx.Err() != nil {
			// FinishRun rolled back and released its lease. Recover the durable
			// checkpoint so shutdown/cancellation cannot strand a running row.
			cleanup, release := context.WithTimeout(context.Background(), cleanupTimeout)
			err = m.recoverInterrupted(cleanup)
			release()
		}
	} else {
		cleanup, release := context.WithTimeout(context.Background(), cleanupTimeout)
		_, err = m.db.FinishRun(cleanup, run.ID, status, message, reason)
		release()
	}
	if err != nil {
		// FinishRun releases its owned advisory connection even on failure.
		// Leave durable recovery to the next startup rather than continuing
		// to claim jobs against an uncertain persistence layer.
		m.fail(errPersistence)
	}
	if err == nil && errors.Is(context.Cause(ctx), errNotifications) {
		// Reconnection may have recovered before this attempt released its
		// provider lease. Reconcile once more after its durable finish; the
		// observer retains this wake-up while its subscription is reconnecting.
		select {
		case m.recovery <- struct{}{}:
		default:
		}
	}
	m.Notify("run", run.ID)
	m.signalWork()
	m.signalSchedule()
}

func (m *Manager) collect(ctx context.Context, run model.Run) (status model.RunStatus, message string, reason model.PauseReason) {
	policy := run.EffectivePolicy()
	var deadline time.Time
	if policy.MaxDurationSeconds > 0 {
		deadline = time.Now().Add(time.Duration(policy.MaxDurationSeconds) * time.Second)
	}
	if err := m.checkControl(ctx, run.ID, true); err != nil {
		return stopReason(ctx, err)
	}
	if run.TraversalDone {
		// The final page and staging committed before a previous attempt
		// stopped. Publish that checkpoint rather than fetching it again.
		return model.StatusSucceeded, "", ""
	}
	if run.Mode == model.ModeMetadata && len(run.MetadataFields()) == 0 {
		// The empty detail selection is still a completed durable traversal.
		// Commit it before FinishRun, without requesting discarded information.
		if _, err := m.db.SavePage(ctx, run, model.Page{Done: true, Next: run.Cursor}, ""); err != nil {
			return stopReason(ctx, errPersistence)
		}
		return model.StatusSucceeded, "", ""
	}
	resolved, err := m.secrets(ctx, run.Config)
	if err != nil {
		if eventErr := m.event(ctx, run.ID, "source_error", "Source credentials are unavailable", map[string]any{"failure_code": "configuration"}); eventErr != nil {
			return stopReason(ctx, eventErr)
		}
		return stopReason(ctx, err)
	}
	environment := connectors.Environment{Network: m.network, Secrets: resolved, MaxQuotaRetries: &policy.MaxQuotaRetries}
	var responseErr, boundaryErr error
	environment.KnownIDs = func(ctx context.Context, ids []string) (map[string]bool, error) {
		known, err := m.db.Known(ctx, run.ProviderID, ids, run.CreatedAt)
		if err != nil {
			boundaryErr = errPersistence
		}
		return known, err
	}
	var pendingResponse connectors.Response
	var responseRetryAt time.Time
	environment.ResponseObserved = func(response connectors.Response, retryAt time.Time) {
		pendingResponse, responseRetryAt = response, retryAt
	}
	responseMetadata := func(page *model.Page) {
		if pendingResponse.StatusCode == 0 && !responseRetryAt.After(time.Now()) {
			return
		}
		if page.Metadata == nil {
			page.Metadata = make(map[string]any)
		}
		if pendingResponse.StatusCode != 0 {
			page.Metadata["http_status"] = pendingResponse.StatusCode
		}
		if !responseRetryAt.IsZero() {
			page.Metadata["retry_at"] = responseRetryAt.UTC().Format(time.RFC3339Nano)
		}
	}
	checkQuotaBoundary := func(ctx context.Context, retryAt time.Time) error {
		if err := m.checkAttempt(ctx, run.ID, deadline); err != nil {
			return err
		}
		if policy.MaxQuotaWaitSeconds > 0 && time.Until(retryAt) > time.Duration(policy.MaxQuotaWaitSeconds)*time.Second {
			return errQuotaPaused
		}
		return nil
	}
	environment.BeforeWait = func(ctx context.Context, until time.Time) (err error) {
		defer func() { boundaryErr = err }()
		return m.waitRateLimit(ctx, run.ID, until, deadline)
	}
	if limits := run.Config.RequestLimits; limits != nil {
		environment.BeforeRequest = func(ctx context.Context) (err error) {
			defer func() { boundaryErr = err }()
			for {
				if err := m.checkAttempt(ctx, run.ID, deadline); err != nil {
					return err
				}
				until, err := m.db.ReserveProviderRequest(ctx, run.ProviderID, *limits)
				if err != nil {
					return errPersistence
				}
				if until.IsZero() {
					return nil
				}
				if err := m.event(ctx, run.ID, "quota_wait", "Configured request quota reached; waiting before the next request", map[string]any{
					"retry_at": until.UTC().Format(time.RFC3339Nano),
				}); err != nil {
					return err
				}
				if err := m.waitRateLimit(ctx, run.ID, until, deadline); err != nil {
					return err
				}
			}
		}
	}
	saveResponseBoundary := func(ctx context.Context, page model.Page) error {
		page.Next = run.Cursor
		saveCtx := ctx
		if ctx.Err() != nil {
			var cancel context.CancelFunc
			saveCtx, cancel = context.WithTimeout(context.Background(), cleanupTimeout)
			defer cancel()
		}
		saved, err := m.db.SavePage(saveCtx, run, page, "")
		if err != nil {
			return errPersistence
		}
		run = saved
		pendingResponse, responseRetryAt = connectors.Response{}, time.Time{}
		m.Notify("run", run.ID)
		return nil
	}
	environment.Checkpoint = func(ctx context.Context, page model.Page) (err error) {
		defer func() { boundaryErr = err }()
		responseMetadata(&page)
		retryAt := responseRetryAt
		if err := saveResponseBoundary(ctx, page); err != nil {
			return err
		}
		if err := checkQuotaBoundary(ctx, retryAt); err != nil {
			return err
		}
		if run.PauseReason == model.PauseNoProgress {
			return errNoProgress
		}
		return nil
	}
	environment.RateLimited = func(ctx context.Context, response connectors.Response, retryAt time.Time, retry bool) (callbackErr error) {
		defer func() { responseErr = callbackErr }()
		page := model.Page{
			Body: response.Body, ContentType: response.Header.Get("Content-Type"), Next: run.Cursor,
			Error: "Source rate limit reached",
			Metadata: map[string]any{
				"failure_code": "http", "http_status": response.StatusCode,
			},
		}
		if !retryAt.IsZero() {
			page.Metadata["retry_at"] = retryAt.UTC().Format(time.RFC3339Nano)
		}
		if err := saveResponseBoundary(ctx, page); err != nil {
			return err
		}
		if err := checkQuotaBoundary(ctx, retryAt); err != nil {
			return err
		}
		if !retry {
			return errQuotaRetries
		}
		return m.waitRateLimit(ctx, run.ID, retryAt, deadline)
	}
	environment.TransientFailure = func(ctx context.Context, response connectors.Response, retryAt time.Time, attempt int, retry bool) (callbackErr error) {
		defer func() { responseErr = callbackErr }()
		message, failureCode := fmt.Sprintf("Source returned HTTP %d on attempt %d", response.StatusCode, attempt), "http"
		if response.UnexpectedHTML {
			message, failureCode = "Source returned HTML instead of JSON after a rate limit", "parse"
		}
		if response.InvalidDocument {
			message, failureCode = fmt.Sprintf("Source HTML detail page was unavailable on attempt %d", attempt), "parse"
		}
		page := model.Page{
			Body: response.Body, ContentType: response.Header.Get("Content-Type"),
			Error: message,
			Metadata: map[string]any{
				"failure_code": failureCode, "http_status": response.StatusCode,
				"retry_attempt": attempt, "retry": retry,
			},
		}
		if response.InvalidDocument {
			page.Metadata["failure_reason"] = "html_detail_missing"
			page.Metadata["html_phase"], page.Metadata["detail_response"] = "detail", true
		}
		if !retryAt.IsZero() {
			page.Metadata["retry_at"] = retryAt.UTC().Format(time.RFC3339Nano)
		}
		if err := saveResponseBoundary(ctx, page); err != nil {
			return err
		}
		if err := m.checkAttempt(ctx, run.ID, deadline); err != nil {
			return err
		}
		if !retry {
			return fmt.Errorf("%w; checkpoint unchanged; resume to retry", response.TransientError(attempt))
		}
		if err := checkQuotaBoundary(ctx, retryAt); err != nil {
			return err
		}
		return m.waitRateLimit(ctx, run.ID, retryAt, deadline)
	}
	if run.Mode == model.ModeMetadata {
		environment.MetadataCandidates = func(ctx context.Context, after string, limit int) ([]model.MetadataCandidate, error) {
			return m.db.MetadataCandidates(ctx, run, after, limit)
		}
	}
	if run.Config.Traversal != nil && run.Mode != model.ModeMetadata {
		environment.CoverageAttempt, err = m.db.CoverageAttempt(ctx, run.ID)
		if err != nil {
			return stopReason(ctx, errPersistence)
		}
		environment.ScopeCounts = func(ctx context.Context) (map[string]int64, error) {
			return m.db.ScopeCounts(ctx, run.ID)
		}
		environment.ObservedIDs = func(ctx context.Context, ids []string) (map[string]bool, error) {
			return m.db.ObservedIDs(ctx, run.ID, ids)
		}
		environment.FilterComplete = func(ctx context.Context, scope string, fingerprints []string) (bool, error) {
			return m.db.FilterComplete(ctx, run.ID, scope, fingerprints)
		}
		environment.NextRefreshID = func(ctx context.Context, after, through int64) (int64, error) {
			return m.db.NextRefreshID(ctx, run.ID, after, through)
		}
	}
	if run.Config.IsRemoteCatalog() {
		environment.LocalInstanceID, err = m.db.CatalogInstanceID(ctx)
		if err != nil {
			return stopReason(ctx, errPersistence)
		}
	}
	retryAt, err := m.db.RunRetryAt(ctx, run.ID)
	if err != nil {
		return stopReason(ctx, errPersistence)
	}
	// Explicit Resume accepts the existing cooldown; the long-wait threshold
	// applies only to newly received quota responses, not this durable hold.
	if err := m.waitRateLimit(ctx, run.ID, retryAt, deadline); err != nil {
		return stopReason(ctx, err)
	}
	source, err := connectors.New(ctx, run.ConnectorConfig(), run.Mode, environment)
	if err != nil {
		if source != nil {
			_ = source.Close()
		}
		if responseErr != nil {
			return stopReason(ctx, responseErr)
		}
		if boundaryErr != nil {
			return stopReason(ctx, boundaryErr)
		}
		code := connectors.FailureCode(err)
		if code == "unknown" {
			code = "configuration"
		}
		if eventErr := m.event(ctx, run.ID, "source_error", "Source initialization failed", map[string]any{"failure_code": code}); eventErr != nil {
			return stopReason(ctx, eventErr)
		}
		return stopReason(ctx, errors.New("Source initialization failed"))
	}
	defer func() {
		if err := source.Close(); err != nil && (status == model.StatusSucceeded || status == model.StatusPaused) {
			status, message, reason = model.StatusFailed, "Source resources could not be released", ""
		}
	}()
	if err := m.event(ctx, run.ID, "started", "Collection attempt started", map[string]any{
		"mode": run.Mode, "committed_pages": run.Pages, "max_pages": run.MaxPages,
	}); err != nil {
		return stopReason(ctx, err)
	}

	attemptPages, knownPages := 0, run.KnownPageStreak
	for {
		if err := m.checkAttempt(ctx, run.ID, deadline); err != nil {
			return stopReason(ctx, err)
		}
		page, fetchErr := source.Fetch(ctx, run.Cursor)
		if responseErr != nil {
			return stopReason(ctx, responseErr)
		}
		if boundaryErr != nil {
			if pendingResponse.StatusCode != 0 {
				responseMetadata(&page)
				if err := m.archiveFailure(ctx, run, page, "Source request stopped at a response boundary", connectors.FailureCode(fetchErr)); err != nil {
					return stopReason(ctx, err)
				}
			}
			return stopReason(ctx, boundaryErr)
		}
		responseMetadata(&page)
		if fetchErr != nil || page.Error != "" || ctx.Err() != nil {
			failure := sourceFailureMessage(page)
			if ctx.Err() != nil {
				failure = "Source request was interrupted"
			}
			code := connectors.FailureCode(fetchErr)
			if metadataCode, ok := page.Metadata["failure_code"].(string); code == "unknown" && ok && connectors.ValidFailureCode(metadataCode) {
				code = metadataCode
			}
			if err := m.archiveFailure(ctx, run, page, failure, code); err != nil {
				return stopReason(ctx, err)
			}
			return stopReason(ctx, errors.New(failure))
		}
		if len(page.Next) > 0 && !json.Valid(page.Next) {
			if err := m.archiveFailure(ctx, run, page, "Source returned an invalid continuation checkpoint", "parse"); err != nil {
				return stopReason(ctx, err)
			}
			return model.StatusFailed, "Source returned an invalid continuation checkpoint", ""
		}
		if !page.Done && !advances(run.Cursor, page.Next) {
			if err := m.archiveFailure(ctx, run, page, "Pagination did not advance", "stalled"); err != nil {
				return stopReason(ctx, err)
			}
			return model.StatusFailed, "Pagination did not advance", ""
		}

		fingerprint, ids, completeIDs := pageFingerprint(page)
		managed, _ := page.Metadata["known_pages_managed"].(bool)
		if run.Mode == model.ModeIncremental && run.Config.Schedule.KnownPages > 0 && !managed {
			allKnown := completeIDs && len(ids) > 0
			if allKnown {
				known, err := m.db.Known(ctx, run.ProviderID, ids, run.CreatedAt)
				if err != nil {
					if archiveErr := m.archiveFailure(ctx, run, page, "Known-record lookup failed", "unknown"); archiveErr != nil {
						return stopReason(ctx, archiveErr)
					}
					return stopReason(ctx, errPersistence)
				}
				for _, id := range ids {
					if !known[id] {
						allKnown = false
						break
					}
				}
			}
			if allKnown {
				knownPages++
			} else {
				knownPages = 0
			}
			page.KnownPageStreak = &knownPages
		} else {
			knownPages = 0
		}
		if run.Mode == model.ModeIncremental && run.Config.Schedule.KnownPages > 0 && knownPages >= run.Config.Schedule.KnownPages {
			page.Done = true
		}
		if err := m.checkControl(ctx, run.ID, false); err != nil {
			if archiveErr := m.archiveFailure(ctx, run, page, "Collection was interrupted before checkpoint commit", "unknown"); archiveErr != nil {
				return stopReason(ctx, archiveErr)
			}
			return stopReason(ctx, err)
		}
		// Persist preview completion with its last budgeted page. A crash
		// between SavePage and FinishRun must not fetch a fourth page.
		if run.Mode == model.ModePreview && run.MaxPages > 0 && attemptPages+1 >= run.MaxPages {
			for _, record := range page.Items {
				if !record.Auxiliary {
					page.Done = true
					break
				}
			}
		}
		committed, err := m.db.SavePage(ctx, run, page, fingerprint)
		if errors.Is(err, model.ErrStalled) {
			// Store has already retained this rejected response as an errored
			// attempt without advancing its successful checkpoint.
			return model.StatusFailed, "Pagination repeated source identities", ""
		}
		if err != nil {
			return stopReason(ctx, errPersistence)
		}
		if run.Mode == model.ModeMetadata && committed.Errors > run.Errors {
			return model.StatusFailed, "Metadata records could not be represented in database fields; correct the source data before resuming", ""
		}
		if run.Config.HTTP.Catalog && committed.Errors > run.Errors {
			return model.StatusFailed, "Remote catalogue records could not be retained faithfully", ""
		}
		if run.Mode == model.ModeFull && committed.Errors > run.Errors {
			return model.StatusFailed, "Full catalogue records could not be retained faithfully; correct the source data before resuming", ""
		}
		attemptPages += committed.Pages - run.Pages
		run = committed
		pendingResponse = connectors.Response{}
		m.Notify("run", run.ID)
		if run.PauseRequested {
			return model.StatusPaused, "Collection manually paused; resume from the last committed checkpoint", model.PauseManual
		}
		if page.Done {
			return model.StatusSucceeded, "", ""
		}
		if run.PauseReason == model.PauseNoProgress {
			return model.StatusPaused, "Useful-progress threshold reached; review run events before resuming", model.PauseNoProgress
		}
		if err := checkQuotaBoundary(ctx, responseRetryAt); err != nil {
			return stopReason(ctx, err)
		}
		if run.MaxPages > 0 && attemptPages >= run.MaxPages {
			if run.Mode == model.ModePreview {
				return model.StatusSucceeded, "", ""
			}
			if run.Trigger == model.TriggerScheduled {
				if run.MetadataParentRunID != "" {
					return model.StatusPaused, "Metadata page budget reached; new torrents will be collected before enrichment continues", model.PauseBudget
				}
				return model.StatusPaused, "Page budget reached; the next scheduled tick will continue this traversal", model.PauseBudget
			}
			return model.StatusPaused, "Page budget reached; resume to continue the traversal", model.PauseBudget
		}
	}
}

func (m *Manager) checkControl(ctx context.Context, id string, pauseBoundary bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	run, err := m.db.GetRun(ctx, id)
	if err != nil {
		return errPersistence
	}
	if run.CancelRequested || run.Status != model.StatusRunning {
		return errCancelled
	}
	if pauseBoundary && run.PauseRequested {
		return errPaused
	}
	return nil
}

// Duration limits stop only at committed boundaries; they never cancel the
// request that is producing the next archive or atomic catalogue publication.
func (m *Manager) checkAttempt(ctx context.Context, id string, deadline time.Time) error {
	if err := m.checkControl(ctx, id, true); err != nil {
		return err
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return errDuration
	}
	return nil
}

// Response cooldown is a committed boundary, not an in-flight request:
// Pause must remain responsive, and Resume reuses the archived deadline.
func (m *Manager) waitRateLimit(ctx context.Context, id string, until, deadline time.Time) error {
	if err := m.checkAttempt(ctx, id, deadline); err != nil {
		return err
	}
	if !until.After(time.Now()) {
		return nil
	}
	wake := until
	if !deadline.IsZero() && deadline.Before(wake) {
		wake = deadline
	}
	timer := time.NewTimer(time.Until(wake))
	defer timer.Stop()
	control := time.NewTicker(250 * time.Millisecond)
	defer control.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return m.checkAttempt(ctx, id, deadline)
		case <-control.C:
			if err := m.checkAttempt(ctx, id, deadline); err != nil {
				return err
			}
		}
	}
}

func (m *Manager) archiveFailure(ctx context.Context, run model.Run, page model.Page, message, code string) error {
	if !connectors.ValidFailureCode(code) {
		code = "unknown"
	}
	if page.Metadata == nil {
		page.Metadata = make(map[string]any)
	}
	if page.Metadata["coverage_incomplete"] == true || page.Metadata["recovery_discovery_error"] == "empty" || page.Metadata["recovery_discovery_error"] == "noncanonical_id" {
		message = sourceFailureMessage(page)
	}
	page.Metadata["failure_code"] = code
	page.Error = message
	page.Next = run.Cursor
	page.Done = false
	page.KnownPageStreak = nil
	// Retain the failed observation and diagnostics before acknowledging
	// cancellation, without persisting its payload or advancing the checkpoint.
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
	}
	if len(page.Body) == 0 && len(page.Items) == 0 {
		data := map[string]any{"failure_code": code}
		connectors.CopyFailureDiagnostics(data, page.Metadata)
		if incomplete, _ := page.Metadata["coverage_incomplete"].(bool); incomplete {
			data["coverage_incomplete"] = true
			// These maps contain connector-produced counts only, not upstream
			// messages or arbitrary metadata. Scope IDs came from the validated
			// immutable source configuration.
			for _, key := range []string{"coverage_expected", "coverage_observed"} {
				if counts, ok := page.Metadata[key].(map[string]int64); ok && run.Config.Traversal != nil {
					safe := make(map[string]int64, len(counts))
					for _, scope := range run.Config.Traversal.Scopes {
						if count, exists := counts[scope.ID]; exists && count >= 0 {
							safe[scope.ID] = count
						}
					}
					data[key] = safe
				}
			}
			if run.Config.Traversal != nil && run.Config.Traversal.MinimumTotal > 0 {
				data["minimum_total"] = run.Config.Traversal.MinimumTotal
			}
			return m.event(ctx, run.ID, "source_error", message, data)
		}
		if data["failure_reason"] != nil {
			return m.event(ctx, run.ID, "source_error", sourceFailureMessage(page), data)
		}
		return m.event(ctx, run.ID, "source_error", "Source request failed before a response could be retained", data)
	}
	if _, err := m.db.SavePage(ctx, run, page, ""); err != nil {
		return errPersistence
	}
	m.Notify("run", run.ID)
	return nil
}

func (m *Manager) event(ctx context.Context, id, kind, message string, data map[string]any) error {
	if _, err := m.db.AddEvent(ctx, id, kind, message, data); err != nil {
		return errPersistence
	}
	m.Notify("run", id)
	return nil
}

func interrupted(ctx context.Context) (model.RunStatus, string, model.PauseReason) {
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errCancelled):
		return model.StatusCancelled, "", ""
	case errors.Is(cause, errNotifications), errors.Is(cause, errPersistence):
		return model.StatusPaused, "Collection persistence was interrupted", model.PauseInterrupted
	default:
		return model.StatusPaused, "Collection interrupted; resume from the last committed checkpoint", model.PauseInterrupted
	}
}

func stopReason(ctx context.Context, err error) (model.RunStatus, string, model.PauseReason) {
	if ctx.Err() != nil {
		return interrupted(ctx)
	}
	switch {
	case errors.Is(err, errCancelled):
		return model.StatusCancelled, "", ""
	case errors.Is(err, errPaused):
		return model.StatusPaused, "Collection manually paused; resume from the last committed checkpoint", model.PauseManual
	case errors.Is(err, errQuotaPaused):
		return model.StatusPaused, "Source quota wait exceeds the configured threshold; resume to honor the remaining cooldown", model.PauseQuota
	case errors.Is(err, errDuration):
		return model.StatusPaused, "Attempt duration budget reached; resume to continue the traversal", model.PauseBudget
	case errors.Is(err, errNoProgress):
		return model.StatusPaused, "Useful-progress threshold reached; review run events before resuming", model.PauseNoProgress
	case errors.Is(err, errSecrets):
		return model.StatusFailed, "A required secret is unavailable", ""
	case errors.Is(err, errPersistence):
		return model.StatusPaused, "Collection persistence was interrupted", model.PauseInterrupted
	default:
		// The only other inputs are fixed messages constructed inside this package.
		return model.StatusFailed, err.Error(), ""
	}
}

func advances(previous, next json.RawMessage) bool {
	next = bytes.TrimSpace(next)
	if len(next) == 0 || bytes.Equal(next, []byte("null")) {
		return false
	}
	var oldState, newState any
	decode := func(raw json.RawMessage, state *any) error {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		return decoder.Decode(state)
	}
	if decode(next, &newState) != nil {
		return false
	}
	if len(previous) == 0 || decode(previous, &oldState) != nil {
		return true
	}
	return !reflect.DeepEqual(oldState, newState)
}

// Identity sets, not counts, item order, output projection or volatile fields,
// define repeats. Explicit adapter scopes distinguish independent traversals.
func pageFingerprint(page model.Page) (string, []string, bool) {
	identities := make(map[string]struct{}, len(page.Items))
	complete := true
	identified := true
	for _, item := range page.Items {
		if item.Auxiliary {
			continue
		}
		if item.SourceID == "" {
			complete = false
			identified = false
			continue
		}
		identities[item.SourceID] = struct{}{}
		if item.Error != "" {
			complete = false
		}
	}
	ids := make([]string, 0, len(identities))
	for id := range identities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 || !identified {
		if page.FallbackFingerprint != "" {
			scope, _ := page.Metadata["fingerprint_scope"].(string)
			return "fallback:" + scope + ":" + page.FallbackFingerprint, ids, complete
		}
		return "", ids, complete
	}
	hash := sha256.New()
	var length [8]byte
	writePart := func(value string) {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	scope, _ := page.Metadata["fingerprint_scope"].(string)
	writePart(scope)
	for _, id := range ids {
		writePart(id)
	}
	return hex.EncodeToString(hash.Sum(nil)), ids, complete
}
