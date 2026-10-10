package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestServiceActivityFiltersSeparateServicesFromIdenticallyNamedSources(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordActivity(ctx, "info", "service.started", "not persisted", "tmdb", "", nil); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"tmdb", "alldebrid", "torbox", "cleanup"} {
		for _, active := range []bool{true, true, false, false} {
			if err := db.AIOIncident(ctx, service, active); err != nil {
				t.Fatal(err)
			}
		}
	}
	for service, wanted := range map[string]int64{"tmdb": 2, "alldebrid": 4, "torbox": 2} {
		logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100}, Service: service})
		if err != nil || logs.Total != wanted || int64(len(logs.Items)) != wanted {
			t.Fatalf("%s filter: %+v %v", service, logs, err)
		}
		for _, log := range logs.Items {
			if log.Service != service || log.ProviderID != "" {
				t.Fatalf("service activity masqueraded as a source: %+v", log)
			}
		}
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100, ProviderID: "tmdb"}})
	if err != nil || logs.Total != 1 || logs.Items[0].Service != "" {
		t.Fatalf("source filter included the TMDB service: %+v %v", logs, err)
	}
	if _, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100}, Service: "untrusted"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("unknown service filter was accepted", err)
	}
}

func TestInformationActivityRejectsUntrustedDiagnosticText(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	const secret = "token-must-not-be-stored"
	for _, d := range []model.ServiceDiagnostic{
		{Operation: secret, FailureCode: "unknown"},
		{Operation: "changes", FailureCode: secret},
		{Operation: "changes", FailureCode: "http", StatusCode: 999},
	} {
		if err := db.RecordInformationActivity(ctx, d, 0); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("untrusted diagnostic was accepted", err)
		}
	}
	for range 2 {
		if err := db.RecordInformationActivity(ctx, model.ServiceDiagnostic{Operation: "changes", FailureCode: "http", StatusCode: 502}, 0); err != nil {
			t.Fatal(err)
		}
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100}, Service: "tmdb"})
	if err != nil || logs.Total != 2 {
		t.Fatalf("repeat diagnostic was lost: %+v %v", logs, err)
	}
	encoded, _ := json.Marshal(logs)
	if strings.Contains(string(encoded), secret) {
		t.Fatal("diagnostics persisted arbitrary text")
	}
}

func TestServiceActivityMigrationRecoversExistingIncidentContext(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.migrateThrough(ctx, 31); err != nil {
		t.Fatal(err)
	}

	const message = "The service could not complete the operation. Check its credentials and status."
	for _, service := range []string{"tmdb", "alldebrid", "torbox", "cleanup"} {
		var id int64
		if err := db.pool.QueryRow(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message) VALUES($1,'error','aiosource.incident',$2) RETURNING id`, "fixture-"+service, message).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.notifications(activity_id,level,kind,title,message,created_at) VALUES($1,'error','aiosource.incident',$2,$3,now())`, id, service+" unavailable", message); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100}})
	if err != nil || logs.Total != 4 {
		t.Fatalf("migration lost existing incident history: %+v %v", logs, err)
	}
	for _, log := range logs.Items {
		if log.Service == "" || log.Message != message || log.ProviderID != "" {
			t.Fatalf("migration lost service context or altered history: %+v", log)
		}
	}
	ad, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100}, Service: "alldebrid"})
	if err != nil || ad.Total != 2 {
		t.Fatalf("AllDebrid lost the cleanup incident: %+v %v", ad, err)
	}
}
