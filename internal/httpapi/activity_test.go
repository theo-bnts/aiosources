package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestLogsFilterTMDBAllDebridAndTorBoxServices(t *testing.T) {
	f, _, _ := addonAPI(t)
	for _, service := range []string{"tmdb", "alldebrid", "torbox"} {
		if err := f.db.AIOIncident(f.ctx, service, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, service := range []string{"tmdb", "alldebrid", "torbox"} {
		response := f.request("GET", "/api/logs?service="+service, "", true, "")
		var logs model.List[model.LogEntry]
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &logs) != nil || logs.Total != 1 || logs.Items[0].Service != service {
			t.Fatalf("service %s was not filterable: %d %s", service, response.Code, response.Body.String())
		}
	}
	if response := f.request("GET", "/api/logs?service=unknown", "", true, ""); response.Code != http.StatusBadRequest && response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown service was accepted: %d", response.Code)
	}
}

func TestLogsReplaceNotificationsWithImportantEventsAndOptionalDebug(t *testing.T) {
	f, _, _ := addonAPI(t)
	run, err := f.db.CreateRun(f.ctx, model.Run{ProviderID: "synthetic", Config: model.Provider{ID: "synthetic"}, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"page_saved", "failed"} {
		if _, err := f.db.AddEvent(f.ctx, run.ID, kind, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		query string
		count int64
	}{{"", 2}, {"&level=debug", 1}, {"&level=debug,info,warn,error", 3}} {
		response := f.request("GET", "/api/logs?run="+run.ID+test.query, "", true, "")
		var logs model.List[model.LogEntry]
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &logs) != nil || logs.Total != test.count {
			t.Fatalf("logs lost important events or defaulted to request noise: %d %+v", response.Code, logs)
		}
	}
	for _, path := range []string{"/api/notifications", "/api/webhooks"} {
		if response := f.request("GET", path, "", true, ""); response.Code != http.StatusNotFound {
			t.Fatalf("retired API remains active: %s %d", path, response.Code)
		}
	}
}
