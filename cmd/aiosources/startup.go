package main

import (
	"log/slog"
	"time"
)

// Container logs identify a slow startup even before HTTP and activity logs
// are available. Stage names are fixed; configuration and secrets stay private.
func startupStage(enabled bool, stage string) func() {
	if !enabled {
		return func() {}
	}
	started := time.Now()
	slog.Info("Startup stage started", "stage", stage)
	return func() {
		slog.Info("Startup stage completed", "stage", stage, "duration_ms", time.Since(started).Milliseconds())
	}
}
