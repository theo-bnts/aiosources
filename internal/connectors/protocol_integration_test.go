package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func protocolProvider(adapter, endpoint string) model.Provider {
	return model.Provider{Version: 1, ID: "fixture", Name: "Synthetic fixture", Adapter: adapter, URL: endpoint, Enabled: true, RequestInterval: "1ns", PageSize: 2, Auth: model.Auth{Type: "none"}}
}

func protocolSecrets(context.Context, string) (string, error) {
	return "fixture-secret-value", nil
}

func protocolOpen(t *testing.T, p model.Provider, mode model.RunMode, env Environment) Connector {
	t.Helper()
	c, err := New(t.Context(), p, mode, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func protocolCursor(t *testing.T, state any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func protocolRaw(t *testing.T, page model.Page, body string, fragments ...string) {
	t.Helper()
	if string(page.Body) != body {
		t.Fatalf("response bytes changed: got %q, want %q", page.Body, body)
	}
	if len(page.Items) != len(fragments) {
		t.Fatalf("retained %d records, want %d", len(page.Items), len(fragments))
	}
	for i, fragment := range fragments {
		if string(page.Items[i].Raw) != fragment {
			t.Errorf("record %d bytes changed: got %q, want %q", i, page.Items[i].Raw, fragment)
		}
	}
}

func TestHTTPJSONLosslessMappingFailureAndProjection(t *testing.T) {
	const valid = `{ "id":9007199254740993, "title":"A\\B", "size":0, "peers":null, "vendor":{"ratio":1.2300e+2} }`
	const invalid = `{ "title":"unidentified", "size":"not-a-number", "untouched":[true,null] }`
	body := "{\n\"data/set\":{\"~items\":[" + valid + ",\n" + invalid + "]}}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
	t.Cleanup(server.Close)
	p := protocolProvider("http_json", server.URL)
	p.HTTP.ItemsPath = "/data~1set/~0items"
	p.Pagination.Type = "none"
	p.Mapping = model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title", "size": "/size", "peers": "/peers", "vendor": "/vendor", "absent": "/absent"}}
	p.Output.Fields = []string{"title"}
	page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	protocolRaw(t, page, body, valid, invalid)
	if !page.Done || page.Items[0].SourceID != "9007199254740993" || page.Items[0].Error != "" {
		t.Fatalf("lossless stable identity was not collected: %#v", page)
	}
	fields := page.Items[0].Fields
	if fields["size"] != int64(0) {
		t.Fatalf("present zero or unprojected canonical size lost: %#v", fields)
	}
	for _, name := range []string{"peers", "absent"} {
		if _, exists := fields[name]; exists {
			t.Errorf("absent/null field %q was invented", name)
		}
	}
	vendor, ok := fields["vendor"].(map[string]any)
	if !ok || vendor["ratio"] != json.Number("1.2300e+2") {
		t.Fatalf("unknown numeric value lost precision or lexeme: %#v", fields["vendor"])
	}
	if page.Items[1].SourceID != "" || page.Items[1].Error == "" || page.Items[1].Ignored {
		t.Fatalf("unmappable original was not retained as an error: %#v", page.Items[1])
	}
}

func TestNativePublishedAtNormalization(t *testing.T) {
	for _, adapter := range []string{"http_json", "torznab"} {
		for _, unit := range []string{"", "seconds", "milliseconds"} {
			t.Run(adapter+"/"+unit, func(t *testing.T) {
				negative, positive := "1969-12-31T23:59:59Z", "1970-01-01T00:00:01Z"
				lower, upper := "-62135596800", "253402300799"
				before, after := "-62135596801", "253402300800"
				last := "9999-12-31T23:59:59Z"
				if unit == "milliseconds" {
					negative, positive = "1969-12-31T23:59:59.999Z", "1970-01-01T00:00:00.001Z"
					lower, upper = "-62135596800000", "253402300799999"
					before, after = "-62135596800001", "253402300800000"
					last = "9999-12-31T23:59:59.999Z"
				}
				cases := []struct {
					id      string
					input   string
					number  bool
					want    string
					invalid bool
				}{
					{"rfc3339", "2024-01-02T03:04:05.123456789+02:00", false, "2024-01-02T01:04:05.123456789Z", false},
					{"rfc5322", "Tue, 02 Jan 2024 03:04:05 -0500", false, "2024-01-02T08:04:05Z", false},
					{"missing", "", false, "", false},
					{"epoch", "0", true, "1970-01-01T00:00:00Z", unit == ""},
					{"negative", "-1", true, negative, unit == ""},
					{"integer_string", "1", false, positive, unit == ""},
					{"first_year", lower, true, "0001-01-01T00:00:00Z", unit == ""},
					{"last_year", upper, true, last, unit == ""},
					{"fraction", "1.5", true, "", true},
					{"fraction_string", "1.5", false, "", true},
					{"overflow", "9223372036854775808", true, "", true},
					{"before_first_year", before, true, "", true},
					{"after_last_year", after, true, "", true},
					{"invalid_text", "not-a-date", false, "", true},
					{"utc_before_first_year", "0001-01-01T00:00:00+01:00", false, "", true},
					{"utc_after_last_year", "9999-12-31T23:59:59-01:00", false, "", true},
				}
				fragments := make([]string, len(cases))
				for i, tc := range cases {
					if adapter == "torznab" {
						date := ""
						if tc.input != "" {
							date = "<pubDate>" + tc.input + "</pubDate>"
						}
						fragments[i] = `<item><guid>` + tc.id + `</guid><title>Same title</title>` + date + `<vendor> unchanged </vendor></item>`
					} else {
						date := ""
						if tc.input != "" {
							value := tc.input
							if !tc.number {
								encoded, err := json.Marshal(value)
								if err != nil {
									t.Fatal(err)
								}
								value = string(encoded)
							}
							date = `, "published_at":` + value
						}
						fragments[i] = `{ "id":"` + tc.id + `", "title":"Same title"` + date + `, "vendor":1.2300e+2 }`
					}
				}
				body := "{\n\"items\":[" + strings.Join(fragments, ",\n") + "]}\n"
				caps := strings.ReplaceAll(protocolCaps, `="2"`, `="100"`)
				if adapter == "torznab" {
					body = fmt.Sprintf(`<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="0" total="%d"/>%s</channel></rss>`, len(cases), strings.Join(fragments, "\n"))
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("t") == "caps" {
						_, _ = io.WriteString(w, caps)
						return
					}
					_, _ = io.WriteString(w, body)
				}))
				t.Cleanup(server.Close)
				p := protocolProvider(adapter, server.URL)
				p.PageSize = 100
				if unit != "" {
					p.Options = map[string]any{"published_at_unit": unit}
				}
				if adapter == "http_json" {
					p.HTTP.ItemsPath = "/items"
					p.Pagination.Type = "none"
					p.Mapping.Fields = map[string]string{"title": "/title", "published_at": "/published_at"}
				}
				page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
				if err != nil || !page.Done {
					t.Fatalf("publication-date page did not complete: %+v, %v", page, err)
				}
				offset := 0
				if adapter == "torznab" {
					fragments = append([]string{caps}, fragments...)
					offset = 1
				}
				protocolRaw(t, page, body, fragments...)
				for i, tc := range cases {
					record := page.Items[i+offset]
					if record.SourceID != tc.id || record.Fields["title"] != "Same title" || (record.Error != "") != tc.invalid {
						t.Errorf("%s: identity, other fields, or error changed: %+v", tc.id, record)
					}
					value, exists := record.Fields["published_at"]
					if tc.invalid || tc.want == "" {
						if exists {
							t.Errorf("%s: missing/invalid date invented: %#v", tc.id, value)
						}
					} else if value != tc.want {
						t.Errorf("%s: publication date = %#v, want %q", tc.id, value, tc.want)
					}
				}
			})
		}
	}
}

func TestPublishedAtUnitValidation(t *testing.T) {
	for _, adapter := range []string{"http_json", "torznab"} {
		t.Run(adapter, func(t *testing.T) {
			p := protocolProvider(adapter, "https://example.com/api")
			for _, unit := range []string{"seconds", "milliseconds"} {
				p.Options = map[string]any{"published_at_unit": unit}
				if err := Validate(p); err != nil {
					t.Errorf("supported unit %q rejected: %v", unit, err)
				}
			}
			for _, option := range []map[string]any{
				{"published_at_unit": ""},
				{"published_at_unit": "nanoseconds"},
				{"published_at_unit": 1000},
				{"published_at_unit": nil},
				{"published_at_unit": "seconds", "unknown": true},
			} {
				p.Options = option
				if err := Validate(p); !errors.Is(err, model.ErrInvalid) {
					t.Errorf("invalid adapter options accepted: %#v, %v", option, err)
				}
			}
		})
	}
	p := remoteProvider("https://example.com/api/catalogs/share")
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{"seconds", "milliseconds"} {
		p.Options = map[string]any{"published_at_unit": unit}
		if err := Validate(p); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("remote catalogue accepted normalization option %q: %v", unit, err)
		}
	}
}

func TestHTTPJSONPaginationSurvivesReopen(t *testing.T) {
	for _, kind := range []string{"page", "offset", "cursor", "next_url"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			var p model.Provider
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				step := int(calls.Add(1)) - 1
				if step > 1 {
					t.Errorf("request after explicit completion: %s", r.URL)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				current, total := step+1, 3
				next := "null"
				switch kind {
				case "page":
					var body map[string]any
					decoder := json.NewDecoder(r.Body)
					decoder.UseNumber()
					if err := decoder.Decode(&body); err != nil || r.Method != http.MethodPost || body["page"] != json.Number(fmt.Sprint(current)) || body["limit"] != json.Number("2") || body["scope"] != "archive" {
						t.Errorf("resumed POST pagination: method=%s body=%#v err=%v", r.Method, body, err)
					}
					if step == 0 {
						next = "2"
					}
				case "offset":
					current, total = 4+step, 7
					if r.URL.Query().Get("offset") != fmt.Sprint(current) {
						t.Errorf("offset did not advance by actual returned count: %s", r.URL)
					}
					if step == 0 {
						next = "5"
					}
				case "cursor":
					current = step
					want := ""
					if step == 1 {
						want = "opaque-A"
					}
					if r.URL.Query().Get("cursor") != want {
						t.Errorf("cursor not restored: %s", r.URL)
					}
					if step == 0 {
						next = `"opaque-A"`
					}
				case "next_url":
					if step == 0 {
						if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("scope") != "archive" {
							t.Errorf("initial URL query: %s", r.URL)
						}
						next = `"/continuation?opaque=A"`
					} else if r.URL.Path != "/continuation" || r.URL.RawQuery != "opaque=A" {
						t.Errorf("server continuation was altered: %s", r.URL)
					}
				}
				items := `{ "id":"first" }`
				if step == 1 {
					items = `{ "id":"second" }, { "id":"third" }`
				}
				_, _ = fmt.Fprintf(w, `{"items":[%s],"current":%d,"total":%d,"next":%s}`, items, current, total, next)
			}))
			t.Cleanup(server.Close)
			p = protocolProvider("http_json", server.URL+"/listing")
			p.HTTP.ItemsPath = "/items"
			p.Pagination = model.Pagination{Type: kind, NextPath: "/next", TotalPath: "/total", CurrentPath: "/current"}
			if kind == "page" {
				p.Pagination.Start, p.Pagination.In = 1, "body"
				p.HTTP.Method, p.HTTP.Body = http.MethodPost, map[string]any{"scope": "archive"}
			}
			if kind == "offset" {
				p.Pagination.Start = 4
			}
			if kind == "next_url" {
				p.Pagination.Type, p.Pagination.Start = "page", 1
				p.HTTP.Query = map[string]any{"scope": "archive"}
			}
			firstConnector := protocolOpen(t, p, model.ModeFull, Environment{})
			first, err := firstConnector.Fetch(t.Context(), nil)
			if err != nil || first.Done || len(first.Items) != 1 {
				t.Fatalf("short first page must continue: %#v, %v", first, err)
			}
			_ = firstConnector.Close()
			resumed := protocolOpen(t, p, model.ModeFull, Environment{})
			last, err := resumed.Fetch(t.Context(), first.Next)
			if err != nil || !last.Done || len(last.Items) != 2 || last.Items[1].SourceID != "third" {
				t.Fatalf("resumed final page: %#v, %v", last, err)
			}
			terminal, err := resumed.Fetch(t.Context(), last.Next)
			if err != nil || !terminal.Done || calls.Load() != 2 {
				t.Fatalf("done cursor performed more work: calls=%d page=%#v err=%v", calls.Load(), terminal, err)
			}
		})
	}
}

func TestHTTPJSONContradictionsRetainRawWithoutAdvancing(t *testing.T) {
	const item = `{ "id":"retained", "unknown":1.00 }`
	for _, tc := range []struct {
		name    string
		kind    string
		cursor  jsonCursor
		body    string
		stalled bool
	}{
		{"empty_before_total", "offset", jsonCursor{Version: 1, Offset: 1}, `{"items":[],"total":4,"next":null}`, true},
		{"records_exceed_total", "offset", jsonCursor{Version: 1, Offset: 1}, `{"items":[` + item + `],"total":1,"next":null}`, true},
		{"early_terminator", "offset", jsonCursor{Version: 1}, `{"items":[` + item + `],"total":4,"next":null}`, true},
		{"continuation_at_total", "cursor", jsonCursor{Version: 1}, `{"items":[` + item + `],"total":1,"next":"more"}`, true},
		{"repeated_cursor", "cursor", jsonCursor{Version: 1, Offset: 1, Cursor: "same"}, `{"items":[` + item + `],"total":4,"next":"same"}`, true},
		{"skipped_page", "page", jsonCursor{Version: 1, Page: 1}, `{"items":[` + item + `],"total":4,"next":3}`, true},
		{"repeated_offset", "offset", jsonCursor{Version: 1, Offset: 1}, `{"items":[` + item + `],"total":4,"next":1}`, true},
		{"missing_next", "cursor", jsonCursor{Version: 1}, `{"items":[` + item + `],"total":4}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.HTTP.ItemsPath = "/items"
			p.Pagination = model.Pagination{Type: tc.kind, TotalPath: "/total", NextPath: "/next"}
			cursor := protocolCursor(t, tc.cursor)
			page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), cursor)
			if err == nil || page.Error == "" || page.Done || !bytes.Equal(page.Next, cursor) {
				t.Fatalf("unsafe page advanced or lost error: %#v, %v", page, err)
			}
			if tc.stalled && !errors.Is(err, model.ErrStalled) {
				t.Errorf("contradiction did not report stalled traversal: %v", err)
			}
			if tc.name == "empty_before_total" {
				protocolRaw(t, page, tc.body)
			} else {
				protocolRaw(t, page, tc.body, item)
			}
		})
	}
}

func TestHTTPJSONNextURLRejectsRepetitionAndForeignOrigin(t *testing.T) {
	var foreignCalls atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls.Add(1) }))
	t.Cleanup(foreign.Close)
	for _, foreignNext := range []bool{false, true} {
		t.Run(fmt.Sprint("foreign=", foreignNext), func(t *testing.T) {
			const item = `{ "id":"original" }`
			var body string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			t.Cleanup(server.Close)
			next := server.URL + "/next"
			if foreignNext {
				next = foreign.URL + "/steal"
			}
			body = fmt.Sprintf(`{"items":[%s],"next":%q}`, item, next)
			p := protocolProvider("http_json", server.URL)
			p.HTTP.ItemsPath = "/items"
			p.Pagination = model.Pagination{Type: "cursor", NextPath: "/next"}
			cursor := protocolCursor(t, jsonCursor{Version: 1, Offset: 1, URL: server.URL + "/next"})
			page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), cursor)
			if err == nil || page.Error == "" || page.Done || !bytes.Equal(page.Next, cursor) {
				t.Fatalf("unsafe URL accepted: %#v, %v", page, err)
			}
			protocolRaw(t, page, body, item)
		})
	}
	if foreignCalls.Load() != 0 {
		t.Fatal("foreign continuation was contacted")
	}
}

func TestClientCredentialsStayPrivateAcrossRedirectsAndPublicRequests(t *testing.T) {
	for _, auth := range []model.Auth{
		{Type: "query", Name: "apikey", SecretRef: "key"},
		{Type: "header", Name: "X-Api-Key", SecretRef: "key"},
		{Type: "bearer", SecretRef: "key"},
		{Type: "basic", UsernameRef: "user", PasswordRef: "password"},
		{Type: "cookie", SecretRef: "cookie"},
	} {
		t.Run(auth.Type, func(t *testing.T) {
			var foreignCalls, publicCalls atomic.Int32
			foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls.Add(1) }))
			t.Cleanup(foreign.Close)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/public" {
					publicCalls.Add(1)
					for _, name := range []string{"Authorization", "Cookie", "X-Api-Key", "X-Fixture-Secret", "X-Private-Context"} {
						if value := r.Header.Get(name); value != "" {
							t.Errorf("public request leaked %s: %q", name, value)
						}
					}
					if r.URL.Query().Get("apikey") != "" {
						t.Error("public request leaked query credential")
					}
					_, _ = io.WriteString(w, "public metadata")
					return
				}
				if r.URL.Path == "/public-redirect" {
					http.Redirect(w, r, foreign.URL, http.StatusFound)
					return
				}
				if r.Header.Get("X-Fixture-Secret") != "fixture-secret-value" {
					t.Error("primary secret header missing")
				}
				switch auth.Type {
				case "query":
					if r.URL.Query().Get("apikey") != "fixture-secret-value" {
						t.Error("primary query authentication missing")
					}
				case "header":
					if r.Header.Get("X-Api-Key") != "fixture-secret-value" {
						t.Error("primary header authentication missing")
					}
				case "bearer":
					if r.Header.Get("Authorization") != "Bearer fixture-secret-value" {
						t.Error("primary bearer authentication missing")
					}
				case "basic":
					user, password, ok := r.BasicAuth()
					if !ok || user != "fixture-secret-value" || password != "fixture-secret-value" {
						t.Error("primary basic authentication missing")
					}
				case "cookie":
					cookie, err := r.Cookie("session")
					if err != nil || cookie.Value != "fixture-session" {
						t.Error("primary session cookie missing")
					}
				}
				http.SetCookie(w, &http.Cookie{Name: "rotated", Value: "private-session", Path: "/"})
				http.Redirect(w, r, foreign.URL, http.StatusFound)
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.Auth = auth
			p.HTTP.SecretHeaders = map[string]string{"X-Fixture-Secret": "secret-header"}
			p.HTTP.Headers = map[string]string{"X-Private-Context": "provider-only"}
			env := Environment{Secrets: func(ctx context.Context, ref string) (string, error) {
				if ref == "cookie" {
					return "session=fixture-session", nil
				}
				return protocolSecrets(ctx, ref)
			}}
			client, err := NewClient(t.Context(), p, env)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			_, err = client.Do(t.Context(), Request{})
			if err == nil || strings.Contains(err.Error(), "fixture-secret-value") || strings.Contains(err.Error(), "fixture-session") {
				t.Fatalf("redirect did not fail safely: %v", err)
			}
			host, _ := url.Parse(server.URL)
			response, err := client.DoPublic(t.Context(), Request{URL: server.URL + "/public"}, []string{host.Hostname()})
			if err != nil || string(response.Body) != "public metadata" {
				t.Fatalf("public enrichment failed: %q, %v", response.Body, err)
			}
			_, err = client.DoPublic(t.Context(), Request{URL: server.URL + "/public-redirect"}, []string{host.Hostname()})
			if err == nil || foreignCalls.Load() != 0 || publicCalls.Load() != 1 {
				t.Fatalf("origin isolation failed: foreign=%d public=%d err=%v", foreignCalls.Load(), publicCalls.Load(), err)
			}
		})
	}
}

const protocolCaps = `<caps><limits default="2" max="2"/><searching><search available="yes" supportedParams="q"/></searching><categories><category id="2000" name="Films"/><category id="5000" name="Series"/></categories></caps>`

func TestTorznabRawFragmentsAndCategoryContinuation(t *testing.T) {
	const valid = `<item><guid>release-A</guid><title>A &amp; B</title><x:attr name="size" value="0"/><x:attr name="tag" value="first"/><x:attr name="tag" value="second"/></item>`
	const invalid = `<item><guid>release-B</guid><x:attr name="size" value="broken"/><vendor> untouched </vendor></item>`
	const last = `<item><guid>release-C</guid><title>Last scope</title></item>`
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "fixture-secret-value" {
			t.Error("SDK request bypassed primary authentication")
		}
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, protocolCaps)
			return
		}
		step := searches.Add(1)
		category, items, total := "2000", valid+invalid, 2
		if step == 2 {
			category, items, total = "5000", last, 1
		}
		if step > 2 || r.URL.Query().Get("cat") != category || r.URL.Query().Get("offset") != "0" {
			t.Errorf("scope continuation: step=%d query=%s", step, r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="0" total="%d"/>%s</channel></rss>`, total, items)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL+"/api")
	p.Auth = model.Auth{Type: "query", Name: "apikey", SecretRef: "key"}
	p.Search.Categories = []int{2000, 5000}
	p.Options = map[string]any{"category_scope": "each"}
	p.Schedule.KnownPages = 2 // Full must ignore the Incremental boundary.
	p.Output.Fields = []string{"title"}
	firstConnector := protocolOpen(t, p, model.ModeFull, Environment{Secrets: protocolSecrets})
	first, err := firstConnector.Fetch(t.Context(), nil)
	if err != nil || first.Done {
		t.Fatalf("first scope incorrectly finished traversal: %#v, %v", first, err)
	}
	body := `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="0" total="2"/>` + valid + invalid + `</channel></rss>`
	protocolRaw(t, first, body, protocolCaps, valid, invalid)
	if !first.Items[0].Auxiliary || !first.Items[0].Ignored || first.Items[1].Error != "" || first.Items[2].Error == "" {
		t.Fatalf("capabilities or malformed item not retained separately: %#v", first.Items)
	}
	attributes, ok := first.Items[1].Fields["attributes"].(map[string][]string)
	if !ok || !reflect.DeepEqual(attributes["tag"], []string{"first", "second"}) || first.Items[1].Fields["size"] != int64(0) {
		t.Fatalf("projection lost repeated attributes/zero: %#v", first.Items[1].Fields)
	}
	_ = firstConnector.Close()
	resumed := protocolOpen(t, p, model.ModeFull, Environment{Secrets: protocolSecrets})
	final, err := resumed.Fetch(t.Context(), first.Next)
	if err != nil || !final.Done {
		t.Fatalf("resumed scope did not finish: %#v, %v", final, err)
	}
	protocolRaw(t, final, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="0" total="1"/>`+last+`</channel></rss>`, protocolCaps, last)
	if first.Metadata["fingerprint_scope"] == final.Metadata["fingerprint_scope"] {
		t.Error("different category scopes cannot share a repeated-page boundary")
	}
	_, err = resumed.Fetch(t.Context(), final.Next)
	if err != nil || searches.Load() != 2 {
		t.Fatalf("terminal scope performed more searches: %d, %v", searches.Load(), err)
	}
}

func TestTorznabContradictoryResponseRetainsOriginalItems(t *testing.T) {
	const item = `<item><guid>retained</guid><title>Original</title></item>`
	for _, tc := range []struct {
		name     string
		response string
		fragment string
	}{
		{"wrong_offset", `<x:response offset="0" total="4"/>`, item},
		{"empty_before_total", `<x:response offset="1" total="4"/>`, ""},
		{"over_total", `<x:response offset="1" total="1"/>`, item},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel>` + tc.response + tc.fragment + `</channel></rss>`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("t") == "caps" {
					_, _ = io.WriteString(w, protocolCaps)
				} else {
					_, _ = io.WriteString(w, body)
				}
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("torznab", server.URL+"/api")
			cursor := protocolCursor(t, torznabCursor{Version: 1, Scopes: [][]int{{}}, Offset: 1})
			page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), cursor)
			if err == nil || page.Error == "" || page.Done || !bytes.Equal(page.Next, cursor) {
				t.Fatalf("invalid page advanced traversal: %#v, %v", page, err)
			}
			switch tc.name {
			case "wrong_offset":
				if page.Metadata["failure_reason"] != "position_mismatch" || page.Metadata["expected_position"] != 1 || page.Metadata["actual_position"] != 0 {
					t.Fatalf("SDK offset rejection lost numeric context: %+v", page.Metadata)
				}
			case "empty_before_total":
				if page.Metadata["failure_reason"] != "empty_before_total" || page.Metadata["expected_total"] != 4 || page.Metadata["actual_position"] != 1 {
					t.Fatalf("empty rejection lost numeric context: %+v", page.Metadata)
				}
			case "over_total":
				if page.Metadata["failure_reason"] != "records_exceed_total" || page.Metadata["expected_total"] != 1 || page.Metadata["actual_position"] != 2 {
					t.Fatalf("SDK total bound rejection lost numeric context: %+v", page.Metadata)
				}
			}
			fragments := []string{protocolCaps}
			if tc.fragment != "" {
				fragments = append(fragments, tc.fragment)
			}
			protocolRaw(t, page, body, fragments...)
		})
	}
}

func TestProtocolChangedAdvertisedTotalFailsWithoutAdvancing(t *testing.T) {
	for _, adapter := range []string{"http_json", "torznab"} {
		for _, returnedTotal := range []int{2, 4} {
			t.Run(fmt.Sprintf("%s/total=%d", adapter, returnedTotal), func(t *testing.T) {
				item := `{ "id":"retained", "vendor":1.00 }`
				body := fmt.Sprintf(`{"items":[%s],"total":%d}`, item, returnedTotal)
				if adapter == "torznab" {
					item = `<item><guid>retained</guid><vendor> original </vendor></item>`
					body = fmt.Sprintf(`<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="1" total="%d"/>%s</channel></rss>`, returnedTotal, item)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("t") == "caps" {
						_, _ = io.WriteString(w, protocolCaps)
						return
					}
					_, _ = io.WriteString(w, body)
				}))
				t.Cleanup(server.Close)
				p := protocolProvider(adapter, server.URL+"/api")
				previousTotal := 3
				var cursor json.RawMessage
				fragments := []string{item}
				if adapter == "http_json" {
					p.HTTP.ItemsPath = "/items"
					p.Pagination = model.Pagination{Type: "offset", TotalPath: "/total"}
					cursor = protocolCursor(t, jsonCursor{Version: 1, Offset: 1, Total: &previousTotal})
				} else {
					cursor = protocolCursor(t, torznabCursor{Version: 1, Scopes: [][]int{{}}, Offset: 1, Total: &previousTotal})
					fragments = []string{protocolCaps, item}
				}
				page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), cursor)
				if !errors.Is(err, model.ErrStalled) || page.Error == "" || page.Done || !bytes.Equal(page.Next, cursor) {
					t.Fatalf("changed advertised total accepted or progressed: %#v, %v", page, err)
				}
				if page.Metadata["failure_reason"] != "total_changed" || page.Metadata["expected_total"] != previousTotal || page.Metadata["actual_total"] != returnedTotal || page.Metadata["http_status"] != http.StatusOK {
					t.Fatalf("total rejection lost safe HTTP 200 diagnostics: %+v", page.Metadata)
				}
				protocolRaw(t, page, body, fragments...)
			})
		}
	}
}

func TestHTTPJSONShortUnboundedPagesNeedAnEmptyTerminator(t *testing.T) {
	for _, kind := range []string{"page", "offset"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				step := int(calls.Add(1)) - 1
				if step > 1 || r.URL.Query().Get(kind) != fmt.Sprint(step) {
					t.Errorf("unexpected continuation after short page: %s", r.URL)
				}
				if step == 0 {
					_, _ = io.WriteString(w, `[ { "id":"only" } ]`)
					return
				}
				_, _ = io.WriteString(w, "[]")
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.Pagination.Type = kind
			c := protocolOpen(t, p, model.ModeFull, Environment{})
			first, err := c.Fetch(t.Context(), nil)
			if err != nil || first.Done {
				t.Fatalf("short page without a declared end completed early: %#v, %v", first, err)
			}
			protocolRaw(t, first, `[ { "id":"only" } ]`, `{ "id":"only" }`)
			last, err := c.Fetch(t.Context(), first.Next)
			if err != nil || !last.Done || calls.Load() != 2 {
				t.Fatalf("empty terminal page failed: %#v, %v", last, err)
			}
			protocolRaw(t, last, "[]")
		})
	}
}
