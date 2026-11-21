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
	"github.com/theo-bnts/aiosources/internal/music"
	"github.com/theo-bnts/aiosources/internal/store"
)

const (
	musicBrainzExportBase           = "https://data.metabrainz.org/pub/musicbrainz/data/fullexport"
	musicBrainzReplicationBase      = "https://metabrainz.org/api/musicbrainz"
	musicBrainzAgent                = "AIOSources/" + Version + " (+https://github.com/theo-bnts/aiosources)"
	musicReplicationMaxLag          = 7 * 24 * time.Hour
	musicReplicationMaxPacketsInRun = 168
)

func trustedMusicExport(r *http.Request, via []*http.Request) error {
	host := strings.ToLower(r.URL.Hostname())
	port := r.URL.Port()
	if len(via) > 5 || r.URL.Scheme != "https" || r.URL.User != nil || (port != "" && port != "443") ||
		!(host == "metabrainz.org" || strings.HasSuffix(host, ".metabrainz.org") ||
			host == "musicbrainz.org" || strings.HasSuffix(host, ".musicbrainz.org") ||
			host == "archive.org" || strings.HasSuffix(host, ".archive.org")) {
		return errors.New("untrusted music export redirect")
	}
	return nil
}

type musicError struct {
	message string
	retryAt time.Time
}

func (e *musicError) Error() string { return e.message }

func (s *Service) MusicState() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.musicRunning
}

func (s *Service) StartMusic(ctx context.Context, trigger string) error {
	if trigger != "manual" && trigger != "automatic" {
		return model.ErrInvalid
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	if c.MusicBrainzSecret != "" {
		if _, err = s.key(ctx, c.MusicBrainzSecret); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.ErrConflict
	}
	if s.musicRunning || s.reset.Status == "running" {
		return model.ErrBusy
	}
	runCtx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.musicRunning, s.musicCancel, s.musicDone = true, cancel, done
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		_ = s.SyncMusic(runCtx, trigger)
		s.mu.Lock()
		s.musicRunning, s.musicCancel, s.musicDone = false, nil, nil
		close(done)
		s.mu.Unlock()
		s.wakeAutomation()
	}()
	return nil
}

func (s *Service) StopMusic() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.musicCancel != nil {
		s.musicCancel()
	}
}

func (s *Service) SyncMusic(ctx context.Context, trigger string) (syncErr error) {
	if trigger != "manual" && trigger != "automatic" {
		return model.ErrInvalid
	}
	release, locked, err := s.DB.AIOLock(ctx, "musicbrainz")
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
	live := c.MusicBrainzSecret != ""
	if trigger == "automatic" {
		if !c.MusicBrainzEnabled {
			return nil
		}
		due, err := s.DB.MusicDue(ctx, c.MusicBrainzMinutes, live)
		if err != nil || !due {
			return err
		}
	}
	token := ""
	if live {
		token, err = s.key(ctx, c.MusicBrainzSecret)
		if err != nil {
			return err
		}
	}

	h, err := s.beginOperation(ctx, "metadata", "musicbrainz", trigger, "music", "musicbrainz")
	if err != nil {
		return err
	}
	if err = s.DB.PrepareMusicImport(ctx, c); err != nil {
		h.finish(ctx, err)
		return err
	}
	defer func() {
		h.finish(ctx, syncErr)
		final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if syncErr != nil && !errors.Is(syncErr, context.Canceled) {
			var me *musicError
			retryAt := time.Now().Add(15 * time.Minute)
			msg := "Music information update failed. Saved progress is preserved."
			if errors.As(syncErr, &me) {
				msg = me.message
				if !me.retryAt.IsZero() {
					retryAt = me.retryAt
				}
			}
			if !errors.Is(syncErr, music.ErrUnsupportedSchema) {
				_ = s.DB.FailMusicUpdate(final, msg, retryAt)
			}
		} else if errors.Is(syncErr, context.Canceled) {
			_ = s.DB.FailMusicUpdate(final, "Music information update interrupted. Saved progress is preserved.", time.Now().Add(5*time.Minute))
		}
		_ = s.DB.RecordMusicInformationActivity(final, syncErr, h.run.Processed)
	}()

	state, err := s.DB.MusicStatus(ctx)
	if err != nil {
		return err
	}
	if trigger == "automatic" && state.RetryAt != nil && state.RetryAt.After(time.Now()) {
		return nil
	}

	// Case 1: Initial import, changed catalog filters, or pending export recovery.
	if !state.Complete || state.NeedsExport {
		return s.syncMusicExport(ctx, h, c, state, state.RecoveryReason)
	}

	// Case 2: Live Data Feed replication when an admin token is configured.
	if token != "" && state.ReplicationSequence > 0 {
		// If updates were paused for a long time (e.g. weeks or 3 months),
		// skip replaying hundreds/thousands of expired hourly packets and
		// recover directly from the latest bi-weekly fullexport.
		if state.ChangesThrough != nil && time.Since(*state.ChangesThrough) > musicReplicationMaxLag {
			return s.syncMusicExport(ctx, h, c, state, "replication_lag")
		}
		return s.syncMusicReplication(ctx, h, c, state, token)
	}

	// Case 3: Dump-only mode (no token, 100% CC0 core): poll LATEST and import
	// only when a newer twice-weekly snapshot is published.
	latest, err := s.fetchLatestMusicSnapshot(ctx, h)
	if err != nil {
		return err
	}
	h.run.Results["export_checked"] = 1
	if latest == state.Snapshot {
		h.checkpoint(ctx, "up_to_date")
		return s.DB.TouchMusicCheck(ctx)
	}
	return s.syncMusicExport(ctx, h, c, state, "")
}

func (s *Service) musicExportBaseURL() string {
	if s.MusicExportBase != "" {
		return strings.TrimRight(s.MusicExportBase, "/")
	}
	return musicBrainzExportBase
}

func (s *Service) musicReplicationBaseURL() string {
	if s.MusicReplicationBase != "" {
		return strings.TrimRight(s.MusicReplicationBase, "/")
	}
	return musicBrainzReplicationBase
}

func (s *Service) fetchLatestMusicSnapshot(ctx context.Context, h *operationHistory) (string, error) {
	endpoint := s.musicExportBaseURL() + "/LATEST"
	body, status, retryAfter, err := s.musicGETBytes(ctx, h, "music_snapshot", endpoint, 256)
	if err != nil {
		return "", err
	}
	if status == 429 || status >= 500 {
		return "", &musicError{message: "MusicBrainz exports are temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(max(retryAfter, 5*time.Minute))}
	}
	if status != http.StatusOK {
		return "", &musicError{message: "Could not read the latest MusicBrainz export index."}
	}
	snapshot, ok := music.ExportSnapshot(string(body))
	if !ok {
		return "", &musicError{message: "The MusicBrainz export index returned an unexpected snapshot identifier."}
	}
	return snapshot, nil
}

func (s *Service) syncMusicExport(ctx context.Context, h *operationHistory, c model.AIOConfig, state model.MusicStatus, recoveryReason string) error {
	h.checkpoint(ctx, "music_snapshot")
	snapshot, err := s.fetchLatestMusicSnapshot(ctx, h)
	if err != nil {
		return err
	}
	h.run.Results["export_checked"] = 1
	// If recovering from a replication gap or schema bump, wait cleanly if the
	// upstream fullexport has not yet caught up past our existing snapshot.
	if recoveryReason != "" && state.Complete && state.Snapshot != "" && snapshot <= state.Snapshot {
		return s.DB.MarkMusicNeedsExport(ctx, recoveryReason, "A newer MusicBrainz export is needed to recover missing changes. Existing music information is preserved.", false, time.Now().Add(6*time.Hour))
	}

	var snapshotAt *time.Time
	if at, ok := music.ExportDate(snapshot); ok {
		snapshotAt = &at
	}
	sel := store.MusicSelection(c)
	gen, err := s.DB.BeginMusicDump(ctx, snapshot, snapshotAt, sel.Key(), recoveryReason)
	if err != nil {
		return err
	}

	// Phase 1: Stream mbdump.tar.bz2 (Core CC0 tables only).
	h.checkpoint(ctx, "music_dump")
	coreURL := s.musicExportBaseURL() + "/" + snapshot + "/mbdump.tar.bz2"
	header, err := s.streamMusicArchive(ctx, h, "music_dump", coreURL, music.WatchedCoreTables, gen)
	if err != nil {
		if errors.Is(err, music.ErrUnsupportedSchema) {
			_ = s.DB.MarkMusicNeedsExport(ctx, "schema_unsupported", "MusicBrainz updated its database schema. Saved music information is preserved; update AIOSources to resume updates.", true, time.Now().Add(6*time.Hour))
		}
		return err
	}
	if err = s.DB.PruneUnlinkedMusicURLs(ctx); err != nil {
		return err
	}

	// Phase 2: Stream mbdump-cover-art-archive.tar.bz2 (optional, on by default).
	if c.MusicBrainzIncludeCovers {
		if err = s.DB.SetMusicPhase(ctx, "covers"); err != nil {
			return err
		}
		h.checkpoint(ctx, "music_covers")
		coverURL := s.musicExportBaseURL() + "/" + snapshot + "/mbdump-cover-art-archive.tar.bz2"
		if _, err = s.streamMusicArchive(ctx, h, "music_covers", coverURL, music.WatchedCoverTables, gen); err != nil {
			if errors.Is(err, music.ErrUnsupportedSchema) {
				_ = s.DB.MarkMusicNeedsExport(ctx, "schema_unsupported", "MusicBrainz updated its database schema. Saved music information is preserved; update AIOSources to resume updates.", true, time.Now().Add(6*time.Hour))
			}
			return err
		}
	}

	// Phase 3: Project filtered release groups into aiosources.aio_works.
	if err = s.DB.SetMusicPhase(ctx, "projecting"); err != nil {
		return err
	}
	h.checkpoint(ctx, "music_projecting")
	worksCount, _, _, err := s.DB.ProjectMusicCatalog(ctx, gen, sel, nil)
	if err != nil {
		return err
	}
	h.run.Processed += worksCount

	// Phase 4: Prune old generation rows & commit final state + backfill queue.
	if err = s.DB.SetMusicPhase(ctx, "cleanup"); err != nil {
		return err
	}
	h.checkpoint(ctx, "music_cleanup")
	if err = s.DB.FinishMusicDump(ctx, gen, header, snapshot, snapshotAt, c.MusicBrainzIncludeCovers); err != nil {
		return err
	}
	h.checkpoint(ctx, "completed")
	return nil
}

func (s *Service) streamMusicArchive(ctx context.Context, h *operationHistory, callName, endpoint string, watched map[string]bool, gen int64) (music.DumpHeader, error) {
	if err := s.musicRate.wait(ctx); err != nil {
		return music.DumpHeader{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return music.DumpHeader{}, ErrInvalidResponse
	}
	req.Header.Set("User-Agent", musicBrainzAgent)
	client, err := s.prepareOutgoingClient(ctx, s.MusicHTTP, "inventory_seconds", false)
	if err != nil {
		return music.DumpHeader{}, err
	}
	// Streaming multi-gigabyte dumps relies on context cancellation, not a short
	// whole-request deadline, while preserving connection/header timeouts.
	streamClient := client
	streamClient.Timeout = 0

	started := time.Now()
	res, err := streamClient.Do(req)
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	if h != nil {
		_ = s.DB.RecordProviderCall(ctx, h.run.ID, callName, status, time.Since(started).Milliseconds(), "")
	}
	if err != nil {
		if ctx.Err() != nil {
			return music.DumpHeader{}, ctx.Err()
		}
		return music.DumpHeader{}, &musicError{message: "MusicBrainz export could not be reached. Check network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == 429 || res.StatusCode >= 500 {
		wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		return music.DumpHeader{}, &musicError{message: "MusicBrainz exports are temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(wait)}
	}
	if res.StatusCode != http.StatusOK {
		return music.DumpHeader{}, &musicError{message: fmt.Sprintf("MusicBrainz export returned HTTP %d.", res.StatusCode)}
	}
	return music.StreamDumpArchive(ctx, res.Body, watched, 2000, func(batch music.DumpTableBatch) error {
		if err := s.DB.SaveMusicDumpBatch(ctx, gen, batch); err != nil {
			return err
		}
		h.run.Processed += int64(batch.Len())
		return nil
	})
}

func (s *Service) syncMusicReplication(ctx context.Context, h *operationHistory, c model.AIOConfig, state model.MusicStatus, token string) error {
	if err := s.DB.SetMusicPhase(ctx, "replication"); err != nil {
		return err
	}
	h.checkpoint(ctx, "music_replication")
	sel := store.MusicSelection(c)
	seq := state.ReplicationSequence
	applied := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if applied >= musicReplicationMaxPacketsInRun {
			return s.syncMusicExport(ctx, h, c, state, "replication_lag")
		}
		nextSeq := seq + 1
		// Keep the token strictly inside the outbound HTTP request; never log or
		// include the request URL in errors or diagnostics.
		endpoint := s.musicReplicationBaseURL() + "/replication-" + strconv.FormatInt(nextSeq, 10) + "-v2.tar.bz2?token=" + url.QueryEscape(token)
		packet, status, retryAfter, err := s.fetchReplicationPacket(ctx, h, endpoint, c.MusicBrainzIncludeCovers)
		if err != nil {
			if errors.Is(err, music.ErrUnsupportedSchema) {
				_ = s.DB.MarkMusicNeedsExport(ctx, "schema_unsupported", "MusicBrainz updated its database schema. Saved music information is preserved; update AIOSources to resume updates.", true, time.Now().Add(6*time.Hour))
			}
			return err
		}
		switch status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return &musicError{message: "MusicBrainz Live Data Feed rejected the token. Check the secret.", retryAt: time.Now().Add(15 * time.Minute)}
		case http.StatusTooManyRequests:
			return &musicError{message: "MusicBrainz Live Data Feed requested a waiting period. Saved progress is preserved.", retryAt: time.Now().Add(max(retryAfter, 5*time.Minute))}
		case http.StatusNotFound, http.StatusGone:
			// Distinguish between being caught up at the live tip vs. missing an
			// expired packet after a long pause (e.g. 3 months offline).
			if applied == 0 && state.ChangesThrough != nil && time.Since(*state.ChangesThrough) > 24*time.Hour {
				return s.syncMusicExport(ctx, h, c, state, "packet_expired")
			}
			if applied == 0 {
				if err = s.DB.TouchMusicCheck(ctx); err != nil {
					return err
				}
			}
			h.checkpoint(ctx, "completed")
			return nil
		case http.StatusOK:
			if packet.ReplicationSequence != nextSeq {
				return &musicError{message: "MusicBrainz replication packet sequence did not match the requested packet."}
			}
			// Schema upgrade between packets: recover cleanly via fresh fullexport.
			if state.SchemaSequence > 0 && packet.SchemaSequence != state.SchemaSequence {
				return s.syncMusicExport(ctx, h, c, state, "schema_upgrade")
			}
			affected, err := s.DB.ApplyMusicReplicationPacket(ctx, packet, sel)
			if err != nil {
				return err
			}
			seq = nextSeq
			applied++
			h.run.Results["packets_applied"]++
			h.run.Processed += int64(affected)
			h.checkpoint(ctx, "music_replication")
			if !packet.Timestamp.IsZero() {
				ts := packet.Timestamp
				state.ChangesThrough = &ts
			}
			state.ReplicationSequence = seq
			state.SchemaSequence = packet.SchemaSequence
		default:
			if status >= 500 {
				return &musicError{message: "MusicBrainz Live Data Feed is temporarily unavailable. Saved progress is preserved.", retryAt: time.Now().Add(max(retryAfter, 5*time.Minute))}
			}
			return &musicError{message: fmt.Sprintf("MusicBrainz Live Data Feed returned HTTP %d.", status)}
		}
	}
}

func (s *Service) fetchReplicationPacket(ctx context.Context, h *operationHistory, endpoint string, includeCovers bool) (music.ReplicationPacket, int, time.Duration, error) {
	policy, err := s.DB.RuntimeValues(ctx, "metadata_retries")
	if err != nil {
		return music.ReplicationPacket{}, 0, 0, err
	}
	attempts := max(1, policy["request_attempts"])
	for attempt := 0; attempt < attempts; attempt++ {
		if err := s.musicRate.wait(ctx); err != nil {
			return music.ReplicationPacket{}, 0, 0, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return music.ReplicationPacket{}, 0, 0, ErrInvalidResponse
		}
		req.Header.Set("User-Agent", musicBrainzAgent)
		client, err := s.prepareOutgoingClient(ctx, s.MusicHTTP, "metadata_seconds", false)
		if err != nil {
			return music.ReplicationPacket{}, 0, 0, err
		}
		started := time.Now()
		res, err := client.Do(req)
		status := 0
		if res != nil {
			status = res.StatusCode
		}
		if h != nil {
			_ = s.DB.RecordProviderCall(ctx, h.run.ID, "music_replication", status, time.Since(started).Milliseconds(), "")
		}
		if err != nil {
			if ctx.Err() != nil {
				return music.ReplicationPacket{}, 0, 0, ctx.Err()
			}
			return music.ReplicationPacket{}, 0, 0, &musicError{message: "MusicBrainz Live Data Feed could not be reached. Check network settings."}
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			wait := metadataRetryAfter(res.Header.Get("Retry-After"), time.Duration(max(1, policy["request_retry_seconds"]))*time.Second, time.Now())
			res.Body.Close()
			if wait > time.Minute || attempt >= attempts-1 {
				return music.ReplicationPacket{}, res.StatusCode, wait, nil
			}
			if err := sleepContext(ctx, wait); err != nil {
				return music.ReplicationPacket{}, 0, 0, err
			}
			continue
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return music.ReplicationPacket{}, res.StatusCode, 0, nil
		}
		packet, parseErr := music.ParseReplicationPacket(ctx, io.LimitReader(res.Body, 128<<20), includeCovers)
		res.Body.Close()
		if parseErr != nil {
			return music.ReplicationPacket{}, http.StatusOK, 0, parseErr
		}
		return packet, http.StatusOK, 0, nil
	}
	return music.ReplicationPacket{}, http.StatusServiceUnavailable, 5 * time.Minute, nil
}

func (s *Service) musicGETBytes(ctx context.Context, h *operationHistory, callName, endpoint string, maxBytes int64) ([]byte, int, time.Duration, error) {
	if err := s.musicRate.wait(ctx); err != nil {
		return nil, 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, ErrInvalidResponse
	}
	req.Header.Set("User-Agent", musicBrainzAgent)
	client, err := s.prepareOutgoingClient(ctx, s.MusicHTTP, "metadata_seconds", false)
	if err != nil {
		return nil, 0, 0, err
	}
	started := time.Now()
	res, err := client.Do(req)
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	if h != nil {
		_ = s.DB.RecordProviderCall(ctx, h.run.ID, callName, status, time.Since(started).Milliseconds(), "")
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, 0, ctx.Err()
		}
		return nil, 0, 0, &musicError{message: "MusicBrainz could not be reached. Check network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == 429 || res.StatusCode >= 500 {
		wait := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		return nil, res.StatusCode, wait, nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, res.StatusCode, 0, ErrInvalidResponse
	}
	return data, res.StatusCode, 0, nil
}
