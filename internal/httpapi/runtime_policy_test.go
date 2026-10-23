package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAuthenticationPolicyControlsNewSessionsAndEnrollment(t *testing.T) {
	f := newSecurityFixture(t)
	policy, _ := f.db.RuntimeSection(f.ctx, "security")
	policy.Values["session_hours"], policy.Values["session_limit"], policy.Values["enrollment_minutes"] = 1, 1, 5
	if err := f.db.UpdateRuntimeSection(f.ctx, "security", policy); err != nil {
		t.Fatal(err)
	}
	cookie := f.login(t, "")
	if cookie.MaxAge != 3600 || time.Until(cookie.Expires) < 3598*time.Second || time.Until(cookie.Expires) > time.Hour {
		t.Fatal("cookie duration ignores saved policy")
	}
	sessions, err := f.db.ListAdminSessions(f.ctx, "")
	if err != nil || len(sessions) != 1 || sessions[0].ExpiresAt.Sub(cookie.Expires) > time.Second {
		t.Fatal("session and cookie deadlines differ", err)
	}
	if response := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password"}, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatal("active session limit ignored", response.Code)
	}
	if response := f.request(http.MethodGet, "/api/session", nil, cookie); !strings.Contains(response.Body.String(), `"authenticated":true`) {
		t.Fatal("reducing capacity revoked an existing session")
	}
	response := f.request(http.MethodPost, "/api/security/mfa/enroll", map[string]string{"password": "synthetic-test-password"}, cookie)
	var enrollment struct {
		Expires time.Time `json:"expires_at"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &enrollment) != nil || time.Until(enrollment.Expires) < 298*time.Second || time.Until(enrollment.Expires) > 5*time.Minute {
		t.Fatal("setup validity ignored", response.Code)
	}
}

func TestConfiguredSignInLimitIsEnforcedBeforePasswordWork(t *testing.T) {
	f := newSecurityFixture(t)
	f.auth.clientAddress = func(*http.Request) string { return "192.0.2.1" }
	policy, _ := f.db.RuntimeSection(f.ctx, "security")
	policy.Values["login_per_minute"] = 1
	if err := f.db.UpdateRuntimeSection(f.ctx, "security", policy); err != nil {
		t.Fatal(err)
	}
	if response := f.request(http.MethodPost, "/api/login", map[string]string{"password": "incorrect-synthetic-password"}, nil); response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code)
	}
	if response := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password"}, nil); response.Code != http.StatusTooManyRequests {
		t.Fatal("per-address limit ignored", response.Code)
	}
}

func TestGlobalSignInLimitIsSharedBetweenAuthenticationInstances(t *testing.T) {
	f := newSecurityFixture(t)
	policy, _ := f.db.RuntimeSection(f.ctx, "security")
	policy.Values["login_per_minute"], policy.Values["login_global_per_minute"] = 1, 1
	if err := f.db.UpdateRuntimeSection(f.ctx, "security", policy); err != nil {
		t.Fatal(err)
	}
	other, err := newAuthentication("synthetic-test-password", "https://admin.example.test", f.db, f.vault)
	if err != nil {
		t.Fatal(err)
	}
	if response := f.request(http.MethodPost, "/api/login", map[string]string{"password": "incorrect-synthetic-password"}, nil); response.Code != 401 {
		t.Fatal(response.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "https://admin.example.test/api/login", strings.NewReader(`{"password":"synthetic-test-password"}`)).WithContext(f.ctx)
	r.RemoteAddr = "198.51.100.2:1234"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	other.loginHandler(w, r)
	if w.Code != 429 {
		t.Fatal("another process bypassed the shared authentication limit", w.Code)
	}
}

func TestDownloadPolicyKeepsIssuedLinksAndControlsActualTransferDeadline(t *testing.T) {
	f, addon, _ := addonAPI(t)
	policy, _ := f.db.RuntimeSection(f.ctx, "downloads")
	policy.Values["ticket_minutes"], policy.Values["duration_hours"] = 30, 1
	if err := f.db.UpdateRuntimeSection(f.ctx, "downloads", policy); err != nil {
		t.Fatal(err)
	}
	s := &server{options: Options{Store: f.db, Vault: addon.Vault}, files: make(chan struct{}, 1)}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(f.ctx), 3*time.Hour)
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/issue", nil).WithContext(context.WithValue(ctx, sessionContextKey{}, session{id: "synthetic-session"}))
	w := httptest.NewRecorder()
	s.writeCatalogDownload(w, r, model.CatalogDownload{Status: "ready", URL: "https://cdn.example/ready"}, nil)
	var result model.CatalogDownload
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("ticket issuance failed", w.Code)
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(result.URL, "/api/catalog-downloads/"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := addon.Vault.Open(catalogFilePurpose, sealed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(data)
	var ticket catalogFileTicket
	if json.Unmarshal(data, &ticket) != nil || ticket.Expires-time.Now().Unix() < 1798 || ticket.Expires-time.Now().Unix() > 1800 {
		t.Fatal("ticket expiry ignores saved policy")
	}
	s.fileClient = &http.Client{Transport: catalogFileRoundTrip(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) < 3598*time.Second || time.Until(deadline) > time.Hour {
			t.Error("actual transfer deadline ignored")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"3"}}, Body: io.NopCloser(strings.NewReader("abc")), Request: request}, nil
	})}
	// A smaller validity applies to newly issued links. It must not invalidate
	// an authenticated, already-issued link whose fixed expiry is still valid.
	policy, _ = f.db.RuntimeSection(f.ctx, "downloads")
	policy.Values["ticket_minutes"] = 1
	if err := f.db.UpdateRuntimeSection(f.ctx, "downloads", policy); err != nil {
		t.Fatal(err)
	}
	r.SetPathValue("token", strings.TrimPrefix(result.URL, "/api/catalog-downloads/"))
	delivery := &publicDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	s.catalogFile(delivery, r)
	if delivery.Code != 200 || delivery.Body.String() != "abc" || time.Until(delivery.deadline) < 3598*time.Second || time.Until(delivery.deadline) > time.Hour {
		t.Fatal("issued link or transfer deadline changed", delivery.Code)
	}
}

func TestSourceDefinitionLimitDoesNotInvalidateSavedDefinitions(t *testing.T) {
	f, _, _ := addonAPI(t)
	definition := `{"version":1,"id":"size-test","name":"Size test","adapter":"json","url":"https://source.example.invalid/api","enabled":false,"http":{"items_path":"/items"},"mapping":{"id":"/id","fields":{"title":"/title"}},"options":{"note":"` + strings.Repeat("x", 17*1024) + `"}}`
	body, _ := json.Marshal(map[string]string{"json": definition})
	if response := f.request(http.MethodPut, "/api/providers/size-test", string(body), true, ""); response.Code != 200 {
		t.Fatal("could not save initial source", response.Code)
	}
	policy, _ := f.db.RuntimeSection(f.ctx, "payloads")
	policy.Values["source_kib"] = 16
	if err := f.db.UpdateRuntimeSection(f.ctx, "payloads", policy); err != nil {
		t.Fatal(err)
	}
	if response := f.request(http.MethodGet, "/api/providers/size-test", "", true, ""); response.Code != 200 {
		t.Fatal("saved definition became unreadable", response.Code)
	}
	for method, endpoint := range map[string]string{http.MethodPut: "/api/providers/size-test", http.MethodPost: "/api/providers/validate"} {
		if response := f.request(method, endpoint, string(body), true, ""); response.Code != 422 || !strings.Contains(response.Body.String(), "16 KiB") {
			t.Fatal("definition write limit ignored", response.Code)
		}
	}
}
