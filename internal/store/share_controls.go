package store

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// CatalogDownloadTimeout bounds database work and network delivery. A lease
// outlives that hard deadline, so another replica cannot admit a replacement
// while a slow response is still allowed to write.
const CatalogDownloadTimeout = 20 * time.Second
const catalogLeaseLifetime = CatalogDownloadTimeout + 5*time.Second

// CatalogQuotaError is safe to expose: it contains no credential or share data.
type CatalogQuotaError struct {
	RetryAfter int
	Concurrent bool
}

func (e *CatalogQuotaError) Error() string {
	if e.Concurrent {
		return "Too many active catalogue downloads; retry after an active download finishes"
	}
	return "Catalogue request limit reached; retry after the indicated delay"
}

// CatalogDownload keeps admission separate from page generation so HTTP callers
// retain the durable lease until the complete response has been flushed. It
// never retains the password. Close is safe to call after cancellation and more
// than once; crashed instances leave only a short, expiring lease.
type CatalogDownload struct {
	store     *Store
	id        string
	shareID   string
	revision  int64
	deadline  time.Time
	closeOnce sync.Once
	closeErr  error
}

func (s *Store) BeginCatalogDownload(ctx context.Context, shareID, password string) (*CatalogDownload, error) {
	deadline := time.Now().Add(CatalogDownloadTimeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(password)
	if err != nil || len(decoded) != 32 || len(password) != 43 || len(shareID) > 128 {
		return nil, ErrCatalogAuthentication
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, databaseError("begin catalogue admission", err)
	}
	defer rollback(tx)
	var hash []byte
	var enabled, unexpired bool
	var revision int64
	var requests, concurrent int
	// This row lock serializes admissions, policy changes and revocation across
	// instances. Authenticate before either charging quota or clearing leases.
	err = tx.QueryRow(ctx, `SELECT password_hash,enabled,expires_at IS NULL OR expires_at>clock_timestamp(),revision,requests_per_minute,max_concurrent_downloads FROM ingest.shares WHERE id=$1 FOR UPDATE`, shareID).Scan(&hash, &enabled, &unexpired, &revision, &requests, &concurrent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCatalogAuthentication
	}
	if err != nil {
		return nil, databaseError("authorize catalogue admission", err)
	}
	candidate := sharingPasswordHash(shareID, password)
	if subtle.ConstantTimeCompare(hash, candidate[:]) != 1 || !enabled || !unexpired {
		return nil, ErrCatalogAuthentication
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, databaseError("read catalogue quota clock", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ingest.share_requests WHERE share_id=$1 AND requested_at<=$2 AND (lease_until IS NULL OR lease_until<=$3)`, shareID, now.Add(-time.Minute), now); err != nil {
		return nil, databaseError("expire catalogue admission", err)
	}
	var recent, active int
	var firstRequest, firstLease *time.Time
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE requested_at>$2),count(*) FILTER (WHERE lease_until>$3),min(requested_at) FILTER (WHERE requested_at>$2),min(lease_until) FILTER (WHERE lease_until>$3) FROM ingest.share_requests WHERE share_id=$1`, shareID, now.Add(-time.Minute), now).Scan(&recent, &active, &firstRequest, &firstLease)
	if err != nil {
		return nil, databaseError("read catalogue admission", err)
	}
	if recent >= requests || active >= concurrent {
		retry := 1
		if recent >= requests && firstRequest != nil {
			retry = max(retry, int(math.Ceil(firstRequest.Add(time.Minute).Sub(now).Seconds())))
		}
		if active >= concurrent && firstLease != nil {
			retry = max(retry, int(math.Ceil(firstLease.Sub(now).Seconds())))
		}
		return nil, &CatalogQuotaError{RetryAfter: retry, Concurrent: active >= concurrent}
	}
	id, err := newRunID()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ingest.share_requests(id,share_id,requested_at,lease_until) VALUES($1,$2,$3,$4)`, id, shareID, now, now.Add(catalogLeaseLifetime)); err != nil {
		return nil, databaseError("reserve catalogue download", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, databaseError("commit catalogue admission", err)
	}
	return &CatalogDownload{store: s, id: id, shareID: shareID, revision: revision, deadline: deadline}, nil
}

// Validate must run after encoding and immediately before sending a response.
// It also rejects a download admitted before expiry or a concurrent policy
// change, so admission is not a capability that bypasses current permissions.
func (d *CatalogDownload) Validate(ctx context.Context) error {
	ctx, cancel := context.WithDeadline(ctx, d.deadline)
	defer cancel()
	var allowed bool
	err := d.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.shares s JOIN ingest.share_requests r ON r.share_id=s.id WHERE s.id=$1 AND s.enabled AND s.revision=$2 AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()) AND r.id=$3 AND r.lease_until>clock_timestamp())`, d.shareID, d.revision, d.id).Scan(&allowed)
	if err != nil {
		return databaseError("validate catalogue delivery", err)
	}
	if !allowed {
		return ErrCatalogAuthentication
	}
	return nil
}

func (d *CatalogDownload) Close() error {
	d.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := d.store.pool.Exec(ctx, `UPDATE ingest.share_requests SET lease_until=NULL WHERE id=$1 AND share_id=$2`, d.id, d.shareID)
		d.closeErr = databaseError("release catalogue download", err)
	})
	return d.closeErr
}
