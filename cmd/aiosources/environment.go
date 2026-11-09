package main

import (
	"fmt"
	"os"
)

// Reject retired deployment names before opening the database or generating a
// password/key. Silently ignoring an old vault-key setting could lock out users.
func validateEnvironment() error {
	for _, name := range []string{"DATA_DIR", "PROVIDERS_DIR", "BIND", "PUBLIC_URL", "ADMIN_PASSWORD", "MASTER_KEY"} {
		if os.Getenv("INGEST_"+name) != "" {
			return fmt.Errorf("rename INGEST_%s to %s in the deployment environment before starting", name, name)
		}
	}
	return nil
}
