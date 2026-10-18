package store

import (
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestRunHistoryKeepsSourceAndRemoteSnapshotsSeparate(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, provider := range []model.Provider{{ID: "native", Adapter: "http_json"}, mirrorSource("remote-copy")} {
		run, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Mode: model.ModeFull, Config: provider})
		if err != nil {
			t.Fatal(err)
		}
		if run.RemoteCatalog != provider.IsRemoteCatalog() {
			t.Fatal("run origin was lost")
		}
	}
	for _, kind := range []string{"", "source", "remote"} {
		page, err := db.ListRuns(ctx, model.ListOptions{SourceKind: kind, Limit: 1})
		want := int64(1)
		if kind == "" {
			want = 2
		}
		if err != nil || page.Total != want || len(page.Items) != 1 {
			t.Fatalf("history %q: %+v %v", kind, page, err)
		}
		if kind != "" && page.Items[0].RemoteCatalog != (kind == "remote") {
			t.Fatal("wrong history namespace")
		}
	}
	wrong, err := db.ListRuns(ctx, model.ListOptions{SourceKind: "source", ProviderID: "remote-copy"})
	if err != nil || wrong.Total != 0 {
		t.Fatalf("source filter widened remote history: %+v %v", wrong, err)
	}
	if _, err = db.ListRuns(ctx, model.ListOptions{SourceKind: "invalid"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("invalid namespace accepted", err)
	}
	overview, err := db.CollectionOverview(ctx)
	if err != nil || overview.Queued != 2 || overview.SourceQueued != 1 || overview.SourceRunning != 0 {
		t.Fatalf("queue statistics mixed origins: %+v %v", overview, err)
	}
}
