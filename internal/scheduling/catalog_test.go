package scheduling

import (
	"reflect"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogSyncUsesOnlyIntervalAndRetainsLegacyActivation(t *testing.T) {
	disabled := false
	for _, test := range []struct {
		name     string
		schedule model.Schedule
		sync     *model.CatalogSync
		want     model.CatalogSync
	}{
		{"manual stays manual", model.Schedule{}, nil, model.CatalogSync{IntervalMinutes: 60}},
		{"interval", model.Schedule{Every: "2m30s", FullEvery: "24h", MaxPages: 4, KnownPages: 8, Mode: model.ModeIncremental}, nil, model.CatalogSync{Enabled: true, IntervalMinutes: 3}},
		{"disabled", model.Schedule{Enabled: &disabled, Every: "1h"}, nil, model.CatalogSync{IntervalMinutes: 60}},
		{"daily", model.Schedule{Cron: "0 6 * * *", Timezone: "Europe/Paris", Mode: model.ModeFull, MaxPages: 2}, nil, model.CatalogSync{Enabled: true, IntervalMinutes: 1440}},
		{"weekly", model.Schedule{Cron: "0 6 * * 1"}, nil, model.CatalogSync{Enabled: true, IntervalMinutes: 10080}},
		{"dedicated settings", model.Schedule{Cron: "invalid legacy data", FullEvery: "invalid", MaxPages: 900}, &model.CatalogSync{Enabled: true, IntervalMinutes: 45}, model.CatalogSync{Enabled: true, IntervalMinutes: 45}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := model.Provider{Adapter: "http_json", HTTP: model.HTTPConfig{Catalog: true}, Schedule: test.schedule, CatalogSync: test.sync}
			before := p
			got, err := CatalogSynchronization(p)
			if err != nil || got != test.want || !reflect.DeepEqual(p, before) {
				t.Fatalf("catalog settings: %+v %v", got, err)
			}
			settings, err := Settings(p)
			plan, parseErr := Parse(settings)
			if err != nil || parseErr != nil || plan.FullEvery != 0 || plan.Mode != model.ModeIncremental || settings.Cron != "" || settings.MaxPages != 0 || settings.KnownPages != 0 {
				t.Fatalf("advanced source settings reached catalog sync: %+v %v %v", settings, err, parseErr)
			}
			if plan.every != time.Duration(test.want.IntervalMinutes)*time.Minute || plan.Enabled != test.want.Enabled {
				t.Fatalf("wrong interval or activation: %+v", plan)
			}
		})
	}
	for _, minutes := range []int{0, -1, model.MaxCatalogSyncMinutes + 1} {
		p := model.Provider{Adapter: "http_json", HTTP: model.HTTPConfig{Catalog: true}, CatalogSync: &model.CatalogSync{IntervalMinutes: minutes}}
		if _, err := Settings(p); err == nil {
			t.Fatalf("invalid catalog interval accepted: %d", minutes)
		}
	}
	if _, err := Settings(model.Provider{CatalogSync: &model.CatalogSync{IntervalMinutes: 60}}); err == nil {
		t.Fatal("catalog settings accepted on a native source")
	}
}

func TestLegacyCatalogFullRunResumesWithoutCreatingRecurringFulls(t *testing.T) {
	p := model.Provider{ID: "remote", Adapter: "http_json", Enabled: true, HTTP: model.HTTPConfig{Catalog: true}, Schedule: model.Schedule{Every: "1h", Mode: model.ModeFull, MaxPages: 2}}
	source := Sources(map[string]model.ProviderDocument{"remote": {Provider: p, Revision: "saved"}})[0]
	run := model.Run{ID: "partial", Revision: "saved", Mode: model.ModeFull, Status: model.StatusPaused, Trigger: model.TriggerScheduled, PauseReason: model.PauseBudget}
	state, resume, _ := source.RunState([]model.Run{run})
	if state != "paused" || resume == nil || resume.ID != run.ID || source.Plan.Mode != model.ModeIncremental || source.Plan.FullEvery != 0 {
		t.Fatalf("legacy catalog continuation lost: %s %+v", state, resume)
	}
	run.PauseReason = model.PauseManual
	if state, _, _ := source.RunState([]model.Run{run}); state != "blocked" {
		t.Fatal("catalog sync bypassed a manual hold")
	}
}
