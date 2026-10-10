package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSourceTorrentStatsRequireAdministrationAndExposeOnlyCounts(t *testing.T) {
	f := sharingAPI(t, "https://owner.example")
	const route = "/api/source-torrent-stats"
	if response := f.request(http.MethodGet, route, "", false, ""); response.Code != http.StatusUnauthorized {
		t.Fatal("source counts are public")
	}
	run, err := f.db.CreateRun(f.ctx, model.Run{ProviderID: "fixture", Mode: model.ModeIncremental, Config: model.Provider{ID: "fixture", Adapter: "http_json"}})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := f.db.ClaimNext(f.ctx)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("claim fixture: %+v %v", claimed, err)
	}
	run, err = f.db.SavePage(f.ctx, *claimed, model.Page{Items: []model.Record{{SourceID: "private-source-id", Fields: map[string]any{"title": "Private release title", "download_url": "https://source.invalid/private-path", "arbitrary": "synthetic-private-field"}}}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.FinishRun(f.ctx, run.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	response := f.request(http.MethodGet, route, "", true, "")
	var body struct {
		Items []model.SourceTorrentStats `json:"items"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Items) != 1 || body.Items[0].ProviderID != "fixture" || body.Items[0].Imported != 1 || body.Items[0].Pending != 1 || body.Items[0].Matched != 0 {
		t.Fatalf("current source counts omitted: %d %s", response.Code, response.Body.String())
	}
	for _, private := range []string{"private-path", "Private release", "synthetic-private-field", "private-source-id", "download_url"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("counts exposed a record field: %q", private)
		}
	}
}
