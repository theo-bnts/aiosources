package connectors

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestHTTPJSONPaginationDiagnosticsNeverExposeUntrustedValues(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		context            map[string]any
	}{
		{"position mismatch", `{"items":[{"id":"one"}],"current":8,"total":12}`, "position_mismatch", map[string]any{"expected_position": 2, "actual_position": 8}},
		{"invalid position", `{"items":[],"current":"credential-secret","total":12}`, "invalid_position", nil},
		{"invalid total", `{"items":[],"current":2,"total":"https://private.example/?token=credential-secret"}`, "invalid_total", nil},
		{"unsafe numeric total", `{"items":[],"current":2,"total":9007199254740993}`, "total_changed", map[string]any{"expected_total": 12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Private", "credential-secret")
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.HTTP.ItemsPath = "/items"
			p.Pagination = model.Pagination{Type: "offset", CurrentPath: "/current", TotalPath: "/total"}
			total := 12
			cursor := protocolCursor(t, jsonCursor{Version: 1, Offset: 2, Total: &total})
			page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), cursor)
			if err == nil || page.Done || !bytes.Equal(page.Next, cursor) || page.Metadata["failure_reason"] != tc.reason {
				t.Fatalf("rejection lost reason or advanced checkpoint: %+v %v", page.Metadata, err)
			}
			for key, want := range tc.context {
				if page.Metadata[key] != want {
					t.Fatalf("missing %s: %+v", key, page.Metadata)
				}
			}
			if tc.name == "unsafe numeric total" && page.Metadata["actual_total"] != nil {
				t.Fatalf("unsafe browser integer published: %+v", page.Metadata)
			}
			encoded, err := json.Marshal(page.Metadata)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded)+page.Error, "credential-secret") || strings.Contains(string(encoded), "private.example") {
				t.Fatalf("upstream data escaped diagnostics: %s %s", encoded, page.Error)
			}
		})
	}
}

func TestFailureDiagnosticsRejectNonFiniteAndPrivateContext(t *testing.T) {
	unsafe := map[string]any{
		"failure_reason": "credential-secret", "expected_total": "credential-secret",
		"actual_total": json.Number("9007199254740993"), "expected_position": -1,
		"actual_position": math.NaN(), "offset": 1.5, "page": math.Inf(1), "http_status": 999,
		"requested_page": "credential-secret", "record_index": json.Number("9007199254740993"),
		"Authorization": "credential-secret",
		"field_name":    "credential-secret",
	}
	public := make(map[string]any)
	CopyFailureDiagnostics(public, unsafe)
	if len(public) != 0 {
		t.Fatalf("unsafe diagnostics accepted: %+v", public)
	}
	SanitizeFailureDiagnostics(unsafe)
	if !reflect.DeepEqual(unsafe, map[string]any{"Authorization": "credential-secret"}) {
		t.Fatalf("diagnostic namespace not sanitized: %+v", unsafe)
	}
	CopyFailureDiagnostics(public, map[string]any{"failure_reason": "total_changed", "expected_total": json.Number("9007199254740991"), "actual_total": float64(0), "offset": int64(3), "http_status": 200})
	want := map[string]any{"failure_reason": "total_changed", "expected_total": int64(9007199254740991), "actual_total": int64(0), "offset": int64(3), "http_status": int64(200)}
	if !reflect.DeepEqual(public, want) {
		t.Fatalf("safe boundary diagnostics changed: %+v", public)
	}
}

func TestHTMLFieldDiagnosticsExposeOnlyAValidatedFieldName(t *testing.T) {
	data := map[string]any{"failure_reason": "html_field_invalid", "field_name": "info_hash", "record_index": 3, "field_value": "private-credential"}
	public := make(map[string]any)
	CopyFailureDiagnostics(public, data)
	want := map[string]any{"failure_reason": "html_field_invalid", "field_name": "info_hash", "record_index": int64(3)}
	if !reflect.DeepEqual(public, want) {
		t.Fatalf("HTML field diagnostics lost context or retained a value: %+v", public)
	}
	data["field_name"] = "https://private.invalid/?token=private-credential"
	SanitizeFailureDiagnostics(data)
	if data["field_name"] != nil {
		t.Fatal("HTML diagnostic field name retained untrusted text")
	}
}
