package aiosource

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/vault"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const installationSecret = "aiosource_installation"

type Service struct {
	sources                   *providers.Registry
	DB                        *store.Store
	Vault                     *vault.Vault
	Providers                 *ProviderHTTP
	TMDBBase, ExportBase      string
	MetadataHTTP              *http.Client
	metadataRate              metadataRequestLimiter
	ctx                       context.Context
	cancel                    context.CancelFunc
	mu                        sync.Mutex
	wg                        sync.WaitGroup
	closed                    bool
	syncCancel, matchCancel   context.CancelFunc
	syncRunning, matchRunning bool
	lastError                 string
	flights                   map[string]*flight
	probes                    map[string]*adProbe
	slots                     chan struct{}
	downloadSlots             chan struct{}
	cleanupSlots              chan struct{}
	publicProbe               chan struct{}
	publicADMu                sync.Mutex
	publicADFlight            *flight
	automationWake            chan struct{}
}
type flight struct {
	warm   bool // Immutable admission state, shared by callers joining this check.
	done   chan struct{}
	err    error
	cancel context.CancelFunc
}

func New(ctx context.Context, db *store.Store, v *vault.Vault, sources *providers.Registry) (*Service, error) {
	if db == nil || v == nil {
		return nil, errors.New("AIOSources dependencies required")
	}
	life, cancel := context.WithCancel(ctx)
	s := &Service{DB: db, Vault: v, sources: sources, Providers: NewProviderHTTP(), TMDBBase: "https://api.themoviedb.org/3", ExportBase: "https://files.tmdb.org/p/exports", MetadataHTTP: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, ctx: life, cancel: cancel, flights: map[string]*flight{}, probes: map[string]*adProbe{}, slots: make(chan struct{}, 4), cleanupSlots: make(chan struct{}, 4)}
	release, locked, err := db.AIOLock(ctx, "installation")
	if err != nil {
		cancel()
		return nil, err
	}
	if !locked {
		cancel()
		return nil, model.ErrBusy
	}
	defer release()
	s.automationWake = make(chan struct{}, 1)
	s.downloadSlots = make(chan struct{}, 4)
	// Public cache checks must leave connection capacity for administration.
	s.publicProbe = make(chan struct{}, 1)
	if _, err = v.Resolve(ctx, installationSecret); errors.Is(err, model.ErrNotFound) {
		if _, err = s.RotateToken(ctx); err != nil {
			cancel()
			return nil, err
		}
	} else if err != nil {
		cancel()
		return nil, err
	}
	if err = db.RecoverOperations(ctx); err != nil {
		cancel()
		return nil, err
	}
	if err = s.migrateTrackerSecrets(ctx); err != nil {
		cancel()
		return nil, err
	}
	s.wg.Add(3)
	go s.retainHistory()
	go s.maintenance()
	go s.automation()
	return s, nil
}
func (s *Service) RotateToken(ctx context.Context) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	return token, s.Vault.Put(ctx, installationSecret, token)
}
func (s *Service) Token(ctx context.Context) (string, error) {
	return s.Vault.Resolve(ctx, installationSecret)
}
func (s *Service) Authorized(ctx context.Context, token string) bool {
	return s.ValidateToken(ctx, token) == nil
}

func (s *Service) ValidateToken(ctx context.Context, token string) error {
	if len(token) != 43 {
		return ErrAuth
	}
	expected, err := s.Token(ctx)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(expected), []byte(token)) != 1 {
		return ErrAuth
	}
	return nil
}
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		c, err := s.DB.AIOConfig(ctx)
		if err != nil {
			return err
		}
		if c.AllDebridSecret != "" {
			key, err := s.key(ctx, c.AllDebridSecret)
			if err != nil {
				return err
			}
			return s.RecoverCleanup(ctx, key, false)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) State() (bool, bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncRunning, s.matchRunning, s.lastError
}

var secretRefRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func (s *Service) SaveConfig(ctx context.Context, c model.AIOConfig) error {
	if c.SyncMinutes < 5 || c.SyncMinutes > 1440 || c.TMDBRequestsPerSecond < 5 || c.TMDBRequestsPerSecond > 40 || c.CacheMinutes < 5 || c.CacheMinutes > 43200 || c.ResponseSeconds < 2 || c.ResponseSeconds > 60 || len(c.Trackers) > 200 {
		return model.ErrInvalid
	}
	refs := []string{c.TMDBSecret, c.AllDebridSecret, c.TorBoxSecret}
	for id, t := range c.Trackers {
		if !providers.ValidID(id) {
			return model.ErrInvalid
		}
		if (t.Announce == "") != (t.PasskeySecret == "") {
			return model.ErrInvalid
		}
		if t.AnnounceSecret != "" && t.Announce != "" {
			return model.ErrInvalid
		}
		refs = append(refs, t.AnnounceSecret, t.PasskeySecret)
	}
	for _, name := range refs {
		if name == "" {
			continue
		}
		if !secretRefRE.MatchString(name) || name == installationSecret {
			return model.ErrInvalid
		}
		if _, err := s.Vault.Resolve(ctx, name); err != nil {
			return err
		}
	}
	if err := s.saveTrackerSecrets(ctx, c); err != nil {
		return err
	}
	s.wakeAutomation()
	return nil
}
func (s *Service) key(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	v, err := s.Vault.Resolve(ctx, name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}
func (s *Service) StartSync(ctx context.Context) error {
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	if c.TMDBSecret == "" {
		return model.ErrInvalid
	}
	if _, err = s.key(ctx, c.TMDBSecret); err != nil {
		return err
	}
	return s.startJob(true, func(ctx context.Context) error { return s.Sync(ctx, c) })
}
func (s *Service) StartMatch() error { return s.startMatch("manual") }
func (s *Service) startMatch(trigger string) error {
	return s.startJob(false, func(ctx context.Context) error { return s.match(ctx, trigger) })
}
func (s *Service) startJob(syncJob bool, run func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.ErrConflict
	}
	if (syncJob && s.syncRunning) || (!syncJob && s.matchRunning) {
		return model.ErrBusy
	}
	ctx, cancel := context.WithCancel(s.ctx)
	if syncJob {
		s.syncRunning = true
		s.syncCancel = cancel
	} else {
		s.matchRunning = true
		s.matchCancel = cancel
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := run(ctx)
		cancel()
		s.mu.Lock()
		if syncJob {
			s.syncRunning = false
			s.syncCancel = nil
		} else {
			s.matchRunning = false
			s.matchCancel = nil
		}
		// Information failures are retained in PostgreSQL. A successful local
		// identification pass must never erase an unrelated TMDB failure.
		if !syncJob && err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, model.ErrBusy) {
			s.lastError = "The operation failed. Check credentials, network and database status."
		} else if !syncJob && err == nil {
			s.lastError = ""
		}
		s.mu.Unlock()
		if err == nil {
			s.wakeAutomation()
		}
	}()
	return nil
}
func (s *Service) Stop(syncJob, matchJob bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if syncJob && s.syncCancel != nil {
		s.syncCancel()
	}
	if matchJob && s.matchCancel != nil {
		s.matchCancel()
	}
}
func (s *Service) Reset(ctx context.Context, scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || (scope != "cache" && (s.syncRunning || s.matchRunning)) {
		return model.ErrBusy
	}
	var releases []func()
	if scope == "metadata" || scope == "mapping" {
		names := []string{"matcher"}
		if scope == "metadata" {
			names = append(names, "metadata")
		}
		for _, name := range names {
			release, locked, err := s.DB.AIOLock(ctx, name)
			if err != nil || !locked {
				for _, release := range releases {
					release()
				}
				if err != nil {
					return err
				}
				return model.ErrBusy
			}
			releases = append(releases, release)
		}
		defer func() {
			for _, release := range releases {
				release()
			}
		}()
	}
	switch scope {
	case "mapping":
		return s.DB.ResetAIOMapping(ctx)
	case "metadata":
		return s.DB.ResetAIOMetadata(ctx)
	case "cache":
		for _, f := range s.flights {
			f.cancel()
		}
		return s.DB.ResetAIOCache(ctx)
	default:
		return model.ErrInvalid
	}
}
func (s *Service) Match(ctx context.Context) error {
	return s.match(ctx, "manual")
}
func (s *Service) match(ctx context.Context, trigger string) (matchErr error) {
	release, locked, err := s.DB.AIOLock(ctx, "matcher")
	if err != nil {
		return err
	}
	if !locked {
		return model.ErrBusy
	}
	defer release()
	history, err := s.beginOperation(ctx, "matching", "local", trigger, "", "matcher")
	if err != nil {
		return err
	}
	defer func() { history.finish(ctx, matchErr) }()
	history.checkpoint(ctx, "identification")
	return s.drainMatching(ctx, history)
}

func (s *Service) maintenance() {
	defer s.wg.Done()
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}
		c, err := s.DB.AIOConfig(s.ctx)
		if err != nil {
			continue
		}
		_ = s.DB.RecoverOperations(s.ctx)
		if c.AllDebridSecret != "" {
			key, e := s.key(s.ctx, c.AllDebridSecret)
			if e == nil {
				_ = s.RecoverCleanup(s.ctx, key, false)
			}
		}
		for service, name := range map[string]string{"alldebrid": c.AllDebridSecret, "torbox": c.TorBoxSecret} {
			if name == "" {
				continue
			}
			key, e := s.key(s.ctx, name)
			if e != nil {
				continue
			}
			resources, e := s.DB.AIOResources(s.ctx, service, fingerprint(key), time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
			if e != nil {
				continue
			}
			for _, r := range resources {
				warm := freshResource(r, r.Hashes, time.Now(), time.Duration(c.CacheMinutes)*time.Minute)
				_, _ = s.cacheFlightTriggered(service, key, r.Key, r.Hashes, c, "automatic", warm)
			}
		}
	}
}
func (s *Service) observe(service string, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancel()
	_ = s.DB.AIOIncident(ctx, service, err != nil)
}
