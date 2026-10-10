package aiosource

import (
	"context"
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"net/url"
	"strings"
	"time"
)

type Stream struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	InfoHash string   `json:"infoHash,omitempty"`
	URL      string   `json:"url,omitempty"`
	Sources  []string `json:"sources,omitempty"`
	Hints    Behavior `json:"behaviorHints"`
}
type Behavior struct {
	BingeGroup  string `json:"bingeGroup"`
	Size        int64  `json:"videoSize,omitempty"`
	Filename    string `json:"filename"`
	NotWebReady bool   `json:"notWebReady,omitempty"`
	FolderName  string `json:"folderName,omitempty"`
	FolderSize  int64  `json:"folderSize,omitempty"`
}

func Manifest() map[string]any {
	return map[string]any{"id": "org.aiosource.v2", "version": "2.0.0", "name": "AIOSources", "description": "Local sources with verified IMDb mapping and optional AllDebrid / TorBox playback", "resources": []any{map[string]any{"name": "stream", "types": []string{"movie", "series", "anime"}, "idPrefixes": []string{"tt", "imdb:", "tmdb:", "tvdb:", "mal:"}}}, "types": []string{"movie", "series", "anime"}, "catalogs": []any{}, "behaviorHints": map[string]bool{"configurable": true}}
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
	details := "📦 " + streamSize(rec.Size) + "  🌐 " + sourceName
	if label := streamLanguageLabel(languages); label != "" {
		details += "  🏷️ " + label
	}
	return Stream{Name: "［AIO " + sourceName + "］ " + q, InfoHash: rec.Hash, Title: rec.Title + "\n" + details, Hints: Behavior{BingeGroup: streamBingeGroup(rec, r, q, languages), Size: rec.Size, Filename: rec.Title, NotWebReady: true}}
}
func (s *Service) Streams(ctx context.Context, base, token, mediaType, id string) (streams []Stream, streamErr error) {
	history := newStremioHistory(mediaType, id)
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
		s.saveStremioHistory(history)
	}()
	recordResult := func(rec model.AIORecord, item Stream, service, cache string) {
		if len(history.Results) >= 1000 {
			history.Truncated = true
			return
		}
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
	history.Matched = len(rows)
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	eligible := []model.AIORecord{}
	hashes := []string{}
	for _, rec := range rows {
		if !hashRE.MatchString(rec.Hash) {
			history.InvalidHash++
			continue
		}
		if !MatchesEpisode(rec, ParseRelease(rec.Title), r) {
			history.WrongEpisode++
			continue
		}
		eligible = append(eligible, rec)
		hashes = append(hashes, rec.Hash)
	}
	history.Eligible = len(eligible)
	if len(eligible) == 0 {
		return []Stream{}, nil
	}
	availability, err := s.Availability(ctx, c, streamResourceKey(r, rows), hashes)
	if err != nil {
		return nil, err
	}
	// Resolve names once for the whole response, including remote catalogs.
	// Presentation must not prevent playback if a definition becomes unreadable.
	if s.sources != nil {
		if sources, err := s.sources.List(); err == nil {
			for _, source := range sources {
				if name := strings.TrimSpace(source.Name); name != "" {
					names[source.ID] = name
				}
			}
		}
	}
	out := []Stream{}
	for _, rec := range eligible {
		stream := baseStream(rec, r, names[rec.Provider])
		if len(availability) == 0 {
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
			tracker, configured := c.Trackers[rec.Provider]
			if !configured {
				tracker.Private = true
			}
			if known && !observed.Cached && tracker.Private && tracker.AnnounceSecret == "" {
				history.PrivateUnavailable++
				continue
			}
			selected, hasFile := SelectFile(observed.Files, r)
			if known && observed.Cached && len(observed.Files) > 0 && !hasFile {
				history.FileUnavailable++
				continue
			}
			if observed.Cached && r.Episode != nil && !hasFile {
				// Global readiness cannot certify an episode when listing files
				// failed. Retain playback but present the cache as unverified.
				known = false
			}
			item := stream
			item.InfoHash = ""
			item.Hints.NotWebReady = false
			item.Hints.BingeGroup += "|" + service
			marker := "TB"
			label := "TorBox"
			if service == "alldebrid" {
				marker = "AD"
				label = "AllDebrid"
			}
			if known {
				if observed.Cached {
					item.Name = "[" + marker + "+] " + stream.Name
				} else {
					item.Name = "[" + marker + " download] " + stream.Name
				}
			}
			item.Title += "\n" + label
			if !known {
				item.Title += " · Cache unknown"
			}
			if hasFile {
				item.Hints.Filename = selected.Path
				item.Hints.Size = selected.Size
				item.Hints.FolderName = rec.Title
				item.Hints.FolderSize = rec.Size
			}
			segments := []string{"stremio", token, "play", service, rec.Provider, rec.SourceID, mediaType, id}
			for i := range segments {
				segments[i] = url.PathEscape(segments[i])
			}
			item.URL = strings.TrimRight(base, "/") + "/" + strings.Join(segments, "/")
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
	if service != "alldebrid" && service != "torbox" {
		return "", model.ErrInvalid
	}
	r, err := ParseID(id, mediaType)
	if err != nil {
		return "", model.ErrInvalid
	}
	life, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	rec, err := s.DB.AIOTorrent(life, provider, sourceID)
	if err != nil {
		return "", err
	}
	if !hashRE.MatchString(rec.Hash) || !MatchesEpisode(rec, ParseRelease(rec.Title), r) {
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
	name := c.TorBoxSecret
	if service == "alldebrid" {
		name = c.AllDebridSecret
	}
	if name == "" {
		return "", model.ErrInvalid
	}
	key, err := s.key(life, name)
	if err != nil {
		return "", err
	}
	magnet, _, err := s.magnet(life, rec, c)
	if err != nil {
		return "", err
	}
	var link string
	if service == "torbox" {
		link, err = s.Providers.TBPlay(life, key, rec.Hash, magnet, r)
	} else {
		stage := "cleanup"
		err = s.withHash(life, fingerprint(key), rec.Hash, func() error {
			if err := s.cleanupHash(life, key, rec.Hash, false); err != nil {
				return err
			}
			stage = "inventory"
			inventory, err := s.Providers.ADInventory(life, key)
			if err != nil {
				return err
			}
			existing := existingAD(inventory, rec.Hash)
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
					return err
				}
			}
			if item.Ready == nil || !*item.Ready {
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
			return err
		})
		if err != nil {
			err = &playbackError{stage: stage, cause: err}
		}
	}
	s.recordPlaybackFailure(service, err)
	if !errors.Is(err, ErrPending) && !errors.Is(err, context.Canceled) {
		s.observe(service, err)
	}
	return link, err
}
