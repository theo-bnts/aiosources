package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestTorznabSavedSortingCapAndIndependentTimeouts(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for i, provider := range []string{"alpha", "beta", "gamma"} {
		publishSharingRow(t, ctx, db, provider, "one", fmt.Sprintf(`{"title":"Example.%d","info_hash":"%s","size":%d}`, i, strings.Repeat(string(rune('a'+i)), 40), (i+1)*100), nil)
	}
	for _, tc := range []struct {
		rules []model.ResultSort
		first string
	}{{[]model.ResultSort{{Field: "size", Direction: "asc"}}, "alpha"}, {[]model.ResultSort{{Field: "size", Direction: "desc"}}, "gamma"}, {[]model.ResultSort{{Field: "source", Direction: "asc"}}, "gamma"}, {[]model.ResultSort{{Field: "name", Direction: "desc"}}, "gamma"}, {[]model.ResultSort{{Field: "cached", Direction: "desc"}, {Field: "size", Direction: "asc"}}, "alpha"}} {
		prefs := model.TorznabPreferences{ResultPreferences: model.ResultPreferences{Sorting: tc.rules, MaxResults: 2, ResponseSeconds: 240}}
		grant, err := db.CreateShare(ctx, model.ShareInput{Format: "torznab", Name: "Sorted access", Enabled: true, Scope: "all", Fields: CatalogFields(), TorznabPreferences: &prefs})
		if err != nil {
			t.Fatal(err)
		}
		for _, offset := range []int{0, 1, 2, 100} {
			request, err := db.BeginTorznabRequest(ctx, grant.Share.ID, grant.Password)
			if err != nil {
				t.Fatal(err)
			}
			instanceBudget, _ := db.ConnectionTimeout(ctx, "torznab")
			expected := min(instanceBudget, 240*time.Second)
			if left := time.Until(request.Deadline()); left < expected-5*time.Second || left > expected {
				t.Fatal("connection timeout escaped the instance ceiling", left, instanceBudget)
			}
			result, err := request.Torznab(ctx, model.TorznabQuery{Mode: "search", Limit: 1, Offset: offset, SourceNames: map[string]string{"alpha": "Zulu", "beta": "Bravo", "gamma": "Alpha"}}, nil)
			_ = request.Close()
			if err != nil || result.Total != 2 {
				t.Fatal(result, err)
			}
			if offset == 0 && (len(result.Items) != 1 || result.Items[0].Provider != tc.first) {
				t.Fatal(tc, result)
			}
			if offset >= 2 && len(result.Items) != 0 {
				t.Fatal("later page escaped the configured cap", result)
			}
		}
	}
}
