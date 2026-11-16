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
