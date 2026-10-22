package aiosource

import (
	"context"
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Stream struct {
	Name     string   `json:"name"`
	Title    string   `json:"description"`
	InfoHash string   `json:"infoHash,omitempty"`
	URL      string   `json:"url,omitempty"`
	Sources  []string `json:"sources,omitempty"`
	Hints    Behavior `json:"behaviorHints"`
}
type Behavior struct {
	BingeGroup  string  `json:"bingeGroup"`
	Size        int64   `json:"videoSize,omitempty"`
	Filename    string  `json:"filename"`
	NotWebReady bool    `json:"notWebReady,omitempty"`
	FolderName  string  `json:"folderName,omitempty"`
	FolderSize  int64   `json:"folderSize,omitempty"`
	Indexer     string  `json:"indexer,omitempty"`
	Service     string  `json:"service,omitempty"`
	Cached      *bool   `json:"cached,omitempty"`
	PublishDate string  `json:"publishDate,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
}

func Manifest() map[string]any {
	return map[string]any{"id": "org.aiosource.v2", "version": "2.1.0", "name": "AIOSources", "description": "Local sources with verified IMDb mapping and optional AllDebrid / TorBox playback", "resources": []any{map[string]any{"name": "stream", "types": []string{"movie", "series", "anime"}, "idPrefixes": []string{"tt", "imdb:", "tmdb:", "tvdb:", "mal:"}}}, "types": []string{"movie", "series", "anime"}, "catalogs": []any{}, "behaviorHints": map[string]bool{"configurable": true}}
}
func (s *Service) magnet(ctx context.Context, rec model.AIORecord, c model.AIOConfig) (string, []string, error) {
	q := url.Values{"dn": {rec.Title}}
	sources := []string{}
	t := c.Trackers[rec.Provider]
	announce, err := s.trackerAnnounce(ctx, t)
	if err != nil {
		return "", nil, err
	}
	if announce != "" {
		q.Set("tr", announce)
		sources = append(sources, "tracker:"+announce)
	}
	// Retain v1's literal exact topic first, as in the provider's documented
	// magnet form. Escape display/announce values before the outer API form.
	query := strings.ReplaceAll(q.Encode(), "+", "%20")
	return "magnet:?xt=urn:btih:" + strings.ToLower(rec.Hash) + "&" + query, sources, nil
}
func quality(title string) string {
	upper := strings.ToUpper(title)
	out := []string{}
	for _, v := range []string{"2160P", "1080P", "720P", "480P", "WEB-DL", "WEBRIP", "BLURAY", "REMUX", "HEVC", "X265", "H264", "X264", "AV1", "HDR", "DOLBY VISION"} {
		if strings.Contains(upper, v) {
			out = append(out, v)
		}
	}
	return strings.Join(out, " ")
}
func baseStream(rec model.AIORecord, r Resource, sourceName string) Stream {
	if strings.TrimSpace(sourceName) == "" {
		sourceName = rec.Provider
	}
	q := quality(rec.Title)
	languages := streamLanguages(rec)
	hints := Behavior{BingeGroup: streamBingeGroup(rec, r, q, languages), Size: max(0, rec.Size), Filename: rec.Title, NotWebReady: true, Indexer: sourceName, Duration: rec.DurationSeconds}
	if rec.PublishedAt != nil {
		hints.PublishDate = rec.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	return formatStream(Stream{InfoHash: rec.Hash, Hints: hints}, rec)
}
func (s *Service) Streams(ctx context.Context, base, token, mediaType, id string) (streams []Stream, streamErr error) {
	return s.streams(ctx, base, token, mediaType, id, nil)
}
func (s *Service) streams(ctx context.Context, base, token, mediaType, id string, visitor *PublicInstallation) (streams []Stream, streamErr error) {
	history := newStremioHistory(mediaType, id)
	if visitor != nil {
		history.Origin = "public"
	}
	names := map[string]string{}
	defer func() {
		history.DurationMS = time.Since(history.CreatedAt).Milliseconds()
		if streamErr != nil {
			history.Outcome = "failed"
			history.Results = nil
			if errors.Is(streamErr, context.Canceled) {
				history.Outcome = "cancelled"
			}
		} else if history.Outcome != "invalid" {
			history.Outcome = "empty"
			if len(streams) > 0 {
				history.Outcome = "results"
			}
			history.Returned = len(streams)
		}
		if streamErr == nil {
			sort.SliceStable(streams, func(i, j int) bool { return streams[i].Hints.Size > streams[j].Hints.Size })
			sort.SliceStable(history.Results, func(i, j int) bool { return history.Results[i].Size > history.Results[j].Size })
			if len(history.Results) > 1000 {
				history.Results = history.Results[:1000]
				history.Truncated = true
			}
		}
		s.saveStremioHistory(history)
	}()
	recordResult := func(rec model.AIORecord, item Stream, service, cache string) {
		history.Results = append(history.Results, model.StremioResult{Name: item.Name, Provider: rec.Provider, ProviderName: names[rec.Provider], SourceID: rec.SourceID, Title: item.Title, Service: service, Cache: cache, Filename: item.Hints.Filename, Size: max(0, item.Hints.Size)})
	}
	r, err := ParseID(id, mediaType)
	if err != nil {
		return []Stream{}, nil
	}
	rows, err := s.DB.AIOStreams(ctx, r.Kind, r.ID, mediaType)
	if err != nil {
		return nil, err
	}
	var preferences model.StreamPreferences
	if visitor != nil {
		preferences = visitor.Preferences
	} else {
		preferences, err = s.PrivatePreferences(ctx, token)
		if err != nil {
			return nil, err
		}
	}
	if !AllowsInstallation(preferences, "", "", mediaType) {
		return []Stream{}, nil
	}
	history.Matched = len(rows)
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	announces, err := s.visitorAnnounces(ctx, c, visitor)
	if err != nil {
		return nil, err
	}
	eligible := []model.AIORecord{}
	hashes := []string{}
	for _, rec := range rows {
		if !AllowsInstallation(preferences, rec.Provider, "", mediaType) {
			continue
		}
		if !hashRE.MatchString(rec.Hash) {
			history.InvalidHash++
			continue
		}
		if !MatchesEpisode(rec, ParseRelease(rec.Title), r) {
			history.WrongEpisode++
			continue
		}
		if !AllowsLanguage(rec, preferences) {
			history.LanguageExcluded++
			continue
		}
		eligible = append(eligible, rec)
		hashes = append(hashes, rec.Hash)
	}
	history.Eligible = len(eligible)
	if len(eligible) == 0 {
		return []Stream{}, nil
	}
	var availability map[string]map[string]model.AIOAvailability
	// Filter before availability checks, so an unselected service is never
	// contacted. The stored configuration and visitor credential remain intact.
	if !AllowsInstallation(preferences, "", "alldebrid", mediaType) {
		c.AllDebridSecret = ""
	}
	if !AllowsInstallation(preferences, "", "torbox", mediaType) {
		c.TorBoxSecret = ""
	}
	if visitor == nil {
		availability, err = s.Availability(ctx, c, streamResourceKey(r, rows), hashes)
	} else {
		filtered := *visitor
		filtered.Services = map[string]string{}
		for service, key := range visitor.Services {
			if AllowsInstallation(preferences, "", service, mediaType) {
				filtered.Services[service] = key
			}
		}
		availability, err = s.publicAvailability(ctx, c, streamResourceKey(r, rows), hashes, &filtered)
		clear(filtered.Services)
	}
	if err != nil {
		return nil, err
	}
	// Resolve names once for the whole response, including remote catalogs.
	// Presentation must not prevent playback if a definition becomes unreadable.
	names = s.sourceNames()
	exposeNames := true
	if visitor != nil {
		access, e := s.DB.PublicAccess(ctx)
		if e != nil {
			return nil, e
		}
		exposeNames = access.ExposeSources
	}
	out := []Stream{}
	for _, rec := range eligible {
		displayRecord := rec
		sourceName := names[rec.Provider]
		if !exposeNames {
			displayRecord.Provider = ""
			sourceName = ""
		}
		stream := baseStream(displayRecord, r, sourceName)
		if visitor == nil && len(availability) == 0 {
			if !AllowsInstallation(preferences, "", "direct", mediaType) || preferences.CachedOnly {
				continue
			}
			if tracker := c.Trackers[rec.Provider]; tracker.Private && tracker.AnnounceSecret == "" {
				history.PrivateUnavailable++
				continue
			}
			magnet, sources, err := s.magnet(ctx, rec, c)
			if err != nil {
				return nil, err
			}
			if len(sources) > 0 {
				stream.URL = magnet
				stream.Sources = sources
			}
			out = append(out, stream)
			recordResult(rec, stream, "direct", "direct")
			continue
		}
		for _, service := range []string{"alldebrid", "torbox"} {
			states, enabled := availability[service]
			if !enabled {
				continue
			}
			observed, known := states[rec.Hash]
			if preferences.CachedOnly && (!known || !observed.Cached) {
				continue
			}
			tracker, configured := c.Trackers[rec.Provider]
			if !configured {
				tracker.Private = true
			}
			canDownload := tracker.AnnounceSecret != ""
			if visitor != nil {
				canDownload = announces[rec.Provider] != ""
			}
			cachedOnly := tracker.Private && !canDownload
			if cachedOnly && (!known || !observed.Cached) {
				history.PrivateUnavailable++
				continue
			}
			selected, hasFile := SelectFile(observed.Files, r)
			if visitor != nil && cachedOnly && !hasFile {
				history.FileUnavailable++
				continue
			}
			if known && observed.Cached && len(observed.Files) > 0 && !hasFile {
				history.FileUnavailable++
				continue
			}
			if observed.Cached && !hasFile && (r.Episode != nil || (visitor != nil && tracker.Private)) {
				if cachedOnly {
					history.FileUnavailable++
					continue
				}
				// Global readiness cannot certify the requested video when listing files
				// failed. Retain playback but present the cache as unverified.
				known = false
			}
			segments := []string{"stremio", token, "play", service, rec.Provider, rec.SourceID, mediaType, id}
			if visitor != nil {
				locator, e := s.sealPublicSource(rec.Provider, rec.SourceID)
				if e != nil {
					return nil, e
				}
				segments = []string{"public", "stremio", token, "play", service, locator, mediaType, id}
				if tracker.Private {
					stream.InfoHash = ""
				}
			}
			for i := range segments {
				segments[i] = url.PathEscape(segments[i])
			}
			var cached *bool
			if known {
				value := observed.Cached
				cached = &value
			}
			var file *model.AIOFile
			if hasFile {
				file = &selected
			}
			item := playbackStream(stream, rec, service, cached, file, strings.TrimRight(base, "/")+"/"+strings.Join(segments, "/"))
			out = append(out, item)
			cache := "unknown"
			if known {
				cache = "download"
				if observed.Cached {
					cache = "cached"
				}
			}
			recordResult(rec, item, service, cache)
		}
	}
	return out, nil
}
func (s *Service) Playback(ctx context.Context, service, provider, sourceID, mediaType, id string) (string, error) {
	return s.playback(ctx, service, provider, sourceID, mediaType, id, nil)
}
func (s *Service) PlaybackWithPreferences(ctx context.Context, service, provider, sourceID, mediaType, id string, p model.StreamPreferences) (string, error) {
	if !model.ValidPlaybackPreferences(p) || !AllowsInstallation(p, provider, service, mediaType) {
		return "", model.ErrInvalid
	}
	return s.playbackWithPolicy(ctx, service, provider, sourceID, mediaType, id, nil, playbackPolicy{CachedOnly: p.CachedOnly, Preferences: &p})
}
func (s *Service) PublicPlayback(ctx context.Context, service, provider, sourceID, mediaType, id string, visitor *PublicInstallation) (string, error) {
	if visitor == nil || !validPublicKeys(visitor.Services) || visitor.Services[service] == "" {
		return "", ErrAuth
	}
	if !AllowsInstallation(visitor.Preferences, provider, service, mediaType) {
		return "", model.ErrInvalid
	}
	return s.playbackWithPolicy(ctx, service, provider, sourceID, mediaType, id, visitor, playbackPolicy{CachedOnly: visitor.Preferences.CachedOnly, Preferences: &visitor.Preferences})
}
func (s *Service) playback(ctx context.Context, service, provider, sourceID, mediaType, id string, visitor *PublicInstallation) (string, error) {
	return s.playbackWithPolicy(ctx, service, provider, sourceID, mediaType, id, visitor, playbackPolicy{})
}

type playbackPolicy struct {
	Account       string
	CachedOnly    bool
	Preferences   *model.StreamPreferences
	Fingerprint   string
	PreparationID string
	PreparedID    *string
}

func (s *Service) playbackWithPolicy(ctx context.Context, service, provider, sourceID, mediaType, id string, visitor *PublicInstallation, policy playbackPolicy) (string, error) {
	if service != "alldebrid" && service != "torbox" {
		return "", model.ErrInvalid
	}
	r, err := ParseID(id, mediaType)
	if err != nil {
		return "", model.ErrInvalid
	}
	budget, err := s.DB.ConnectionTimeout(ctx, "stremio")
	if err != nil {
		return "", err
	}
	life, cancel := context.WithTimeout(ctx, max(time.Second, budget-10*time.Second))
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	rec, err := s.DB.AIOTorrent(life, provider, sourceID)
	if err != nil {
		return "", err
	}
	if policy.Fingerprint != "" && rec.Fingerprint != policy.Fingerprint {
		return "", model.ErrConflict
	}
	if !hashRE.MatchString(rec.Hash) || !MatchesEpisode(rec, ParseRelease(rec.Title), r) {
		return "", model.ErrInvalid
	}
	if policy.Preferences != nil && !AllowsLanguage(rec, *policy.Preferences) {
		return "", model.ErrInvalid
	}
	// Token holders can only play a torrent still published for this resource.
	rows, err := s.DB.AIOStreams(life, r.Kind, r.ID, mediaType)
	if err != nil {
		return "", err
	}
	allowed := false
	for _, row := range rows {
		if row.Provider == provider && row.SourceID == sourceID {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", model.ErrNotFound
	}
	c, err := s.DB.AIOConfig(life)
	if err != nil {
		return "", err
	}
	cachedOnly := false
	var key, magnet string
	var observed model.AIOAvailability
	if visitor == nil {
		name := c.TorBoxSecret
		if service == "alldebrid" {
			name = c.AllDebridSecret
		}
		if name == "" {
			return "", model.ErrInvalid
		}
		key, err = s.key(life, name)
		if err != nil {
			return "", err
		}
		if policy.Account != "" && fingerprint(key) != policy.Account {
			return "", model.ErrConflict
		}
		tracker, configured := c.Trackers[rec.Provider]
		cachedOnly = policy.CachedOnly || ((!configured || tracker.Private) && tracker.AnnounceSecret == "")
		magnetConfig := c
		if cachedOnly {
			magnetConfig.Trackers = nil
		}
		magnet, _, err = s.magnet(life, rec, magnetConfig)
		if err != nil {
			return "", err
		}
	} else {
		key = visitor.Services[service]
		if key == "" {
			return "", ErrAuth
		}
		tracker, configured := c.Trackers[rec.Provider]
		private := !configured || tracker.Private
		announces, e := s.visitorAnnounces(life, c, visitor)
		if e != nil {
			return "", e
		}
		announce := ""
		if private {
			items, e := s.DB.SharedAvailability(life, service, []string{rec.Hash}, time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
			if e != nil {
				return "", e
			}
			if len(items) == 1 && items[0].Cached {
				observed = items[0]
				_, cachedOnly = SelectFile(observed.Files, r)
				if !cachedOnly && len(observed.Files) > 0 {
					return "", ErrPending
				}
			}
			if !cachedOnly {
				announce = announces[rec.Provider]
			}
			if !cachedOnly && announce == "" {
				return "", ErrPending
			}
		}
		magnet = publicMagnet(rec, announce)
		if policy.CachedOnly {
			cachedOnly = true
			magnet = publicMagnet(rec, "")
		}
	}
	var link string
	generation, err := s.DB.AIOCacheGeneration(life)
	if err != nil {
		return "", err
	}
	if service == "torbox" {
		var prepared string
		link, prepared, err = s.Providers.tbPrepare(life, key, rec.Hash, magnet, r, cachedOnly, policy.PreparationID)
		if policy.PreparedID != nil {
			*policy.PreparedID = prepared
		}
		if providerFailureCode(err) == "not_cached" {
			// This response describes provider-wide cache availability, unlike
			// ITEM_NOT_FOUND, which may only describe the caller's account.
			_ = s.DB.SaveAIOAvailability(life, service, fingerprint(key), model.AIOAvailability{
				Hash: rec.Hash, CheckedAt: time.Now(), Generation: generation, Shareable: true,
			})
		}
	} else {
		stage := "cleanup"
		err = s.withHash(life, fingerprint(key), rec.Hash, func() error {
			if visitor == nil {
				if err := s.cleanupHash(life, key, rec.Hash, false); err != nil {
					return err
				}
				stage = "inventory"
			}
			inventory, err := s.Providers.ADInventory(life, key)
			if err != nil {
				return err
			}
			existing := existingAD(inventory, rec.Hash)
			if policy.PreparationID != "" {
				existing = nil
				for i := range inventory {
					if itoa64(inventory[i].ID) == policy.PreparationID && strings.EqualFold(inventory[i].Hash, rec.Hash) {
						existing = &inventory[i]
						break
					}
				}
				if existing == nil {
					return newProviderError("not_found", "")
				}
			}
			var item adMagnet
			if existing != nil {
				if *existing.Status < 0 || *existing.Status > 4 {
					return ErrInvalidResponse
				}
				item = *existing
				ready := *item.Status == 4
				item.Ready = &ready
			} else {
				stage = "add"
				item, err = s.Providers.ADAdd(life, key, rec.Hash, magnet)
				if err != nil {
					if cachedOnly {
						s.cleanupCachedOnlyMagnet(life, key, rec.Hash, item, inventory, visitor == nil)
					}
					return err
				}
			}
			if policy.PreparedID != nil {
				*policy.PreparedID = itoa64(item.ID)
			}
			if item.Ready == nil || !*item.Ready {
				if cachedOnly && existing == nil && item.ID > 0 {
					s.cleanupCachedOnlyMagnet(life, key, rec.Hash, item, inventory, visitor == nil)
					if !observed.CheckedAt.IsZero() {
						_ = s.DB.ForgetSharedAvailability(life, service, rec.Hash, observed.CheckedAt)
					}
				}
				return ErrPending
			}
			stage = "file_list"
			files, err := s.Providers.ADFiles(life, key, item.ID)
			if err != nil {
				return err
			}
			stage = "file_selection"
			file, ok := SelectFile(files, r)
			if !ok {
				return errors.New("no unambiguous matching video")
			}
			stage = "unlock"
			link, err = s.Providers.ADUnlock(life, key, file.Link)
			if err == nil && existing == nil {
				_ = s.DB.SaveSharedAvailability(life, service, model.AIOAvailability{Hash: rec.Hash, Cached: true, CheckedAt: time.Now(), Files: files, Generation: generation})
			}
			return err
		})
		if err != nil {
			err = &playbackError{stage: stage, cause: err}
		}
	}
	if visitor != nil && service == "torbox" && err == nil {
		if items, e := s.Providers.TBCheck(life, key, []string{rec.Hash}); e == nil {
			for _, a := range items {
				a.Generation = generation
				_ = s.DB.SaveSharedAvailability(life, service, a)
			}
		}
	}
	s.recordPlaybackFailure(service, err)
	if visitor == nil && !errors.Is(err, ErrPending) && !errors.Is(err, context.Canceled) {
		s.observe(service, err)
	}
	return link, err
}
