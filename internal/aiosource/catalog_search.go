package aiosource

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *Service) CatalogSearch(ctx context.Context, opts model.ListOptions, filters model.CatalogSearchFilters) (model.List[model.CatalogTitle], error) {
	return s.DB.CatalogTitles(ctx, opts, Normalize(opts.Query), filters)
}

// Start a series at an episode actually represented by its local releases.
// No remote metadata request or download is needed to choose the controls.
func (s *Service) CatalogTitle(ctx context.Context, workID int64, locales ...string) (model.CatalogTitle, error) {
	title, err := s.DB.CatalogTitle(ctx, workID, locales...)
	if err != nil || title.Type != "tv" {
		return title, err
	}
	rows, err := s.DB.CatalogRecords(ctx, workID)
	if err != nil {
		return title, err
	}
	bySeason := map[int]int{}
	for _, row := range rows {
		parsed := ParseRelease(row.Title)
		if parsed.InvalidNumbering && !row.ReviewedEpisode {
			continue
		}
		seasons := parsed.Seasons
		if row.Season != nil && (row.ReviewedEpisode || !parsed.Pack || len(parsed.Seasons) < 2 || !contains(parsed.Seasons, *row.Season)) {
			seasons = []int{*row.Season}
		}
		episode := 1
		if len(parsed.Episodes) > 0 {
			episode = slices.Min(parsed.Episodes)
		}
		if row.Episode != nil {
			episode = *row.Episode
		}
		for _, season := range seasons {
			if season < 0 || season > 999 || episode < 1 || episode > 9999 {
				continue
			}
			if !MatchesEpisode(row.AIORecord, parsed, Resource{Type: "series", Season: &season, Episode: &episode}) {
				continue
			}
			if previous, ok := bySeason[season]; !ok || episode < previous {
				bySeason[season] = episode
			}
		}
	}
	for season := range bySeason {
		title.Seasons = append(title.Seasons, season)
	}
	slices.Sort(title.Seasons)
	season, episode := 1, 1
	if len(title.Seasons) > 0 {
		season = title.Seasons[0]
		if season == 0 && len(title.Seasons) > 1 {
			season = title.Seasons[1]
		}
		episode = bySeason[season]
	}
	title.Selection = model.CatalogSelection{Season: &season, Episode: &episode}
	return title, nil
}

func catalogResource(title model.CatalogTitle, selection model.CatalogSelection) (Resource, string, error) {
	typ := "movie"
	id := "tmdb:" + title.TMDB
	if title.Type == "tv" {
		typ = "series"
		if selection.Season == nil || selection.Episode == nil || *selection.Season < 0 || *selection.Season > 999 || *selection.Episode < 1 || *selection.Episode > 9999 {
			return Resource{}, "", model.ErrInvalid
		}
		id += ":" + strconv.Itoa(*selection.Season) + ":" + strconv.Itoa(*selection.Episode)
	} else if selection.Season != nil || selection.Episode != nil {
		return Resource{}, "", model.ErrInvalid
	}
	r, err := ParseID(id, typ)
	if err != nil {
		return r, "", model.ErrInvalid
	}
	return r, id, nil
}

func (s *Service) CatalogReleases(ctx context.Context, workID int64, selection model.CatalogSelection, opts model.ListOptions, options ...model.CatalogReleaseFilters) (model.List[model.CatalogRelease], error) {
	limit := opts.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := max(0, opts.Offset)
	result := model.List[model.CatalogRelease]{Items: []model.CatalogRelease{}, Limit: limit, Offset: offset}
	if opts.Sort != "" && opts.Sort != "recent" && opts.Sort != "size_desc" && opts.Sort != "source" {
		return result, model.ErrInvalid
	}
	title, err := s.DB.CatalogTitle(ctx, workID)
	if err != nil {
		return result, err
	}
	r, _, err := catalogResource(title, selection)
	if err != nil {
		return result, err
	}
	rows, err := s.DB.CatalogRecords(ctx, workID)
	if err != nil {
		return result, err
	}
	rows = slices.DeleteFunc(rows, func(rec store.CatalogRecord) bool { return !MatchesEpisode(rec.AIORecord, ParseRelease(rec.Title), r) })
	filters := model.CatalogReleaseFilters{}
	if len(options) > 0 {
		filters = options[0]
	}
	prefs := filters.StreamPreferences
	if !ValidStreamPreferences(prefs) {
		return result, model.ErrInvalid
	}
	rows = slices.DeleteFunc(rows, func(rec store.CatalogRecord) bool { return !AllowsLanguage(rec.AIORecord, prefs) })
	if opts.ProviderID != "" {
		rows = slices.DeleteFunc(rows, func(rec store.CatalogRecord) bool { return rec.Provider != opts.ProviderID })
	}
	if opts.Sort == "" || opts.Sort == "size_desc" {
		slices.SortStableFunc(rows, func(a, b store.CatalogRecord) int {
			if a.Size > b.Size {
				return -1
			}
			if a.Size < b.Size {
				return 1
			}
			return 0
		})
	}
	if opts.Sort == "source" {
		names := s.sourceNames()
		name := func(id string) string {
			if names[id] != "" {
				return strings.ToLower(names[id])
			}
			return strings.ToLower(id)
		}
		slices.SortStableFunc(rows, func(a, b store.CatalogRecord) int {
			return strings.Compare(name(a.Provider), name(b.Provider))
		})
	}
	result.Total = int64(len(rows))
	start := min(offset, len(rows))
	page := rows[start : start+min(limit, len(rows)-start)]
	if filters.CachedOnly {
		// Evaluate availability before pagination: an uncached first page must
		// not hide cached releases further down the result set or inflate totals.
		page = rows
	}
	if len(page) == 0 {
		return result, nil
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return result, err
	}
	hashes := []string{}
	records := []model.AIORecord{}
	for _, row := range page {
		if hashRE.MatchString(row.Hash) {
			hashes = append(hashes, row.Hash)
		}
		records = append(records, row.AIORecord)
	}
	slices.Sort(hashes)
	availability, err := s.availability(ctx, c, "catalog:"+streamResourceKey(r, records)+":"+fingerprint(strings.Join(hashes, ",")), hashes, "manual")
	if err != nil {
		return result, err
	}
	for _, row := range page {
		languages := []string{}
		for _, language := range streamLanguages(row.AIORecord) {
			languages = append(languages, strings.ToUpper(language.code))
		}
		item := model.CatalogRelease{Fingerprint: row.Fingerprint, OccurrenceID: row.OccurrenceID, Provider: row.Provider, Title: row.Title, Size: max(0, row.Size), PublishedAt: row.PublishedAt, Languages: languages, Services: []model.CatalogAvailability{}}
		tracker, configured := c.Trackers[row.Provider]
		canAdd := configured && (!tracker.Private || tracker.AnnounceSecret != "")
		for _, service := range []string{"alldebrid", "torbox"} {
			states, enabled := availability[service]
			if !enabled {
				continue
			}
			state := model.CatalogAvailability{Service: service, State: "unknown", CanAdd: canAdd}
			if !hashRE.MatchString(row.Hash) {
				state.State = "invalid"
				state.CanAdd = false
			} else if observed, known := states[row.Hash]; known {
				state.State = "uncached"
				if observed.Cached {
					file, found := SelectFile(observed.Files, r)
					state.State = "cached"
					if found {
						state.Filename = file.Path
						state.Size = file.Size
					} else if len(observed.Files) > 0 {
						state.State = "unavailable"
						state.CanAdd = false
					} else if r.Episode != nil {
						state.State = "unknown"
					}
				}
			}
			item.Services = append(item.Services, state)
		}
		if !filters.CachedOnly || slices.ContainsFunc(item.Services, func(service model.CatalogAvailability) bool { return service.State == "cached" }) {
			result.Items = append(result.Items, item)
		}
	}
	if filters.CachedOnly {
		result.Total = int64(len(result.Items))
		start = min(offset, len(result.Items))
		result.Items = result.Items[start : start+min(limit, len(result.Items)-start)]
	}
	return result, nil
}

// A download is an explicit administrator action. Readiness checks use the
// providers' cached-only safeguards and never start a new uncached download.
func (s *Service) CatalogDownload(ctx context.Context, workID int64, input model.CatalogDownloadInput) (model.CatalogDownload, error) {
	return s.catalogDownload(ctx, workID, input, "", "")
}

func (s *Service) catalogDownload(ctx context.Context, workID int64, input model.CatalogDownloadInput, operationID, expectedAccount string) (result model.CatalogDownload, downloadErr error) {
	if (input.Service != "alldebrid" && input.Service != "torbox") || !store.ValidOccurrenceID(input.OccurrenceID) || input.Fingerprint == "" || len(input.Fingerprint) > 128 {
		return result, model.ErrInvalid
	}
	if input.PreparationID != "" {
		id, err := strconv.ParseInt(input.PreparationID, 10, 64)
		if input.AllowUncached || err != nil || id < 0 || (input.Service == "alldebrid" && id == 0) || strconv.FormatInt(id, 10) != input.PreparationID {
			return result, model.ErrInvalid
		}
	}
	select {
	case s.downloadSlots <- struct{}{}:
		defer func() { <-s.downloadSlots }()
	default:
		return result, model.ErrBusy
	}
	title, err := s.DB.CatalogTitle(ctx, workID)
	if err != nil {
		return result, err
	}
	r, id, err := catalogResource(title, input.CatalogSelection)
	if err != nil {
		return result, err
	}
	rows, err := s.DB.CatalogRecords(ctx, workID)
	if err != nil {
		return result, err
	}
	var selected *store.CatalogRecord
	for i := range rows {
		if rows[i].OccurrenceID == input.OccurrenceID {
			selected = &rows[i]
			break
		}
	}
	if selected == nil || !hashRE.MatchString(selected.Hash) || !MatchesEpisode(selected.AIORecord, ParseRelease(selected.Title), r) {
		return result, model.ErrNotFound
	}
	if input.Fingerprint != selected.Fingerprint {
		return result, model.ErrConflict
	}
	lock := "catalog-download:" + input.Service + ":" + selected.OccurrenceID
	release, locked, err := s.DB.AIOLock(ctx, lock)
	if err != nil {
		return result, err
	}
	if !locked {
		return result, model.ErrBusy
	}
	defer release()
	var h *operationHistory
	if operationID != "" {
		run, e := s.DB.ResumeDownloadOperation(ctx, operationID)
		err = e
		h = &operationHistory{service: s, run: run}
	} else {
		h, err = s.beginOperation(ctx, "download", input.Service, "manual", r.Type+":"+id, lock)
	}
	if err != nil {
		return result, err
	}
	result.OperationID = h.run.ID
	result.Service = input.Service
	defer func() {
		if errors.Is(downloadErr, ErrPending) {
			h.run.Processed = 1
			h.run.Phase = "waiting"
			h.finish(ctx, nil)
		} else {
			h.finish(ctx, downloadErr)
		}
	}()
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return result, err
	}
	secret := config.TorBoxSecret
	if input.Service == "alldebrid" {
		secret = config.AllDebridSecret
	}
	key, err := s.key(ctx, secret)
	if err != nil {
		return result, err
	}
	if expectedAccount != "" && fingerprint(key) != expectedAccount {
		return result, model.ErrConflict
	}
	continuation := model.OperationDownload{WorkID: workID, Input: input, Account: fingerprint(key)}
	if err = s.DB.SaveOperationDownload(ctx, result.OperationID, continuation); err != nil {
		return result, err
	}
	h.checkpoint(ctx, "download")
	result.URL, downloadErr = s.playbackWithPolicy(withProviderHistory(ctx, h), input.Service, selected.Provider, selected.SourceID, r.Type, id, nil, playbackPolicy{CachedOnly: !input.AllowUncached, Fingerprint: input.Fingerprint, Account: fingerprint(key), PreparationID: input.PreparationID, PreparedID: &result.PreparationID})
	if result.PreparationID != "" {
		continuation.Input.PreparationID = result.PreparationID
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = s.DB.SaveOperationDownload(saveCtx, result.OperationID, continuation)
		cancel()
		if err != nil {
			return result, err
		}
	}
	result.Status = "ready"
	if errors.Is(downloadErr, ErrPending) {
		result.Status = "pending"
		return result, downloadErr
	}
	if downloadErr == nil {
		h.run.Processed = 1
		h.run.Phase = "ready"
	}
	result.PreparationID = ""
	return result, downloadErr
}

// Following a saved preparation never starts an uncached download. It also
// cannot reuse an object ID after the administrator changes provider accounts.
func (s *Service) CheckOperationDownload(ctx context.Context, id string) (result model.CatalogDownload, checkErr error) {
	defer func() {
		if errors.Is(checkErr, model.ErrNotFound) || errors.Is(checkErr, model.ErrConflict) {
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.DB.UnavailableOperationDownload(final, id)
		}
	}()
	saved, err := s.DB.OperationDownload(ctx, id)
	if err != nil {
		return model.CatalogDownload{}, err
	}
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return model.CatalogDownload{}, err
	}
	secret := config.TorBoxSecret
	if saved.Input.Service == "alldebrid" {
		secret = config.AllDebridSecret
	}
	key, err := s.key(ctx, secret)
	if err != nil {
		return model.CatalogDownload{}, err
	}
	if key == "" || fingerprint(key) != saved.Account {
		return model.CatalogDownload{}, model.ErrConflict
	}
	saved.Input.AllowUncached = false
	return s.catalogDownload(ctx, saved.WorkID, saved.Input, id, saved.Account)
}
