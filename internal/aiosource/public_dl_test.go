package aiosource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPublicDebridLinkProbeCleansOnlyConfirmedNewObjects(t *testing.T) {
	for _, scenario := range []string{"new", "existing on later page", "inventory failure", "cancelled", "incomplete"} {
		t.Run(scenario, func(t *testing.T) {
			hash := strings.Repeat("a", 40)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			adds, deletes := 0, 0
			p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-key" {
					t.Error("public check used another account")
				}
				switch r.URL.Path {
				case "/seedbox/list":
					if r.URL.Query().Get("ids") != "" {
						if scenario == "cancelled" {
							cancel()
							return
						}
						fmt.Fprint(w, `{"success":true,"value":[{"id":"probe"}]}`)
						return
					}
					if scenario == "inventory failure" {
						w.WriteHeader(503)
						return
					}
					if scenario == "existing on later page" && r.URL.Query().Get("page") == "0" {
						items := []map[string]string{}
						for i := range 100 {
							items = append(items, map[string]string{"id": fmt.Sprint("prior", i)})
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "value": items})
					} else if scenario == "existing on later page" {
						fmt.Fprint(w, `{"success":true,"value":[{"id":"probe"}]}`)
					} else {
						fmt.Fprint(w, `{"success":true,"value":[]}`)
					}
				case "/seedbox/add":
					adds++
					var input map[string]any
					_ = json.NewDecoder(r.Body).Decode(&input)
					if input["url"] != hash {
						t.Error("probe included a source URL or announce")
					}
					if scenario == "incomplete" {
						fmt.Fprint(w, `{"success":true,"value":{"id":"probe"}}`)
					} else if scenario == "cancelled" {
						fmt.Fprintf(w, `{"success":true,"value":{"id":"probe","hashString":"%s","isZip":true}}`, hash)
					} else {
						fmt.Fprintf(w, `{"success":true,"value":{"id":"probe","hashString":"%s","downloadPercent":100,"files":[{"name":"Example.mkv","size":100,"downloadPercent":100,"downloadUrl":"https://provider.example/private"}]}}`, hash)
					}
				case "/seedbox/probe/remove":
					deletes++
					if r.Method != http.MethodDelete {
						t.Error("unexpected removal method")
					}
					fmt.Fprint(w, `{"success":true,"value":["probe"]}`)
				default:
					t.Error("unexpected endpoint")
				}
			})
			items, err := p.dlPublicCheck(ctx, "synthetic-key", hash)
			if scenario == "new" || scenario == "existing on later page" {
				if err != nil || len(items) != 1 || !items[0].Shareable || !items[0].Cached {
					t.Fatal("verified cache evidence lost", err)
				}
			} else if err == nil || len(items) != 0 {
				t.Fatal("failed or incomplete result was accepted", err)
			}
			wantAdds, wantDeletes := 1, 1
			if scenario == "inventory failure" {
				wantAdds, wantDeletes = 0, 0
			} else if scenario == "existing on later page" || scenario == "incomplete" {
				wantDeletes = 0
			}
			if adds != wantAdds || deletes != wantDeletes {
				t.Fatal("ownership protection or compensation failed", adds, deletes)
			}
		})
	}
}
