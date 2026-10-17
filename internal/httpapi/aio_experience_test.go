package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestIdentificationReviewAndStremioHistoryRequireAdministratorAndCSRF(t *testing.T) {
	f, addon, conn := addonAPI(t)
	for _, path := range []string{"/api/aiosource/matches/invalid", "/api/aiosource/candidates?q=example", "/api/stremio/statistics", "/api/stremio/requests", "/api/stremio/requests/1"} {
		if response := f.request("GET", path, "", false, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("admin-only history exposed at %s", path)
		}
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('one','item','{"title":"Example Film","download_url":"https://synthetic.invalid/private-value"}');
 INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','101','tt1234567',ARRAY[2021]);
 INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,'Example Film','example film' FROM ingest.aio_works`); err != nil {
		t.Fatal(err)
	}
	items, _ := f.db.ListAIOMatchDiagnostics(f.ctx, model.ListOptions{})
	id := items.Items[0].OccurrenceID
	detailResponse := f.request("GET", "/api/aiosource/matches/"+id, "", true, "")
	if detailResponse.Code != 200 || strings.Contains(detailResponse.Body.String(), "private-value") {
		t.Fatal("review exposes private fields")
	}
	var detail model.AIOReviewDetail
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	var works struct{ Items []model.AIOWork }
	response := f.request("GET", "/api/aiosource/candidates?q=Example&type=movie", "", true, "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &works) != nil || len(works.Items) != 1 {
		t.Fatal("partial review search failed")
	}
	input, _ := json.Marshal(model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, WorkID: works.Items[0].ID, WorkRevision: works.Items[0].Revision})
	noCSRF := httptest.NewRequest("PUT", "/api/aiosource/matches/"+id, strings.NewReader(string(input))).WithContext(f.ctx)
	noCSRF.AddCookie(f.cookie)
	noCSRF.Header.Set("Content-Type", "application/json")
	denied := httptest.NewRecorder()
	f.handler.ServeHTTP(denied, noCSRF)
	if denied.Code != 403 {
		t.Fatal("review bypassed CSRF")
	}
	if response = f.request("PUT", "/api/aiosource/matches/"+id, string(input), true, ""); response.Code != 204 {
		t.Fatalf("review rejected: %d %s", response.Code, response.Body.String())
	}
	token, _ := testPrivateInstallation(t, f.ctx, addon)
	response = f.request("GET", "/stremio/"+token+"/stream/movie/tt1234567.json", "", false, "")
	if response.Code != 200 {
		t.Fatal("public stream failed")
	}
	response = f.request("GET", "/api/stremio/requests", "", true, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), token) {
		t.Fatal("history contains installation token")
	}
	var history model.List[model.StremioRequest]
	if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil || history.Total != 1 || history.Items[0].InvalidHash != 1 {
		t.Fatal("public stream result not recorded", err)
	}
	if response = f.request("GET", "/stremio/"+token+"/stream/movie/invalid-suffix", "", false, ""); response.Code != 400 {
		t.Fatal("malformed route was accepted")
	}
	if response = f.request("GET", "/stremio/invalid/stream/movie/tt1234567.json", "", false, ""); response.Code != 401 {
		t.Fatal("unauthorized history input accepted")
	}
	response = f.request("GET", "/api/stremio/requests", "", true, "")
	if json.Unmarshal(response.Body.Bytes(), &history) != nil || history.Total != 2 {
		t.Fatal("rejected authorized resource missing, or unauthorized input logged")
	}
	if response = f.request("GET", "/api/raw", "", true, ""); response.Code != 400 {
		t.Fatal("global processed-record search remains available")
	}
	response = f.request("GET", "/stremio/"+token+"/configure", "", false, "")
	if response.Code != 303 || response.Header().Get("Location") != "/addon/settings" {
		t.Fatal("configuration no longer opens addon settings")
	}
}
