package jobs

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestJSONCollectionModesKeepMetadataIndependentAcrossResume(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	var count atomic.Int32
	count.Store(6)
	var listRequests, detailRequests atomic.Int32
	hash := func(id int) string {
		if id == 1 {
			id = 2
		}
		return fmt.Sprintf("%040x", id)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/list":
			listRequests.Add(1)
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			var items []string
			for offset := (page - 1) * 2; offset < page*2 && offset < int(count.Load()); offset++ {
				id := offset + 1
				if r.URL.Query().Get("sortOrder") == "desc" {
					id = int(count.Load()) - offset
				}
				items = append(items, fmt.Sprintf(`{"id":%d,"title":"Listing %d","infoHash":%q,"category":1,"attributes":{"seeders":["5"]}}`, id, id, hash(id)))
			}
			_, _ = fmt.Fprintf(w, `{"items":[%s],"total":%d}`, strings.Join(items, ","), count.Load())
		case strings.HasPrefix(r.URL.Path, "/detail/"):
			detailRequests.Add(1)
			id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/detail/"), 16, 64)
			if err != nil {
				t.Errorf("invalid fixture locator: %v", err)
				http.Error(w, "invalid locator", http.StatusBadRequest)
				return
			}
			external := fmt.Sprintf(`[{"kind":"imdb","value":"tt%d"}]`, id)
			if id == 3 {
				external = "[]"
			}
			_, _ = fmt.Fprintf(w, `{"id":%d,"title":"Detail must not replace listing","infoHash":%q,"category":1,"externalIds":%s,"metadata":{"complete":true},"attributes":{"seeders":["999"]}}`, id, hash(int(id)), external)
		default:
			t.Errorf("unexpected traversal request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	provider := schedulerProvider("json-modes", server.URL+"/list")
	provider.Schedule = model.Schedule{KnownPages: 1}
	provider.PageSize = 2
	provider.Pagination = model.Pagination{Type: "page", In: "query", PageParam: "page", SizeParam: "limit", Start: 1}
	provider.Mapping.Fields["info_hash"] = "/infoHash"
	provider.Mapping.Fields["category_id"] = "/category"
	provider.Mapping.Fields["attributes"] = "/attributes"
	provider.Traversal = &model.JSONTraversal{
		WindowPages: 100, TotalMode: "at_least", TotalPaths: []string{"/total"},
		Scopes:       []model.JSONScope{{ID: "selected", Match: map[string]any{"category_id": 1}}},
		EnrichFields: []string{"external_ids", "metadata"},
		IDRecovery: &model.JSONIDRecovery{
			First: 1, DiscoveryQuery: map[string]any{"sortOrder": "desc"},
			ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f0-9]{40}$", DetailURL: "/detail/{value}",
			Mapping: model.Mapping{ID: "/id", Fields: map[string]string{
				"title": "/title", "info_hash": "/infoHash", "category_id": "/category",
				"external_ids": "/externalIds", "metadata": "/metadata", "attributes": "/attributes",
			}},
		},
	}
	doc := saveScheduledProvider(t, registry, provider, "")
	manager := New(db, registry, nil, 1)
	full, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	full = collectScheduledAttempt(t, ctx, manager, db, full.ID, model.StatusSucceeded)
	if full.DistinctRecords != 6 || listRequests.Load() != 3 || detailRequests.Load() != 0 {
		t.Fatalf("Full did not collect all identities independently: run=%+v lists=%d details=%d", full, listRequests.Load(), detailRequests.Load())
	}
	count.Store(8)
	knownPages := 2
	incremental, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeIncremental, KnownPages: &knownPages})
	if err != nil {
		t.Fatal(err)
	}
	incremental = collectScheduledAttempt(t, ctx, manager, db, incremental.ID, model.StatusSucceeded)
	if incremental.DistinctRecords != 6 || listRequests.Load() != 6 || detailRequests.Load() != 0 {
		t.Fatalf("Incremental ignored its two-known-page boundary or fetched details: run=%+v lists=%d details=%d", incremental, listRequests.Load(), detailRequests.Load())
	}
	budget := 2
	metadata, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeMetadata, MaxPages: &budget})
	if err != nil {
		t.Fatal(err)
	}
	metadata = collectScheduledAttempt(t, ctx, manager, db, metadata.ID, model.StatusPaused)
	// Editing the live definition cannot redirect an already queued metadata job.
	provider.Traversal.IDRecovery.DetailURL = "/changed/{value}"
	saveScheduledProvider(t, registry, provider, doc.Revision)
	for _, status := range []model.RunStatus{model.StatusPaused, model.StatusPaused, model.StatusSucceeded} {
		if _, err := manager.Resume(ctx, metadata.ID); err != nil {
			t.Fatal(err)
		}
		metadata = collectScheduledAttempt(t, ctx, manager, db, metadata.ID, status)
	}
	if metadata.Errors != 0 || metadata.DistinctRecords != 7 || detailRequests.Load() != 8 || listRequests.Load() != 6 {
		t.Fatalf("Metadata omitted old rows, repeated committed details, or crawled listings: run=%+v lists=%d details=%d", metadata, listRequests.Load(), detailRequests.Load())
	}
	published, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || published.Total != 8 {
		t.Fatalf("Metadata altered catalogue membership: %+v %v", published, err)
	}
	for _, record := range published.Items {
		if record.Fields["title"] != "Listing "+record.SourceID {
			t.Fatalf("Metadata overwrote a known listing value: %+v", record)
		}
		if record.SourceID == "1" {
			if record.Fields["external_ids"] != nil || record.Fields["metadata"] != nil {
				t.Fatalf("shared hash merged another native identity: %+v", record)
			}
			continue
		}
		if record.Fields["external_ids"] == nil || record.Fields["metadata"] == nil {
			t.Fatalf("old or recent missing metadata was skipped: %+v", record)
		}
		attributes := record.Fields["attributes"].(map[string]any)
		if _, present := attributes["seeders"]; present {
			t.Fatalf("Transient seeder count was retained: %+v", attributes)
		}
	}
}

func TestMetadataDatabaseRejectionStopsAndResumesWithoutSkipping(t *testing.T) {
	for _, invalidID := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid_id=%t", invalidID), func(t *testing.T) {
			ctx, _, db := schedulerDatabase(t)
			// This scenario verifies the optional complete processing history.
			useFullStorage(t, ctx, db)
			var corrected atomic.Bool
			var requests [4]atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/list" {
					var items []string
					for id := 1; id <= 3; id++ {
						items = append(items, fmt.Sprintf(`{"id":%d,"title":"Listing %d","infoHash":"%040x","category":1}`, id, id, id))
					}
					_, _ = fmt.Fprintf(w, `{"items":[%s],"total":3}`, strings.Join(items, ","))
					return
				}
				id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/detail/"), 16, 64)
				if err != nil || id < 1 || id > 3 {
					t.Errorf("unexpected Metadata request: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				attempt := requests[id].Add(1)
				note := `"readable note"`
				returnedID := strconv.FormatInt(id, 10)
				if id == 2 && !corrected.Load() {
					// Bound the old retry loop so its regression fails immediately.
					if attempt > 1 {
						http.Error(w, "repeated rejected detail", http.StatusBadRequest)
						return
					}
					if invalidID {
						returnedID = `"unrepresentable\u0000id"`
					} else {
						note = `"unrepresentable\u0000note"`
					}
				}
				nfo := `"readable NFO"`
				if id == 2 {
					nfo = `"binary\u0000NFO π\\u0000\r\n"`
				}
				_, _ = fmt.Fprintf(w, `{"id":%s,"title":"Detail","infoHash":"%040x","category":1,"metadata":{"note":%s,"nfoContent":%s}}`, returnedID, id, note, nfo)
			}))
			t.Cleanup(server.Close)
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			provider := schedulerProvider("metadata-rejected", server.URL+"/list")
			provider.Schedule = model.Schedule{}
			provider.PageSize = 3
			provider.Pagination = model.Pagination{Type: "page", In: "query", PageParam: "page", SizeParam: "limit", Start: 1}
			provider.Mapping.Fields["info_hash"] = "/infoHash"
			provider.Mapping.Fields["category_id"] = "/category"
			provider.Traversal = &model.JSONTraversal{
				WindowPages: 100, TotalMode: "at_least", TotalPaths: []string{"/total"},
				Scopes:       []model.JSONScope{{ID: "selected", Match: map[string]any{"category_id": 1}}},
				EnrichFields: []string{"metadata"},
				IDRecovery: &model.JSONIDRecovery{
					First: 1, DiscoveryQuery: map[string]any{"sortOrder": "desc"},
					ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f0-9]{40}$", DetailURL: "/detail/{value}",
					Mapping: model.Mapping{ID: "/id", Fields: map[string]string{
						"title": "/title", "info_hash": "/infoHash", "category_id": "/category", "metadata": "/metadata",
					}},
				},
			}
			saveScheduledProvider(t, registry, provider, "")
			manager := New(db, registry, nil, 1)
			seed, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
			if err != nil {
				t.Fatal(err)
			}
			collectScheduledAttempt(t, ctx, manager, db, seed.ID, model.StatusSucceeded)
			metadata, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeMetadata})
			if err != nil {
				t.Fatal(err)
			}
			metadata = collectScheduledAttempt(t, ctx, manager, db, metadata.ID, model.StatusFailed)
			if requests[1].Load() != 1 || requests[2].Load() != 1 || requests[3].Load() != 0 || metadata.Errors == 0 {
				t.Fatalf("rejected Metadata was retried or skipped: requests=%d/%d/%d errors=%d", requests[1].Load(), requests[2].Load(), requests[3].Load(), metadata.Errors)
			}
			var checkpoint struct {
				After string `json:"after"`
			}
			if err := json.Unmarshal(metadata.Cursor, &checkpoint); err != nil || checkpoint.After != "1" || metadata.TraversalDone {
				t.Fatalf("rejected Metadata changed the successful checkpoint: %s %v", metadata.Cursor, err)
			}
			published, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
			if err != nil || published.Total != 3 {
				t.Fatalf("Metadata rejection changed native membership: %+v %v", published, err)
			}
			for _, record := range published.Items {
				if record.SourceID != "1" && record.Fields["metadata"] != nil {
					t.Fatalf("failed Metadata attempt updated an uncommitted identity: %+v", record)
				}
			}
			corrected.Store(true)
			if _, err := manager.Resume(ctx, metadata.ID); err != nil {
				t.Fatal(err)
			}
			collectScheduledAttempt(t, ctx, manager, db, metadata.ID, model.StatusSucceeded)
			if requests[1].Load() != 1 || requests[2].Load() != 2 || requests[3].Load() != 1 {
				t.Fatalf("Metadata resume replayed a committed identity or skipped the rejected one: requests=%d/%d/%d", requests[1].Load(), requests[2].Load(), requests[3].Load())
			}
			published, err = db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
			if err != nil || published.Total != 3 {
				t.Fatalf("Metadata resume changed native membership: %+v %v", published, err)
			}
			for _, record := range published.Items {
				fields, ok := record.Fields["metadata"].(map[string]any)
				if !ok || fields["note"] != "readable note" {
					t.Fatalf("corrected Metadata was not retained: %+v", record)
				}
				if record.SourceID == "2" {
					encoded, ok := fields["nfoContent"].(map[string]any)
					if !ok || encoded["encoding"] != "base64" {
						t.Fatalf("persisted binary NFO has no encoding marker: %+v", fields)
					}
					data, ok := encoded["data"].(string)
					if !ok {
						t.Fatalf("persisted NFO data is not a string: %+v", encoded)
					}
					decoded, err := base64.StdEncoding.DecodeString(data)
					if err != nil || string(decoded) != "binary\x00NFO π\\u0000\r\n" {
						t.Fatalf("Metadata resume changed the binary NFO: %q %v", decoded, err)
					}
				} else if fields["nfoContent"] != "readable NFO" {
					t.Fatalf("ordinary NFO text was changed: %+v", fields)
				}
			}
		})
	}
}

func TestFullDatabaseRejectionPreservesCatalogueAndResumesRejectedPage(t *testing.T) {
	var collecting, corrected atomic.Bool
	var requests [2]atomic.Int32
	ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !collecting.Load() {
			_, _ = w.Write([]byte(`{"items":[{"id":"original","title":"Published baseline"}],"total":1}`))
			return
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 || offset >= len(requests) {
			t.Errorf("unexpected collection offset: %q", r.URL.Query().Get("offset"))
			http.Error(w, "invalid offset", http.StatusBadRequest)
			return
		}
		requests[offset].Add(1)
		title := `"Retained release"`
		if offset == 1 && !corrected.Load() {
			title = `"unrepresentable\u0000title"`
		}
		_, _ = fmt.Fprintf(w, `{"items":[{"id":"new-%d","title":%s}],"total":2}`, offset, title)
	}, nil)
	seed, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	collectScheduledAttempt(t, ctx, manager, db, seed.ID, model.StatusSucceeded)
	collecting.Store(true)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	failed := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusFailed)
	if failed.Pages != 1 || failed.Errors != 1 || failed.TraversalDone || requests[0].Load() != 1 || requests[1].Load() != 1 {
		t.Fatalf("rejected Full page advanced, retried, or completed: %+v requests=%d/%d", failed, requests[0].Load(), requests[1].Load())
	}
	published, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || published.Total != 1 || published.Items[0].SourceID != "original" {
		t.Fatalf("failed Full replaced published membership: %+v %v", published, err)
	}
	corrected.Store(true)
	resumed, err := manager.Resume(ctx, run.ID)
	if err != nil || !bytes.Equal(resumed.Cursor, failed.Cursor) {
		t.Fatalf("resume replaced the successful checkpoint: %+v %v", resumed, err)
	}
	finished := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
	if finished.Pages != 2 || finished.DistinctRecords != 2 || requests[0].Load() != 1 || requests[1].Load() != 2 {
		t.Fatalf("resume skipped the rejection or replayed committed work: %+v requests=%d/%d", finished, requests[0].Load(), requests[1].Load())
	}
	published, err = db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || published.Total != 2 {
		t.Fatalf("corrected Full did not publish its complete catalogue: %+v %v", published, err)
	}
	for _, record := range published.Items {
		if record.SourceID != "new-0" && record.SourceID != "new-1" {
			t.Fatalf("corrected Full retained a superseded identity: %+v", record)
		}
	}
}

func TestIncrementalKnownPageBoundarySurvivesBudgetResume(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown-boundary=%t", reset), func(t *testing.T) {
			var incremental atomic.Bool
			var requests atomic.Int32
			ids := []string{"known-0", "known-1", "beyond"}
			if reset {
				ids = []string{"known-0", "new", "known-1", "known-2", "beyond"}
			}
			ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
				offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
				if err != nil || offset < 0 {
					t.Error("invalid offset")
					http.Error(w, "invalid offset", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if !incremental.Load() {
					_, _ = fmt.Fprintf(w, `{"items":[{"id":"known-%d","title":"Baseline"}],"total":3}`, offset)
					return
				}
				requests.Add(1)
				if offset >= len(ids) {
					t.Error("incremental traversal exceeded its source")
					http.Error(w, "invalid offset", http.StatusBadRequest)
					return
				}
				_, _ = fmt.Fprintf(w, `{"items":[{"id":%q,"title":"Refreshed"}],"total":%d}`, ids[offset], len(ids))
			}, nil)
			seed, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
			if err != nil {
				t.Fatal(err)
			}
			collectScheduledAttempt(t, ctx, manager, db, seed.ID, model.StatusSucceeded)
			incremental.Store(true)
			maxPages, knownPages := 1, 2
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeIncremental, MaxPages: &maxPages, KnownPages: &knownPages})
			if err != nil {
				t.Fatal(err)
			}
			stopAt := len(ids) - 1
			for page := 1; page <= stopAt; page++ {
				if page > 1 {
					if _, err := manager.Resume(ctx, run.ID); err != nil {
						t.Fatal(err)
					}
				}
				status := model.StatusPaused
				if page == stopAt {
					status = model.StatusSucceeded
				}
				run = collectScheduledAttempt(t, ctx, manager, db, run.ID, status)
				if run.Pages != page || requests.Load() != int32(page) {
					t.Fatalf("budget continuation replayed or skipped a page: %+v requests=%d", run, requests.Load())
				}
			}
			published, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range published.Items {
				if record.SourceID == "beyond" {
					t.Fatal("incremental traversal fetched beyond its consecutive known-page boundary")
				}
			}
		})
	}
}
