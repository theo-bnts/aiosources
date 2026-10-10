// Package jobs coordinates durable, resumable provider collections.
package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
)

var (
	errPersistence   = errors.New("collection persistence is unavailable")
	errSecrets       = fmt.Errorf("%w: a required secret is unavailable", model.ErrInvalid)
	errCancelled     = errors.New("collection cancellation requested")
	errPaused        = errors.New("collection manual pause requested")
	errQuotaPaused   = errors.New("collection quota wait exceeds the pause threshold")
	errDuration      = errors.New("collection attempt duration budget reached")
	errQuotaRetries  = errors.New("source rate limit reached; automatic retry budget exhausted")
	errNoProgress    = errors.New("collection useful-progress threshold reached")
	errShutdown      = errors.New("collection manager is shutting down")
	errNotifications = errors.New("collection change subscription was interrupted")
)

// Notice deliberately contains identifiers only, never source data or credentials.
type Notice struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type activeAttempt struct {
	cancel context.CancelCauseFunc
}

type Manager struct {
	db             *store.Store
	registry       *providers.Registry
	resolver       model.SecretResolver
	initialWorkers int

	lifecycle   sync.Mutex
	mu          sync.Mutex
	started     bool
	closed      bool
	connected   bool
	ctx         context.Context
	cancel      context.CancelCauseFunc
	failure     error
	workers     sync.WaitGroup
	done        chan struct{}
	work        chan struct{}
	schedule    chan struct{}
	recovery    chan struct{}
	active      map[string]*activeAttempt
	subscribers map[chan Notice]struct{}
}

func New(db *store.Store, registry *providers.Registry, resolver model.SecretResolver, initialWorkers int) *Manager {
	if initialWorkers < 1 {
		initialWorkers = 1
	}
	return &Manager{
		db: db, registry: registry, resolver: resolver, initialWorkers: initialWorkers,
		done: make(chan struct{}), work: make(chan struct{}), schedule: make(chan struct{}, 1),
		recovery: make(chan struct{}, 1),
		active:   make(map[string]*activeAttempt), subscribers: make(map[chan Notice]struct{}),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	if m.started || m.closed {
		m.mu.Unlock()
		return fmt.Errorf("%w: collection manager cannot be started twice", model.ErrConflict)
	}
	m.mu.Unlock()
	if m.db == nil || m.registry == nil {
		return fmt.Errorf("%w: collection manager dependencies are missing", model.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, cancel := context.WithCancelCause(ctx)
	changes, unsubscribe, err := m.db.SubscribeChanges(root)
	if err != nil {
		cancel(errPersistence)
		return errPersistence
	}
	if err := m.recoverInterrupted(root); err != nil {
		cancel(errPersistence)
		stopSubscription(changes, unsubscribe)
		return errPersistence
	}
	if err := m.db.InitializeCollectionSettings(root, m.initialWorkers); err != nil {
		cancel(errPersistence)
		stopSubscription(changes, unsubscribe)
		return errPersistence
	}
	definitions, stopDefinitions, err := m.registry.SubscribeChanges(root)
	if err != nil {
		cancel(err)
		stopSubscription(changes, unsubscribe)
		return err
	}
	m.mu.Lock()
	m.ctx, m.cancel = root, cancel
	m.started, m.connected = true, true
	m.mu.Unlock()
	// One dispatcher starts only durably admitted attempts. Its wait-group
	// entry keeps the counter nonzero while it adds attempt goroutines.
	m.workers.Add(4)
	go m.worker()
	go m.observeChanges(changes, unsubscribe)
	go m.observeDefinitions(definitions, stopDefinitions)
	go m.scheduler()
	go func() {
		m.workers.Wait()
		m.mu.Lock()
		m.closed = true
		for subscriber := range m.subscribers {
			close(subscriber)
			delete(m.subscribers, subscriber)
		}
		close(m.done)
		m.mu.Unlock()
	}()
	return nil
}

// Done closes after all workers and their source requests have stopped.
func (m *Manager) Done() <-chan struct{} { return m.done }

// Close does not close the shared database or registry. The caller owns them.
// A deadline only bounds the caller's wait; cleanup continues until held locks
// and connector transports are released.
func (m *Manager) Close(ctx context.Context) error {
	m.lifecycle.Lock()
	m.mu.Lock()
	if !m.started && !m.closed {
		m.closed = true
		for subscriber := range m.subscribers {
			close(subscriber)
			delete(m.subscribers, subscriber)
		}
		close(m.done)
	}
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel(errShutdown)
	}
	m.lifecycle.Unlock()
	select {
	case <-m.done:
		m.mu.Lock()
		err := m.failure
		m.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Enqueue(ctx context.Context, req model.StartRun) (model.Run, error) {
	if err := m.accepting(); err != nil {
		return model.Run{}, err
	}
	mode := req.Mode
	if mode == "" {
		mode = model.ModePreview
	}
	if mode != model.ModePreview && mode != model.ModeIncremental && mode != model.ModeFull && mode != model.ModeMetadata {
		return model.Run{}, fmt.Errorf("%w: unsupported collection mode", model.ErrInvalid)
	}
	if req.KnownPages != nil && (mode != model.ModeIncremental || *req.KnownPages < 0 || *req.KnownPages > 10000) {
		return model.Run{}, fmt.Errorf("%w: known_pages is only valid for Incremental and must be between 0 and 10000", model.ErrInvalid)
	}
	settings, err := m.db.CollectionSettings(ctx)
	if err != nil {
		return model.Run{}, publicStoreError(err)
	}
	maxPages := 0
	if mode == model.ModePreview {
		maxPages = model.DefaultPreviewPages
	}
	if req.MaxPages != nil {
		maxPages = *req.MaxPages
	}
	if maxPages < 0 || maxPages > 10000 {
		return model.Run{}, fmt.Errorf("%w: max_pages must be between 0 and 10000", model.ErrInvalid)
	}
	policy := settings.Policy()
	if req.MaxDurationSeconds != nil {
		policy.MaxDurationSeconds = *req.MaxDurationSeconds
	}
	if policy.MaxDurationSeconds < 0 || policy.MaxDurationSeconds > 604800 {
		return model.Run{}, fmt.Errorf("%w: max_duration_seconds must be between 0 and 604800", model.ErrInvalid)
	}
	var run model.Run
	err = m.withDefinition(req.ProviderID, func(doc model.ProviderDocument) error {
		if mode == model.ModeMetadata && !doc.Provider.SupportsMetadata() {
			return fmt.Errorf("%w: Metadata requires a native JSON source with configured detail fields", model.ErrInvalid)
		}
		if _, err := m.secrets(ctx, doc.Provider); err != nil {
			return err
		}
		config, err := snapshot(doc.Provider)
		if err != nil {
			return fmt.Errorf("%w: provider snapshot cannot be encoded", model.ErrInvalid)
		}
		if req.KnownPages != nil {
			config.Schedule.KnownPages = *req.KnownPages
		}
		if mode != model.ModePreview && req.MaxPages == nil {
			maxPages = config.Schedule.MaxPages
			if maxPages < 0 || maxPages > 10000 {
				return fmt.Errorf("%w: source max_pages must be between 0 and 10000", model.ErrInvalid)
			}
		}
		input := model.Run{
			ProviderID: config.ID, ProviderName: config.Name, Mode: mode,
			Status: model.StatusQueued, MaxPages: maxPages, Revision: doc.Revision, Config: config, Policy: &policy,
		}
		settings.ApplyDefaults(&input)
		if err := connectors.Validate(input.Config); err != nil {
			return fmt.Errorf("%w: requested collection settings are not compatible with the provider", model.ErrInvalid)
		}
		run, err = m.db.EnqueueRun(ctx, input)
		if err != nil {
			return publicStoreError(err)
		}
		return nil
	})
	if err != nil {
		return model.Run{}, err
	}
	m.signalWork()
	m.signalSchedule()
	m.Notify("run", run.ID)
	return run, nil
}

func (m *Manager) Pause(ctx context.Context, id string) (model.Run, error) {
	if err := m.accepting(); err != nil {
		return model.Run{}, err
	}
	run, err := m.db.RequestPause(ctx, id)
	if err != nil {
		return model.Run{}, publicStoreError(err)
	}
	// Do not cancel the attempt: its current observation and checkpoint must
	// commit before the durable hold is acknowledged as paused.
	m.Notify("run", id)
	m.signalWork()
	m.signalSchedule()
	return run, nil
}

func (m *Manager) Cancel(ctx context.Context, id string) (model.Run, error) {
	if err := m.accepting(); err != nil {
		return model.Run{}, err
	}
	m.mu.Lock()
	attempt := m.active[id]
	m.mu.Unlock()
	run, err := m.db.RequestCancel(ctx, id)
	if err != nil {
		return model.Run{}, publicStoreError(err)
	}
	if run.CancelRequested || run.Status == model.StatusCancelled {
		if attempt != nil {
			attempt.cancel(errCancelled)
		}
	}
	m.Notify("run", id)
	m.signalWork()
	m.signalSchedule()
	return run, nil
}

func (m *Manager) Resume(ctx context.Context, id string) (model.Run, error) {
	if err := m.accepting(); err != nil {
		return model.Run{}, err
	}
	run, err := m.db.GetRun(ctx, id)
	if err != nil {
		return model.Run{}, publicStoreError(err)
	}
	if run.Status != model.StatusPaused && run.Status != model.StatusFailed && run.Status != model.StatusCancelled {
		return model.Run{}, fmt.Errorf("%w: only paused, failed or cancelled collections can resume", model.ErrConflict)
	}
	err = m.withDefinition(run.ProviderID, func(model.ProviderDocument) error {
		if err := connectors.Validate(run.Config); err != nil {
			return fmt.Errorf("%w: saved provider configuration is not valid", model.ErrInvalid)
		}
		if _, err := m.secrets(ctx, run.Config); err != nil {
			return err
		}
		var err error
		run, err = m.db.ResumeRun(ctx, id)
		if err != nil {
			return publicStoreError(err)
		}
		return nil
	})
	if err != nil {
		return model.Run{}, err
	}
	m.signalWork()
	m.signalSchedule()
	m.Notify("run", id)
	return run, nil
}

func (m *Manager) Subscribe() (<-chan Notice, func()) {
	ch := make(chan Notice, 32)
	m.mu.Lock()
	if m.closed {
		close(ch)
	} else {
		m.subscribers[ch] = struct{}{}
	}
	m.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			m.mu.Lock()
			if _, ok := m.subscribers[ch]; ok {
				delete(m.subscribers, ch)
				close(ch)
			}
			m.mu.Unlock()
		})
	}
}

// Notify also invalidates the scheduler when editable definitions or secrets change.
func (m *Manager) Notify(kind, id string) {
	m.mu.Lock()
	for ch := range m.subscribers {
		select {
		case ch <- Notice{Kind: kind, ID: id}:
		default:
		}
	}
	m.mu.Unlock()
	if kind == "provider" || kind == "providers" || kind == "secret" || kind == "secrets" {
		m.signalSchedule()
	}
}

func (m *Manager) accepting() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || (m.ctx != nil && m.ctx.Err() != nil) {
		return fmt.Errorf("%w: collection manager is shutting down", model.ErrConflict)
	}
	return nil
}

func (m *Manager) withDefinition(id string, fn func(model.ProviderDocument) error) error {
	if !providers.ValidID(id) {
		return fmt.Errorf("%w: provider definition is unavailable", model.ErrInvalid)
	}
	selected := false
	err := m.registry.WithDefinitions(func(documents map[string]model.ProviderDocument) error {
		selected = true
		doc, ok := documents[id]
		if !ok {
			return model.ErrNotFound
		}
		if err := validateDefinition(id, doc); err != nil {
			return err
		}
		return fn(doc)
	})
	if err != nil && !selected {
		return fmt.Errorf("%w: provider definition is unavailable", model.ErrInvalid)
	}
	return err
}

func validateDefinition(id string, doc model.ProviderDocument) error {
	if len(doc.Issues) != 0 || doc.Provider.ID != id || doc.Revision == "" {
		return fmt.Errorf("%w: provider definition must be repaired before collecting", model.ErrInvalid)
	}
	if !doc.Provider.Enabled {
		return fmt.Errorf("%w: provider is disabled", model.ErrInvalid)
	}
	if err := connectors.Validate(doc.Provider); err != nil {
		return fmt.Errorf("%w: provider configuration is not valid", model.ErrInvalid)
	}
	return nil
}

func (m *Manager) recoverInterrupted(ctx context.Context) error {
	return m.registry.WithDefinitions(func(documents map[string]model.ProviderDocument) error {
		eligible := make(map[string]bool, len(documents))
		for id, doc := range documents {
			if validateDefinition(id, doc) == nil {
				eligible[id] = true
			}
		}
		return m.db.RecoverInterrupted(ctx, eligible)
	})
}

func snapshot(p model.Provider) (model.Provider, error) {
	encoded, err := json.Marshal(p)
	if err != nil {
		return model.Provider{}, err
	}
	var frozen model.Provider
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&frozen); err != nil {
		return model.Provider{}, err
	}
	return frozen, nil
}

// Resolve once per attempt so a rotation cannot produce mixed credentials in a
// client. Nothing in this map is serialized into a run, an event or an error.
func (m *Manager) secrets(ctx context.Context, p model.Provider) (model.SecretResolver, error) {
	names, err := p.SecretReferences()
	if err != nil {
		return nil, fmt.Errorf("%w: a required secret reference is missing", model.ErrInvalid)
	}
	values := make(map[string]string, len(names))
	for _, name := range names {
		if m.resolver == nil {
			return nil, errSecrets
		}
		value, err := m.resolver(ctx, name)
		if err != nil || value == "" {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errSecrets
		}
		values[name] = value
	}
	return func(ctx context.Context, name string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		value, ok := values[name]
		if !ok {
			return "", errSecrets
		}
		return value, nil
	}, nil
}

func publicStoreError(err error) error {
	for _, known := range []error{model.ErrNotFound, model.ErrBusy, model.ErrConflict, model.ErrInvalid, model.ErrStalled, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return known
		}
	}
	return errPersistence
}

func (m *Manager) signalWork() {
	m.mu.Lock()
	close(m.work)
	m.work = make(chan struct{})
	m.mu.Unlock()
}

func (m *Manager) signalSchedule() {
	select {
	case m.schedule <- struct{}{}:
	default:
	}
}

func (m *Manager) fail(err error) {
	m.mu.Lock()
	if m.failure == nil {
		m.failure = err
	}
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel(err)
	}
}

func (m *Manager) observeChanges(changes <-chan struct{}, unsubscribe func()) {
	defer m.workers.Done()
	defer func() { stopSubscription(changes, unsubscribe) }()
	backoff := 250 * time.Millisecond
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.recovery:
			if err := m.recoverInterrupted(m.ctx); err != nil {
				if m.ctx.Err() == nil {
					m.fail(errPersistence)
				}
				return
			}
		case _, ok := <-changes:
			if !ok {
				m.mu.Lock()
				m.connected = false
				for _, attempt := range m.active {
					attempt.cancel(errNotifications)
				}
				m.mu.Unlock()
				unsubscribe()
				for {
					// Reconnection has bounded backoff, including connections
					// that succeed then immediately drop. This is not polling.
					timer := time.NewTimer(backoff)
					select {
					case <-timer.C:
					case <-m.ctx.Done():
						timer.Stop()
						return
					}
					nextChanges, nextUnsubscribe, err := m.db.SubscribeChanges(m.ctx)
					if err == nil {
						changes, unsubscribe = nextChanges, nextUnsubscribe
						if err = m.recoverInterrupted(m.ctx); err == nil {
							m.mu.Lock()
							m.connected = true
							m.mu.Unlock()
							backoff = 250 * time.Millisecond
							break
						}
						stopSubscription(changes, unsubscribe)
					}
					if backoff < 15*time.Second {
						backoff *= 2
						if backoff > 15*time.Second {
							backoff = 15 * time.Second
						}
					}
				}
			}
		}
		m.reconcileCancellations()
		m.signalWork()
		m.signalSchedule()
		// Other processes may have changed any run. Empty ID invalidates lists.
		m.Notify("run", "")
	}
}

func stopSubscription(changes <-chan struct{}, unsubscribe func()) {
	unsubscribe()
	// Store closes this channel after closing the dedicated LISTEN connection.
	for range changes {
	}
}

func (m *Manager) reconcileCancellations() {
	m.mu.Lock()
	active := make(map[string]context.CancelCauseFunc, len(m.active))
	for id, attempt := range m.active {
		active[id] = attempt.cancel
	}
	m.mu.Unlock()
	for id, cancel := range active {
		run, err := m.db.GetRun(m.ctx, id)
		if err != nil {
			cancel(errPersistence)
		} else if run.CancelRequested || run.Status != model.StatusRunning {
			cancel(errCancelled)
		}
	}
}
