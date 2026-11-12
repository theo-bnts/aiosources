package aiosource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

const publicInstallationPurpose = "public-installation-v1"

// MaxPublicInstallationSize bounds encrypted links and setup request bodies.
const MaxPublicInstallationSize = 8192

type PublicProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Adding a provider requires an adapter; unrecognized URL fields never become endpoints.
var PublicProviders = func() []PublicProvider {
	providers := []PublicProvider{}
	for _, provider := range model.DebridProviders() {
		providers = append(providers, PublicProvider{provider.ID, provider.Name})
	}
	return providers
}()

type PublicInstallation struct {
	Trusted           *model.TrustedGrant     `json:"-"`
	Preferences       model.StreamPreferences `json:"-"`
	Version           int                     `json:"version"`
	Generation        int64                   `json:"generation"`
	StremioGeneration int64                   `json:"stremio_generation,omitempty"`
	Audience          string                  `json:"audience"`
	Services          map[string]string       `json:"services"`
	Announces         map[string]string       `json:"announces,omitempty"`
}

// This list contains source names, never a source definition or secret
// reference. Unclassified sources are private, as in playback and the settings UI.
type PublicPrivateSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Service) publicPrivateSources(c model.AIOConfig) ([]PublicPrivateSource, error) {
	out := []PublicPrivateSource{}
	if s.sources == nil {
		return out, nil
	}
	sources, err := s.sources.List()
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if tracker, configured := c.Trackers[source.ID]; source.Valid && (!configured || tracker.Private) {
			name := strings.TrimSpace(source.Name)
			if name == "" {
				name = source.ID
			}
			out = append(out, PublicPrivateSource{ID: source.ID, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a == b {
			return out[i].ID < out[j].ID
		}
		return a < b
	})
	return out, nil
}

// PublicSources returns source names for enabled public features, never definitions.
func (s *Service) PublicSources(ctx context.Context) ([]PublicPrivateSource, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return nil, err
	}
	if !access.Enabled || s.sources == nil {
		return nil, nil
	}
	list, err := s.sources.List()
	if err != nil {
		return nil, nil
	}
	out := []PublicPrivateSource{}
	for _, source := range list {
		if source.Valid {
			out = append(out, PublicPrivateSource{ID: source.ID, Name: source.Name})
		}
	}
	return out, nil
}

func (s *Service) PublicPrivateSources(ctx context.Context) ([]PublicPrivateSource, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return nil, err
	}
	if !access.Enabled {
		return nil, nil
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := s.publicPrivateSources(c)
	if err != nil {
		// The optional list cannot authorize an uncached download when source
		// definitions are unavailable. Keep cached-only setup usable, as in
		// visitorAnnounces; validation of supplied announces still fails closed.
		return nil, nil
	}
	return sources, nil
}

func validPublicAnnounces(announces map[string]string) bool {
	for source, value := range announces {
		if source == "" || validateAnnounce(value) != nil {
			return false
		}
	}
	return true
}

func (s *Service) validatePublicAnnounces(ctx context.Context, access model.PublicAccess, announces map[string]string) error {
	if len(announces) == 0 {
		return nil
	}
	if !validPublicAnnounces(announces) {
		return model.ErrInvalid
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	sources, err := s.publicPrivateSources(c)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, source := range sources {
		allowed[source.ID] = true
	}
	for source := range announces {
		if !allowed[source] {
			return model.ErrInvalid
		}
	}
	return nil
}

// Re-read permissions for every stream/playback request. Instance credentials
// are resolved only under a live grant; encrypted URLs never carry their values.
func (s *Service) visitorAnnounces(ctx context.Context, c model.AIOConfig, visitor *PublicInstallation) (map[string]string, error) {
	if visitor == nil {
		return nil, nil
	}
	var access model.PublicAccess
	var err error
	if visitor.Trusted == nil {
		access, err = s.DB.PublicAccess(ctx)
		if err != nil {
			return nil, err
		}
		// A bare-hash cache lookup requires no tracker credentials at all.
		if !access.UseInstanceAnnounces && len(visitor.Announces) == 0 {
			return nil, nil
		}
		if !access.Enabled || access.Generation != visitor.Generation {
			return nil, ErrAuth
		}
	} else {
		access, err = s.visitorCredentialAccess(ctx, visitor)
		if err != nil {
			return nil, err
		}
	}
	if access.UseInstanceAnnounces {
		announces := map[string]string{}
		for id, tracker := range c.Trackers {
			if !tracker.Private || tracker.AnnounceSecret == "" {
				continue
			}
			value, e := s.key(ctx, tracker.AnnounceSecret)
			if e != nil {
				clear(announces)
				return nil, e
			}
			announces[id] = value
		}
		return announces, nil
	}
	if !validPublicAnnounces(visitor.Announces) {
		return nil, ErrAuth
	}
	if len(visitor.Announces) == 0 {
		return nil, nil
	}

	sources, err := s.publicPrivateSources(c)
	if err != nil {
		// An unreadable definition cannot authorize an uncached download, but
		// must not prevent existing cached playback through an encrypted link.
		return nil, nil
	}
	allowed := map[string]string{}
	for _, source := range sources {
		if value := visitor.Announces[source.ID]; value != "" {
			allowed[source.ID] = value
		}
	}
	return allowed, nil
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
		if !supported || len(key) < 8 || len(key) > 512 || (service == "alldebrid" && allDebridDemoKey(key)) {
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
func (s *Service) CreatePublicInstallation(ctx context.Context, audience string, keys, announces map[string]string) (string, error) {
	return s.createPublicInstallation(ctx, audience, keys, announces, true)
}

// Search uses the same credential policy without granting an addon connection
// when Stremio has been disabled independently.
func (s *Service) CreatePublicSearchCredential(ctx context.Context, audience string, keys, announces map[string]string) (string, error) {
	return s.createPublicInstallation(ctx, audience, keys, announces, false)
}

func (s *Service) createPublicInstallation(ctx context.Context, audience string, keys, announces map[string]string, stremio bool) (string, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return "", err
	}
	if !access.Enabled || stremio && !access.StremioEnabled || !stremio && !access.SearchEnabled {
		return "", model.ErrNotFound
	}
	if (!access.UseInstanceDebrid && !validPublicKeys(keys) || len(keys) > 0 && !validPublicKeys(keys)) || audience == "" {
		return "", model.ErrInvalid
	}
	if err = s.validatePublicAnnounces(ctx, access, announces); err != nil {
		return "", err
	}
	for _, provider := range PublicProviders {
		if key, ok := keys[provider.ID]; ok && !access.UseInstanceDebrid {
			if err = s.Providers.ValidateKey(ctx, provider.ID, key); err != nil {
				return "", err
			}
		}
	}
	// Provider validation may take seconds. Do not create a new link after public
	// access or the optional source permission was withdrawn during those calls.
	latest, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return "", err
	}
	if !latest.Enabled || latest.Generation != access.Generation {
		return "", model.ErrNotFound
	}
	if stremio && (!latest.StremioEnabled || latest.StremioGeneration != access.StremioGeneration) || !stremio && !latest.SearchEnabled {
		return "", model.ErrNotFound
	}
	if err = s.validatePublicAnnounces(ctx, latest, announces); err != nil {
		return "", err
	}
	if access.UseInstanceDebrid {
		keys = nil
	}
	if access.UseInstanceAnnounces {
		announces = nil
	}
	installation := PublicInstallation{Version: 1, Generation: access.Generation, Audience: audience, Services: keys, Announces: announces}
	if stremio {
		installation.StremioGeneration = access.StremioGeneration
	}
	return s.sealPublicInstallation(ctx, installation)
}

// Old installation links had no protocol counter. They remain valid only until
// the first Stremio policy change. Browser credentials deliberately ignore it.
func (s *Service) CheckPublicStremio(ctx context.Context, visitor *PublicInstallation) error {
	if visitor != nil && visitor.Trusted != nil {
		_, err := s.visitorAccess(ctx, visitor, "stremio")
		return err
	}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return err
	}
	if visitor == nil || !access.Enabled || !access.StremioEnabled || visitor.Generation != access.Generation ||
		visitor.StremioGeneration != access.StremioGeneration && !(visitor.StremioGeneration == 0 && access.StremioGeneration == 1) {
		return ErrAuth
	}
	return nil
}

func (s *Service) sealPublicInstallation(ctx context.Context, installation PublicInstallation) (string, error) {
	data, err := json.Marshal(installation)
	if err != nil {
		return "", model.ErrInvalid
	}
	defer clear(data)
	sealed, err := s.Vault.Seal(publicInstallationPurpose, data)
	if err != nil {
		return "", err
	}
	configuration := base64.RawURLEncoding.EncodeToString(sealed)
	if err := s.CheckPublicConfigurationSize(ctx, "stremio", configuration); err != nil {
		return "", err
	}
	return configuration, nil
}

func (s *Service) CheckPublicConfigurationSize(ctx context.Context, format, configuration string) error {
	if format != "stremio" && format != "torznab" {
		return model.ErrInvalid
	}
	limit, err := s.DB.RuntimeValue(ctx, "payloads", "public_"+format+"_bytes")
	if err != nil {
		return err
	}
	if len(configuration) > limit {
		return model.ErrConfigurationLimit
	}
	return nil
}

func (s *Service) OpenPublicInstallation(ctx context.Context, audience, value string) (*PublicInstallation, error) {
	visitor, err := s.openPublicCredential(ctx, audience, value, true)
	if err != nil {
		return nil, err
	}
	access, err := s.visitorCredentialAccess(ctx, visitor)
	if err == nil {
		err = s.resolveVisitorServices(ctx, visitor, access.CredentialPolicy)
	}
	if err != nil {
		clear(visitor.Services)
		clear(visitor.Announces)
		return nil, err
	}
	return visitor, nil
}

func (s *Service) openPublicCredential(ctx context.Context, audience, value string, requireServices bool) (*PublicInstallation, error) {
	if len(value) < 32 || len(value) > MaxPublicInstallationSize {
		return nil, ErrAuth
	}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return nil, err
	}
	if !access.Enabled {
		return nil, ErrAuth
	}
	credential, preferences, err := InstallationPreferences(value)
	if err != nil {
		return nil, ErrAuth
	}
	sealed, err := base64.RawURLEncoding.DecodeString(credential)
	if err != nil {
		return nil, ErrAuth
	}
	data, err := s.Vault.Open(publicInstallationPurpose, sealed)
	if err != nil {
		return nil, ErrAuth
	}
	defer clear(data)
	var out PublicInstallation
	if json.Unmarshal(data, &out) != nil || out.Version != 1 || out.Generation != access.Generation || out.Audience != audience || (requireServices && !access.UseInstanceDebrid || len(out.Services) > 0) && !validPublicKeys(out.Services) || !validPublicAnnounces(out.Announces) {
		clear(out.Services)
		clear(out.Announces)
		return nil, ErrAuth
	}
	out.Preferences = preferences
	return &out, nil
}
func (s *Service) PublicStreams(ctx context.Context, base, configuration, mediaType, id string, visitor *PublicInstallation) ([]Stream, error) {
	if visitor == nil {
		return nil, ErrAuth
	}
	if visitor.Trusted == nil && !validPublicKeys(visitor.Services) {
		access, err := s.visitorCredentialAccess(ctx, visitor)
		// A live instance-credential grant remains valid while no service is
		// configured. Return an empty result rather than invalidating its URL.
		if err != nil || !access.UseInstanceDebrid || len(visitor.Services) != 0 {
			return nil, ErrAuth
		}
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
	if service == "deepbrid" {
		generation, e := s.DB.AIOCacheGeneration(ctx)
		if e != nil {
			return nil, e
		}
		pending := []string{}
		for _, hash := range missing {
			item := s.visitorPreparation(service, key, hash)
			if len(item.Files) > 0 && item.Generation == generation {
				states[hash] = model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Files: item.Files}
			} else if item.ID != "" {
				pending = append(pending, hash)
			}
		}
		if len(pending) > 0 {
			check, cancel := context.WithTimeout(ctx, time.Duration(c.ResponseSeconds)*time.Second)
			observed, _ := s.checkPublicService(check, service, key, resource, pending)
			cancel()
			for _, item := range observed {
				states[item.Hash] = item
			}
		}

	} else {
		check, cancel := context.WithTimeout(ctx, time.Duration(c.ResponseSeconds)*time.Second)
		observed, _ := s.checkPublicService(check, service, key, resource, missing)
		cancel()
		for _, item := range observed {
			states[item.Hash] = item
		}
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

func (s *Service) checkPublicTorBox(ctx context.Context, key, resource string, hashes []string) error {
	_, err := s.checkPublicService(ctx, "torbox", key, resource, hashes)
	return err
}
func (p *ProviderHTTP) ValidateKey(ctx context.Context, service, key string) error {
	path := "user/me?settings=false"
	if service == "alldebrid" {
		path = "v4/user"
	} else if service != "torbox" {
		return p.validateExtendedKey(ctx, service, key)
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
	if model.IsDebridService(service) {
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

// Public magnets never resolve administrator source credentials. A visitor's
// own announce is included only after the current uncached policy allows it.
func publicMagnet(rec model.AIORecord, announce string) string {
	magnet := "magnet:?xt=urn:btih:" + strings.ToLower(rec.Hash)
	if announce != "" {
		magnet += "&tr=" + strings.ReplaceAll(url.QueryEscape(announce), "+", "%20")
	}
	return magnet
}

func createdADMagnet(hash string, item adMagnet, inventory []adMagnet) bool {
	if item.ID <= 0 || !strings.EqualFold(item.Hash, hash) {
		return false
	}
	for _, prior := range inventory {
		if prior.ID == item.ID {
			return false
		}
	}
	return true
}

func (s *Service) cleanupCachedOnlyMagnet(ctx context.Context, key, hash string, item adMagnet, inventory []adMagnet, administrator bool) error {
	if !createdADMagnet(hash, item, inventory) {
		return nil
	}
	// Even an incomplete readiness response can return a confirmed new ID.
	// The administrator's configured account can recover cleanup after restart.
	// Visitor jobs stay ephemeral because their credentials are never retained.
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	if administrator {
		if err := s.DB.AddAIOCleanup(cleanup, model.AIOCleanup{Account: fingerprint(key), Hash: hash, ID: item.ID}); err == nil {
			if err = s.cleanupHash(cleanup, key, hash, false); err != nil {
				s.recordPlaybackFailure("alldebrid", &playbackError{stage: "cleanup", cause: err})
			}
			return err
		}
		// If ownership could not be saved, still compensate directly while its
		// identity is confirmed, as for an ephemeral visitor request.
	}
	if err := s.Providers.ADDelete(cleanup, key, item.ID); err != nil {
		s.recordPlaybackFailure("alldebrid", &playbackError{stage: "cleanup", cause: err})
		return err
	}
	return nil
}
