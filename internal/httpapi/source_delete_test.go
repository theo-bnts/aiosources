package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSourceDeletionChecksRevisionAndActiveRunsBeforePurging(t *testing.T) {
	f := sharingAPI(t, "https://owner.example.test")
	response := f.request(http.MethodGet, "/api/providers/fixture", "", true, "")
	var doc model.ProviderDocument
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &doc) != nil {
		t.Fatalf("read definition: %d", response.Code)
	}
	body, _ := json.Marshal(map[string]string{"revision": doc.Revision})
	if response = f.request(http.MethodDelete, "/api/providers/fixture", string(body), false, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous deletion: %d", response.Code)
	}
	untrusted := httptest.NewRequest(http.MethodDelete, "https://owner.example.test/api/providers/fixture", strings.NewReader(string(body))).WithContext(f.ctx)
	untrusted.AddCookie(f.cookie)
	untrusted.Header.Set("Origin", "https://owner.example.test")
	untrusted.Header.Set("Content-Type", "application/json")
	rejected := httptest.NewRecorder()
	f.handler.ServeHTTP(rejected, untrusted)
	if rejected.Code != http.StatusForbidden {
		t.Fatalf("deletion without CSRF protection: %d", rejected.Code)
	}
	if err := f.db.PutSecret(f.ctx, "retained-secret", []byte("encrypted-synthetic-value")); err != nil {
		t.Fatal(err)
	}
	run, err := f.db.CreateRun(f.ctx, model.Run{ProviderID: "fixture", Config: doc.Provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	if response = f.request(http.MethodDelete, "/api/providers/fixture", `{"revision":"stale"}`, true, ""); response.Code != http.StatusConflict {
		t.Fatalf("stale deletion: %d", response.Code)
	}
	if response = f.request(http.MethodDelete, "/api/providers/fixture", string(body), true, ""); response.Code != http.StatusConflict {
		t.Fatalf("active deletion: %d", response.Code)
	}
	if _, err = f.db.RequestCancel(f.ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if response = f.request(http.MethodDelete, "/api/providers/fixture", string(body), true, ""); response.Code != http.StatusNoContent {
		t.Fatalf("delete source: %d %s", response.Code, response.Body.String())
	}
	if response = f.request(http.MethodGet, "/api/providers/fixture", "", true, ""); response.Code != http.StatusNotFound {
		t.Fatalf("definition remains: %d", response.Code)
	}
	if _, err = f.db.GetRun(f.ctx, run.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("history remains: %v", err)
	}
	if value, err := f.db.Secret(f.ctx, "retained-secret"); err != nil || string(value) != "encrypted-synthetic-value" {
		t.Fatalf("secret changed: %v", err)
	}
}
