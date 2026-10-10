package store

import "testing"

func TestActiveSessionsPinCurrentDeviceAndSortByRecentActivity(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.admin_sessions(id,token_hash,user_agent,created_at,last_seen_at,expires_at)
VALUES
(repeat('1',32),decode(repeat('1',64),'hex'),'Current device',now()-interval '10 days',now()-interval '30 minutes',now()+interval '1 day'),
(repeat('2',32),decode(repeat('2',64),'hex'),'New but idle device',now()-interval '1 day',now()-interval '10 minutes',now()+interval '1 day'),
(repeat('3',32),decode(repeat('3',64),'hex'),'Recently active device',now()-interval '5 days',now()-interval '1 minute',now()+interval '1 day'),
(repeat('4',32),decode(repeat('4',64),'hex'),'Tied recent device',now()-interval '5 days',now()-interval '1 minute',now()+interval '1 day'),
(repeat('5',32),decode(repeat('5',64),'hex'),'Expired device',now(),now(),now()-interval '1 minute')`); err != nil {
		t.Fatal(err)
	}
	// A missing current session must not hide active devices.
	current := "00000000000000000000000000000000"
	for _, scenario := range []struct {
		current string
		order   []string
	}{
		{current, []string{"Tied recent device", "Recently active device", "New but idle device", "Current device"}},
		{"11111111111111111111111111111111", []string{"Current device", "Tied recent device", "Recently active device", "New but idle device"}},
	} {
		sessions, err := db.ListAdminSessions(ctx, scenario.current)
		if err != nil || len(sessions) != len(scenario.order) {
			t.Fatalf("active session list: %+v %v", sessions, err)
		}
		for index, session := range sessions {
			if session.UserAgent != scenario.order[index] || session.Current != (session.ID == scenario.current) {
				t.Fatalf("session %d must follow recent activity and identify the current device: %+v", index, session)
			}
		}
	}
}
