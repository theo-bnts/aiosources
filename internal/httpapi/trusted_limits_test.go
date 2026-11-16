package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestTrustedSessionDurationCapacityAndReauthentication(t *testing.T) {
	f, _, _ := addonAPI(t)
	created := f.request("POST", "/api/trusted-users", `{"email":"reader@example.invalid","password":"synthetic trusted password","enabled":true,"search_enabled":true}`, true, "")
	var user model.TrustedUser
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &user) != nil {
		t.Fatal(created.Code, created.Body)
	}
	policy, _ := f.db.RuntimeSection(f.ctx, "trusted_security")
	policy.Values["session_hours"], policy.Values["session_limit"] = 2, 1
	if err := f.db.UpdateRuntimeSection(f.ctx, "trusted_security", policy); err != nil {
		t.Fatal(err)
	}
	proof := `{"email":"reader@example.invalid","password":"synthetic trusted password"}`
	login := trustedRequest(f, nil, "POST", "/api/trusted/login", proof)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body)
	}
	var cookie *http.Cookie
	for _, v := range login.Result().Cookies() {
		if v.Name == trustedSessionCookie {
			cookie = v
		}
	}
	if cookie == nil || cookie.MaxAge < 7198 || cookie.MaxAge > 7200 || time.Until(cookie.Expires) < 7198*time.Second {
		t.Fatal("trusted sign-in ignores its own duration", cookie)
	}
	blocked := trustedRequest(f, nil, "POST", "/api/trusted/login", proof)
	if blocked.Code != 409 {
		t.Fatal("session capacity not reported correctly", blocked.Code, blocked.Body)
	}
	replacement := trustedRequest(f, cookie, "POST", "/api/trusted/login", proof)
	if replacement.Code != 200 {
		t.Fatal("same browser cannot replace its session", replacement.Code, replacement.Body)
	}
	if old := trustedRequest(f, cookie, "GET", "/api/trusted/config", ""); old.Code != 401 {
		t.Fatal("replaced cookie still authenticates", old.Code)
	}
	var next *http.Cookie
	for _, v := range replacement.Result().Cookies() {
		if v.Name == trustedSessionCookie {
			next = v
		}
	}
	if next == nil || trustedRequest(f, next, "GET", "/api/trusted/config", "").Code != 200 {
		t.Fatal("new session unavailable")
	}
}

func TestTrustedSignInDoesNotConsumeAdministratorAttempts(t *testing.T) {
	f, _, _ := addonAPI(t)
	policy, _ := f.db.RuntimeSection(f.ctx, "security")
	policy.Values["login_per_minute"], policy.Values["login_global_per_minute"] = 2, 2
	if err := f.db.UpdateRuntimeSection(f.ctx, "security", policy); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		response := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"unknown@example.invalid","password":"incorrect password"}`)
		if response.Code != 401 {
			t.Fatal("trusted sign-ins used administrator limits", response.Code)
		}
	}
	// A wrong administrator proof must reach password verification, rather than
	// being rejected by attempts already charged to a different role.
	if admin := f.request("POST", "/api/login", `{"password":"incorrect administrator password"}`, false, ""); admin.Code != 401 {
		t.Fatal("trusted attempts exhausted administrator admission", admin.Code)
	}
}

func TestRoleSwitchReplacesOnlyTheCurrentBrowserSessionAndRollsBackOnFailure(t *testing.T) {
	f, _, conn := addonAPI(t)
	created := f.request("POST", "/api/trusted-users", `{"email":"reader@example.invalid","password":"synthetic trusted password","enabled":true,"search_enabled":true}`, true, "")
	if created.Code != http.StatusCreated {
		t.Fatal("create trusted account", created.Code, created.Body)
	}
	for section, limit := range map[string]int{"security": 2, "trusted_security": 1} {
		policy, err := f.db.RuntimeSection(f.ctx, section)
		if err != nil {
			t.Fatal(err)
		}
		policy.Values["session_limit"] = limit
		if err := f.db.UpdateRuntimeSection(f.ctx, section, policy); err != nil {
			t.Fatal(err)
		}
	}
	cookieFrom := func(response *http.Response, name string) *http.Cookie {
		t.Helper()
		for _, cookie := range response.Cookies() {
			if cookie.Name == name && cookie.MaxAge > 0 {
				return cookie
			}
		}
		t.Fatalf("missing %s session cookie", name)
		return nil
	}
	trustedProof := `{"email":"reader@example.invalid","password":"synthetic trusted password"}`
	adminProof := `{"password":"synthetic-admin-password"}`
	trustedLogin := trustedRequest(f, nil, "POST", "/api/trusted/login", trustedProof)
	if trustedLogin.Code != http.StatusOK {
		t.Fatal("trusted sign-in", trustedLogin.Code, trustedLogin.Body)
	}
	oldTrusted := cookieFrom(trustedLogin.Result(), trustedSessionCookie)
	adminLogin := trustedRequest(f, oldTrusted, "POST", "/api/login", adminProof)
	if adminLogin.Code != http.StatusOK {
		t.Fatal("switch to administrator", adminLogin.Code, adminLogin.Body)
	}
	admin := cookieFrom(adminLogin.Result(), sessionCookie)
	if got := trustedRequest(f, oldTrusted, "GET", "/api/trusted/config", ""); got.Code != http.StatusUnauthorized {
		t.Fatal("previous trusted session remains authorized", got.Code)
	}
	trustedLogin = trustedRequest(f, admin, "POST", "/api/trusted/login", trustedProof)
	if trustedLogin.Code != http.StatusOK {
		t.Fatal("switch back to trusted", trustedLogin.Code, trustedLogin.Body)
	}
	trusted := cookieFrom(trustedLogin.Result(), trustedSessionCookie)
	if got := trustedRequest(f, admin, "GET", "/api/security/sessions", ""); got.Code != http.StatusUnauthorized {
		t.Fatal("previous administrator session remains authorized", got.Code)
	}
	if got := trustedRequest(f, f.cookie, "GET", "/api/security/sessions", ""); got.Code != http.StatusOK {
		t.Fatal("another browser lost its administrator session", got.Code)
	}
	var administrators, trustedSessions int
	if err := conn.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM aiosources.admin_sessions), (SELECT count(*) FROM aiosources.trusted_sessions)`).Scan(&administrators, &trustedSessions); err != nil || administrators != 1 || trustedSessions != 1 {
		t.Fatal("role switches retained obsolete sessions", administrators, trustedSessions, err)
	}
	policy, err := f.db.RuntimeSection(f.ctx, "security")
	if err != nil {
		t.Fatal(err)
	}
	policy.Values["session_limit"] = 1
	if err := f.db.UpdateRuntimeSection(f.ctx, "security", policy); err != nil {
		t.Fatal(err)
	}
	blocked := trustedRequest(f, trusted, "POST", "/api/login", adminProof)
	if blocked.Code != http.StatusConflict {
		t.Fatal("administrator capacity was bypassed", blocked.Code, blocked.Body)
	}
	if got := trustedRequest(f, trusted, "GET", "/api/trusted/config", ""); got.Code != http.StatusOK {
		t.Fatal("failed role switch revoked the working trusted session", got.Code)
	}
}
