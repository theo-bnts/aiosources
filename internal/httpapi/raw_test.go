package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestImportDiagnosticsProcessingFiltersCountsAndPagination(t *testing.T) {
	f, _, conn := addonAPI(t)
	var selectedRun string
	for _, provider := range []string{"one", "two"} {
		run, err := f.db.CreateRun(f.ctx, model.Run{ProviderID: provider, Config: model.Provider{ID: provider}, Mode: model.ModePreview})
		if err != nil {
			t.Fatal(err)
		}
		selectedRun = run.ID
		var pageID int64
		if err = conn.QueryRow(f.ctx, `INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type,payload_retained) VALUES($1,$2,0,''::bytea,'application/json',false) RETURNING id`, run.ID, provider).Scan(&pageID); err != nil {
			t.Fatal(err)
		}
		for _, item := range []struct {
			id, problem        string
			ignored, auxiliary bool
		}{
			{"accepted", "", false, false}, {"error", "Missing ID", false, false}, {"ignored", "", true, false}, {"enrichment", "", true, true}, {"priority-error", "Invalid fields", true, true},
		} {
			if _, err = conn.Exec(f.ctx, `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields,error,ignored,auxiliary,payload_retained) VALUES($1,$2,$3,$4,0,''::bytea,'application/json','{"title":"Literal %_ sample"}',$5,$6,$7,false)`, run.ID, provider, item.id, pageID, item.problem, item.ignored, item.auxiliary); err != nil {
				t.Fatal(err)
			}
		}
		for result, total := range map[string]int64{"": 5, "accepted": 1, "error": 2, "ignored": 1, "enrichment": 1} {
			response := f.request("GET", "/api/raw?provider="+provider+"&run="+run.ID+"&q=%25_&status="+result+"&limit=1", "", true, "")
			var list model.RawListing
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || list.Total != total || len(list.Items) != 1 {
				t.Fatalf("processing %q: %d %s", result, response.Code, response.Body.String())
			}
			if list.Counts != (model.RawProcessingCounts{Accepted: 1, Errors: 2, Ignored: 1, Enrichment: 1}) || list.Items[0].ProviderID != provider || list.Items[0].RunID != run.ID || list.Items[0].PayloadRetained {
				t.Fatalf("counts or provenance incorrect: %+v", list)
			}
			item := list.Items[0]
			if result == "error" && item.Error == "" || result == "ignored" && (!item.Ignored || item.Auxiliary || item.Error != "") || result == "enrichment" && (!item.Auxiliary || item.Error != "") {
				t.Fatalf("processing precedence incorrect: %+v", item)
			}
		}
	}
	response := f.request("GET", "/api/raw?run="+selectedRun+"&status=error&limit=1&offset=1", "", true, "")
	var list model.RawListing
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || list.Total != 2 || len(list.Items) != 1 || list.Counts.Errors != 2 || list.Counts.Accepted != 1 {
		t.Fatalf("paginated counts: %d %s", response.Code, response.Body.String())
	}
	if response := f.request("GET", "/api/raw?status=unknown", "", true, ""); response.Code != 400 {
		t.Fatal("invalid processing result accepted")
	}
}

func TestRawAPISeparatesUnstoredObservationsFromHistoricalArchives(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, model.Run{ProviderID: "raw-api", Config: model.Provider{ID: "raw-api"}, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if _, err := db.SavePage(ctx, *claimed, model.Page{
		Body: []byte(`{"items":[{"id":"new","title":"Parsed sample"}]}`), ContentType: "application/json", Done: true,
		Items: []model.Record{{SourceID: "new", Raw: []byte(`{"id":"new","title":"Parsed sample"}`), Fields: map[string]any{"title": "Parsed sample"}}},
	}, ""); err != nil {
		t.Fatal(err)
	}
	observations, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID, Limit: 10})
	if err != nil || len(observations.Items) != 1 {
		t.Fatalf("saved observation: %+v %v", observations, err)
	}
	current := observations.Items[0]
	conn, err := pgx.Connect(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	archived := func(sourceID string, raw, body []byte) (int64, int64) {
		t.Helper()
		var pageID, rawID int64
		// Omitting payload_retained models historical rows: an empty archived
		// payload is still a downloadable payload, unlike a new observation.
		if err := conn.QueryRow(ctx, `INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type)
			VALUES($1,$2,0,$3,'application/octet-stream') RETURNING id`, run.ID, run.ProviderID, body).Scan(&pageID); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRow(ctx, `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type)
			VALUES($1,$2,$3,$4,0,$5,'application/octet-stream') RETURNING id`, run.ID, run.ProviderID, sourceID, pageID, raw).Scan(&rawID); err != nil {
			t.Fatal(err)
		}
		return rawID, pageID
	}
	oldRaw := []byte("original\x00record\xff\n")
	oldBody := []byte("original\x00response\xff\n")
	oldID, oldPageID := archived("historical", oldRaw, oldBody)
	emptyID, emptyPageID := archived("historical-empty", []byte{}, []byte{})
	srv := &server{options: Options{Store: db}}
	request := func(handler http.HandlerFunc, id int64) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/raw/%d", id), nil).WithContext(ctx)
		r.SetPathValue("id", fmt.Sprint(id))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	for _, test := range []struct {
		name     string
		rawID    int64
		pageID   int64
		retained bool
		raw      []byte
		body     []byte
	}{
		{"new observation", current.ID, current.PageID, false, nil, nil},
		{"historical bytes", oldID, oldPageID, true, oldRaw, oldBody},
		{"historical empty payload", emptyID, emptyPageID, true, []byte{}, []byte{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			detail := request(srv.rawDetail, test.rawID)
			var result struct {
				Record     model.RawRecord `json:"record"`
				Raw        *string         `json:"raw"`
				ByteLength *int            `json:"byte_length"`
				ValidUTF8  *bool           `json:"valid_utf8"`
			}
			if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &result) != nil {
				t.Fatalf("observation detail: %d %s", detail.Code, detail.Body.String())
			}
			if result.Record.PayloadRetained != test.retained {
				t.Fatalf("payload availability: %+v", result.Record)
			}
			if test.retained {
				if result.Raw == nil || result.ByteLength == nil || *result.ByteLength != len(test.raw) || result.ValidUTF8 == nil || *result.ValidUTF8 != utf8.Valid(test.raw) {
					t.Fatalf("historical payload incorrectly unavailable: %+v", result)
				}
			} else if result.Raw != nil || result.ByteLength != nil || result.ValidUTF8 != nil || result.Record.Fields["title"] != "Parsed sample" {
				t.Fatalf("unstored payload must be explicit without losing the parsed preview: %+v", result)
			}
			for _, download := range []struct {
				name    string
				handler http.HandlerFunc
				id      int64
				want    []byte
			}{
				{"record", srv.rawDownload, test.rawID, test.raw},
				{"response", srv.pageDownload, test.pageID, test.body},
			} {
				response := request(download.handler, download.id)
				if !test.retained {
					if response.Code != http.StatusNotFound || response.Header().Get("Content-Disposition") != "" {
						t.Fatalf("%s unexpectedly downloadable: %d %s", download.name, response.Code, response.Body.String())
					}
				} else if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), download.want) || response.Header().Get("Content-Disposition") == "" {
					t.Fatalf("%s archive not preserved byte-for-byte: status %d, body %q", download.name, response.Code, response.Body.Bytes())
				}
			}
		})
	}
}
