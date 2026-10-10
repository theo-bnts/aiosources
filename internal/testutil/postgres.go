// Package testutil supplies isolated databases for opt-in integration checks.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// NewDatabase never reuses or clears an existing database. The supplied role
// needs CREATE DATABASE; use a disposable local PostgreSQL instance.
func NewDatabase(t testing.TB) (context.Context, string) {
	t.Helper()
	connection := os.Getenv("TEST_DATABASE_URL")
	// The retained GitHub workflow still supplies this test-only legacy name.
	if connection == "" {
		connection = os.Getenv("INGEST_TEST_DATABASE_URL")
	}
	if connection == "" {
		t.Skip("set TEST_DATABASE_URL to run isolated PostgreSQL integration checks")
	}
	parsed, err := url.Parse(connection)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		t.Fatal("TEST_DATABASE_URL must be a PostgreSQL URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, connection)
	if err != nil {
		t.Fatal("connect integration PostgreSQL:", err)
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	name := "ingest_test_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		t.Fatal("create isolated integration database:", err)
	}
	parsed.Path = "/" + name
	parsed.RawPath = ""
	query := parsed.Query()
	query.Del("database")
	query.Del("dbname")
	parsed.RawQuery = query.Encode()
	connectionURL := parsed.String()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("drop isolated integration database:", err)
		}
		_ = admin.Close(cleanup)
	})
	return ctx, connectionURL
}
