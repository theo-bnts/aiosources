package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func extendedProviderTest(t *testing.T, handler http.HandlerFunc) *ProviderHTTP {
	t.Helper()
	p := providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-key" || r.URL.Query().Has("apikey") || r.URL.Query().Has("token") {
			t.Error("provider credential transport changed")
		}
		handler(w, r)
	})
	p.RDBase, p.PMBase, p.DLBase, p.DBBase = p.ADBase, p.ADBase, p.ADBase, p.ADBase
	return p
}

func TestProviderFilesRejectAmbiguousDuplicatePaths(t *testing.T) {
	// Selection by path must not silently choose one of two different
	// downloads with the same name, even when their sizes are equal.
	if validProviderFiles([]model.AIOFile{{Path: "Example.S01E02.mkv", Size: 100, Link: "https://cdn.example/one"}, {Path: "Example.S01E02.mkv", Size: 100, Link: "https://cdn.example/two"}}) {
		t.Fatal("ambiguous file list accepted")
	}
}

func TestExtendedProviderCredentialsRequireUsablePremiumAccounts(t *testing.T) {
	for _, tc := range []struct {
		service, path, response string
		valid                   bool
	}{
		{"realdebrid", "/user", `{"type":"premium","premium":3600}`, true},
		{"realdebrid", "/user", `{"type":"free","premium":0}`, false},
		{"realdebrid", "/user", `{}`, false},
		{"premiumize", "/account/info", fmt.Sprintf(`{"status":"success","customer_id":"1","premium_until":%d}`, time.Now().Add(time.Hour).Unix()), true},
		{"premiumize", "/account/info", `{"status":"success","customer_id":"1","premium_until":null}`, false},
		{"debridlink", "/account/infos", `{"success":true,"value":{"accountType":1,"premiumLeft":3600}}`, true},
		{"debridlink", "/account/infos", `{"success":true,"value":{"accountType":2,"premiumLeft":0}}`, true},
		{"debridlink", "/account/infos", `{"success":true,"value":{"accountType":0,"premiumLeft":0}}`, false},
		{"deepbrid", "/user", `{"type":"premium"}`, true},
		{"deepbrid", "/user", `{"type":"free"}`, false},
		{"deepbrid", "/user", `{"error":401,"message":"do not expose upstream-private-data"}`, false},
	} {
		t.Run(tc.service+tc.response, func(t *testing.T) {
			p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Method != "GET" {
					t.Error("wrong account endpoint")
				}
				fmt.Fprint(w, tc.response)
			})
			err := p.ValidateKey(context.Background(), tc.service, "synthetic-key")
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected account validation: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private-data") {
				t.Fatal("upstream error leaked")
			}
		})
	}
}

func TestExtendedProviderErrorsRetainCategoriesAndRetryHints(t *testing.T) {
	for _, tc := range []struct {
		service, raw, category string
		status                 int
	}{
		{"realdebrid", `{"error_code":8,"error":"private upstream detail"}`, "authentication", 401},
		{"realdebrid", `{"error_code":21,"error":"private upstream detail"}`, "account_limit", 403},
		{"realdebrid", `{"error_code":34,"error":"private upstream detail"}`, "rate_limit", 429},
		{"realdebrid", `{"error_code":30,"error":"private upstream detail"}`, "invalid_torrent", 400},
		{"debridlink", `{"success":false,"error":"badToken"}`, "authentication", 401},
		{"debridlink", `{"success":false,"error":"maxTorrent"}`, "account_limit", 429},
		{"debridlink", `{"success":false,"error":"torrentTooBig"}`, "file_too_large", 400},
		{"debridlink", `{"success":false,"error":"floodDetected"}`, "rate_limit", 400},
		{"premiumize", `{"status":"error","code":"authentication_failed","message":"private upstream detail"}`, "authentication", 200},
		{"premiumize", `{"status":"error","code":"account_limit_reached"}`, "account_limit", 200},
		{"premiumize", `{"status":"error","code":"not_found"}`, "not_found", 200},
		{"deepbrid", `{"error":2,"message":"private upstream detail"}`, "subscription", 200},
		{"deepbrid", `{"error":429}`, "rate_limit", 429},
		{"deepbrid", `{"error":503}`, "provider_unavailable", 503},
	} {
		t.Run(tc.service+tc.category, func(t *testing.T) {
			p := extendedProviderTest(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "27")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.raw)
			})
			var out any
			err := p.providerJSON(context.Background(), tc.service, "synthetic-key", "GET", "test", nil, &out)
			if providerFailureCode(err) != tc.category {
				t.Fatalf("wrong failure: %v", err)
			}
			if strings.Contains(err.Error(), "private upstream") {
				t.Fatal("upstream error retained")
			}
			var failure *ProviderError
			if (tc.category == "rate_limit" || tc.category == "provider_unavailable") && (!errors.As(err, &failure) || failure.RetryAfter != 27) {
				t.Fatalf("lost retry deadline: %v", err)
			}
		})
	}
}

func TestPremiumizeStopsOnFileListingFailureWithoutPublishingIncompleteHits(t *testing.T) {
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)}
	for _, failure := range []struct {
		status int
		body   string
	}{
		{429, `{"status":"error","code":"rate_limit_reached"}`},
		{200, `{"status":"success","content":[{"path":"Example.S01E02.mkv","size":100}]}`},
	} {
		calls := 0
		p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/cache/check" {
				fmt.Fprint(w, `{"status":"success","response":[false,true,true]}`)
				return
			}
			if r.URL.Path != "/transfer/directdl" {
				t.Error("unexpected provider request")
			}
			calls++
			w.WriteHeader(failure.status)
			fmt.Fprint(w, failure.body)
		})
		items, err := p.PMCheck(context.Background(), "synthetic-key", hashes)
		if err == nil || calls != 1 || len(items) != 1 || items[0].Hash != hashes[0] || items[0].Cached {
			t.Fatalf("failed listing was ignored or published as availability: %+v %v", items, err)
		}
	}
}

func TestRealDebridCacheChecksReadOnlyAndLeaveMissingHashesUnknown(t *testing.T) {
	hash, missing := strings.Repeat("a", 40), strings.Repeat("b", 40)
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("availability started or changed a torrent")
		}
		switch r.URL.Path {
		case "/torrents":
			fmt.Fprintf(w, `[{"id":"ready","hash":"%s","status":"downloaded"}]`, hash)
		case "/torrents/info/ready":
			fmt.Fprintf(w, `{"id":"ready","hash":"%s","status":"downloaded","files":[{"id":1,"path":"/Example.S01E02.mkv","bytes":100,"selected":1}],"links":["https://provider.example/one"]}`, hash)
		default:
			t.Fatal("unexpected availability endpoint")
		}
	})
	items, err := p.RDCheck(context.Background(), "synthetic-key", []string{hash, missing})
	if err != nil || len(items) != 1 || !items[0].Cached || items[0].Hash != hash || items[0].Shareable || len(items[0].Files) != 1 {
		t.Fatalf("unsafe account availability: %+v %v", items, err)
	}
}

func TestRealDebridUsesSelectedFileLinkOrderAndHandlesEmptyResponses(t *testing.T) {
	hash := strings.Repeat("a", 40)
	selected, additions := false, 0
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents/addMagnet":
			additions++
			w.WriteHeader(201)
			fmt.Fprint(w, `{"id":"new"}`)
		case "/torrents/info/new":
			if !selected {
				fmt.Fprintf(w, `{"id":"new","hash":"%s","status":"waiting_files_selection","files":[{"id":4,"path":"/Example.S01E02.mkv","bytes":100,"selected":0}],"links":[]}`, hash)
				return
			}
			fmt.Fprintf(w, `{"id":"new","hash":"%s","status":"downloaded","files":[{"id":3,"path":"/Example.S01E01.mkv","bytes":200,"selected":0},{"id":4,"path":"/Example.S01E02.mkv","bytes":100,"selected":1}],"links":["https://provider.example/episode-two"]}`, hash)
		case "/torrents/selectFiles/new":
			selected = true
			w.WriteHeader(204)
		case "/unrestrict/link":
			_ = r.ParseForm()
			if r.Form.Get("link") != "https://provider.example/episode-two" {
				t.Error("wrong selected-file link")
			}
			fmt.Fprint(w, `{"download":"https://cdn.example/episode-two.mkv"}`)
		default:
			t.Fatal("unexpected playback endpoint")
		}
	})
	r, _ := ParseID("tt123:1:2", "series")
	prepared := ""
	result, err := p.rdPrepare(context.Background(), "synthetic-key", hash, "magnet:?xt=urn:btih:"+hash, r, false, "", func(id string) error { prepared = id; return nil })
	if err != nil || result.URL != "https://cdn.example/episode-two.mkv" || prepared != "new" || additions != 1 {
		t.Fatalf("playback: %+v %v", result, err)
	}
}

func TestPremiumizeBatchOrderAndExactEpisodeSelection(t *testing.T) {
	hash, missing := strings.Repeat("a", 40), strings.Repeat("b", 40)
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/cache/check":
			if len(r.Form["items[]"]) == 2 {
				fmt.Fprint(w, `{"status":"success","response":[true,false]}`)
			} else {
				fmt.Fprint(w, `{"status":"success","response":[true]}`)
			}
		case "/transfer/directdl":
			fmt.Fprint(w, `{"status":"success","content":[{"path":"Example.S01E01.mkv","size":200,"link":"https://cdn.example/one"},{"path":"Example.S01E02.mkv","size":100,"link":"https://cdn.example/two"}]}`)
		default:
			t.Fatal("cache hit created a cloud transfer")
		}
	})
	items, err := p.PMCheck(context.Background(), "synthetic-key", []string{hash, missing})
	if err != nil || len(items) != 2 || !items[0].Cached || items[1].Cached || !items[0].Shareable || len(items[0].Files) != 2 {
		t.Fatalf("batch result: %+v %v", items, err)
	}
	r, _ := ParseID("tt123:1:2", "series")
	result, err := p.pmPrepare(context.Background(), "synthetic-key", hash, "unused", r, true, "", func(string) error { t.Fatal("unexpected transfer"); return nil })
	if err != nil || result.URL != "https://cdn.example/two" {
		t.Fatalf("episode selection: %+v %v", result, err)
	}
}

func TestDebridLinkCachedOnlySendsHashWithoutTrackerAndUsesReadyFile(t *testing.T) {
	hash := strings.Repeat("a", 40)
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seedbox/add" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("wrong seedbox request")
		}
		var body struct {
			URL string `json:"url"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.URL != hash {
			t.Fatal("cached-only request included a magnet or tracker")
		}
		fmt.Fprintf(w, `{"success":true,"value":{"id":"ready","hashString":"%s","downloadPercent":100,"isZip":false,"files":[{"name":"Example.S01E01.mkv","size":200,"downloadUrl":"https://cdn.example/one","downloadPercent":100},{"name":"Example.S01E02.mkv","size":100,"downloadUrl":"https://cdn.example/two","downloadPercent":100}]}}`, hash)
	})
	r, _ := ParseID("tt123:1:2", "series")
	result, err := p.dlPrepare(context.Background(), "synthetic-key", hash, "magnet:?xt=urn:btih:"+hash+"&tr=https://private.example/passkey", r, true, "", func(string) error { return nil })
	if err != nil || result.URL != "https://cdn.example/two" {
		t.Fatalf("cached playback: %+v %v", result, err)
	}
}

func TestExtendedReadinessNeverRecreatesDeletedTransfers(t *testing.T) {
	for _, service := range []string{"realdebrid", "premiumize", "debridlink", "deepbrid"} {
		t.Run(service, func(t *testing.T) {
			p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("deleted transfer was recreated")
				}
				switch service {
				case "realdebrid":
					w.WriteHeader(404)
					fmt.Fprint(w, `{"error_code":7,"error":"not found"}`)
				case "premiumize":
					fmt.Fprint(w, `{"status":"success","transfers":[]}`)
				case "debridlink":
					fmt.Fprint(w, `{"success":true,"value":[]}`)
				case "deepbrid":
					fmt.Fprint(w, `{"error":1,"message":"No data"}`)
				}
			})
			r, _ := ParseID("tt123", "movie")
			hash := strings.Repeat("a", 40)
			save := func(string) error { t.Fatal("new transfer ID issued"); return nil }
			var err error
			switch service {
			case "realdebrid":
				_, err = p.rdPrepare(context.Background(), "synthetic-key", hash, "unused", r, true, "gone", save)
			case "premiumize":
				_, err = p.pmPrepare(context.Background(), "synthetic-key", hash, "unused", r, true, "gone", save)
			case "debridlink":
				_, err = p.dlPrepare(context.Background(), "synthetic-key", hash, "unused", r, true, "gone", save)
			case "deepbrid":
				_, err = p.dbPrepare(context.Background(), "synthetic-key", hash, "unused", r, true, "17", save)
			}
			if providerFailureCode(err) != "not_found" {
				t.Fatalf("deleted transfer not reported consistently: %v", err)
			}
		})
	}
}

func TestDeepbridRequiresExplicitDownloadAndRejectsAmbiguousFiles(t *testing.T) {
	hash := strings.Repeat("a", 40)
	calls := 0
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/torrents/add":
			fmt.Fprint(w, `{"error":0,"id":17}`)
		case "/torrents/info":
			fmt.Fprint(w, `{"error":0,"id":"17","status":"downloaded","filename":"Example.S01.zip","links":["https://www.deepbrid.com/mytorrents?file=one"]}`)
		case "/generate/link":
			fmt.Fprint(w, `{"error":0,"filename":"Example.S01.zip","link":"https://cdn.example/pack.zip"}`)
		default:
			t.Fatal("unexpected deepbrid request")
		}
	})
	r, _ := ParseID("tt123:1:2", "series")
	_, err := p.dbPrepare(context.Background(), "synthetic-key", hash, "unused", r, true, "", func(string) error { return nil })
	if providerFailureCode(err) != "not_cached" || calls != 0 {
		t.Fatal("cached-only request submitted a torrent")
	}
	id := ""
	_, err = p.dbPrepare(context.Background(), "synthetic-key", hash, "magnet:?xt=urn:btih:"+hash, r, false, "", func(value string) error { id = value; return nil })
	if err == nil || id != "17" || calls != 3 {
		t.Fatalf("unsafe archive playback: %v", err)
	}
}
