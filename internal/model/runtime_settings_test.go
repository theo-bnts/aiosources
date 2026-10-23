package model

import (
	"testing"
	"time"
)

func TestRuntimeRelationshipsRejectContradictorySettings(t *testing.T) {
	for _, section := range RuntimeSections() {
		if !DefaultRuntimeSection(section).Valid(section) {
			t.Fatal("invalid recommended settings", section)
		}
	}
	for _, test := range []struct {
		section, key string
		value        int
	}{
		{"identification", "queue_batch", 10},
		{"metadata", "queue_batch", 1},
		{"metadata_retries", "sync_retry_seconds", 1000},
		{"metadata_retries", "detail_retry_max_seconds", 60},
		{"debrid_retries", "delay_minutes", 1440},
		{"security", "login_global_per_minute", 1},
		{"source_retries", "maximum_seconds", 1},
	} {
		v := DefaultRuntimeSection(test.section)
		if test.section == "metadata_retries" && test.key == "detail_retry_max_seconds" {
			v.Values["detail_retry_seconds"] = 120
		}
		if test.section == "debrid_retries" {
			v.Values["maximum_hours"] = 1
		}
		v.Values[test.key] = test.value
		if v.Valid(test.section) {
			t.Fatal("contradictory settings accepted", test)
		}
	}
}

func TestRetryBackoffSaturatesAndKeepsInitialDelay(t *testing.T) {
	for _, test := range []struct {
		attempt int
		want    time.Duration
	}{{-1, time.Minute}, {0, time.Minute}, {1, 2 * time.Minute}, {4, 15 * time.Minute}, {1000000, 15 * time.Minute}} {
		if got := RetryBackoff(test.attempt, time.Minute, 15*time.Minute); got != test.want {
			t.Fatal(test.attempt, got, test.want)
		}
	}
}
