package aiosource

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/notifications"
	"github.com/theo-bnts/aiosources/internal/store"
)

var ErrAlertAlreadyAvailable = errors.New("source releases are already available for this target")

func (s *Service) wakeAlerts() {
	if s.alertWake == nil {
		return
	}
	select {
	case s.alertWake <- struct{}{}:
	default:
	}
}

func (s *Service) wakeAlertsForWorks(ctx context.Context, workIDs []int64) {
	if len(workIDs) == 0 {
		return
	}
	unique := slices.Clone(workIDs)
	slices.Sort(unique)
	unique = slices.Compact(unique)
	wakeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if has, err := s.DB.WakeReleaseAlertsForWorks(wakeCtx, unique); err == nil && has {
		s.wakeAlerts()
	}
}

func (s *Service) SetPublicURL(raw string) {
	s.mu.Lock()
	s.publicURL = strings.TrimSpace(raw)
	s.mu.Unlock()
}

func (s *Service) SetPublicURLIfEmpty(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	s.mu.Lock()
	if s.publicURL == "" {
		s.publicURL = raw
	}
	s.mu.Unlock()
}

func (s *Service) SetNotificationOptions(opts notifications.Options) {
	s.mu.Lock()
	s.notificationOpts = opts
	s.mu.Unlock()
}

func (s *Service) releaseAlerts() {
	defer s.wg.Done()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		case <-s.alertWake:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(s.runReleaseAlertsTick())
	}
}

func (s *Service) runReleaseAlertsTick() time.Duration {
	poll := time.Minute
	release, ok := s.beginBackgroundTick()
	if !ok {
		return poll
	}
	defer release()
	_ = s.ProcessReleaseAlerts(s.ctx)
	if next, err := s.DB.NextReleaseAlertRetry(s.ctx); err == nil && next != nil {
		if wait := time.Until(*next); wait > 0 && wait < poll {
			poll = wait
		} else if wait <= 0 {
			poll = 5 * time.Second
		}
	}
	return max(poll, time.Second)
}

func (s *Service) NotificationChannels(ctx context.Context) ([]model.NotificationChannel, error) {
	return s.DB.NotificationChannels(ctx)
}

func (s *Service) TrustedNotificationChannels(ctx context.Context) ([]model.NotificationChannelSummary, error) {
	return s.DB.TrustedNotificationChannels(ctx)
}

func (s *Service) SaveNotificationChannel(ctx context.Context, value model.NotificationChannel) (model.NotificationChannel, error) {
	if err := value.NormalizeAndValidate(); err != nil {
		return model.NotificationChannel{}, err
	}
	saved, err := s.DB.SaveNotificationChannel(ctx, value)
	if err != nil {
		return saved, err
	}
	if saved.Enabled {
		s.wakeAlerts()
	}
	return saved, nil
}

func (s *Service) DeleteNotificationChannel(ctx context.Context, id string, revision int64) error {
	return s.DB.DeleteNotificationChannel(ctx, id, revision)
}

func (s *Service) resolveChannelSecrets(ctx context.Context, channel model.NotificationChannel) (notifications.Secrets, error) {
	var out notifications.Secrets
	if channel.SecretRef != "" {
		v, err := s.Vault.Resolve(ctx, channel.SecretRef)
		if err != nil || strings.TrimSpace(v) == "" {
			return out, errors.New("configured notification credential is unavailable")
		}
		out.Primary = v
	}
	if channel.HeaderSecretRef != "" {
		v, err := s.Vault.Resolve(ctx, channel.HeaderSecretRef)
		if err != nil || strings.TrimSpace(v) == "" {
			return out, errors.New("configured notification header credential is unavailable")
		}
		out.Header = v
	}
	return out, nil
}

func (s *Service) notificationOptions() notifications.Options {
	s.mu.Lock()
	opts := s.notificationOpts
	s.mu.Unlock()
	if opts.Client == nil {
		opts.Client = s.NotificationHTTP
	}
	return opts
}

func (s *Service) TestNotificationChannel(ctx context.Context, id string) error {
	channel, err := s.DB.NotificationChannel(ctx, id)
	if err != nil {
		return err
	}
	secrets, err := s.resolveChannelSecrets(ctx, channel)
	if err != nil {
		return err
	}
	return notifications.Send(ctx, s.notificationOptions(), channel, secrets, notifications.TestMessage())
}

func (s *Service) ReleaseAlerts(ctx context.Context, ownerType, ownerID, locale string) ([]model.ReleaseAlert, error) {
	return s.DB.ListReleaseAlerts(ctx, ownerType, ownerID, locale)
}

func (s *Service) validateAlertFilters(ctx context.Context, filters *model.ReleaseAlertFilters, grant *model.TrustedGrant) error {
	if err := filters.NormalizeAndValidate(); err != nil {
		return err
	}
	if filters.LanguageMode != "" {
		prefs := model.StreamPreferences{Language: filters.Language, Mode: filters.LanguageMode}
		if !ValidStreamPreferences(prefs) {
			return errors.New("Select a valid language filter")
		}
	}
	if grant != nil && len(filters.Providers) > 0 {
		sources, err := s.TrustedSources(ctx, *grant)
		if err != nil {
			return err
		}
		allowed := make(map[string]bool, len(sources))
		for _, source := range sources {
			allowed[source.ID] = true
		}
		for _, provider := range filters.Providers {
			if !allowed[provider] {
				return errors.New("Selected source is not permitted for your account")
			}
		}
	}
	return nil
}

func (s *Service) CreateReleaseAlert(ctx context.Context, ownerType, ownerID string, grant *model.TrustedGrant, input model.ReleaseAlertInput, locale string) (model.ReleaseAlert, error) {
	if err := s.validateAlertFilters(ctx, &input.Filters, grant); err != nil {
		return model.ReleaseAlert{}, err
	}
	work, err := s.DB.CatalogWork(ctx, input.WorkID, locale)
	if err != nil {
		return model.ReleaseAlert{}, err
	}
	if work.Type == "tv" {
		if input.Season == nil || input.Episode == nil || *input.Season < 0 || *input.Season > 999 || *input.Episode < 1 || *input.Episode > 9999 {
			return model.ReleaseAlert{}, errors.New("Select a valid season and episode for a series alert")
		}
	} else {
		if input.Season != nil || input.Episode != nil {
			return model.ReleaseAlert{}, errors.New("Season and episode apply to TV series only")
		}
	}
	rows, err := s.DB.CatalogRecords(ctx, work.ID)
	if err != nil {
		return model.ReleaseAlert{}, err
	}
	if work.Type == "tv" {
		res := Resource{Type: "series", Season: input.Season, Episode: input.Episode}
		for _, row := range rows {
			if MatchesEpisode(row.AIORecord, ParseRelease(row.Title), res) {
				return model.ReleaseAlert{}, ErrAlertAlreadyAvailable
			}
		}
	} else if len(rows) > 0 {
		return model.ReleaseAlert{}, ErrAlertAlreadyAvailable
	}
	return s.DB.CreateReleaseAlert(ctx, ownerType, ownerID, work.Type, input, locale)
}

func (s *Service) UpdateReleaseAlert(ctx context.Context, id, ownerType, ownerID string, grant *model.TrustedGrant, input model.ReleaseAlertInput, locale string) (model.ReleaseAlert, error) {
	if err := s.validateAlertFilters(ctx, &input.Filters, grant); err != nil {
		return model.ReleaseAlert{}, err
	}
	updated, err := s.DB.UpdateReleaseAlert(ctx, id, ownerType, ownerID, input, locale)
	if err != nil {
		return updated, err
	}
	s.wakeAlerts()
	return updated, nil
}

func (s *Service) DeleteReleaseAlert(ctx context.Context, id, ownerType, ownerID string, revision int64) error {
	return s.DB.DeleteReleaseAlert(ctx, id, ownerType, ownerID, revision)
}

func (s *Service) ProcessReleaseAlerts(ctx context.Context) error {
	s.alertsMu.Lock()
	defer s.alertsMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	release, locked, err := s.DB.AIOLock(ctx, "release-alerts")
	if err != nil || !locked {
		return err
	}
	defer release()

	alerts, err := s.DB.DueReleaseAlerts(ctx, time.Now(), 50)
	if err != nil || len(alerts) == 0 {
		return err
	}
	names := s.sourceNames()
	for _, alert := range alerts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.evaluateReleaseAlert(ctx, alert, names); err != nil {
			continue
		}
	}
	return nil
}

func (s *Service) evaluateReleaseAlert(ctx context.Context, alert model.ReleaseAlert, names map[string]string) error {
	channel, err := s.DB.NotificationChannel(ctx, alert.ChannelID)
	if err != nil || !channel.Enabled {
		return err
	}
	var allowedSources map[string]bool
	if alert.OwnerType == model.AlertOwnerTrusted {
		if !channel.AllowTrusted {
			return nil
		}
		users, err := s.DB.TrustedUsers(ctx)
		if err != nil {
			return err
		}
		var owner *model.TrustedUser
		for i := range users {
			if users[i].ID == alert.OwnerID {
				owner = &users[i]
				break
			}
		}
		if owner == nil || !owner.Enabled || !owner.AlertsEnabled {
			return nil
		}
		sources, err := s.TrustedSources(ctx, model.TrustedGrant{UserID: owner.ID, Generation: owner.Generation})
		if err != nil {
			return err
		}
		allowedSources = make(map[string]bool, len(sources))
		for _, source := range sources {
			allowedSources[source.ID] = true
		}
	}

	locale := "en"
	if alert.Filters.Language != "" {
		locale = alert.Filters.Language
	}
	work, err := s.DB.CatalogWork(ctx, alert.WorkID, locale)
	if err != nil {
		return err
	}
	rows, err := s.DB.CatalogRecords(ctx, alert.WorkID)
	if err != nil {
		return err
	}
	selection := model.CatalogSelection{Season: alert.Season, Episode: alert.Episode}
	resource, _, err := catalogResource(work, selection)
	if err != nil {
		return err
	}

	// Step 1: apply local metadata/release filters (episode, sources, size, language).
	prefs := model.StreamPreferences{Language: alert.Filters.Language, Mode: alert.Filters.LanguageMode}
	candidates := make([]store.CatalogRecord, 0, len(rows))
	for _, rec := range rows {
		if !MatchesEpisode(rec.AIORecord, ParseRelease(rec.Title), resource) {
			continue
		}
		if allowedSources != nil && !allowedSources[rec.Provider] {
			continue
		}
		if len(alert.Filters.Providers) > 0 && !slices.Contains(alert.Filters.Providers, rec.Provider) {
			continue
		}
		if alert.Filters.MinSize != nil && rec.Size < *alert.Filters.MinSize {
			continue
		}
		if alert.Filters.MaxSize != nil && rec.Size > *alert.Filters.MaxSize {
			continue
		}
		if prefs.Mode != "" && prefs.Mode != "all" && !AllowsLanguage(rec.AIORecord, prefs) {
			continue
		}
		candidates = append(candidates, rec)
	}

	if len(candidates) == 0 {
		// Re-check periodically or immediately when WakeReleaseAlertsForWorks fires.
		return s.DB.DeferReleaseAlert(ctx, alert.ID, alert.Revision, time.Now().Add(10*time.Minute), "", nil)
	}

	// Step 2: optional debrid cache check for matching candidates.
	cachedByOccurrence := map[string]bool{}
	if alert.Filters.CachedOnly {
		cfg, err := s.DB.AIOConfig(ctx)
		if err != nil {
			return err
		}
		hashes := make([]string, 0, len(candidates))
		records := make([]model.AIORecord, 0, len(candidates))
		for _, rec := range candidates {
			if hashRE.MatchString(rec.Hash) {
				hashes = append(hashes, rec.Hash)
			}
			records = append(records, rec.AIORecord)
		}
		slices.Sort(hashes)
		hashes = slices.Compact(hashes)
		if len(hashes) == 0 {
			return s.DB.DeferReleaseAlert(ctx, alert.ID, alert.Revision, time.Now().Add(10*time.Minute), "", nil)
		}
		resKey := "alert:" + streamResourceKey(resource, records) + ":" + fingerprint(strings.Join(hashes, ","))
		availability, err := s.availability(ctx, cfg, resKey, hashes, "automatic")
		if err != nil {
			return s.DB.DeferReleaseAlert(ctx, alert.ID, alert.Revision, time.Now().Add(5*time.Minute), "", nil)
		}
		cachedCandidates := make([]store.CatalogRecord, 0, len(candidates))
		for _, rec := range candidates {
			if !hashRE.MatchString(rec.Hash) {
				continue
			}
			cached := false
			for _, service := range model.DebridServices() {
				states, enabled := availability[service]
				if !enabled {
					continue
				}
				if obs, ok := states[rec.Hash]; ok && obs.Cached {
					if _, found := SelectFile(obs.Files, resource); found || len(obs.Files) == 0 {
						cached = true
						break
					}
					if (resource.Type == "book" || resource.Type == "game" || resource.Type == "software" || resource.Type == "music") && len(resourceDownloadFiles(obs.Files, resource)) > 0 {
						cached = true
						break
					}
				}
			}
			if cached {
				cachedByOccurrence[rec.OccurrenceID] = true
				cachedCandidates = append(cachedCandidates, rec)
			}
		}
		candidates = cachedCandidates
		if len(candidates) == 0 {
			return s.DB.DeferReleaseAlert(ctx, alert.ID, alert.Revision, time.Now().Add(5*time.Minute), "", nil)
		}
	}

	// Step 3: pick the best matching release (largest first, then newest).
	ranks := make(map[string]resultRank, len(candidates))
	for _, rec := range candidates {
		ranks[rec.OccurrenceID] = recordRank(rec.AIORecord, names[rec.Provider], alert.Filters.Language)
	}
	slices.SortStableFunc(candidates, func(a, b store.CatalogRecord) int {
		return compareResults(ranks[a.OccurrenceID], ranks[b.OccurrenceID], nil)
	})
	best := candidates[0]
	languages := []string{}
	for _, lang := range streamLanguages(best.AIORecord) {
		languages = append(languages, strings.ToUpper(lang.code))
	}
	sourceName := names[best.Provider]
	if sourceName == "" {
		sourceName = best.Provider
	}
	msg := notifications.Message{
		Event:        notifications.EventReleaseAvailable,
		WorkID:       work.ID,
		MediaType:    work.Type,
		Title:        work.Title,
		Years:        work.Years,
		Season:       alert.Season,
		Episode:      alert.Episode,
		CoverURL:     work.Cover,
		ItemURL:      s.alertItemURL(alert),
		ReleaseTitle: best.Title,
		ReleaseSize:  max(0, best.Size),
		SourceName:   sourceName,
		Languages:    languages,
		Cached:       cachedByOccurrence[best.OccurrenceID],
		ExtraMatches: len(candidates) - 1,
		Timestamp:    time.Now().UTC(),
	}

	secrets, err := s.resolveChannelSecrets(ctx, channel)
	if err == nil {
		err = notifications.Send(ctx, s.notificationOptions(), channel, secrets, msg)
	}
	logDetails := map[string]any{
		"alert_id":     alert.ID,
		"work_id":      alert.WorkID,
		"title":        work.Title,
		"media_type":   work.Type,
		"channel_id":   channel.ID,
		"channel_name": channel.Name,
		"channel_type": channel.Type,
		"owner_type":   alert.OwnerType,
	}
	if alert.Season != nil && alert.Episode != nil {
		logDetails["episode"] = fmt.Sprintf("S%02dE%02d", *alert.Season, *alert.Episode)
	}
	if err != nil {
		logDetails["error"] = err.Error()
		delay := model.RetryBackoff(alert.Attempts, time.Minute, 30*time.Minute)
		return s.DB.DeferReleaseAlert(ctx, alert.ID, alert.Revision, time.Now().Add(delay), err.Error(), logDetails)
	}
	logDetails["release_title"] = best.Title
	_, err = s.DB.CompleteReleaseAlert(ctx, alert.ID, alert.Revision, logDetails)
	return err
}

func (s *Service) alertItemURL(alert model.ReleaseAlert) string {
	s.mu.Lock()
	raw := s.publicURL
	s.mu.Unlock()
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return ""
	}
	path := fmt.Sprintf("/browse/search/%d", alert.WorkID)
	if alert.OwnerType == model.AlertOwnerTrusted {
		path = fmt.Sprintf("/trusted/browse/search/%d", alert.WorkID)
	}
	if alert.MediaType == "tv" && alert.Season != nil && alert.Episode != nil {
		path += fmt.Sprintf("?season=%d&episode=%d", *alert.Season, *alert.Episode)
	}
	return base + path
}
