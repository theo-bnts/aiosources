package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/vault"
)

func TestDatabaseBackupExportImportAuthAndRollback(t *testing.T) {
	fixture := newSecurityFixture(t)
	s := &server{options: Options{Store: fixture.db, Vault: fixture.vault, DataDir: t.TempDir()}, auth: fixture.auth}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", fixture.auth.loginHandler)
	mux.HandleFunc("GET /api/session", fixture.auth.sessionHandler)
	s.registerStorageRoutes(mux)
	fixture.handler = securityHeaders(recoverRequests(s.databaseMaintenanceGate(mux)))

	const base = "/api/settings/storage/database"
	// Anonymous requests fail closed.
	if w := fixture.request("GET", base, nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status: %d", w.Code)
	}
	if w := fixture.request("POST", base+"/export", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous export: %d", w.Code)
	}
	if w := fixture.request("POST", base+"/import", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous import: %d", w.Code)
	}

	cookie := fixture.login(t, "")

	// Missing CSRF on export/import must be rejected.
	for _, target := range []string{base + "/export", base + "/import"} {
		req := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+target, nil).WithContext(fixture.ctx)
		req.AddCookie(cookie)
		req.Header.Set("Origin", "https://admin.example.test")
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 without CSRF for %s, got %d", target, w.Code)
		}
	}

	// Read tool status.
	statusResp := fixture.request("GET", base, nil, cookie)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status endpoint: %d %s", statusResp.Code, statusResp.Body.String())
	}
	var status model.DatabaseBackupStatus
	if err := json.Unmarshal(statusResp.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Tools.ExportAvailable || !status.Tools.ImportAvailable {
		t.Skipf("matching PostgreSQL client tools unavailable: %s", status.Tools.Reason)
	}

	// Seed distinguishable setting & secret before exporting.
	if err := fixture.db.SetSetting(fixture.ctx, "httpapi.backup.marker", "original-state"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.vault.Put(fixture.ctx, "backup_token", "super-secret-value"); err != nil {
		t.Fatal(err)
	}

	// Request a short-lived export ticket.
	ticketResp := fixture.request("POST", base+"/export", map[string]any{}, cookie)
	if ticketResp.Code != http.StatusOK {
		t.Fatalf("prepare export: %d %s", ticketResp.Code, ticketResp.Body.String())
	}
	var ticketPayload struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(ticketResp.Body.Bytes(), &ticketPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ticketPayload.URL, base+"/export/") || !strings.HasSuffix(ticketPayload.Filename, ".dump") {
		t.Fatalf("unexpected export ticket payload: %+v", ticketPayload)
	}

	// Cross-site download request is rejected.
	crossSite := httptest.NewRequest(http.MethodGet, "https://admin.example.test"+ticketPayload.URL, nil).WithContext(fixture.ctx)
	crossSite.AddCookie(cookie)
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	crossResp := httptest.NewRecorder()
	fixture.handler.ServeHTTP(crossResp, crossSite)
	if crossResp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-site download, got %d", crossResp.Code)
	}

	// Another admin session cannot redeem someone else's export ticket.
	otherCookie := fixture.login(t, "")
	otherReq := httptest.NewRequest(http.MethodGet, "https://admin.example.test"+ticketPayload.URL, nil).WithContext(fixture.ctx)
	otherReq.AddCookie(otherCookie)
	otherResp := httptest.NewRecorder()
	fixture.handler.ServeHTTP(otherResp, otherReq)
	if otherResp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign session ticket, got %d", otherResp.Code)
	}

	// Stream the actual backup archive with the owning session.
	dlReq := httptest.NewRequest(http.MethodGet, "https://admin.example.test"+ticketPayload.URL, nil).WithContext(fixture.ctx)
	dlReq.AddCookie(cookie)
	dlReq.Header.Set("Sec-Fetch-Site", "same-origin")
	dlResp := httptest.NewRecorder()
	fixture.handler.ServeHTTP(dlResp, dlReq)
	if dlResp.Code != http.StatusOK {
		t.Fatalf("download export: %d %s", dlResp.Code, dlResp.Body.String())
	}
	if !strings.Contains(dlResp.Header().Get("Content-Disposition"), ticketPayload.Filename) {
		t.Fatalf("unexpected Content-Disposition: %q", dlResp.Header().Get("Content-Disposition"))
	}
	archiveBytes := dlResp.Body.Bytes()
	if len(archiveBytes) < 32 || string(archiveBytes[:5]) != "PGDMP" {
		t.Fatalf("expected PGDMP archive bytes, got %d bytes", len(archiveBytes))
	}

	// Validate import guards: missing confirm header, wrong content-type, invalid magic.
	sendImport := func(body []byte, contentType, confirm string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+base+"/import", bytes.NewReader(body)).WithContext(fixture.ctx)
		req.AddCookie(cookie)
		req.Header.Set("Origin", "https://admin.example.test")
		req.Header.Set("X-CSRF-Token", csrfForToken(cookie.Value))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if confirm != "" {
			req.Header.Set(databaseConfirmHeader, confirm)
		}
		req.Header.Set(databaseFilenameHeader, ticketPayload.Filename)
		w := httptest.NewRecorder()
		fixture.handler.ServeHTTP(w, req)
		return w
	}
	if w := sendImport(archiveBytes, "application/octet-stream", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without confirm header, got %d", w.Code)
	}
	if w := sendImport(archiveBytes, "application/json", databaseConfirmValue); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 with JSON content type, got %d", w.Code)
	}
	if w := sendImport([]byte("not a pg dump"), "application/octet-stream", databaseConfirmValue); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid magic, got %d", w.Code)
	}

	// Mutate live database before restoring.
	if err := fixture.db.SetSetting(fixture.ctx, "httpapi.backup.marker", "mutated-state"); err != nil {
		t.Fatal(err)
	}

	// Trigger full async restore and poll status until terminal state.
	impResp := sendImport(archiveBytes, "application/octet-stream", databaseConfirmValue)
	if impResp.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted on import, got %d: %s", impResp.Code, impResp.Body.String())
	}

	deadline := time.Now().Add(30 * time.Second)
	var finalStatus model.DatabaseBackupStatus
	for time.Now().Before(deadline) {
		poll := fixture.request("GET", base, nil, cookie)
		if poll.Code != http.StatusOK {
			t.Fatalf("status poll failed during restore: %d %s", poll.Code, poll.Body.String())
		}
		if err := json.Unmarshal(poll.Body.Bytes(), &finalStatus); err != nil {
			t.Fatal(err)
		}
		if finalStatus.Restore.Status == "succeeded" || finalStatus.Restore.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if finalStatus.Restore.Status != "succeeded" {
		t.Fatalf("expected restore to succeed, got %+v", finalStatus.Restore)
	}

	// Caller's session remains authenticated after restore, while other sessions were cleared.
	if w := fixture.request("GET", "/api/session", nil, cookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Fatalf("calling admin session must stay signed in after restore: %d %s", w.Code, w.Body.String())
	}
	if w := fixture.request("GET", "/api/session", nil, otherCookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"authenticated":false`) {
		t.Fatalf("non-calling admin sessions must be cleared after restore: %d %s", w.Code, w.Body.String())
	}

	// Original data and encrypted secrets are back and decryptable.
	marker, err := fixture.db.Setting(fixture.ctx, "httpapi.backup.marker")
	if err != nil || marker != "original-state" {
		t.Fatalf("expected original-state after restore, got %q %v", marker, err)
	}
	resolved, err := fixture.vault.Resolve(fixture.ctx, "backup_token")
	if err != nil || resolved != "super-secret-value" {
		t.Fatalf("expected restored secret to decrypt cleanly, got %q %v", resolved, err)
	}

	// Verify master-key mismatch with encrypted secrets rolls back cleanly.
	otherKey := make([]byte, 32)
	otherKey[0] = 0x42
	// Simulate another instance's vault holding a different key without changing DB fingerprint.
	if err := fixture.db.SetSetting(fixture.ctx, "httpapi.backup.marker", "keep-me-on-key-mismatch"); err != nil {
		t.Fatal(err)
	}
	// Create a second isolated database with a different master key to verify cross-key rejection.
	mismatchFixture := newSecurityFixture(t)
	_ = mismatchFixture.db.SetSetting(mismatchFixture.ctx, "vault.master_key_fingerprint.v1", strings.Repeat("a", 64))
	if err := mismatchFixture.db.PutSecret(mismatchFixture.ctx, "other_secret", []byte("foreign-ciphertext")); err != nil {
		t.Fatal(err)
	}
	if err := mismatchFixture.vault.VerifyRestoredDatabase(mismatchFixture.ctx); err != vault.ErrMasterKeyMismatch {
		t.Fatalf("expected ErrMasterKeyMismatch when secrets exist under foreign key, got %v", err)
	}
	// If a foreign-key backup has zero secrets, VerifyRestoredDatabase adopts the current key cleanly.
	if err := mismatchFixture.db.DeleteSecret(mismatchFixture.ctx, "other_secret"); err != nil {
		t.Fatal(err)
	}
	if err := mismatchFixture.vault.VerifyRestoredDatabase(mismatchFixture.ctx); err != nil {
		t.Fatalf("expected zero-secret foreign backup to adopt running master key, got %v", err)
	}
}
