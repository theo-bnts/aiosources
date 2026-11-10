package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type igdbToken struct {
	value, client, credential string
	expires                   time.Time
}
type igdbGame struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Updated       int64  `json:"updated_at"`
	FirstRelease  int64  `json:"first_release_date"`
	Parent        int64  `json:"parent_game"`
	VersionParent int64  `json:"version_parent"`
	VersionTitle  string `json:"version_title"`
	Cover         struct {
		Image string `json:"image_id"`
	} `json:"cover"`
	Aliases []struct {
		Name string `json:"name"`
	} `json:"alternative_names"`
	Platforms []model.GamePlatform `json:"platforms"`
	Kind      struct {
		Type string `json:"type"`
	} `json:"game_type"`
	Localizations []struct {
		Name   string `json:"name"`
		Region struct {
			Identifier string `json:"identifier"`
		} `json:"region"`
		Cover struct {
			Image string `json:"image_id"`
		} `json:"cover"`
	} `json:"game_localizations"`
}

const igdbFields = "id,name,updated_at,first_release_date,parent_game,version_parent,version_title,alternative_names.name,cover.image_id,platforms.id,platforms.name,platforms.slug,platforms.abbreviation,game_type.type,game_localizations.name,game_localizations.region.identifier,game_localizations.cover.image_id"

func decodeGame(d igdbGame) (model.AIOWork, error) {
	w := model.AIOWork{Type: "game", IGDB: strconv.FormatInt(d.ID, 10), DisplayTitle: strings.TrimSpace(d.Name), Titles: []string{}, Years: []int{}, LocalizedTitles: map[string]string{}, LocalizedPosters: map[string]string{}}
	if d.ID <= 0 || d.ID > 1<<53-1 || w.DisplayTitle == "" || len(w.DisplayTitle) > 1000 || d.Updated <= 0 {
		return w, ErrInvalidResponse
	}
	w.Game = model.GameInformation{Platforms: d.Platforms, Kind: d.Kind.Type, Edition: d.VersionTitle}
	if d.VersionParent > 0 {
		w.Game.Parent = strconv.FormatInt(d.VersionParent, 10)
	} else if d.Parent > 0 {
		w.Game.Parent = strconv.FormatInt(d.Parent, 10)
	}
	for _, p := range w.Game.Platforms {
		if p.ID <= 0 || p.Name == "" || len(p.Name) > 200 || len(p.Slug) > 160 {
			return w, ErrInvalidResponse
		}
	}
	slices.SortFunc(w.Game.Platforms, func(a, b model.GamePlatform) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	w.LocalizedTitles["en"] = w.DisplayTitle
	if model.CoverURL("game", d.Cover.Image, 0) != "" {
		w.PosterPath = d.Cover.Image
	}
	add := func(title string) {
		title = strings.TrimSpace(title)
		if title != "" && len(title) <= 1000 && !slices.Contains(w.Titles, title) && len(w.Titles) < 200 {
			w.Titles = append(w.Titles, title)
		}
	}
	add(w.DisplayTitle)
	if d.VersionTitle != "" && !strings.Contains(Normalize(w.DisplayTitle), Normalize(d.VersionTitle)) {
		add(w.DisplayTitle + " " + d.VersionTitle)
	}
	for _, alias := range d.Aliases {
		add(alias.Name)
	}
	for _, local := range d.Localizations {
		add(local.Name)
		// IGDB regions identify a market, not necessarily a language. Only
		// unambiguous language regions may override a browser's title/cover.
		lang := map[string]string{"france": "fr", "germany": "de", "italy": "it", "spain": "es", "japan": "ja", "china": "zh", "korea": "ko", "brazil": "pt", "united_kingdom": "en", "north_america": "en"}[strings.ToLower(local.Region.Identifier)]
		if lang != "" {
			if Normalize(local.Name) != "" {
				w.LocalizedTitles[lang] = local.Name
			}
			if model.CoverURL("game", local.Cover.Image, 0) != "" {
				w.LocalizedPosters[lang] = local.Cover.Image
			}
		}
	}
	if d.FirstRelease > 0 {
		year := time.Unix(d.FirstRelease, 0).UTC().Year()
		if year >= 1900 && year <= 2200 {
			w.Years = append(w.Years, year)
		}
	}
	return w, nil
}

func (s *Service) GameState() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.gamesRunning }
func (s *Service) StartGames(ctx context.Context, trigger string) error {
	if trigger != "manual" && trigger != "automatic" {
		return model.ErrInvalid
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	if c.IGDBClientID == "" || c.IGDBSecret == "" {
		return model.ErrInvalid
	}
	if _, err = s.key(ctx, c.IGDBSecret); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.ErrConflict
	}
	if s.gamesRunning || s.reset.Status == "running" {
		return model.ErrBusy
	}
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.gamesRunning, s.gamesCancel, s.gamesDone = true, cancel, done
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		_ = s.SyncGames(ctx, trigger)
		s.mu.Lock()
		s.gamesRunning, s.gamesCancel, s.gamesDone = false, nil, nil
		close(done)
		s.mu.Unlock()
		s.wakeAutomation()
	}()
	return nil
}
func (s *Service) StopGames() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gamesCancel != nil {
		s.gamesCancel()
	}
}

func (s *Service) SyncGames(ctx context.Context, trigger string) (syncErr error) {
	release, locked, err := s.DB.AIOLock(ctx, "igdb")
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
	if c.IGDBClientID == "" || c.IGDBSecret == "" {
		return model.ErrInvalid
	}
	h, err := s.beginOperation(ctx, "metadata", "igdb", trigger, "games", "igdb")
	if err != nil {
		return err
	}
	defer func() {
		if syncErr != nil && !errors.Is(syncErr, context.Canceled) {
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			message := "Game information could not be updated. Saved progress is preserved."
			var e *igdbError
			if errors.As(syncErr, &e) {
				message = e.message
			}
			_ = s.DB.GameUpdateFailure(final, message, time.Now().Add(5*time.Minute))
		}
		h.finish(ctx, syncErr)
	}()
	s.gameRate.setRate(c.IGDBRequestsPerSecond)
	state, err := s.DB.BeginGameUpdate(ctx, c.IGDBReconcileDays)
	if err != nil {
		return err
	}
	h.run.Processed = state.Processed
	h.checkpoint(ctx, "game_"+state.Phase)
	if state.After == 0 && state.Phase != "changes" {
		var count struct {
			Count int64 `json:"count"`
		}
		if err = s.igdbPOST(ctx, c, h, "games/count", "", &count); err != nil {
			return err
		}
		if count.Count < 0 || state.Works > 100 && count.Count < state.Works*95/100 {
			return ErrInvalidResponse
		}
		if err = s.DB.SetGameTotal(ctx, count.Count); err != nil {
			return err
		}
	}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		state, err = s.DB.GameStatus(ctx)
		if err != nil {
			return err
		}
		query := fmt.Sprintf("fields %s; where id > %d", igdbFields, state.After)
		if state.Phase == "changes" {
			lower := state.StartedAt.Add(-5 * time.Minute)
			if state.ChangesThrough != nil {
				lower = state.ChangesThrough.Add(-5 * time.Minute)
			}
			query += fmt.Sprintf(" & updated_at >= %d & updated_at <= %d", lower.Unix(), state.StartedAt.Unix())
		}
		query += "; sort id asc; limit 500;"
		var page []igdbGame
		if err = s.igdbPOST(ctx, c, h, "games", query, &page); err != nil {
			return err
		}
		if page == nil || len(page) > 500 {
			return ErrInvalidResponse
		}
		if len(page) == 0 {
			if state.Phase != "changes" && state.Total > 0 && state.Processed < state.Total*95/100 {
				return &igdbError{message: "The game catalog response was incomplete. Saved progress is preserved."}
			}
			if err = s.DB.CompleteGameUpdate(ctx, state); err != nil {
				return err
			}
			h.checkpoint(ctx, "completed")
			return nil
		}
		// Validate the complete response before acknowledging even its first
		// batch. Missing rows or unordered IDs never silently advance a cursor.
		works := make([]model.AIOWork, len(page))
		after := state.After
		for i, d := range page {
			if d.ID <= after {
				return ErrInvalidResponse
			}
			after = d.ID
			works[i], err = decodeGame(d)
			if err != nil {
				return err
			}
		}
		for at := 0; at < len(works); at += 50 {
			batch := works[at:min(at+50, len(works))]
			end, _ := strconv.ParseInt(batch[len(batch)-1].IGDB, 10, 53)
			if err = s.DB.PublishGames(ctx, state, batch, end); err != nil {
				return err
			}
			state.After = end
			state.Processed += int64(len(batch))
			h.run.Processed = state.Processed
			h.checkpoint(ctx, "game_"+state.Phase)
		}
	}
}

type igdbError struct{ message string }

func (e *igdbError) Error() string { return e.message }

func (s *Service) igdbAccessToken(ctx context.Context, c model.AIOConfig, h *operationHistory) (string, error) {
	key, err := s.key(ctx, c.IGDBSecret)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", model.ErrInvalid
	}
	// This mutex belongs to the sole IGDB worker. It also protects explicit
	// test calls and prevents token renewal storms on simultaneous requests.
	s.gameTokenMu.Lock()
	defer s.gameTokenMu.Unlock()
	if s.gameToken.client == c.IGDBClientID && s.gameToken.credential == fingerprint(key) && time.Until(s.gameToken.expires) > time.Minute {
		return s.gameToken.value, nil
	}
	body := url.Values{"client_id": {c.IGDBClientID}, "client_secret": {key}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.IGDBTokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", ErrInvalidResponse
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "AIOSources/"+Version)
	client := *s.GameHTTP
	timeout, err := s.outgoingTimeout(ctx, "metadata_seconds")
	if err != nil {
		return "", err
	}
	client.Timeout = timeout
	started := time.Now()
	res, err := client.Do(req)
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	if h != nil {
		_ = s.DB.RecordProviderCall(ctx, h.run.ID, "authentication", status, time.Since(started).Milliseconds(), "")
	}
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &igdbError{message: "IGDB authentication is unreachable. Check the network settings."}
	}
	defer res.Body.Close()
	if res.StatusCode == 429 || res.StatusCode >= 500 {
		message := "IGDB authentication is temporarily unavailable. Saved progress is preserved."
		retry := metadataRetryAfter(res.Header.Get("Retry-After"), 5*time.Minute, time.Now())
		_ = s.DB.GameUpdateFailure(ctx, message, time.Now().Add(retry))
		return "", &igdbError{message: message}
	}
	if res.StatusCode != http.StatusOK {
		return "", &igdbError{message: "IGDB authentication failed. Check the Twitch client ID and client secret."}
	}
	var token struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
		Type    string `json:"token_type"`
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil || len(data) > 65536 || json.Unmarshal(data, &token) != nil || token.Token == "" || len(token.Token) > 4096 || token.Expires <= 60 || !strings.EqualFold(token.Type, "bearer") {
		return "", ErrInvalidResponse
	}
	s.gameToken = igdbToken{value: token.Token, client: c.IGDBClientID, credential: fingerprint(key), expires: time.Now().Add(time.Duration(min(token.Expires, 86400)) * time.Second)}
	return token.Token, nil
}

func (s *Service) igdbPOST(ctx context.Context, c model.AIOConfig, h *operationHistory, path, query string, out any) error {
	policy, err := s.DB.RuntimeValues(ctx, "metadata_retries")
	if err != nil {
		return err
	}
	renewed := false
	for attempt := 0; attempt < policy["request_attempts"]; attempt++ {
		if err = s.gameRate.wait(ctx); err != nil {
			return err
		}
		token, err := s.igdbAccessToken(ctx, c, h)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.IGDBBase+"/"+path, strings.NewReader(query))
		if err != nil {
			return ErrInvalidResponse
		}
		req.Header.Set("Client-ID", c.IGDBClientID)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("User-Agent", "AIOSources/"+Version)
		client := *s.GameHTTP
		timeout, err := s.outgoingTimeout(ctx, "metadata_seconds")
		if err != nil {
			return err
		}
		client.Timeout = timeout
		started := time.Now()
		res, err := client.Do(req)
		status := 0
		if res != nil {
			status = res.StatusCode
		}
		if h != nil {
			_ = s.DB.RecordProviderCall(ctx, h.run.ID, "provider_request", status, time.Since(started).Milliseconds(), "")
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &igdbError{message: "IGDB could not be reached. Saved progress is preserved."}
		}
		if res.StatusCode == 401 && !renewed {
			renewed = true
			attempt--
			res.Body.Close()
			s.gameTokenMu.Lock()
			s.gameToken = igdbToken{}
			s.gameTokenMu.Unlock()
			continue
		}
		if res.StatusCode == 429 || res.StatusCode >= 500 {
			wait := metadataRetryAfter(res.Header.Get("Retry-After"), time.Duration(max(1, policy["request_retry_seconds"]))*time.Second, time.Now())
			res.Body.Close()
			if wait > time.Minute || attempt >= policy["request_attempts"]-1 {
				_ = s.DB.GameUpdateFailure(ctx, "IGDB is temporarily unavailable or rate limited. Saved progress is preserved.", time.Now().Add(max(wait, 5*time.Minute)))
				return &igdbError{message: "IGDB is temporarily unavailable or rate limited. Saved progress is preserved."}
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return &igdbError{message: "IGDB rejected the information request. Check the credentials and operation history."}
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
		res.Body.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || len(data) > 8<<20 || json.Unmarshal(data, out) != nil {
			return ErrInvalidResponse
		}
		return nil
	}
	return ErrInvalidResponse
}
