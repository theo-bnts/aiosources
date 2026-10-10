package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type blockedCatalogResponse struct {
	*httptest.ResponseRecorder
	started chan struct{}
	unblock <-chan struct{}
	ctx     context.Context
	once    sync.Once
}

func (w *blockedCatalogResponse) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	select {
	case <-w.unblock:
		return w.ResponseRecorder.Write(data)
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

func TestCatalogHTTPLeaseCoversResponseAndReleasesAfterDisconnect(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	concurrent := 1
	grant, err := fixture.db.CreateShare(fixture.ctx, model.ShareInput{Name: "Transport", Enabled: true, Scope: "all", Fields: []string{"title"}, MaxConcurrentDownloads: &concurrent})
	if err != nil {
		t.Fatal(err)
	}
	feed := "/api/catalogs/" + grant.Share.ID
	for _, disconnect := range []bool{false, true} {
		ctx, cancel := context.WithCancel(fixture.ctx)
		t.Cleanup(cancel)
		unblock := make(chan struct{})
		writer := &blockedCatalogResponse{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), unblock: unblock, ctx: ctx}
		request := httptest.NewRequest(http.MethodGet, feed, nil).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer "+grant.Password)
		done := make(chan struct{})
		go func() { fixture.handler.ServeHTTP(writer, request); close(done) }()
		select {
		case <-writer.started:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("catalogue never reached response delivery")
		}
		limited := fixture.request(http.MethodHead, feed, "", false, grant.Password)
		retry, err := strconv.Atoi(limited.Header().Get("Retry-After"))
		if limited.Code != http.StatusTooManyRequests || err != nil || retry < 1 {
			cancel()
			t.Fatalf("lease ended before response delivery: %d retry=%q", limited.Code, limited.Header().Get("Retry-After"))
		}
		if disconnect {
			cancel()
		} else {
			close(unblock)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("catalogue response did not finish")
		}
		if next := fixture.request(http.MethodGet, feed, "", false, grant.Password); next.Code != http.StatusOK {
			t.Fatalf("response completion/disconnect retained its concurrency lease: %d", next.Code)
		}
		cancel()
	}
}

func TestCatalogHTTPHeadConsumesAuthenticatedRateBudget(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	created := fixture.request(http.MethodPost, "/api/shares", `{"name":"One request","enabled":true,"scope":"all","fields":["title"],"requests_per_minute":1,"max_concurrent_downloads":1}`, true, "")
	var grant model.ShareCredential
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &grant) != nil {
		t.Fatalf("create policy: %d", created.Code)
	}
	feed := "/api/catalogs/" + grant.Share.ID
	if head := fixture.request(http.MethodHead, feed, "", true, ""); head.Code != http.StatusUnauthorized {
		t.Fatalf("admin session bypassed catalogue authentication: %d", head.Code)
	}
	if head := fixture.request(http.MethodHead, feed, "", false, grant.Password); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("authenticated HEAD failed: %d", head.Code)
	}
	limited := fixture.request(http.MethodGet, feed, "", false, grant.Password)
	retry, err := strconv.Atoi(limited.Header().Get("Retry-After"))
	if limited.Code != http.StatusTooManyRequests || err != nil || retry < 1 || retry > 60 {
		t.Fatalf("HEAD did not spend the authenticated quota: %d retry=%q", limited.Code, limited.Header().Get("Retry-After"))
	}
}

func TestSharingRejectsExplicitInvalidNumericPolicy(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	for _, policy := range []string{`"requests_per_minute":0`, `"requests_per_minute":3601`, `"max_concurrent_downloads":0`, `"max_concurrent_downloads":17`, `"requests_per_minute":null`, `"max_concurrent_downloads":1.5`} {
		response := fixture.request(http.MethodPost, "/api/shares", `{"name":"Invalid policy","scope":"all","fields":["title"],`+policy+`}`, true, "")
		if response.Code != http.StatusBadRequest && response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid numeric policy was defaulted or accepted: %s: %d", policy, response.Code)
		}
	}
}
