package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestAccessLimitsUpgradePreservesCustomPoliciesAndSessions(t *testing.T) {
	for _, tc := range []struct {
		name                                                              string
		hours, sessions, perIP, total, wantHours, wantSessions, wantTotal int
	}{
		{"former defaults", 12, 256, 10, 60, 24, 10, 50},
		{"custom choices", 36, 16, 15, 80, 36, 16, 80},
		{"old larger cap", 48, 400, 10, 90, 48, 100, 90},
		{"global covers custom address limit", 12, 256, 60, 60, 24, 10, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, dsn := testutil.NewDatabase(t)
			db, err := Open(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = db.migrateThrough(ctx, 97); err != nil {
				t.Fatal(err)
			}
			_, err = db.pool.Exec(ctx, `UPDATE aiosources.runtime_settings SET values=jsonb_build_object('session_hours',$1::int,'session_limit',$2::int,'login_per_minute',$3::int,'login_global_per_minute',$4::int),revision=9 WHERE section='security';`, tc.hours, tc.sessions, tc.perIP, tc.total)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.pool.Exec(ctx, `INSERT INTO aiosources.admin_sessions(id,token_hash,user_agent,expires_at) VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',decode(repeat('42',32),'hex'),'Other browser',now()+interval '3 days')`); err != nil {
				t.Fatal(err)
			}
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			policy, err := db.RuntimeSection(ctx, "security")
			if err != nil || policy.Values["session_hours"] != tc.wantHours || policy.Values["session_limit"] != tc.wantSessions || policy.Values["login_per_minute"] != tc.perIP || policy.Values["login_global_per_minute"] != tc.wantTotal || policy.Revision != 10 {
				t.Fatal(policy, err)
			}
			trusted, err := db.RuntimeSection(ctx, "trusted_security")
			if err != nil || !trusted.Valid("trusted_security") || trusted.Values["session_limit"] != 10 || trusted.Values["session_hours"] != tc.wantHours || trusted.Values["login_per_minute"] != tc.perIP || trusted.Values["login_account_per_minute"] != tc.perIP || trusted.Values["login_global_per_minute"] != tc.wantTotal {
				t.Fatal(trusted, err)
			}
			sessions, err := db.ListAdminSessions(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			if err != nil || len(sessions) != 1 || time.Until(sessions[0].ExpiresAt) < 71*time.Hour {
				t.Fatal("upgrade changed existing sessions", err)
			}
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			again, _ := db.RuntimeSection(ctx, "security")
			if again.Revision != policy.Revision {
				t.Fatal("migration repeated")
			}
		})
	}
}

func TestTrustedSessionCapacityIsAtomicPerAccountAndSupportsReplacement(t *testing.T) {
	ctx, dsn, db := activityDatabase(t)
	peer, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	user, err := db.SaveTrustedUser(ctx, model.TrustedUser{Email: "reader@example.invalid", Enabled: true}, "opaque-hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.SaveTrustedUser(ctx, model.TrustedUser{Email: "other@example.invalid", Enabled: true}, "opaque-hash")
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := db.RuntimeSection(ctx, "trusted_security")
	policy.Values["session_limit"] = 3
	if err = db.UpdateRuntimeSection(ctx, "trusted_security", policy); err != nil {
		t.Fatal(err)
	}
	var success atomic.Int32
	var wg sync.WaitGroup
	hashes := make(chan []byte, 16)
	for n := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := db
			if n%2 == 1 {
				store = peer
			}
			hash := sha256.Sum256([]byte{byte(n)})
			_, _, err := store.CreateTrustedSession(ctx, model.TrustedGrant{UserID: user.ID, Generation: user.Generation}, hash[:], time.Hour)
			if err == nil {
				success.Add(1)
				hashes <- hash[:]
			} else if !errors.Is(err, ErrSecurityLimit) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(hashes)
	if success.Load() != 3 {
		t.Fatal("concurrent sessions exceeded or undershot cap", success.Load())
	}
	previous := <-hashes
	replacement := sha256.Sum256([]byte("replacement"))
	grant := model.TrustedGrant{UserID: user.ID, Generation: user.Generation}
	if _, _, err = db.CreateTrustedSession(ctx, grant, replacement[:], time.Hour, previous); err != nil {
		t.Fatal("same browser cannot reauthenticate at capacity", err)
	}
	if _, _, _, err = db.TrustedSession(ctx, previous); !errors.Is(err, ErrAuthentication) {
		t.Fatal("previous session survived replacement", err)
	}
	if _, _, _, err = db.TrustedSession(ctx, replacement[:]); err != nil {
		t.Fatal(err)
	}
	otherHash := sha256.Sum256([]byte("other account"))
	if _, _, err = db.CreateTrustedSession(ctx, model.TrustedGrant{UserID: other.ID, Generation: other.Generation}, otherHash[:], time.Hour); err != nil {
		t.Fatal("one account exhausted another account's capacity", err)
	}
	next := sha256.Sum256([]byte("extra"))
	if _, _, err = db.CreateTrustedSession(ctx, grant, next[:], time.Hour, otherHash[:]); !errors.Is(err, ErrSecurityLimit) {
		t.Fatal("another account's cookie bypassed capacity", err)
	}
	if _, _, _, err = db.TrustedSession(ctx, otherHash[:]); err != nil {
		t.Fatal("another account's session revoked", err)
	}
	policy, _ = db.RuntimeSection(ctx, "trusted_security")
	policy.Values["session_limit"] = 1
	if err = db.UpdateRuntimeSection(ctx, "trusted_security", policy); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.CreateTrustedSession(ctx, grant, next[:], time.Hour); !errors.Is(err, ErrSecurityLimit) {
		t.Fatal("lower limit not enforced", err)
	}
	if _, _, _, err = db.TrustedSession(ctx, replacement[:]); err != nil {
		t.Fatal("lowering cap revoked existing session", err)
	}
}

func TestSignInAdmissionPoliciesAreIndependentBetweenRoles(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	admin, _ := db.RuntimeSection(ctx, "security")
	admin.Values["login_per_minute"], admin.Values["login_global_per_minute"] = 1, 1
	trusted, _ := db.RuntimeSection(ctx, "trusted_security")
	trusted.Values["login_per_minute"], trusted.Values["login_account_per_minute"], trusted.Values["login_global_per_minute"] = 2, 1, 2
	if err := db.UpdateRuntimeSection(ctx, "security", admin); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRuntimeSection(ctx, "trusted_security", trusted); err != nil {
		t.Fatal(err)
	}
	for n := range 2 {
		allowed, err := db.AllowSecurityAttempt(ctx, "synthetic-address")
		if err != nil || allowed != (n == 0) {
			t.Fatal("administrator admission", n, allowed, err)
		}
	}
	for n := range 3 {
		allowed, err := db.AllowTrustedSecurityAttempt(ctx, "synthetic-address")
		if err != nil || allowed != (n < 2) {
			t.Fatal("trusted admission charged to administrator policy", n, allowed, err)
		}
	}
	for n := range 2 {
		allowed, err := db.AllowTrustedAccountAttempt(ctx, "synthetic-account")
		if err != nil || allowed != (n == 0) {
			t.Fatal("per-account limit not applied", n, allowed, err)
		}
	}
	trusted.Values["login_account_per_minute"] = 3
	if trusted.Valid("trusted_security") {
		t.Fatal("account limit exceeds global capacity")
	}
}

func TestTrustedSessionExpiryCleanupIsGlobalBoundedAndSkipsLockedRows(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	user, err := db.SaveTrustedUser(ctx, model.TrustedUser{Email: "active@example.invalid", Enabled: true}, "opaque-hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.SaveTrustedUser(ctx, model.TrustedUser{Email: "inactive@example.invalid", Enabled: true}, "opaque-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO aiosources.trusted_sessions(id,token_hash,user_id,generation,expires_at)
 SELECT 'expired-'||n,decode(lpad(to_hex(n),64,'0'),'hex'),$1,1,now()-interval '1 day' FROM generate_series(1,1200) n`, other.ID); err != nil {
		t.Fatal(err)
	}
	locked, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(ctx)
	if _, err = locked.Exec(ctx, `SELECT id FROM aiosources.trusted_sessions WHERE id='expired-1' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	grant := model.TrustedGrant{UserID: user.ID, Generation: user.Generation}
	token := sha256.Sum256([]byte("bounded-expiry-first"))
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, _, err = db.CreateTrustedSession(deadline, grant, token[:], time.Hour); err != nil {
		t.Fatal("an unrelated locked session blocked sign-in", err)
	}
	var remaining int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.trusted_sessions WHERE user_id=$1`, other.ID).Scan(&remaining); err != nil || remaining != 200 {
		t.Fatal("expiry cleanup was not bounded", remaining, err)
	}
	if err = locked.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	second := sha256.Sum256([]byte("bounded-expiry-second"))
	if _, _, err = db.CreateTrustedSession(ctx, grant, second[:], time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.trusted_sessions WHERE user_id=$1`, other.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("inactive account retained expired sessions", remaining, err)
	}
	if _, _, _, err = db.TrustedSession(ctx, token[:]); err != nil {
		t.Fatal("cleanup revoked an active session", err)
	}
}
