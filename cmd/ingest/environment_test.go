package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredDeploymentNamesFailBeforeGeneratingPrivateState(t *testing.T) {
	for _, name := range []string{"DATA_DIR", "PROVIDERS_DIR", "BIND", "PUBLIC_URL", "ADMIN_PASSWORD", "MASTER_KEY"} {
		t.Setenv("INGEST_"+name, "")
	}
	for _, name := range []string{"DATA_DIR", "PROVIDERS_DIR", "BIND", "PUBLIC_URL", "ADMIN_PASSWORD", "MASTER_KEY"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("INGEST_"+name, "private-value-must-not-appear")
			state := filepath.Join(t.TempDir(), "state")
			err := command(context.Background(), []string{"init", "--data-dir", state})
			if err == nil || !strings.Contains(err.Error(), "rename INGEST_"+name+" to "+name) || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("retired setting did not give a safe migration error: %v", err)
			}
			if _, err := os.Stat(state); !os.IsNotExist(err) {
				t.Fatal("obsolete environment modified private state before rejection")
			}
		})
	}
}

func TestUnprefixedCredentialsAndBindAreUsedByNativeCommands(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "synthetic-administrator-password")
	t.Setenv("MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	state := filepath.Join(t.TempDir(), "unused-state")
	password, err := administratorPassword(state)
	if err != nil || password != "synthetic-administrator-password" {
		t.Fatal("ADMIN_PASSWORD was not used")
	}
	key, err := masterKey(state)
	defer clear(key)
	if err != nil || len(key) != 32 {
		t.Fatal("MASTER_KEY was not used")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("explicit credentials unexpectedly generated private state")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Error("healthcheck used the wrong path")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("BIND", strings.TrimPrefix(server.URL, "http://"))
	if err := command(context.Background(), []string{"healthcheck"}); err != nil {
		t.Fatalf("healthcheck did not use BIND: %v", err)
	}
}
