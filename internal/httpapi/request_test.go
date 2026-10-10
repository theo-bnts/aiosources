package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestJSONRequestContract(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"empty object", `{}`, "application/json", 200},
		{"charset", `{"value":"safe"}`, "application/json; charset=utf-8", 200},
		{"nested nullable values", `{"nested":{"items":[null,false,0,{"left":1,"right":2}]}}`, "application/json", 200},
		{"exact numeric lexeme", `{"number":123456789012345678901234567890}`, "application/json", 200},
		{"missing media type", `{}`, "", 415},
		{"wrong media type", `{}`, "text/plain", 415},
		{"root null", `null`, "application/json", 400},
		{"array", `[]`, "application/json", 400},
		{"empty body", ``, "application/json", 400},
		{"trailing document", `{} {}`, "application/json", 400},
		{"trailing junk", `{} true`, "application/json", 400},
		{"unknown field", `{"unexpected":true}`, "application/json", 400},
		{"wrong field type", `{"value":12}`, "application/json", 400},
		{"duplicate field", `{"value":"first","value":"second"}`, "application/json", 400},
		{"escaped duplicate", `{"value":"first","\u0076alue":"second"}`, "application/json", 400},
		{"nested duplicate", `{"nested":{"enabled":true,"enabled":false}}`, "application/json", 400},
		{"array object duplicate", `{"nested":[{"enabled":true,"enabled":false}]}`, "application/json", 400},
		{"too deep", `{"nested":` + strings.Repeat(`[`, 70) + `0` + strings.Repeat(`]`, 70) + `}`, "application/json", 400},
		{"too large", `{"value":"` + strings.Repeat("a", 1<<20) + `"}`, "application/json", 400},
		{"invalid UTF8", "{\"value\":\"\xff\"}", "application/json", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/fixture", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			var input struct {
				Value  string          `json:"value"`
				Nested json.RawMessage `json:"nested"`
				Number json.Number     `json:"number"`
			}
			ok := decodeJSON(response, request, &input)
			if response.Code != test.status || ok != (test.status == 200) {
				t.Fatalf("request accepted=%v status=%d, want %d", ok, response.Code, test.status)
			}
			if test.name == "exact numeric lexeme" && input.Number.String() != "123456789012345678901234567890" {
				t.Fatal("validation changed an exact numeric value")
			}
		})
	}
}

func TestQueryPaginationContract(t *testing.T) {
	for _, test := range []struct {
		query string
		allow bool
		valid bool
	}{
		{"", false, true},
		{"limit=100&offset=1000000", false, true},
		{"q=%zz", false, false},
		{"q=a&q=b", false, false},
		{"limit=10&limit=20", false, false},
		{"offset=0&offset=1", false, false},
		{"limit=0", false, false},
		{"limit=201", false, false},
		{"limit=1.5", false, false},
		{"offset=-1", false, false},
		{"offset=1000001", false, false},
		{"offset=999999999999999999999999", false, false},
		{"provider=first&provider=second", false, false},
		{"provider=first&provider=second", true, true},
	} {
		t.Run(test.query+map[bool]string{true: "/search", false: "/list"}[test.allow], func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/fixture?"+test.query, nil)
			w := httptest.NewRecorder()
			var repeated []string
			if test.allow {
				repeated = []string{"provider"}
			}
			_, valid := listOptions(w, r, repeated...)
			if valid != test.valid || (!valid && w.Code != http.StatusBadRequest) {
				t.Fatalf("pagination valid=%v status=%d", valid, w.Code)
			}
		})
	}
}

func TestBusyResponseAppliesToEveryOperation(t *testing.T) {
	w := httptest.NewRecorder()
	respondError(w, model.ErrBusy)
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "provider") || !strings.Contains(w.Body.String(), "operation") {
		t.Fatal("busy response incorrectly identifies a particular service")
	}
}
