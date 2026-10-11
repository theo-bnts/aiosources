package aiosource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"sort"
	"strings"
	"time"
)

const publicInstallationPurpose = "public-installation-v1"

type PublicProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Adding a provider requires an adapter; unrecognized URL fields never become endpoints.
var PublicProviders = []PublicProvider{{"alldebrid", "AllDebrid"}, {"torbox", "TorBox"}}

type PublicInstallation struct {
	Version    int               `json:"version"`
	Generation int64             `json:"generation"`
	Audience   string            `json:"audience"`
	Services   map[string]string `json:"services"`
}

func validPublicKeys(keys map[string]string) bool {
	if len(keys) == 0 || len(keys) > len(PublicProviders) {
		return false
	}
	for service, key := range keys {
		supported := false
		for _, p := range PublicProviders {
			supported = supported || service == p.ID
		}
		if !supported || len(key) < 8 || len(key) > 512 {
			return false
		}
		for _, c := range key {
			if c < 33 || c > 126 {
				return false
			}
		}
	}
	return true
}
func (s *Service) CreatePublicInstallation(ctx context.Context, audience string, keys map[string]string) (string, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return "", err
	}
	if !access.Enabled {
		return "", model.ErrNotFound
	}
	if !validPublicKeys(keys) || audience == "" {
		return "", model.ErrInvalid
	}
	for _, provider := range PublicProviders {
		if key, ok := keys[provider.ID]; ok {
			if err = s.Providers.ValidateKey(ctx, provider.ID, key); err != nil {
				return "", err
			}
		}
	}
	data, err := json.Marshal(PublicInstallation{Version: 1, Generation: access.Generation, Audience: audience, Services: keys})
	if err != nil {
		return "", model.ErrInvalid
	}
	defer clear(data)
	sealed, err := s.Vault.Seal(publicInstallationPurpose, data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}
func (s *Service) OpenPublicInstallation(ctx context.Context, audience, value string) (*PublicInstallation, error) {
	if len(value) < 32 || len(value) > 4096 {
		return nil, ErrAuth
	}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return nil, err
	}
	if !access.Enabled {
		return nil, ErrAuth
	}
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrAuth
	}
	data, err := s.Vault.Open(publicInstallationPurpose, sealed)
	if err != nil {
		return nil, ErrAuth
	}
	defer clear(data)
	var out PublicInstallation
	if json.Unmarshal(data, &out) != nil || out.Version != 1 || out.Generation != access.Generation || out.Audience != audience || !validPublicKeys(out.Services) {
		return nil, ErrAuth
	}
	return &out, nil
}
func (s *Service) PublicStreams(ctx context.Context, base, configuration, mediaType, id string, visitor *PublicInstallation) ([]Stream, error) {
	if visitor == nil || !validPublicKeys(visitor.Services) {
		return nil, ErrAuth
	}
	return s.streams(ctx, base, configuration, mediaType, id, visitor)
}

type publicSource struct {
	Provider string `json:"provider"`
	Source   string `json:"source"`
}

func (s *Service) sealPublicSource(provider, source string) (string, error) {
	data, err := json.Marshal(publicSource{provider, source})
	if err != nil {
		return "", model.ErrInvalid
	}
	defer clear(data)
	sealed, err := s.Vault.Seal("public-playback-v1", data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}
func (s *Service) PublicPlaybackSource(ctx context.Context, value, service, mediaType, id string, visitor *PublicInstallation) (string, error) {
	if visitor == nil || visitor.Services[service] == "" || len(value) > 4096 {
		return "", ErrAuth
	}
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", model.ErrInvalid
	}
	data, err := s.Vault.Open("public-playback-v1", sealed)
	if err != nil {
		return "", model.ErrInvalid
	}
	defer clear(data)
	var source publicSource
	if json.Unmarshal(data, &source) != nil {
		return "", model.ErrInvalid
	}
	return s.PublicPlayback(ctx, service, source.Provider, source.Source, mediaType, id, visitor)
}
func (s *Service) publicAvailability(ctx context.Context, c model.AIOConfig, resource string, hashes []string, visitor *PublicInstallation) (map[string]map[string]model.AIOAvailability, error) {
	hashes = append([]string{}, hashes...)
	sort.Strings(hashes)
	hashes = uniqueStrings(hashes)
	out := map[string]map[string]model.AIOAvailability{}
	type checkResult struct {
		service string
		states  map[string]model.AIOAvailability
		err     error
	}
	results := make(chan checkResult, len(PublicProviders))
	checks := 0
	for _, provider := range PublicProviders {
		key, enabled := visitor.Services[provider.ID]
		if !enabled {
			continue
		}
		checks++
		go func(service, key string) {
			states, err := s.publicServiceAvailability(ctx, c, resource, hashes, service, key)
			results <- checkResult{service, states, err}
		}(provider.ID, key)
	}
	// Join request-scoped checks before the caller clears the visitor keys. A slow
	// or unavailable provider never consumes the other provider's response budget.
	var checkErr error
	for range checks {
		result := <-results
		out[result.service] = result.states
		if result.err != nil {
			checkErr = result.err
		}
	}
	return out, checkErr
}

func (s *Service) publicServiceAvailability(ctx context.Context, c model.AIOConfig, resource string, hashes []string, service, key string) (map[string]model.AIOAvailability, error) {
	states := map[string]model.AIOAvailability{}
	items, err := s.DB.SharedAvailability(ctx, service, hashes, time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
	if err != nil {
		return nil, err
	}
	for _, a := range items {
		states[a.Hash] = a
	}
	missing := []string{}
	for _, hash := range hashes {
		if _, ok := states[hash]; !ok {
			missing = append(missing, hash)
		}
	}
	if len(missing) == 0 {
		return states, nil
	}
	if service == "alldebrid" {
		// Visitor credentials never enter restartable probes or cleanup jobs.
		if c.AllDebridSecret != "" {
			adminKey, e := s.key(ctx, c.AllDebridSecret)
			if e == nil {
				f, e := s.publicAllDebridFlight(adminKey, resource, missing, c)
				if e == nil {
					wait, cancel := context.WithTimeout(ctx, time.Duration(min(c.ResponseSeconds, 15))*time.Second)
					select {
					case <-f.done:
					case <-wait.Done():
					}
					cancel()
				}
			}
		}
	} else {
		check, cancel := context.WithTimeout(ctx, time.Duration(min(c.ResponseSeconds, 15))*time.Second)
		_ = s.checkPublicTorBox(check, key, resource, missing)
		cancel()
	}
	items, err = s.DB.SharedAvailability(ctx, service, hashes, time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
	if err != nil {
		return nil, err
	}
	for _, a := range items {
		states[a.Hash] = a
	}
	return states, nil
}

func (s *Service) publicAllDebridFlight(key, resource string, hashes []string, c model.AIOConfig) (*flight, error) {
	s.publicADMu.Lock()
	defer s.publicADMu.Unlock()
	if f := s.publicADFlight; f != nil {
		select {
		case <-f.done:
			s.publicADFlight = nil
		default:
			// Public traffic can join the existing recoverable probe, but cannot
			// fill the administrator's background queue with different resources.
			return f, nil
		}
	}
	f, err := s.cacheFlightTriggered("alldebrid", key, resource, hashes, c, "playback", false)
	if err == nil {
		s.publicADFlight = f
	}
	return f, err
}
func (s *Service) checkPublicTorBox(ctx context.Context, key, resource string, hashes []string) (checkErr error) {
	select {
	case s.publicProbe <- struct{}{}:
		defer func() { <-s.publicProbe }()
	case <-ctx.Done():
		return ctx.Err()
	}
	lock := "public-cache:torbox:" + resource
	var release func()
	for {
		var locked bool
		var err error
		release, locked, err = s.DB.AIOLock(ctx, lock)
		if err != nil {
			return err
		}
		if locked {
			break
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer release()
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	previous, err := s.DB.SharedAvailability(ctx, "torbox", hashes, time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, a := range previous {
		known[a.Hash] = true
	}
	todo := []string{}
	for _, h := range hashes {
		if !known[h] {
			todo = append(todo, h)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	h, err := s.beginOperation(ctx, "cache", "torbox", "playback", resource, lock)
	if err != nil {
		return err
	}
	defer func() { h.finish(ctx, checkErr) }()
	ctx = withProviderHistory(ctx, h)
	generation, err := s.DB.AIOCacheGeneration(ctx)
	if err != nil {
		return err
	}
	for start := 0; start < len(todo); start += 100 {
		items, e := s.Providers.TBCheck(ctx, key, todo[start:min(len(todo), start+100)])
		if e != nil {
			h.run.Errors++
			return e
		}
		for _, a := range items {
			a.Generation = generation
			if err = s.DB.SaveSharedAvailability(ctx, "torbox", a); err != nil {
				return err
			}
			h.run.Processed++
			if a.Cached {
				h.run.Results["cached"]++
			} else {
				h.run.Results["uncached"]++
			}
		}
	}
	return nil
}
func (p *ProviderHTTP) ValidateKey(ctx context.Context, service, key string) error {
	path := "user/me?settings=false"
	if service == "alldebrid" {
		path = "v4/user"
	} else if service != "torbox" {
		return model.ErrInvalid
	}
	data, err := p.request(ctx, service, key, "GET", path, nil, "")
	if err != nil {
		return err
	}
	defer clear(data)
	if service == "alldebrid" {
		var payload struct {
			User *struct {
				Premium bool `json:"isPremium"`
			} `json:"user"`
		}
		if json.Unmarshal(data, &payload) != nil || payload.User == nil {
			return ErrInvalidResponse
		}
		if !payload.User.Premium {
			return ErrAuth
		}
	} else {
		var payload struct {
			ID *json.Number `json:"id"`
		}
		if json.Unmarshal(data, &payload) != nil || payload.ID == nil {
			return ErrInvalidResponse
		}
		if id, err := payload.ID.Int64(); err != nil || id < 0 {
			return ErrInvalidResponse
		}
	}
	return nil
}
func (s *Service) RecordAddonRequest(origin, kind, mediaType, id string, started time.Time, err error) {
	item := newStremioHistory(mediaType, id)
	item.Origin = origin
	item.Kind = kind
	s.finishAddonRequest(item, started, err)
}

// TracePlayback retains safe provider diagnostics under the existing request,
// including failures. Neither keys nor the resolved download URL are stored.
func (s *Service) TracePlayback(ctx context.Context, origin, service, mediaType, id string, resolve func(context.Context) (string, error)) (string, error) {
	started := time.Now()
	item := newStremioHistory(mediaType, id)
	item.Origin, item.Kind = origin, "playback"
	if service == "alldebrid" || service == "torbox" {
		item.Service = service
	}
	h := &playbackCalls{}
	link, err := resolve(context.WithValue(ctx, providerHistoryKey{}, h))
	h.mu.Lock()
	item.Calls = append([]model.ProviderCall{}, h.calls...)
	h.mu.Unlock()
	s.finishAddonRequest(item, started, err)
	return link, err
}

func (s *Service) finishAddonRequest(item model.StremioRequest, started time.Time, err error) {
	item.DurationMS = time.Since(started).Milliseconds()
	item.Outcome = "results"
	if item.Kind == "manifest" {
		item.Resource = "Manifest"
		item.MediaType = "invalid"
	}
	if err != nil {
		item.Outcome = "failed"
		if errors.Is(err, ErrPending) {
			item.Outcome = "pending"
		}
		if errors.Is(err, model.ErrInvalid) {
			item.Outcome = "invalid"
		}
		if errors.Is(err, context.Canceled) {
			item.Outcome = "cancelled"
		}
	}
	s.saveStremioHistory(item)
}

// A plain magnet here never resolves administrator source credentials.
func publicMagnet(rec model.AIORecord) string {
	return "magnet:?xt=urn:btih:" + strings.ToLower(rec.Hash)
}

func (s *Service) cleanupPublicMagnet(ctx context.Context, key, hash string, item adMagnet, inventory []adMagnet) {
	if item.ID <= 0 || !strings.EqualFold(item.Hash, hash) {
		return
	}
	for _, prior := range inventory {
		if prior.ID == item.ID {
			return
		}
	}
	// Even an incomplete readiness response can return a confirmed new ID.
	// Compensate while the request still owns its key; never save a guest job.
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	if err := s.Providers.ADDelete(cleanup, key, item.ID); err != nil {
		s.recordPlaybackFailure("alldebrid", &playbackError{stage: "cleanup", cause: err})
	}
}
