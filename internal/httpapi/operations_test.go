package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestOperationHistoryAPIAuthenticationFiltersAndMissingRuns(t *testing.T) {
	f, addon, _ := addonAPI(t)
	for _, route := range []string{"/api/operations", "/api/operations/missing", "/api/operations/missing/events"} {
		if response := f.request("GET", route, "", false, ""); response.Code != 401 {
			t.Fatalf("unprotected history %s: %d", route, response.Code)
		}
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	response := f.request("GET", "/api/operations?group=metadata&kind=matching&service=local&status=succeeded&limit=1", "", true, "")
	var page model.List[model.OperationRun]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("history contract %d %s", response.Code, response.Body.String())
	}
	id := page.Items[0].ID
	for _, route := range []string{"/api/operations/" + id, "/api/operations/" + id + "/events?limit=1"} {
		if response := f.request("GET", route, "", true, ""); response.Code != 200 {
			t.Fatalf("details %s: %d", route, response.Code)
		}
	}
	for _, query := range []string{"kind=unknown", "service=unknown", "status=unknown", "group=unknown", "limit=NaN", "offset=-1", "kind=metadata&kind=cache"} {
		if response := f.request("GET", "/api/operations?"+query, "", true, ""); response.Code != 400 {
			t.Fatalf("accepted invalid history filter %s: %d", query, response.Code)
		}
	}
	for _, route := range []string{"/api/operations/missing", "/api/operations/missing/events"} {
		if response := f.request("GET", route, "", true, ""); response.Code != 404 {
			t.Fatalf("missing detail %d", response.Code)
		}
	}
	response = f.request("GET", "/api/operations?group=debrid", "", true, "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Total != 0 || page.Items == nil {
		t.Fatalf("empty debrid history %d %s", response.Code, response.Body.String())
	}
}
