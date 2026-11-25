package aiosource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/software"
	"github.com/theo-bnts/aiosources/internal/store"
)

const (
	softwareNVDMetaURL          = "https://nvd.nist.gov/feeds/json/cpe/2.0/nvdcpe-2.0.meta"
	softwareNVDDumpURL          = "https://nvd.nist.gov/feeds/json/cpe/2.0/nvdcpe-2.0.tar.gz"
	softwareNVDAPIBase          = "https://services.nvd.nist.gov/rest/json/cpes/2.0"
	softwareWinGetURL           = "https://codeload.github.com/microsoft/winget-pkgs/tar.gz/refs/heads/master"
	softwareHomebrewURL         = "https://formulae.brew.sh/api/cask.json"
	softwareIconsURL            = "https://raw.githubusercontent.com/selfhst/icons/main/index.json"
	softwareAgent               = "AIOSources/" + Version + " (+https://github.com/theo-bnts/aiosources)"
	softwareNVDPaceNoKey        = 6200 * time.Millisecond // NIST limit: 5 requests per 30s without API key
	softwareNVDPaceWithKey      = 650 * time.Millisecond  // NIST limit: 50 requests per 30s with API key
	softwareNVDPageSize         = 2000
	softwareMaxIncrementalPages = 25
)

func trustedSoftwareExport(r *http.Request, via []*http.Request) error {
	host := strings.ToLower(r.URL.Hostname())
	port := r.URL.Port()
	if len(via) > 5 || r.URL.Scheme != "https" || r.URL.User != nil || (port != "" && port != "443") ||
		!(host == "nvd.nist.gov" ||
			host == "services.nvd.nist.gov" ||
			host == "codeload.github.com" ||
			host == "raw.githubusercontent.com" ||
			host == "formulae.brew.sh") {
		return errors.New("untrusted software export redirect")
	}
	return nil
}

type softwareError struct {
	message string
	retryAt time.Time
}

func (e *softwareError) Error() string { return e.message }

var errSoftwareIncrementalFallback = errors.New("nvd incremental fallback required")

func catalogProviderErrorCode(ctx, requestCtx context.Context, status int, err error) string {
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return "cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) || (requestCtx != nil && requestCtx.Err() != nil) {
			return "timeout"
		}
		if errors.Is(err, context.Canceled) {
			return "cancelled"
		}
		return "provider_error"
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "authentication"
	case status == http.StatusTooManyRequests:
		return "rate_limit"
	case status >= 500:
		return "provider_unavailable"
	case status == http.StatusNotFound || status == http.StatusGone:
		return "not_found"
	case status >= 400:
		return "provider_error"
	default:
		return ""
	}
}

func recordCatalogProviderCall(ctx, requestCtx context.Context, h *operationHistory, phase string, status int, started time.Time, err error) {
	if h == nil {
		return
	}
	h.recordCall(ctx, model.ProviderCall{
		Phase:      phase,
		HTTPStatus: status,
		DurationMS: time.Since(started).Milliseconds(),
		ErrorCode:  catalogProviderErrorCode(ctx, requestCtx, status, err),
	})
}

func (s *Service) SoftwareState() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.softwareRunning
}

func (s *Service) StartSoftware(ctx context.Context, trigger string) error {
	if trigger != "manual" && trigger != "automatic" {
		return model.ErrInvalid
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	if c.SoftwareNVDSecret != "" {
		if _, err = s.key(ctx, c.SoftwareNVDSecret); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.ErrConflict
	}
	if s.softwareRunning || s.maintenanceActiveLocked() {
		return model.ErrBusy
	}
	runCtx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.softwareRunning, s.softwareCancel, s.softwareDone = true, cancel, done
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		_ = s.SyncSoftware(runCtx, trigger)
		s.mu.Lock()
		s.softwareRunning, s.softwareCancel, s.softwareDone = false, nil, nil
		close(done)
		s.mu.Unlock()
		s.wakeAutomation()
	}()
	return nil
}

func (s *Service) StopSoftware() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.softwareCancel != nil {
		s.softwareCancel()
	}
}

func (s *Service) softwareNVDMetaEndpoint() string {
	if s.SoftwareNVDMetaURL != "" {
		return s.SoftwareNVDMetaURL
	}
	return softwareNVDMetaURL
}

func (s *Service) softwareNVDDumpEndpoint() string {
	if s.SoftwareNVDDumpURL != "" {
		return s.SoftwareNVDDumpURL
	}
	return softwareNVDDumpURL
}

func (s *Service) softwareNVDAPIEndpoint() string {
	if s.SoftwareNVDAPIBase != "" {
		return strings.TrimRight(s.SoftwareNVDAPIBase, "/")
	}
	return softwareNVDAPIBase
}

func (s *Service) softwareWinGetEndpoint() string {
	if s.SoftwareWinGetURL != "" {
		return s.SoftwareWinGetURL
	}
	return softwareWinGetURL
}

func (s *Service) softwareHomebrewEndpoint() string {
	if s.SoftwareHomebrewURL != "" {
		return s.SoftwareHomebrewURL
	}
	return softwareHomebrewURL
}

func (s *Service) softwareIconsEndpoint() string {
	if s.SoftwareIconsURL != "" {
		return s.SoftwareIconsURL
	}
	return softwareIconsURL
}

func (s *Service) softwareUsingCustomURLs() bool {
	return s.SoftwareNVDMetaURL != "" || s.SoftwareNVDDumpURL != "" || s.SoftwareNVDAPIBase != "" ||
		s.SoftwareWinGetURL != "" || s.SoftwareHomebrewURL != "" || s.SoftwareIconsURL != ""
}

func (s *Service) softwareNVDPace(hasKey bool) time.Duration {
	if s.SoftwareNVDAPIBase != "" {
		return time.Millisecond
	}
	if hasKey {
		return softwareNVDPaceWithKey
	}
	return softwareNVDPaceNoKey
}

func (s *Service) SyncSoftware(ctx context.Context, trigger string) (syncErr error) {
	if trigger != "manual" && trigger != "automatic" {
		return model.ErrInvalid
	}
	release, locked, err := s.DB.AIOLock(ctx, "software")
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
		if !c.SoftwareEnabled {
			return nil
		}
		due, err := s.DB.SoftwareDue(ctx, c.SoftwareMinutes)
		if err != nil || !due {
			return err
		}
	}

	nvdAPIKey := ""
	if c.SoftwareNVDSecret != "" {
		nvdAPIKey, err = s.key(ctx, c.SoftwareNVDSecret)
		if err != nil {
			return err
		}
	}

	if trigger == "automatic" {
		initialState, err := s.DB.SoftwareStatus(ctx)
		if err != nil {
			return err
		}
		if initialState.RetryAt != nil && initialState.RetryAt.After(time.Now()) {
			return nil
		}
	}

	h, err := s.beginOperation(ctx, "metadata", "software", trigger, "software", "software")
	if err != nil {
		return err
	}
	defer func() {
		h.finish(ctx, syncErr)
		final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		errMsg := ""
		if syncErr != nil && !errors.Is(syncErr, context.Canceled) {
			var se *softwareError
			retryAt := time.Now().Add(15 * time.Minute)
			msg := "Software information update failed. Saved progress is preserved."
			if errors.As(syncErr, &se) && se.message != "" {
				msg = se.message
				if !se.retryAt.IsZero() {
					retryAt = se.retryAt
				}
			}
			errMsg = msg
			_ = s.DB.FailSoftwareUpdate(final, msg, retryAt)
		} else if errors.Is(syncErr, context.Canceled) {
			_ = s.DB.FailSoftwareUpdate(final, "Software information update interrupted. Saved progress is preserved.", time.Now().Add(5*time.Minute))
		}
		_ = s.DB.RecordSoftwareInformationActivity(final, syncErr, errMsg, h.run.Processed)
	}()

	state, err := s.DB.SoftwareStatus(ctx)
	if err != nil {
		return err
	}

	sel := store.SoftwareSelection(c)
	needReproject := !state.Complete || state.NeedsFullImport || (state.Selection != "" && state.Selection != sel.Key())
	recoveryReason := state.RecoveryReason
	if c.SoftwareIncludeNVD && state.NVDChangesThrough != nil && time.Since(*state.NVDChangesThrough) > software.MaxNVDIncrementalWindow {
		recoveryReason = "nvd_window_exceeded"
	}

	gen, err := s.DB.BeginSoftwareSync(ctx, sel, recoveryReason)
	if err != nil {
		return err
	}

	var syncMeta store.SoftwareSyncMeta
	sourcesUpdated := 0
	progressReset := false
	markImporting := func() error {
		if progressReset {
			return nil
		}
		if err := s.DB.BeginSoftwareStaging(ctx); err != nil {
			return err
		}
		progressReset = true
		return nil
	}

	// 1. NIST NVD Official CPE Dictionary 2.0 (optional, ON by default).
	if c.SoftwareIncludeNVD {
		updated, err := s.syncSoftwareNVD(ctx, h, state, gen, nvdAPIKey, &syncMeta, markImporting)
		if err != nil {
			return err
		}
		if updated {
			sourcesUpdated++
		}
	}

	// 2. Microsoft WinGet (microsoft/winget-pkgs, streaming tar.gz).
	wingetUpdated, err := s.syncSoftwareWinGet(ctx, h, state, gen, &syncMeta, markImporting)
	if err != nil {
		return err
	}
	if wingetUpdated {
		sourcesUpdated++
	}

	// 3. Homebrew Cask (formulae.brew.sh/api/cask.json).
	brewUpdated, err := s.syncSoftwareHomebrew(ctx, h, state, gen, &syncMeta, markImporting)
	if err != nil {
		return err
	}
	if brewUpdated {
		sourcesUpdated++
	}

	// 4. selfh.st/icons lightweight remote icon index (optional, ON by default).
	if c.SoftwareIncludeIcons {
		iconsUpdated, err := s.syncSoftwareIcons(ctx, h, state, gen, &syncMeta, markImporting)
		if err != nil {
			return err
		}
		if iconsUpdated {
			sourcesUpdated++
		}
	} else if state.IconsIndexed > 0 || state.Icons > 0 {
		if err = markImporting(); err != nil {
			return err
		}
		if err = s.DB.ClearSoftwareIcons(ctx); err != nil {
			return err
		}
		needReproject = true
	}

	if sourcesUpdated > 0 {
		h.run.Results["sources_synced"] = int64(sourcesUpdated)
	}
	if sourcesUpdated == 0 && !needReproject {
		h.checkpoint(ctx, "up_to_date")
		return s.DB.TouchSoftwareCheck(ctx, syncMeta)
	}

	// Phase: Deterministic cross-source fusion and projection into aio_works.
	if err = s.DB.SetSoftwarePhase(ctx, "merging"); err != nil {
		return err
	}
	h.checkpoint(ctx, "software_merging")
	entries, icons, err := s.DB.LoadSoftwareStaging(ctx, c.SoftwareIncludeNVD, c.SoftwareIncludeIcons)
	if err != nil {
		return err
	}
	merged, mStats := software.MergeCatalog(entries, icons, sel)

	if err = s.DB.SetSoftwarePhase(ctx, "projecting"); err != nil {
		return err
	}
	h.checkpoint(ctx, "software_projecting")
	lastHistory := time.Now()
	if err = s.DB.ProjectSoftwareCatalog(ctx, gen, merged, mStats.Filtered, func(projected int64) {
		h.run.Processed += projected
		if time.Since(lastHistory) >= 5*time.Second {
			h.checkpoint(ctx, "software_projecting")
			lastHistory = time.Now()
		}
	}); err != nil {
		return err
	}

	// Phase: Atomic swap/cleanup of stale rows while preserving active reviews.
	if err = s.DB.SetSoftwarePhase(ctx, "cleanup"); err != nil {
		return err
	}
	h.checkpoint(ctx, "software_cleanup")
	if err = s.DB.FinishSoftwareSync(ctx, gen, syncMeta, sel); err != nil {
		return err
	}
	h.checkpoint(ctx, "completed")
	return nil
}

func (s *Service) syncSoftwareNVD(ctx context.Context, h *operationHistory, state model.SoftwareStatus, gen int64, apiKey string, syncMeta *store.SoftwareSyncMeta, markImporting func() error) (bool, error) {
	if err := s.DB.SetSoftwarePhase(ctx, "nvd_meta"); err != nil {
		return false, err
	}
	h.checkpoint(ctx, "software_nvd_meta")
	meta, err := s.fetchNVDMeta(ctx, h)
	if err != nil {
		return false, err
	}
	h.run.Results["nvd_checked"] = 1

	now := time.Now().UTC()
	canIncremental := state.NVDEntries > 0 &&
		state.NVDChangesThrough != nil &&
		!state.NeedsFullImport &&
		now.Sub(*state.NVDChangesThrough) <= software.MaxNVDIncrementalWindow

	// Skip NVD download when both meta hash and timestamp confirm no upstream changes.
	if canIncremental && meta.SHA256 != "" && meta.SHA256 == state.NVDSnapshotSHA256 &&
		(meta.LastModifiedAt == nil || !meta.LastModifiedAt.After(*state.NVDChangesThrough)) {
		return false, nil
	}

	if canIncremental {
		if err := s.DB.SetSoftwarePhase(ctx, "nvd_incremental"); err != nil {
			return false, err
		}
		h.checkpoint(ctx, "software_nvd_incremental")
		changed, err := s.syncSoftwareNVDIncremental(ctx, h, *state.NVDChangesThrough, now, gen, apiKey, markImporting)
		if err == nil {
			syncMeta.NVDChangesThrough = &now
			if meta.LastModifiedRaw != "" {
				syncMeta.NVDSnapshot = meta.LastModifiedRaw
			}
			if meta.SHA256 != "" {
				syncMeta.NVDSnapshotSHA256 = meta.SHA256
			}
			if meta.LastModifiedAt != nil {
				syncMeta.NVDSnapshotAt = meta.LastModifiedAt
			}
			if changed {
				h.run.Results["nvd_incremental"] = 1
			}
			return changed, nil
		}
		if !errors.Is(err, errSoftwareIncrementalFallback) {
			return false, err
		}
		// Automatic fallback to full nvdcpe-2.0.tar.gz while existing catalog stays intact.
	}

	if err := s.DB.SetSoftwarePhase(ctx, "nvd_dump"); err != nil {
		return false, err
	}
	h.checkpoint(ctx, "software_nvd_dump")
	stats, err := s.streamSoftwareNVDDump(ctx, h, meta.SHA256, gen, markImporting)
	if err != nil {
		return false, err
	}
	if err = s.DB.PruneSoftwareSourceEntries(ctx, "nvd", gen); err != nil {
		return false, err
	}
	syncMeta.NVDSnapshot = meta.LastModifiedRaw
	syncMeta.NVDSnapshotSHA256 = meta.SHA256
	if syncMeta.NVDSnapshotSHA256 == "" {
		syncMeta.NVDSnapshotSHA256 = stats.ContentSHA256
	}
	if syncMeta.NVDSnapshotSHA256 == "" {
		syncMeta.NVDSnapshotSHA256 = stats.TarSHA256
	}
	syncMeta.NVDSnapshotAt = meta.LastModifiedAt
	if stats.LatestModified != nil {
		syncMeta.NVDChangesThrough = stats.LatestModified
	} else if meta.LastModifiedAt != nil {
		syncMeta.NVDChangesThrough = meta.LastModifiedAt
	} else {
		syncMeta.NVDChangesThrough = &now
	}
	syncMeta.FullImport = true
	h.run.Results["full_import"] = 1
	return true, nil
}

func (s *Service) fetchNVDMeta(ctx context.Context, h *operationHistory) (software.NVDMeta, error) {
	endpoint := s.softwareNVDMetaEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return software.NVDMeta{}, ErrInvalidResponse
	}
	req.Header.Set("User-Agent", softwareAgent)
	client, err := s.prepareOutgoingClient(ctx, s.SoftwareHTTP, "metadata_seconds", false)
	if err != nil {
		return software.NVDMeta{}, err
	}
	if !s.softwareUsingCustomURLs() {
		client.CheckRedirect = trustedSoftwareExport
	}
	started := time.Now()
	res, err := client.Do(req)
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	recordCatalogProviderCall(ctx, nil, h, "software_nvd_meta", status, started, err)
	if err != nil {
		if ctx.Err() != nil {
			return software.NVDMeta{}, ctx.Err()
		}
		return software.NVDMeta{}, &softwareError{message: "NIST NVD feed metadata could not be reached. Check network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
		wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		return software.NVDMeta{}, &softwareError{message: "NIST NVD feed is temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(wait)}
	}
	if res.StatusCode != http.StatusOK {
		return software.NVDMeta{}, &softwareError{message: fmt.Sprintf("NIST NVD feed metadata returned HTTP %d.", res.StatusCode)}
	}
	meta, err := software.ParseNVDMeta(res.Body)
	if err != nil {
		return software.NVDMeta{}, &softwareError{message: "NIST NVD feed metadata could not be processed. Saved progress is preserved."}
	}
	return meta, nil
}

func (s *Service) streamSoftwareNVDDump(ctx context.Context, h *operationHistory, expectedSHA256 string, gen int64, markImporting func() error) (software.NVDDumpStats, error) {
	endpoint := s.softwareNVDDumpEndpoint()
	client, err := s.prepareOutgoingClient(ctx, s.SoftwareHTTP, "inventory_seconds", false)
	if err != nil {
		return software.NVDDumpStats{}, err
	}
	headerTimeout := client.Timeout
	if headerTimeout <= 0 {
		headerTimeout = 2 * time.Minute
	}
	requestCtx, requestCancel := context.WithCancel(ctx)
	defer requestCancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return software.NVDDumpStats{}, ErrInvalidResponse
	}
	req.Header.Set("User-Agent", softwareAgent)
	streamClient := client
	streamClient.Timeout = 0
	if !s.softwareUsingCustomURLs() {
		streamClient.CheckRedirect = trustedSoftwareExport
	}
	idle := time.AfterFunc(headerTimeout, requestCancel)
	defer idle.Stop()

	started := time.Now()
	res, err := streamClient.Do(req)
	idle.Stop()
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	recordCatalogProviderCall(ctx, requestCtx, h, "software_nvd_dump", status, started, err)
	if err != nil {
		if ctx.Err() != nil {
			return software.NVDDumpStats{}, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return software.NVDDumpStats{}, &softwareError{message: "NIST NVD CPE archive timed out while connecting. Saved progress is preserved.", retryAt: time.Now().Add(5 * time.Minute)}
		}
		return software.NVDDumpStats{}, &softwareError{message: "NIST NVD CPE archive could not be reached. Check network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
		wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		return software.NVDDumpStats{}, &softwareError{message: "NIST NVD CPE archive is temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(wait)}
	}
	if res.StatusCode != http.StatusOK {
		return software.NVDDumpStats{}, &softwareError{message: fmt.Sprintf("NIST NVD CPE archive returned HTTP %d.", res.StatusCode)}
	}
	if err = markImporting(); err != nil {
		return software.NVDDumpStats{}, err
	}
	body := &archiveReader{reader: res.Body, timer: idle}
	lastHistory := time.Now()
	var batchErr error
	stats, err := software.StreamNVDDump(requestCtx, body, expectedSHA256, 1000, func(batch []software.SourceEntry) error {
		if err := s.DB.SaveSoftwareSourceBatch(ctx, gen, batch, int64(len(batch)), 0, false); err != nil {
			batchErr = err
			return err
		}
		h.run.Processed += int64(len(batch))
		if time.Since(lastHistory) >= 5*time.Second {
			h.checkpoint(ctx, "software_nvd_dump")
			lastHistory = time.Now()
		}
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		if batchErr != nil {
			return stats, batchErr
		}
		if requestCtx.Err() != nil {
			return stats, &softwareError{message: "NIST NVD CPE archive timed out while streaming. Saved progress is preserved.", retryAt: time.Now().Add(5 * time.Minute)}
		}
		if errors.Is(err, software.ErrNVDChecksumMismatch) {
			return stats, &softwareError{message: "NIST NVD CPE archive integrity verification failed. Saved progress is preserved.", retryAt: time.Now().Add(15 * time.Minute)}
		}
		return stats, &softwareError{message: "NIST NVD CPE archive could not be processed. Saved progress is preserved."}
	}
	return stats, nil
}

func (s *Service) syncSoftwareNVDIncremental(ctx context.Context, h *operationHistory, since, until time.Time, gen int64, apiKey string, markImporting func() error) (bool, error) {
	pace := s.softwareNVDPace(apiKey != "")
	startIndex := 0
	pages := 0
	total := 0
	var entries []software.SourceEntry
	startStr := since.UTC().Format("2006-01-02T15:04:05.000Z")
	endStr := until.UTC().Format("2006-01-02T15:04:05.000Z")

	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if pages > 0 {
			if err := sleepContext(ctx, pace); err != nil {
				return false, err
			}
		}
		if pages >= softwareMaxIncrementalPages {
			return false, errSoftwareIncrementalFallback
		}
		params := url.Values{
			"lastModStartDate": {startStr},
			"lastModEndDate":   {endStr},
			"resultsPerPage":   {strconv.Itoa(softwareNVDPageSize)},
			"startIndex":       {strconv.Itoa(startIndex)},
		}
		endpoint := s.softwareNVDAPIEndpoint() + "?" + params.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return false, ErrInvalidResponse
		}
		req.Header.Set("User-Agent", softwareAgent)
		if apiKey != "" {
			// Send the optional NVD API key exclusively in the apiKey header.
			req.Header.Set("apiKey", apiKey)
		}
		client, err := s.prepareOutgoingClient(ctx, s.SoftwareHTTP, "metadata_seconds", false)
		if err != nil {
			return false, err
		}
		started := time.Now()
		res, err := client.Do(req)
		status := 0
		if res != nil {
			status = res.StatusCode
		}
		recordCatalogProviderCall(ctx, nil, h, "software_nvd_incremental", status, started, err)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, &softwareError{message: "NIST NVD CPE API could not be reached. Check network settings."}
		}
		if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			res.Body.Close()
			if apiKey != "" {
				return false, &softwareError{message: "NIST NVD rejected the configured API key. Check the secret.", retryAt: time.Now().Add(15 * time.Minute)}
			}
			return false, errSoftwareIncrementalFallback
		}
		if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
			res.Body.Close()
			return false, errSoftwareIncrementalFallback
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
			res.Body.Close()
			return false, &softwareError{message: "NIST NVD CPE API requested a waiting period. Saved progress is preserved.", retryAt: time.Now().Add(wait)}
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return false, errSoftwareIncrementalFallback
		}
		limited := &io.LimitedReader{R: res.Body, N: (64 << 20) + 1}
		page, parseErr := software.ParseNVDAPIResponse(limited)
		res.Body.Close()
		if parseErr != nil || limited.N == 0 || page.StartIndex != startIndex || (pages > 0 && page.TotalResults != total) {
			return false, errSoftwareIncrementalFallback
		}
		total = page.TotalResults
		if total > software.MaxNVDIncrementalResults {
			return false, errSoftwareIncrementalFallback
		}
		// A deprecated CPE can be the last active version of a product. Only a
		// full snapshot can safely recompute the aggregated product and its versions.
		if page.UndatedDeprecations || (page.LatestDeprecation != nil && page.LatestDeprecation.After(since)) {
			return false, errSoftwareIncrementalFallback
		}
		entries = append(entries, page.Entries...)
		pages++
		startIndex += page.ProductCount
		if startIndex == total {
			break
		}
	}
	if len(entries) == 0 {
		return false, nil
	}
	// Only stage incremental records after validating the whole window. A
	// full-export fallback must start without mutations from a rejected page walk.
	if err := markImporting(); err != nil {
		return false, err
	}
	for start := 0; start < len(entries); start += 1000 {
		batch := entries[start:min(start+1000, len(entries))]
		if err := s.DB.SaveSoftwareSourceBatch(ctx, gen, batch, int64(len(batch)), 0, true); err != nil {
			return false, err
		}
		h.run.Processed += int64(len(batch))
		h.checkpoint(ctx, "software_nvd_incremental")
	}
	return true, nil
}

func (s *Service) syncSoftwareWinGet(ctx context.Context, h *operationHistory, state model.SoftwareStatus, gen int64, syncMeta *store.SoftwareSyncMeta, markImporting func() error) (bool, error) {
	if err := s.DB.SetSoftwarePhase(ctx, "winget"); err != nil {
		return false, err
	}
	h.checkpoint(ctx, "software_winget")
	endpoint := s.softwareWinGetEndpoint()
	client, err := s.prepareOutgoingClient(ctx, s.SoftwareHTTP, "inventory_seconds", false)
	if err != nil {
		return false, err
	}
	headerTimeout := client.Timeout
	if headerTimeout <= 0 {
		headerTimeout = 2 * time.Minute
	}
	requestCtx, requestCancel := context.WithCancel(ctx)
	defer requestCancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, ErrInvalidResponse
	}
	req.Header.Set("User-Agent", softwareAgent)
	if state.WinGetEntries > 0 && state.WinGetETag != "" && !state.NeedsFullImport {
		req.Header.Set("If-None-Match", state.WinGetETag)
	}
	streamClient := client
	streamClient.Timeout = 0
	if !s.softwareUsingCustomURLs() {
		streamClient.CheckRedirect = trustedSoftwareExport
	}
	idle := time.AfterFunc(headerTimeout, requestCancel)
	defer idle.Stop()

	started := time.Now()
	res, err := streamClient.Do(req)
	idle.Stop()
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	recordCatalogProviderCall(ctx, requestCtx, h, "software_winget", status, started, err)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return false, &softwareError{message: "Microsoft WinGet repository archive timed out while connecting. Saved progress is preserved.", retryAt: time.Now().Add(5 * time.Minute)}
		}
		return false, &softwareError{message: "Microsoft WinGet repository archive could not be reached. Check network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified && req.Header.Get("If-None-Match") != "" {
		return false, nil
	}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
		wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		return false, &softwareError{message: "Microsoft WinGet archive is temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(wait)}
	}
	if res.StatusCode != http.StatusOK {
		return false, &softwareError{message: fmt.Sprintf("Microsoft WinGet archive returned HTTP %d.", res.StatusCode)}
	}
	if err = markImporting(); err != nil {
		return false, err
	}
	body := &archiveReader{reader: res.Body, timer: idle}
	lastHistory := time.Now()
	var batchErr error
	if _, err = software.StreamWinGetArchive(requestCtx, body, 500, func(batch []software.SourceEntry) error {
		if err := s.DB.SaveSoftwareSourceBatch(ctx, gen, batch, int64(len(batch)), 0, false); err != nil {
			batchErr = err
			return err
		}
		h.run.Processed += int64(len(batch))
		if time.Since(lastHistory) >= 5*time.Second {
			h.checkpoint(ctx, "software_winget")
			lastHistory = time.Now()
		}
		return nil
	}); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if batchErr != nil {
			return false, batchErr
		}
		if requestCtx.Err() != nil {
			return false, &softwareError{message: "Microsoft WinGet archive timed out while streaming. Saved progress is preserved.", retryAt: time.Now().Add(5 * time.Minute)}
		}
		return false, &softwareError{message: "Microsoft WinGet archive could not be processed. Saved progress is preserved."}
	}
	if err = s.DB.PruneSoftwareSourceEntries(ctx, "winget", gen); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	syncMeta.WinGetETag = strings.TrimSpace(res.Header.Get("ETag"))
	syncMeta.WinGetSyncedAt = &now
	return true, nil
}

func (s *Service) syncSoftwareHomebrew(ctx context.Context, h *operationHistory, state model.SoftwareStatus, gen int64, syncMeta *store.SoftwareSyncMeta, markImporting func() error) (bool, error) {
	if err := s.DB.SetSoftwarePhase(ctx, "homebrew"); err != nil {
		return false, err
	}
	h.checkpoint(ctx, "software_homebrew")
	endpoint := s.softwareHomebrewEndpoint()
	client, err := s.prepareOutgoingClient(ctx, s.SoftwareHTTP, "inventory_seconds", false)
	if err != nil {
		return false, err
	}
	headerTimeout := client.Timeout
	if headerTimeout <= 0 {
		headerTimeout = 2 * time.Minute
	}
	requestCtx, requestCancel := context.WithCancel(ctx)
	defer requestCancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, ErrInvalidResponse
	}
	req.Header.Set("User-Agent", softwareAgent)
	if state.HomebrewEntries > 0 && state.HomebrewETag != "" && !state.NeedsFullImport {
		req.Header.Set("If-None-Match", state.HomebrewETag)
	}
	streamClient := client
	streamClient.Timeout = 0
	if !s.softwareUsingCustomURLs() {
		streamClient.CheckRedirect = trustedSoftwareExport
	}
	idle := time.AfterFunc(headerTimeout, requestCancel)
	defer idle.Stop()

	started := time.Now()
	res, err := streamClient.Do(req)
	idle.Stop()
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	recordCatalogProviderCall(ctx, requestCtx, h, "software_homebrew", status, started, err)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return false, &softwareError{message: "Homebrew Cask catalog timed out while connecting. Saved progress is preserved.", retryAt: time.Now().Add(5 * time.Minute)}
		}
		return false, &softwareError{message: "Homebrew Cask catalog could not be reached. Check network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified && req.Header.Get("If-None-Match") != "" {
		return false, nil
	}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
		wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		return false, &softwareError{message: "Homebrew Cask catalog is temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(wait)}
	}
	if res.StatusCode != http.StatusOK {
		return false, &softwareError{message: fmt.Sprintf("Homebrew Cask catalog returned HTTP %d.", res.StatusCode)}
	}
	if err = markImporting(); err != nil {
		return false, err
	}
	body := &archiveReader{reader: res.Body, timer: idle}
	lastHistory := time.Now()
	var batchErr error
	if _, err = software.StreamHomebrewCasks(requestCtx, body, 500, func(batch []software.SourceEntry) error {
		if err := s.DB.SaveSoftwareSourceBatch(ctx, gen, batch, int64(len(batch)), 0, false); err != nil {
			batchErr = err
			return err
		}
		h.run.Processed += int64(len(batch))
		if time.Since(lastHistory) >= 5*time.Second {
			h.checkpoint(ctx, "software_homebrew")
			lastHistory = time.Now()
		}
		return nil
	}); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if batchErr != nil {
			return false, batchErr
		}
		if requestCtx.Err() != nil {
			return false, &softwareError{message: "Homebrew Cask catalog timed out while streaming. Saved progress is preserved.", retryAt: time.Now().Add(5 * time.Minute)}
		}
		return false, &softwareError{message: "Homebrew Cask catalog could not be processed. Saved progress is preserved."}
	}
	if err = s.DB.PruneSoftwareSourceEntries(ctx, "homebrew", gen); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	syncMeta.HomebrewETag = strings.TrimSpace(res.Header.Get("ETag"))
	syncMeta.HomebrewSyncedAt = &now
	return true, nil
}

func (s *Service) syncSoftwareIcons(ctx context.Context, h *operationHistory, state model.SoftwareStatus, gen int64, syncMeta *store.SoftwareSyncMeta, markImporting func() error) (bool, error) {
	if err := s.DB.SetSoftwarePhase(ctx, "icons"); err != nil {
		return false, err
	}
	h.checkpoint(ctx, "software_icons")
	endpoint := s.softwareIconsEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, ErrInvalidResponse
	}
	req.Header.Set("User-Agent", softwareAgent)
	if state.IconsIndexed > 0 && state.IconsETag != "" && !state.NeedsFullImport {
		req.Header.Set("If-None-Match", state.IconsETag)
	}
	client, err := s.prepareOutgoingClient(ctx, s.SoftwareHTTP, "metadata_seconds", false)
	if err != nil {
		return false, err
	}
	if !s.softwareUsingCustomURLs() {
		client.CheckRedirect = trustedSoftwareExport
	}
	started := time.Now()
	res, err := client.Do(req)
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	recordCatalogProviderCall(ctx, nil, h, "software_icons", status, started, err)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, &softwareError{message: "selfh.st software icons index could not be reached. Check network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified && req.Header.Get("If-None-Match") != "" {
		return false, nil
	}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
		wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		return false, &softwareError{message: "selfh.st software icons index is temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(wait)}
	}
	if res.StatusCode != http.StatusOK {
		return false, &softwareError{message: fmt.Sprintf("selfh.st software icons index returned HTTP %d.", res.StatusCode)}
	}
	icons, skipped, err := software.ParseSelfhstIcons(res.Body)
	if err != nil {
		return false, &softwareError{message: "selfh.st software icons index could not be processed. Saved progress is preserved."}
	}
	if err = markImporting(); err != nil {
		return false, err
	}
	if err = s.DB.SaveSoftwareIcons(ctx, gen, icons, skipped); err != nil {
		return false, err
	}
	h.run.Processed += int64(len(icons))
	now := time.Now().UTC()
	syncMeta.IconsETag = strings.TrimSpace(res.Header.Get("ETag"))
	syncMeta.IconsSyncedAt = &now
	return true, nil
}
