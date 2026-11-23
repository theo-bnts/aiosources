package aiosource

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIdentificationRetriesOnlyRolledBackTransactions(t *testing.T) {
	for _, tc := range []struct {
		state string
		calls int
	}{
		{"40P01", 2},
		{"40001", 2},
		{"23514", 1},
	} {
		t.Run(tc.state, func(t *testing.T) {
			failure := &pgconn.PgError{Code: tc.state, Message: "synthetic private record data"}
			calls := 0
			err := retryIdentificationTransaction(t.Context(), func() error {
				calls++
				if calls == 1 {
					return failure
				}
				return nil
			})
			if calls != tc.calls || (tc.calls == 1 && !errors.Is(err, failure)) || (tc.calls == 2 && err != nil) {
				t.Fatalf("unexpected transaction outcome after %d calls: %v", calls, err)
			}
			detail := operationStorageDetail(failure)
			if !strings.Contains(detail, tc.state) || strings.Contains(detail, failure.Message) {
				t.Fatalf("operation detail must contain only a safe SQLSTATE: %q", detail)
			}
		})
	}
}
