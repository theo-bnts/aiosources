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
	if title.Type == "book" {
		if selection.Season != nil || selection.Episode != nil || len(selection.File) > 4096 {
			return Resource{}, "", model.ErrInvalid
		}
		id := "openlibrary:" + title.OpenLibrary
		r, err := ParseID(id, "book")
		r.File = selection.File
		return r, id, err
	}
	if selection.File != "" {
		return Resource{}, "", model.ErrInvalid
	}
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
	return s.catalogReleases(ctx, workID, selection, opts, nil, options...)
}

func (s *Service) catalogReleases(ctx context.Context, workID int64, selection model.CatalogSelection, opts model.ListOptions, visitor *PublicInstallation, options ...model.CatalogReleaseFilters) (model.List[model.CatalogRelease], error) {
	limit := opts.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := max(0, opts.Offset)
	result := model.List[model.CatalogRelease]{Items: []model.CatalogRelease{}, Limit: limit, Offset: offset}
	var access model.PublicAccess
	if visitor != nil {
		var err error
		access, err = s.PublicSearchAccess(ctx, visitor)
		if err != nil {
			return result, err
		}
		if !access.ExposeSources && (opts.ProviderID != "" || opts.Sort == "source") {
			return result, model.ErrInvalid
		}
	}
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
	if visitor != nil {
		if !AllowsInstallation(visitor.Preferences, "", "", r.Type) {
			return result, nil
		}
		rows = slices.DeleteFunc(rows, func(rec store.CatalogRecord) bool {
			return !hashRE.MatchString(rec.Hash) || !AllowsInstallation(visitor.Preferences, rec.Provider, "", r.Type)
		})
	}
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
	rules := prefs.Sorting
	if len(rules) == 0 {
		switch opts.Sort {
		case "recent":
			rules = []model.ResultSort{{Field: "published", Direction: "desc"}}
		case "source":
			rules = []model.ResultSort{{Field: "source", Direction: "asc"}}
		}
	}
	if visitor != nil && !access.ExposeSources && slices.ContainsFunc(rules, func(rule model.ResultSort) bool { return rule.Field == "source" }) {
		return result, model.ErrInvalid
	}
	names := s.sourceNames()
	ranks := make(map[string]resultRank, len(rows))
	for _, row := range rows {
		ranks[row.OccurrenceID] = recordRank(row.AIORecord, names[row.Provider], sortingLanguage(prefs))
	}
	slices.SortStableFunc(rows, func(a, b store.CatalogRecord) int {
		return compareResults(ranks[a.OccurrenceID], ranks[b.OccurrenceID], rules)
	})
	allResults := filters.CachedOnly || visitor != nil || slices.ContainsFunc(rules, func(rule model.ResultSort) bool { return rule.Field == "cached" })
	result.Total = int64(len(rows))
	start := min(offset, len(rows))
	page := rows[start : start+min(limit, len(rows)-start)]
	if allResults {
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
	if prefs.ResponseSeconds > 0 {
		c.ResponseSeconds = prefs.ResponseSeconds
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
	resource := "catalog:" + streamResourceKey(r, records) + ":" + fingerprint(strings.Join(hashes, ","))
	var availability map[string]map[string]model.AIOAvailability
	var announces map[string]string
	if visitor == nil {
		availability, err = s.availability(ctx, c, resource, hashes, "manual")
	} else {
		filtered := *visitor
		filtered.Services = map[string]string{}
		for service, key := range visitor.Services {
			if AllowsInstallation(visitor.Preferences, "", service, r.Type) {
				filtered.Services[service] = key
			}
		}
		availability, err = s.publicAvailability(ctx, c, resource, hashes, &filtered)
		clear(filtered.Services)
		if err == nil {
			announces, err = s.visitorAnnounces(ctx, c, visitor)
		}
	}
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
		if visitor != nil {
			canAdd = (configured && !tracker.Private) || announces[row.Provider] != ""
		}
		for _, service := range model.DebridServices() {
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
					if r.Type == "book" {
						state.FileCount = len(bookFiles(observed.Files))
					}
					if found {
						state.Filename = file.Path
						state.Size = file.Size
					} else if r.Type == "book" && state.FileCount > 1 {
						// The recipient chooses a format or audio track before downloading.
					} else if len(observed.Files) > 0 {
						state.State = "unavailable"
						state.CanAdd = false
					} else if r.Episode != nil || (visitor != nil && (!configured || tracker.Private)) {
						state.State = "unknown"
					}
				}
			}
			if visitor != nil && (state.State == "invalid" || state.State == "unavailable" || (state.State != "cached" && (!canAdd || visitor.Preferences.CachedOnly))) {
				continue
			}
			item.Services = append(item.Services, state)
		}
		if visitor != nil {
			if len(item.Services) == 0 {
				continue
			}
			item.DownloadToken, err = s.sealPublicCatalogSource(workID, row)
			if err != nil {
				return result, err
			}
			item.Fingerprint = ""
			if access.ExposeSources {
				item.ProviderName = names[row.Provider]
			}
			if !access.ExposeSources {
				item.Provider = ""
			}
		}
		rank := ranks[row.OccurrenceID]
		rank.cached = 0
		for _, service := range item.Services {
			if service.State == "cached" {
				rank.cached = 2
				break
			}
			if service.State == "unknown" {
				rank.cached = 1
			}
		}
		if len(item.Services) == 0 {
			rank.cached = 1
		}
		ranks[row.OccurrenceID] = rank
		if !filters.CachedOnly || slices.ContainsFunc(item.Services, func(service model.CatalogAvailability) bool { return service.State == "cached" }) {
			result.Items = append(result.Items, item)
		}
	}
	slices.SortStableFunc(result.Items, func(a, b model.CatalogRelease) int {
		return compareResults(ranks[a.OccurrenceID], ranks[b.OccurrenceID], rules)
	})
	if allResults {
		result.Total = int64(len(result.Items))
		start = min(offset, len(result.Items))
		result.Items = result.Items[start : start+min(limit, len(result.Items)-start)]
	}
	return result, nil
}

// A download is an explicit administrator action. Readiness checks use the
// providers' cached-only safeguards and never start a new uncached download.
func (s *Service) CatalogDownload(ctx context.Context, workID int64, input model.CatalogDownloadInput) (model.CatalogDownload, error) {
	if input.DownloadToken != "" {
		return model.CatalogDownload{}, model.ErrInvalid
	}
	return s.catalogDownload(ctx, workID, input, "", "", nil)
}

func (s *Service) catalogDownload(ctx context.Context, workID int64, input model.CatalogDownloadInput, operationID, expectedAccount string, visitor *PublicInstallation) (result model.CatalogDownload, downloadErr error) {
	if !model.IsDebridService(input.Service) || !store.ValidOccurrenceID(input.OccurrenceID) || input.Fingerprint == "" || len(input.Fingerprint) > 128 {
		return result, model.ErrInvalid
	}
	if input.PreparationID != "" {
		id, err := strconv.ParseInt(input.PreparationID, 10, 64)
		if input.AllowUncached || ((input.Service == "alldebrid" || input.Service == "torbox") && (err != nil || id < 0 || (input.Service == "alldebrid" && id == 0) || strconv.FormatInt(id, 10) != input.PreparationID)) || len(input.PreparationID) > 1024 {
			return result, model.ErrInvalid
		}
	}
	if err := s.acquireCapacity(ctx, &s.downloadSlots, "download_jobs", false); err != nil {
		return result, err
	}
	defer s.downloadSlots.release()
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
	trigger := "manual"
	if visitor != nil {
		if _, err = s.PublicSearchAccess(ctx, visitor); err != nil {
			return result, err
		}
		if visitor.Services[input.Service] == "" || !AllowsInstallation(visitor.Preferences, selected.Provider, input.Service, r.Type) {
			return result, ErrAuth
		}
		lock = "public-" + lock + ":" + fingerprint(visitor.Services[input.Service])
		trigger = "public"
	}
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
		h, err = s.beginOperation(ctx, "download", input.Service, trigger, r.Type+":"+id, lock)
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
	var key string
	if visitor != nil {
		key = visitor.Services[input.Service]
	} else {
		secret := config.DebridSecret(input.Service)
		key, err = s.key(ctx, secret)
		if err != nil {
			return result, err
		}
	}
	if expectedAccount != "" && fingerprint(key) != expectedAccount {
		return result, model.ErrConflict
	}
	continuation := model.OperationDownload{WorkID: workID, Input: input, Account: fingerprint(key)}
	if visitor == nil {
		if err = s.DB.SaveOperationDownload(ctx, result.OperationID, continuation); err != nil {
			return result, err
		}
	}
	h.checkpoint(ctx, "download")
	policy := playbackPolicy{SelectionFile: input.File, CachedOnly: !input.AllowUncached, Fingerprint: input.Fingerprint, Account: fingerprint(key), PreparationID: input.PreparationID, PreparedID: &result.PreparationID}
	if visitor != nil {
		policy.Preferences = &visitor.Preferences
	}
	if title.Type == "book" && input.File == "" && input.PreparationID == "" && !input.AllowUncached {
		observed, e := s.DB.SharedAvailability(ctx, input.Service, []string{selected.Hash}, time.Now().Add(-time.Duration(config.CacheMinutes)*time.Minute))
		if e != nil {
			return result, e
		}
		if len(observed) == 1 && observed[0].Cached {
			if files := bookFiles(observed[0].Files); len(files) > 1 {
				result.Files = files
				result.Status = "pending"
				return result, ErrPending
			}
		}
	}
	result.URL, downloadErr = s.playbackWithPolicy(withProviderHistory(ctx, h), input.Service, selected.Provider, selected.SourceID, r.Type, id, visitor, policy)
	if visitor == nil && result.PreparationID != "" {
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
		var choice *bookFileChoice
		if errors.As(downloadErr, &choice) {
			result.Files = choice.Files
		}
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
func (s *Service) CheckOperationDownload(ctx context.Context, id string, selectedFiles ...string) (result model.CatalogDownload, checkErr error) {
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
	secret := config.DebridSecret(saved.Input.Service)
	key, err := s.key(ctx, secret)
	if err != nil {
		return model.CatalogDownload{}, err
	}
	if key == "" || fingerprint(key) != saved.Account {
		return model.CatalogDownload{}, model.ErrConflict
	}
	if len(selectedFiles) > 0 {
		if len(selectedFiles[0]) > 4096 {
			return model.CatalogDownload{}, model.ErrInvalid
		}
		saved.Input.File = selectedFiles[0]
	}
	saved.Input.AllowUncached = false
	return s.catalogDownload(ctx, saved.WorkID, saved.Input, id, saved.Account, nil)
}
