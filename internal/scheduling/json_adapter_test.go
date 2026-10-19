package scheduling

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAdapterRenameAllowsOnlyAnEquivalentPausedSnapshot(t *testing.T) {
	for _, catalog := range []bool{false, true} {
		p := model.Provider{ID: "example", Adapter: "json", Enabled: true, URL: "https://archive.example/catalog", HTTP: model.HTTPConfig{Catalog: catalog}, Schedule: model.Schedule{Every: "1m", Mode: model.ModeIncremental}}
		if catalog {
			p.CatalogSync = &model.CatalogSync{Enabled: true, IntervalMinutes: 30}
		}
		settings, err := Settings(p)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := Parse(settings)
		if err != nil {
			t.Fatal(err)
		}
		source := Source{ID: p.ID, Revision: "new", Provider: p, Plan: plan, Valid: true}
		previous := p
		previous.Adapter, previous.RequestTimeout, previous.Schedule = "http_json", model.DefaultRequestTimeout, settings
		run := model.Run{ID: "partial", Config: previous, Revision: "old", Mode: model.ModeIncremental, Trigger: model.TriggerScheduled, Status: model.StatusPaused, PauseReason: model.PauseBudget}
		if state, resumed, _ := source.RunState([]model.Run{run}); state != "paused" || resumed == nil || resumed.Revision != "old" || resumed.Config.Adapter != "http_json" {
			t.Fatal("equivalent JSON rename lost the original continuation")
		}
		for _, change := range []func(*model.Provider){
			func(p *model.Provider) { p.URL = "https://different.example/catalog" },
			func(p *model.Provider) { p.RequestTimeout = "71s" },
			func(p *model.Provider) { p.HTTP.ListingPaths = []string{"/another-listing"} },
			func(p *model.Provider) { p.Mapping.ID = "/different_id" },
		} {
			changed := source
			change(&changed.Provider)
			if state, resumed, _ := changed.RunState([]model.Run{run}); state != "blocked" || resumed != nil {
				t.Fatal("a real configuration change bypassed continuation protection")
			}
		}
	}
}
