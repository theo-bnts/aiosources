package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStremioAccessAdmissionRevocationAndCredentialStorage(t *testing.T) {
	ctx, db := sharingDatabase(t)
	concurrent := 1
	input := model.ShareInput{Format: "stremio", Name: "Example player", Enabled: true, Scope: "all", Fields: CatalogFields(), MaxConcurrentDownloads: &concurrent, StreamPreferences: &model.StreamPreferences{Language: "fr", Mode: "unknown"}}
	grant, err := db.CreateShare(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	read, err := db.StremioConnection(ctx, grant.Share.ID, grant.Password)
	if err != nil || read.StreamPreferences.Language != "fr" {
		t.Fatal("settings did not survive the credential-only read", err)
	}
	for _, password := range []string{"", "invalid", strings.Repeat("A", 43), grant.Password + "~fr~all"} {
		if _, err := db.StremioConnection(ctx, grant.Share.ID, password); !errors.Is(err, ErrCatalogAuthentication) {
			t.Fatal("invalid password accepted", err)
		}
	}
	for _, begin := range []func() (*CatalogDownload, error){
		func() (*CatalogDownload, error) { return db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password) },
		func() (*CatalogDownload, error) { return db.BeginTorznabRequest(ctx, grant.Share.ID, grant.Password) },
	} {
		if _, err := begin(); !errors.Is(err, ErrCatalogAuthentication) {
			t.Fatal("cross-protocol credential accepted", err)
		}
	}
	download, err := db.BeginStremioRequest(ctx, grant.Share.ID, grant.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer download.Close()
	var lifetime float64
	var seen bool
	if err = db.pool.QueryRow(ctx, `SELECT extract(epoch FROM r.lease_until-r.requested_at),s.last_access_at IS NOT NULL FROM aiosources.share_requests r JOIN aiosources.shares s ON s.id=r.share_id WHERE r.id=$1`, download.id).Scan(&lifetime, &seen); err != nil || lifetime != 75 || !seen {
		t.Fatal("playback lease too short or last access absent", lifetime, seen, err)
	}
	if _, err = db.BeginStremioRequest(ctx, grant.Share.ID, grant.Password); err == nil {
		t.Fatal("concurrent limit bypassed")
	} else {
		var quota *CatalogQuotaError
		if !errors.As(err, &quota) || !quota.Concurrent || quota.RetryAfter < 1 {
			t.Fatal("invalid concurrency response", err)
		}
	}
	input.Revision = grant.Share.Revision
	input.RequestsPerMinute = &grant.Share.RequestsPerMinute
	input.StreamPreferences.CachedOnly = true
	updated, err := db.UpdateShare(ctx, grant.Share.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err = download.Validate(ctx); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("admitted response ignored a concurrent policy change", err)
	}
	if err = download.Close(); err != nil {
		t.Fatal(err)
	}
	read, err = db.StremioConnection(ctx, grant.Share.ID, grant.Password)
	if err != nil || !read.StreamPreferences.CachedOnly {
		t.Fatal("saved policy not applied to the same key", err)
	}
	rotated, err := db.RotateShare(ctx, grant.Share.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StremioConnection(ctx, grant.Share.ID, grant.Password); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("old credential worked after rotation", err)
	}
	active, err := db.BeginStremioRequest(ctx, grant.Share.ID, rotated.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	for _, table := range []string{"shares", "share_requests", "activity_logs"} {
		var saved bool
		if err = db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.`+table+` t WHERE strpos(row_to_json(t)::text,$1)>0 OR strpos(row_to_json(t)::text,$2)>0)`, grant.Password, rotated.Password).Scan(&saved); err != nil || saved {
			t.Fatal("raw installation key persisted", table, err)
		}
	}
	if err = db.DeleteShare(ctx, grant.Share.ID, rotated.Share.Revision); err != nil {
		t.Fatal(err)
	}
	if err = active.Validate(ctx); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("revocation failed to cancel an admitted response", err)
	}
}
