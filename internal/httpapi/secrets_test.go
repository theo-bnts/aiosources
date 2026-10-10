package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSecretDeletionProtectsDefinitionsAndImmutableRunSnapshots(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	const secretPath = "/api/secrets/source-token"
	if response := fixture.request(http.MethodPut, secretPath, `{"value":"synthetic-source-token","revision":0}`, true, ""); response.Code != http.StatusNoContent {
		t.Fatalf("create secret: status %d", response.Code)
	}
	secrets, err := fixture.db.ListSecrets(fixture.ctx)
	if err != nil || len(secrets) != 1 {
		t.Fatalf("secret metadata: %+v %v", secrets, err)
	}
	deleteBody, err := json.Marshal(map[string]int64{"revision": secrets[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	const definition = `{"version":1,"id":"secret-api","name":"Secret fixture","adapter":"http_json","url":"https://source.invalid/api","enabled":false}`
	save := func(source, revision string) model.ProviderDocument {
		t.Helper()
		body, err := json.Marshal(map[string]string{"json": source, "revision": revision})
		if err != nil {
			t.Fatal(err)
		}
		response := fixture.request(http.MethodPut, "/api/providers/secret-api", string(body), true, "")
		var document model.ProviderDocument
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &document) != nil {
			t.Fatalf("save source: status %d", response.Code)
		}
		return document
	}
	document := save(definition[:len(definition)-1]+`,"http":{"secret_headers":{"X-Token":"source-token"}}}`, "")
	if response := fixture.request(http.MethodDelete, secretPath, string(deleteBody), true, ""); response.Code != http.StatusConflict {
		t.Fatalf("referenced provider secret was removed: status %d", response.Code)
	}
	run, err := fixture.db.CreateRun(fixture.ctx, model.Run{ProviderID: document.Provider.ID, Config: document.Provider, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	save(definition, document.Revision)
	if response := fixture.request(http.MethodDelete, secretPath, string(deleteBody), true, ""); response.Code != http.StatusConflict {
		t.Fatalf("queued snapshot lost its secret after a definition edit: status %d", response.Code)
	}
	if response := fixture.request(http.MethodPost, "/api/runs/"+run.ID+"/pause", "{}", true, ""); response.Code != http.StatusOK {
		t.Fatalf("pause snapshot: status %d", response.Code)
	}
	if response := fixture.request(http.MethodDelete, secretPath, string(deleteBody), true, ""); response.Code != http.StatusConflict {
		t.Fatalf("paused snapshot lost its secret after a definition edit: status %d", response.Code)
	}
	if response := fixture.request(http.MethodPost, "/api/runs/"+run.ID+"/cancel", "{}", true, ""); response.Code != http.StatusOK {
		t.Fatalf("cancel snapshot: status %d", response.Code)
	}
	if response := fixture.request(http.MethodDelete, secretPath, string(deleteBody), true, ""); response.Code != http.StatusNoContent {
		t.Fatalf("unreferenced secret could not be removed: status %d", response.Code)
	}
	if response := fixture.request(http.MethodDelete, secretPath, string(deleteBody), true, ""); response.Code != http.StatusNotFound {
		t.Fatalf("missing secret deletion lost its not-found contract: status %d", response.Code)
	}
}
