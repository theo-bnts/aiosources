// Package store persists ingestion checkpoints, observations and catalogue fields in PostgreSQL.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/theo-bnts/aiosources/internal/model"
)

const notificationChannel = "ingest_run_changes"

type Store struct {
	pool           *pgxpool.Pool
	collectionPool *pgxpool.Pool
	mu             sync.Mutex
	leases         map[string]*runLease
	claimSlots     chan struct{}
	closed         bool
	background     context.Context
	cancel         context.CancelFunc
	listeners      sync.WaitGroup
}

type runLease struct {
	mu   sync.Mutex
	conn *pgxpool.Conn
	key  int64
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid database connection configuration")
	}
	// Short queries retain their own budget, independently of collection leases.
	if config.MaxConns < 4 {
		config.MaxConns = 4
	}
	if config.ConnConfig.ConnectTimeout == 0 {
		config.ConnConfig.ConnectTimeout = 10 * time.Second
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, databaseError("open database", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, databaseError("connect database", err)
	}
	// A run retains its session advisory lock until it finishes. Keeping these
	// sessions separate supports the entire saved 1-32 range even on small
	// machines, without occupying API, scheduling or recovery connections.
	// Open connections only as imports need them, never all 32 at startup.
	collectionConfig := config.Copy()
	collectionConfig.MaxConns = model.MaxCollectionWorkers
	collectionConfig.MinConns, collectionConfig.MinIdleConns = 0, 0
	collectionPool, err := pgxpool.NewWithConfig(ctx, collectionConfig)
	if err != nil {
		pool.Close()
		return nil, databaseError("open collection sessions", err)
	}
	background, cancel := context.WithCancel(context.Background())
	return &Store{pool: pool, collectionPool: collectionPool, leases: make(map[string]*runLease), claimSlots: make(chan struct{}, model.MaxCollectionWorkers), background: background, cancel: cancel}, nil
}

func (s *Store) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	leases := make(map[string]*runLease, len(s.leases))
	for id, lease := range s.leases {
		leases[id] = lease
	}
	s.mu.Unlock()
	for id, lease := range leases {
		lease.mu.Lock()
		s.releaseLease(id, lease)
		lease.mu.Unlock()
	}
	s.listeners.Wait()
	s.collectionPool.Close()
	s.pool.Close()
}

// SubscribeChanges is a coalescing wake-up stream, not an event log. Callers
// reconcile durable rows on each wake-up, and reconnect if the channel closes.
// Its connection is independent of the pool's collection/transaction budget.
func (s *Store) SubscribeChanges(ctx context.Context) (<-chan struct{}, func(), error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, errors.New("database store is closed")
	}
	s.listeners.Add(1)
	s.mu.Unlock()
	listenCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.background, cancel)
	cleanup := func() { cancel(); stop() }
	conn, err := pgx.ConnectConfig(listenCtx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		cleanup()
		s.listeners.Done()
		return nil, nil, databaseError("subscribe to database changes", err)
	}
	if _, err := conn.Exec(listenCtx, "LISTEN "+notificationChannel); err != nil {
		closeConnection(conn)
		cleanup()
		s.listeners.Done()
		return nil, nil, databaseError("subscribe to database changes", err)
	}
	changes := make(chan struct{}, 1)
	changes <- struct{}{}
	go func() {
		defer s.listeners.Done()
		defer cleanup()
		defer close(changes)
		defer closeConnection(conn)
		for {
			if _, err := conn.WaitForNotification(listenCtx); err != nil {
				return
			}
			select {
			case changes <- struct{}{}:
			default:
			}
		}
	}()
	return changes, cleanup, nil
}

func closeConnection(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = conn.Close(ctx)
	// Cancellation can mark pgx closed before its asynchronous network cleanup
	// releases the server session. Wait before attempting orphan recovery.
	select {
	case <-conn.PgConn().CleanupDone():
	case <-ctx.Done():
	}
}

func providerLockKey(providerID string) int64 {
	sum := sha256.Sum256([]byte("scraper.ingest.provider.v1\x00" + providerID))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

func releaseConnection(conn *pgxpool.Conn, key int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	if err := conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&unlocked); err == nil && unlocked {
		conn.Release()
		return
	}
	// Never return a possibly locked or broken session to the pool.
	closeConnection(conn.Hijack())
}

// releaseLease requires lease.mu. It is idempotent when Close races FinishRun.
func (s *Store) releaseLease(id string, lease *runLease) {
	if lease.conn == nil {
		return
	}
	conn := lease.conn
	lease.conn = nil
	s.mu.Lock()
	if s.leases[id] == lease {
		delete(s.leases, id)
	}
	s.mu.Unlock()
	releaseConnection(conn, lease.key)
	<-s.claimSlots
	// A queued successor may already have observed the terminal transaction
	// while its predecessor's session lock was still held. Wake it again only
	// after both the advisory lock and the local claim slot are available.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(ctx, "SELECT pg_notify($1, '')", notificationChannel)
}

func (s *Store) ownedLease(id string) (*runLease, error) {
	s.mu.Lock()
	lease := s.leases[id]
	s.mu.Unlock()
	if lease == nil {
		return nil, model.ErrConflict
	}
	lease.mu.Lock()
	if lease.conn == nil {
		lease.mu.Unlock()
		return nil, model.ErrConflict
	}
	return lease, nil
}

func newRunID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", errors.New("cannot generate collection identity")
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	raw := hex.EncodeToString(value[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], nil
}

func databaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			if pgErr.ConstraintName == "runs_one_active_provider" {
				return model.ErrBusy
			}
			return model.ErrConflict
		}
		return fmt.Errorf("%s failed (SQLSTATE %s)", operation, pgErr.Code)
	}
	// pgx errors can include connection strings and SQL values. Never expose them.
	return fmt.Errorf("%s failed", operation)
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func notify(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_notify($1, '')", notificationChannel)
	return err
}

func pageOptions(opts model.ListOptions) model.ListOptions {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	return opts
}

func jsonObject(value map[string]any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("cannot encode structured fields")
	}
	return encoded, nil
}

func canonicalFields(value map[string]any) ([]byte, error) {
	value = model.WithoutSwarmCounts(value)
	fields := make(map[string]any, len(value))
	for key, item := range value {
		if item == nil {
			continue
		}
		reflected := reflect.ValueOf(item)
		switch reflected.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if reflected.IsNil() {
				continue
			}
		}
		fields[key] = item
	}
	return jsonObject(fields)
}

func jsonCursor(cursor json.RawMessage) (any, error) {
	if len(cursor) == 0 {
		return nil, nil
	}
	if !json.Valid(cursor) {
		return nil, errors.New("invalid collection continuation")
	}
	return []byte(cursor), nil
}
