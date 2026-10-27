package aiosource

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Service) metadataGET(ctx context.Context, key, path string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.TMDBBase+"/"+path, nil)
	if err != nil {
		return false, ErrInvalidResponse
	}
	req.Header.Set("Authorization", "Bearer "+key)
	policy, err := s.DB.RuntimeValues(ctx, "metadata_retries")
	if err != nil {
		return false, err
	}
	var retryErr error
	for attempt := 0; attempt < policy["request_attempts"]; attempt++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := s.metadataRate.wait(ctx); err != nil {
			return false, err
		}
		client := *s.MetadataHTTP
		if client.Timeout == 0 {
			timeout, err := s.outgoingTimeout(ctx, "metadata_seconds")
			if err != nil {
				return false, err
			}
			client.Timeout = timeout
		}
		res, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, metadataTransportError(ctx, err)
		}
		if res.StatusCode == 404 {
			res.Body.Close()
			return false, nil
		}
		if res.StatusCode == 401 || res.StatusCode == 403 {
			res.Body.Close()
			return false, metadataHTTPError(res.StatusCode)
		}
		if res.StatusCode == 429 || res.StatusCode >= 500 {
			retryErr = metadataHTTPError(res.StatusCode)
			res.Body.Close()
			delay := time.Duration(policy["request_retry_seconds"]*(1<<attempt)) * time.Second
			delay = metadataRetryAfter(res.Header.Get("Retry-After"), delay, time.Now())
			if res.StatusCode == 429 {
				s.metadataRate.cooldown(delay)
			}
			if attempt == policy["request_attempts"]-1 {
				break
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return false, metadataHTTPError(res.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
		res.Body.Close()
		if err != nil {
			return false, metadataTransportError(ctx, err)
		}
		if len(b) > 8<<20 {
			return false, ErrInvalidResponse
		}
		if json.Unmarshal(b, out) != nil {
			return false, ErrInvalidResponse
		}
		return true, nil
	}
	return false, retryErr
}

type tmdbSeason struct {
	Number *int   `json:"season_number"`
	Date   string `json:"air_date"`
}
type tmdbSeasonDetails struct {
	Number   *int   `json:"season_number"`
	Date     string `json:"air_date"`
	Episodes []struct {
		Season  *int   `json:"season_number"`
		Episode *int   `json:"episode_number"`
		Date    string `json:"air_date"`
	} `json:"episodes"`
}
type tmdbDetails struct {
	Poster           string `json:"poster_path"`
	Adult            *bool  `json:"adult"`
	ID               int64  `json:"id"`
	Title            string `json:"title"`
	OriginalTitle    string `json:"original_title"`
	OriginalLanguage string `json:"original_language"`
	Name             string `json:"name"`
	OriginalName     string `json:"original_name"`
	Date             string `json:"release_date"`
	AirDate          string `json:"first_air_date"`
	IMDb             string `json:"imdb_id"`
	External         *struct {
		IMDb string `json:"imdb_id"`
		TVDB *int64 `json:"tvdb_id"`
	} `json:"external_ids"`
	Alternatives *struct {
		Titles []struct {
			Title string `json:"title"`
		} `json:"titles"`
		Results []struct {
			Title string `json:"title"`
		} `json:"results"`
	} `json:"alternative_titles"`
	Translations *struct {
		Items []struct {
			Language string                       `json:"iso_639_1"`
			Data     struct{ Title, Name string } `json:"data"`
		} `json:"translations"`
	} `json:"translations"`
	Releases *struct {
		Results []struct {
			Dates []struct {
				Date string `json:"release_date"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	Seasons []tmdbSeason `json:"seasons"`
}

func yearOf(s string) int {
	if len(s) < 10 {
		return 0
	}
	if _, err := time.Parse("2006-01-02", s[:10]); err != nil {
		return 0
	}
	n, _ := strconv.Atoi(s[:4])
	if n < 1800 || n > 2200 {
		return 0
	}
	return n
}
func decodeWork(typ, id string, b []byte) (model.AIOWork, error) {
	var d tmdbDetails
	w := model.AIOWork{Type: typ, TMDB: id, SeasonYears: map[int]int{}, EpisodeYears: map[string]int{}, Years: []int{}}
	if json.Unmarshal(b, &d) != nil || itoa64(d.ID) != id || d.Adult == nil || d.External == nil || d.Alternatives == nil || d.Translations == nil || d.Translations.Items == nil {
		return w, ErrInvalidResponse
	}
	w.Adult = *d.Adult
	w.PosterPath = d.Poster
	primary := d.Title
	date := d.Date
	original := d.OriginalTitle
	if typ == "tv" {
		primary = d.Name
		date = d.AirDate
		original = d.OriginalName
		if d.Seasons == nil || d.Alternatives.Results == nil {
			return w, ErrInvalidResponse
		}
	} else if d.Releases == nil || d.Releases.Results == nil || d.Alternatives.Titles == nil {
		return w, ErrInvalidResponse
	}
	if strings.TrimSpace(primary) == "" {
		return w, ErrInvalidResponse
	}
	w.LocalizedTitles = map[string]string{"en": primary}
	if lang := NormalizeLanguage(d.OriginalLanguage); lang != "" && Normalize(original) != "" {
		w.LocalizedTitles[lang] = original
	}
	titles := []string{primary, original}
	for _, a := range d.Alternatives.Titles {
		titles = append(titles, a.Title)
	}
	for _, a := range d.Alternatives.Results {
		titles = append(titles, a.Title)
	}
	for _, a := range d.Translations.Items {
		titles = append(titles, a.Data.Title, a.Data.Name)
		if lang := NormalizeLanguage(a.Language); lang != "" {
			title := a.Data.Title
			if typ == "tv" {
				title = a.Data.Name
			}
			if Normalize(title) != "" {
				w.LocalizedTitles[lang] = title
			}
		}
	}
	seen := map[string]bool{}
	for _, t := range titles {
		if t != "" && !seen[t] && Normalize(t) != "" {
			seen[t] = true
			w.Titles = append(w.Titles, t)
		}
	}
	if y := yearOf(date); y > 0 {
		w.Years = append(w.Years, y)
	}
	if typ == "movie" {
		for _, country := range d.Releases.Results {
			for _, release := range country.Dates {
				if y := yearOf(release.Date); y > 0 && !contains(w.Years, y) {
					w.Years = append(w.Years, y)
				}
			}
		}
	}
	w.IMDb = canonicalIMDb(d.External.IMDb)
	if w.IMDb == "" {
		w.IMDb = canonicalIMDb(d.IMDb)
	}
	if w.IMDb != "" && !regexpIMDb.MatchString(w.IMDb) {
		return w, ErrInvalidResponse
	}
	if d.External.TVDB != nil && *d.External.TVDB > 0 {
		w.TVDB = itoa64(*d.External.TVDB)
	}
	knownSeasons := map[int]bool{}
	for _, season := range d.Seasons {
		if season.Number == nil || *season.Number < 0 || *season.Number > 2147483647 || knownSeasons[*season.Number] {
			return w, ErrInvalidResponse
		}
		knownSeasons[*season.Number] = true
		if y := yearOf(season.Date); y > 0 {
			w.SeasonYears[*season.Number] = y
		}
	}
	return w, nil
}
func (s *Service) loadDetails(ctx context.Context, key string, j store.AIOJob) (*store.AIOMetadataPublication, error) {
	appendFields := "external_ids,alternative_titles,translations"
	if j.Type == "movie" {
		appendFields += ",release_dates"
	}
	q := url.Values{"append_to_response": {appendFields}}
	var body json.RawMessage
	found, err := s.metadataGET(ctx, key, j.Type+"/"+j.ID+"?"+q.Encode(), &body)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	work, err := decodeWork(j.Type, j.ID, body)
	if err != nil {
		return nil, err
	}
	if j.Type == "tv" {
		seasons, err := s.DB.AIORequestedSeasons(ctx, j.ID, work.IMDb, work.TVDB)
		if err != nil {
			return nil, metadataStorageError(err)
		}
		// A malformed source season must not make the entire series retry
		// forever. Only seasons listed in the complete series response exist.
		var details tmdbDetails
		_ = json.Unmarshal(body, &details)
		known := map[int]bool{}
		for _, season := range details.Seasons {
			if season.Number != nil {
				known[*season.Number] = true
			}
		}
		requested := []int{}
		for _, season := range seasons {
			if known[season] {
				requested = append(requested, season)
			}
		}
		if err := s.loadSeasons(ctx, key, &work, requested); err != nil {
			return nil, err
		}
	}
	normalized := make([]string, len(work.Titles))
	for i, t := range work.Titles {
		normalized[i] = Normalize(t)
	}
	return &store.AIOMetadataPublication{Work: work, Normalized: normalized, Job: &j}, nil
}

func (s *Service) loadSeasons(ctx context.Context, key string, work *model.AIOWork, seasons []int) error {
	for start := 0; start < len(seasons); start += 20 {
		chunk := seasons[start:min(start+20, len(seasons))]
		responses := map[int]tmdbSeasonDetails{}
		if len(chunk) == 1 {
			var data tmdbSeasonDetails
			found, err := s.metadataGET(ctx, key, "tv/"+work.TMDB+"/season/"+strconv.Itoa(chunk[0]), &data)
			if err != nil {
				return err
			}
			if !found {
				return ErrInvalidResponse
			}
			responses[chunk[0]] = data
		} else {
			fields := make([]string, len(chunk))
			for i, season := range chunk {
				fields[i] = "season/" + strconv.Itoa(season)
			}
			var data map[string]json.RawMessage
			q := url.Values{"append_to_response": {strings.Join(fields, ",")}}
			found, err := s.metadataGET(ctx, key, "tv/"+work.TMDB+"?"+q.Encode(), &data)
			if err != nil {
				return err
			}
			var id int64
			if !found || json.Unmarshal(data["id"], &id) != nil || itoa64(id) != work.TMDB {
				return ErrInvalidResponse
			}
			for i, season := range chunk {
				var detail tmdbSeasonDetails
				if json.Unmarshal(data[fields[i]], &detail) != nil {
					return ErrInvalidResponse
				}
				responses[season] = detail
			}
		}
		for _, season := range chunk {
			data := responses[season]
			if data.Number == nil || *data.Number != season || data.Episodes == nil {
				return ErrInvalidResponse
			}
			if y := yearOf(data.Date); y > 0 {
				work.SeasonYears[season] = y
			}
			for _, episode := range data.Episodes {
				if episode.Season == nil || *episode.Season != season || episode.Episode == nil || *episode.Episode < 0 {
					return ErrInvalidResponse
				}
				if y := yearOf(episode.Date); y > 0 {
					work.EpisodeYears[strconvPair(season, *episode.Episode)] = y
				}
			}
		}
	}
	return nil
}

func (s *Service) details(ctx context.Context, key string, job store.AIOJob) (bool, error) {
	publication, err := s.loadDetails(ctx, key, job)
	if err != nil {
		return false, err
	}
	if publication == nil {
		err = s.DB.DeleteAIOMetadataJob(ctx, job)
	} else {
		err = s.DB.SaveAIOMetadataBatch(ctx, []store.AIOMetadataPublication{*publication})
	}
	if err != nil {
		return false, metadataStorageError(err)
	}
	return true, nil
}
func (s *Service) inventory(ctx context.Context, typ, date string) error {
	prefix := "movie"
	if typ == "tv" {
		prefix = "tv_series"
	}
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return err
	}
	u := s.ExportBase + "/" + prefix + "_ids_" + parsed.Format("01_02_2006") + ".json.gz"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	// Inventory files contain the entire TMDB catalog. Download to a bounded
	// temporary file before importing: http.Client.Timeout must not include
	// PostgreSQL batch processing, which can take minutes on a small container.
	client := *s.MetadataHTTP
	client.Timeout, err = s.outgoingTimeout(ctx, "inventory_seconds")
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return metadataTransportError(ctx, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return metadataHTTPError(res.StatusCode)
	}
	file, err := os.CreateTemp("", "aiosources-tmdb-*.json.gz")
	if err != nil {
		return errMetadataStorage
	}
	defer os.Remove(file.Name())
	defer file.Close()
	size, err := io.Copy(file, io.LimitReader(res.Body, (512<<20)+1))
	if err != nil {
		if fileErr := new(os.PathError); errors.As(err, &fileErr) {
			return errMetadataStorage
		}
		return metadataTransportError(ctx, err)
	}
	res.Body.Close()
	if size > 512<<20 {
		return ErrInvalidResponse
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return errMetadataStorage
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return ErrInvalidResponse
	}
	defer reader.Close()
	decompressed := &io.LimitedReader{R: reader, N: (2 << 30) + 1}
	scan := bufio.NewScanner(decompressed)
	scan.Buffer(make([]byte, 8192), 1<<20)
	batch := []string{}
	count := 0
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(scan.Bytes(), &row) != nil || row.ID <= 0 {
			return ErrInvalidResponse
		}
		batch = append(batch, itoa64(row.ID))
		count++
		if len(batch) == 1000 {
			if err = s.DB.QueueAIOInventory(ctx, typ, batch, date); err != nil {
				return metadataStorageError(err)
			}
			batch = batch[:0]
		}
	}
	if err = scan.Err(); err != nil {
		return ErrInvalidResponse
	}
	if count == 0 || decompressed.N == 0 {
		return ErrInvalidResponse
	}
	return metadataStorageError(s.DB.QueueAIOInventory(ctx, typ, batch, date))
}
func (s *Service) changes(ctx context.Context, key string, start, end time.Time) error {
	for _, typ := range []string{"movie", "tv"} {
		for page := 1; page <= 10000; page++ {
			q := url.Values{"start_date": {start.UTC().Format("2006-01-02")}, "end_date": {end.UTC().Format("2006-01-02")}, "page": {strconv.Itoa(page)}}
			var out struct {
				Page    int  `json:"page"`
				Pages   *int `json:"total_pages"`
				Results []struct {
					ID int64 `json:"id"`
				} `json:"results"`
			}
			found, err := s.metadataGET(ctx, key, typ+"/changes?"+q.Encode(), &out)
			if err != nil {
				return err
			}
			if !found || out.Page != page || out.Pages == nil || *out.Pages < 0 || *out.Pages > 10000 || out.Results == nil {
				return ErrInvalidResponse
			}
			ids := []string{}
			for _, r := range out.Results {
				if r.ID <= 0 {
					return ErrInvalidResponse
				}
				ids = append(ids, itoa64(r.ID))
			}
			if err = s.DB.QueueAIOMetadata(ctx, typ, ids, true); err != nil {
				return metadataStorageError(err)
			}
			if page >= *out.Pages {
				break
			}
		}
	}
	return metadataStorageError(s.DB.AIOChangesThrough(ctx, end))
}
func (s *Service) Sync(ctx context.Context, c model.AIOConfig) error {
	return s.syncMetadata(ctx, c, false)
}

func (s *Service) syncMetadata(ctx context.Context, c model.AIOConfig, automatic bool) (syncErr error) {
	release, locked, err := s.DB.AIOLock(ctx, "metadata")
	if err != nil {
		return err
	}
	if !locked {
		return model.ErrBusy
	}
	defer release()
	stage := "configuration"
	attempted := false
	defer func() {
		if errors.Is(syncErr, model.ErrBusy) || (!attempted && syncErr == nil) {
			return
		}
		logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		diagnostic := metadataDiagnostic(stage, syncErr)
		if syncErr != nil && diagnostic.FailureCode != "interrupted" {
			slog.Error(diagnostic.Message(), "service", "tmdb", "operation", stage, "failure_code", diagnostic.FailureCode, "status_code", diagnostic.StatusCode)
		}
		if err := s.DB.RecordInformationActivity(logCtx, diagnostic, 0); err != nil {
			slog.Error("Could not save the TMDB update diagnostic", "service", "tmdb", "operation", stage)
		}
	}()
	if automatic {
		// A second instance or an edited configuration may have changed the
		// clock since polling. Recheck admission while holding the worker lock.
		c, err = s.DB.AIOConfig(ctx)
		if err != nil {
			return err
		}
		status, err := s.DB.AIOAutomationStatus(ctx)
		if err != nil {
			return err
		}
		if AutomationPlan(c, status, false, false, time.Now()).Updates != "due" {
			return nil
		}
	}
	attempt, err := s.DB.AIOBeginSync(ctx)
	if err != nil {
		return err
	}
	attempted = true
	defer func() {
		if syncErr == nil {
			return
		}
		// Shutdown cannot cancel publishing the retry clock. Never persist the
		// provider error text: it may contain a credential-bearing URL.
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		message := metadataDiagnostic(stage, syncErr).Message()
		delay := syncRetryDelay(attempt)
		if policy, err := s.DB.RuntimeValues(recovery, "metadata_retries"); err == nil {
			delay = model.RetryBackoff(attempt-1, time.Duration(policy["sync_retry_seconds"])*time.Second, time.Duration(policy["sync_retry_max_seconds"])*time.Second)
		}
		_ = s.DB.AIOFailedSync(recovery, time.Now().Add(delay), message)
	}()
	trigger := "manual"
	if automatic {
		trigger = "automatic"
	}
	history, err := s.beginOperation(ctx, "metadata", "tmdb", trigger, "", "metadata")
	if err != nil {
		return err
	}
	defer func() { history.run.Phase = stage; history.finish(ctx, syncErr) }()
	key, err := s.key(ctx, c.TMDBSecret)
	if err != nil {
		return err
	}
	if key == "" {
		return ErrAuth
	}
	defer func() {
		code := metadataDiagnostic(stage, syncErr).FailureCode
		if code != "interrupted" && code != "storage" && code != "change_gap" {
			s.observe("tmdb", syncErr)
		}
	}()
	s.metadataRate.setRate(c.TMDBRequestsPerSecond)
	stage = "storage"
	status, err := s.DB.AIOAutomationStatus(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	stage = "changes"
	history.checkpoint(ctx, stage)
	start := now.Add(-24 * time.Hour)
	if status.ChangesThrough != nil {
		start = *status.ChangesThrough
		if now.Sub(start) > 14*24*time.Hour {
			return errMetadataGap
		}
	}
	if status.ChangesThrough == nil {
		if err = s.DB.AIOChangesThrough(ctx, start); err != nil {
			return metadataStorageError(err)
		}
	}
	if err = s.changes(ctx, key, start, now); err != nil {
		return err
	}
	date := now.Add(-24 * time.Hour).Format("2006-01-02")
	if status.InventoryDate != date {
		stage = "inventory"
		history.checkpoint(ctx, stage)
		for _, typ := range []string{"movie", "tv"} {
			if err = s.inventory(ctx, typ, date); err != nil {
				return err
			}
		}
		if err = s.DB.ReconcileAIOInventory(ctx, date); err != nil {
			return metadataStorageError(err)
		}
		if err = s.DB.AIOInventoryDate(ctx, date); err != nil {
			return metadataStorageError(err)
		}
	}
	stage = "storage"
	if err = s.DB.QueueAIORefresh(ctx); err != nil {
		return metadataStorageError(err)
	}
	lastChanges := now
	progress := metadataProgress{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		stage = "storage"
		jobs, err := s.DB.AIOMetadataJobs(ctx)
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			break
		}
		stage = "details"
		history.checkpoint(ctx, stage)
		if err = s.processMetadataJobs(ctx, key, jobs, history, &progress, c.AutoMatch); err != nil {
			return err
		}
		if time.Since(lastChanges) > time.Duration(c.SyncMinutes)*time.Minute {
			stage = "changes"
			history.checkpoint(ctx, stage)
			end := time.Now().UTC()
			if err = s.changes(ctx, key, lastChanges, end); err != nil {
				return err
			}
			lastChanges = end
		}
	}
	if progress.saved == 0 && progress.deferred > 0 {
		stage = "details"
		return progress.firstDeferred
	}
	if progress.deferred > 0 {
		_ = s.DB.RecordInformationActivity(ctx, metadataDiagnostic("details", progress.firstDeferred), progress.deferred)
	}
	stage = "storage"
	return s.DB.AIOCompletedSync(ctx)
}

var errMetadataStorage = errors.New("metadata storage unavailable")
