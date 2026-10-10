package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestActivityConsolidationKeepsHistoryAndRetiresAllOutboundState(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.migrateThrough(ctx, 37); err != nil {
		t.Fatal(err)
	}
	if err := db.PutSecret(ctx, "old-destination", []byte("private-encrypted-value")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message,service) VALUES('legacy-incident','error','aiosource.incident','TMDB is unavailable.','tmdb');
INSERT INTO ingest.notifications(activity_id,level,kind,title,message,service,created_at)
SELECT id,level,kind,'TMDB unavailable',message,service,created_at FROM ingest.activity_logs WHERE event_id='legacy-incident';
INSERT INTO ingest.webhooks(id,name,enabled,url_secret_ref,events) VALUES('old-hook','Private recipient',TRUE,'old-destination',ARRAY['run.failed']);
INSERT INTO ingest.webhook_deliveries(id,webhook_id,event_id,event_type,body,status,attempts,last_status)
VALUES('delivered','old-hook','legacy-incident','run.failed',convert_to('https://private.example/never-log-token','UTF8'),'delivered',2,200),
('pending','old-hook','pending-event','run.failed',convert_to('never-log-token','UTF8'),'pending',1,NULL);`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 50}})
	if err != nil || logs.Total != 3 {
		t.Fatalf("consolidation lost or duplicated history: %+v %v", logs, err)
	}
	incident, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Query: "TMDB unavailable", Limit: 50}, Service: "tmdb"})
	if err != nil || incident.Total != 1 || incident.Items[0].Title != "TMDB unavailable" || incident.Items[0].Message != "TMDB is unavailable." {
		t.Fatalf("notification context missing from searchable logs: %+v %v", incident, err)
	}
	encoded, _ := json.Marshal(logs)
	for _, private := range []string{"never-log-token", "private.example", "Private recipient", "private-encrypted-value", "old-destination"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("retired outbound state leaked %s", private)
		}
	}
	for _, table := range []string{"notifications", "webhooks", "webhook_deliveries"} {
		var exists bool
		if err := db.pool.QueryRow(ctx, "SELECT to_regclass('ingest.'||$1) IS NOT NULL", table).Scan(&exists); err != nil || exists {
			t.Fatalf("retired table %s remains active: %t %v", table, exists, err)
		}
	}
	if err := db.DeleteSecret(ctx, "old-destination"); err != nil {
		t.Fatalf("removed destination retained its secret dependency: %v", err)
	}
	if err := db.AIOIncident(ctx, "tmdb", true); err != nil {
		t.Fatal(err)
	}
	if err := db.AIOIncident(ctx, "tmdb", true); err != nil {
		t.Fatal(err)
	}
	if err := db.AIOIncident(ctx, "tmdb", false); err != nil {
		t.Fatal(err)
	}
	newLogs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 50}, Service: "tmdb"})
	if err != nil || newLogs.Total != 3 {
		t.Fatalf("incident failure/recovery no longer works without notifications: %+v %v", newLogs, err)
	}
}
