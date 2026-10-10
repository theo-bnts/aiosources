package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestPauseRouteRequiresAuthenticationAndPreservesCancellation(t *testing.T) {
	fixture := newSecurityFixture(t)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	fixture.handler, err = New(Options{Store: fixture.db, Providers: registry, Jobs: jobs.New(fixture.db, registry, nil, 1), Vault: fixture.vault,
		AdminPassword: "synthetic-test-password", PublicURL: "https://admin.example.test", Frontend: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("panel")}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := fixture.db.CreateRun(fixture.ctx, model.Run{ProviderID: "pause-api", Config: model.Provider{ID: "pause-api"}, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/runs/" + run.ID + "/pause"
	if response := fixture.request(http.MethodPost, path, nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated pause: %d", response.Code)
	}
	cookie := fixture.login(t, "")
	withoutCSRF := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+path, nil).WithContext(fixture.ctx)
	withoutCSRF.AddCookie(cookie)
	withoutCSRF.Header.Set("Origin", "https://admin.example.test")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, withoutCSRF)
	if response.Code != http.StatusForbidden {
		t.Fatalf("pause accepted without CSRF: %d", response.Code)
	}
	untouched, err := fixture.db.GetRun(fixture.ctx, run.ID)
	if err != nil || untouched.Status != model.StatusQueued || untouched.PauseRequested {
		t.Fatalf("untrusted request changed work: %+v %v", untouched, err)
	}
	for range 2 {
		response := fixture.request(http.MethodPost, path, nil, cookie)
		var held model.Run
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &held) != nil || held.ID != run.ID || held.Status != model.StatusPaused || !held.PauseRequested || held.CancelRequested {
			t.Fatalf("pause did not return durable Run: %d %s", response.Code, response.Body.String())
		}
	}
	if response := fixture.request(http.MethodPost, "/api/runs/"+run.ID+"/cancel", nil, cookie); response.Code != http.StatusOK {
		t.Fatalf("cancel held run: %d %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, path, nil, cookie); response.Code != http.StatusConflict {
		t.Fatalf("late pause overwrote cancellation: %d %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, "/api/runs/missing/pause", nil, cookie); response.Code != http.StatusNotFound {
		t.Fatalf("missing pause target: %d %s", response.Code, response.Body.String())
	}
}

func TestRunResponsesKeepContinuationPrivateAndResumable(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	definition := `{"version":1,"id":"cursor-api","name":"Cursor fixture","adapter":"http_json","url":"https://source.invalid/api","enabled":true}`
	body, err := json.Marshal(map[string]string{"json": definition})
	if err != nil {
		t.Fatal(err)
	}
	response := fixture.request(http.MethodPut, "/api/providers/cursor-api", string(body), true, "")
	var document model.ProviderDocument
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &document) != nil {
		t.Fatalf("create source: status %d", response.Code)
	}
	run, err := fixture.db.CreateRun(fixture.ctx, model.Run{ProviderID: document.Provider.ID, Config: document.Provider, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := fixture.db.ClaimNext(fixture.ctx)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("claim cursor fixture: %v", err)
	}
	const private = "synthetic-private-continuation"
	cursor := json.RawMessage(`{"version":1,"url":"https://source.invalid/next?signature=synthetic-private-continuation","cursor":"synthetic-private-continuation","checkpoint":"synthetic-private-continuation"}`)
	saved, err := fixture.db.SavePage(fixture.ctx, *claimed, model.Page{
		Body: []byte(`{"items":[]}`), ContentType: "application/json", Next: cursor,
		Position: "https://source.invalid/next?signature=" + private,
		Metadata: map[string]any{"next_url": "https://source.invalid/next?signature=" + private},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Cursor) == 0 || !strings.Contains(string(saved.Cursor), private) {
		t.Fatal("fixture did not retain a private continuation")
	}
	if _, err := fixture.db.FinishRun(fixture.ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{
		"/api/runs/" + run.ID, "/api/runs", "/api/providers",
		"/api/runs/" + run.ID + "/events",
	} {
		response := fixture.request(http.MethodGet, endpoint, "", true, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), run.ID) {
			t.Fatalf("run missing from %s: status %d", endpoint, response.Code)
		}
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("private continuation exposed by %s", endpoint)
		}
	}
	for _, action := range []struct {
		path   string
		status int
		state  model.RunStatus
	}{
		{"resume", http.StatusAccepted, model.StatusQueued},
		{"pause", http.StatusOK, model.StatusPaused},
		{"cancel", http.StatusOK, model.StatusCancelled},
	} {
		response := fixture.request(http.MethodPost, "/api/runs/"+run.ID+"/"+action.path, "{}", true, "")
		var changed model.Run
		if response.Code != action.status || json.Unmarshal(response.Body.Bytes(), &changed) != nil || changed.ID != run.ID || changed.Status != action.state {
			t.Fatalf("%s did not preserve run control: status %d", action.path, response.Code)
		}
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("%s exposed private continuation", action.path)
		}
		retained, err := fixture.db.GetRun(fixture.ctx, run.ID)
		if err != nil || !bytes.Equal(retained.Cursor, saved.Cursor) || !reflect.DeepEqual(retained.Config, saved.Config) {
			t.Fatalf("%s changed the durable continuation or source snapshot: %v", action.path, err)
		}
	}
}

func TestProviderDeletionRejectsActiveRunsAndAllowsPausedSnapshots(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	response := fixture.request(http.MethodGet, "/api/providers/fixture", "", true, "")
	var document model.ProviderDocument
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &document) != nil {
		t.Fatalf("read source: status %d", response.Code)
	}
	body, err := json.Marshal(map[string]string{"revision": document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	run, err := fixture.db.CreateRun(fixture.ctx, model.Run{ProviderID: document.Provider.ID, Config: document.Provider, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	rejectDeletion := func() {
		t.Helper()
		response := fixture.request(http.MethodDelete, "/api/providers/fixture", string(body), true, "")
		if response.Code != http.StatusConflict {
			t.Fatalf("active source deletion: status %d", response.Code)
		}
		response = fixture.request(http.MethodGet, "/api/providers/fixture", "", true, "")
		var retained model.ProviderDocument
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &retained) != nil || retained.Revision != document.Revision {
			t.Fatal("rejected deletion changed the source definition")
		}
	}
	rejectDeletion() // Queued.
	claimed, err := fixture.db.ClaimNext(fixture.ctx)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("claim active source: %v", err)
	}
	rejectDeletion() // Running.
	held, err := fixture.db.FinishRun(fixture.ctx, run.ID, model.StatusPaused, "", model.PauseBudget)
	if err != nil {
		t.Fatal(err)
	}
	response = fixture.request(http.MethodDelete, "/api/providers/fixture", string(body), true, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("paused-only source deletion: status %d", response.Code)
	}
	if response := fixture.request(http.MethodGet, "/api/providers/fixture", "", true, ""); response.Code != http.StatusNotFound {
		t.Fatalf("deleted definition remained accessible: status %d", response.Code)
	}
	retained, err := fixture.db.GetRun(fixture.ctx, run.ID)
	if err != nil || retained.Status != model.StatusPaused || !reflect.DeepEqual(retained.Config, held.Config) {
		t.Fatalf("definition deletion changed its paused immutable snapshot: %v", err)
	}
}

func TestProviderSchemaDoesNotShadowSourceIdentity(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	definition := `{"version":1,"id":"schema","name":"Schema-named source","adapter":"http_json","url":"https://source.invalid/api","enabled":false}`
	body, err := json.Marshal(map[string]string{"json": definition})
	if err != nil {
		t.Fatal(err)
	}
	if response := fixture.request(http.MethodPut, "/api/providers/schema", string(body), true, ""); response.Code != http.StatusOK {
		t.Fatalf("create schema-named source: status %d", response.Code)
	}
	response := fixture.request(http.MethodGet, "/api/providers/schema", "", true, "")
	var document model.ProviderDocument
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &document) != nil || document.Provider.ID != "schema" {
		t.Fatal("schema endpoint shadowed a valid source identity")
	}
	response = fixture.request(http.MethodGet, "/api/provider-schema", "", false, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("schema exposed without authentication: status %d", response.Code)
	}
	response = fixture.request(http.MethodGet, "/api/provider-schema", "", true, "")
	var schema struct {
		Type string `json:"type"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &schema) != nil || schema.Type != "object" {
		t.Fatal("authenticated schema endpoint did not return an object schema")
	}
}
