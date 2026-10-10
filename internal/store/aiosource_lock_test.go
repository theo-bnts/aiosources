package store

import (
	"bytes"
	"context"
	"net"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

// Delay the server's receipt of connection termination. Closing a client
// socket alone cannot confirm that PostgreSQL has released its session locks.
type delayedTerminationConn struct{ net.Conn }

func (c *delayedTerminationConn) Write(b []byte) (int, error) {
	if bytes.Equal(b, []byte{'X', 0, 0, 0, 4}) {
		return len(b), nil
	}
	return c.Conn.Write(b)
}

func (c *delayedTerminationConn) Close() error { return nil }

func TestAIOLockReleaseConfirmsUnlockBeforeConnectionTermination(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("sslmode", "disable") // Inspect only this isolated test's protocol.
	u.RawQuery = query.Encode()
	config, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = conn.Close() })
		return &delayedTerminationConn{conn}, nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	worker := &Store{pool: pool}
	other, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	release, locked, err := worker.AIOLock(ctx, "resource:alldebrid:account:movie:example")
	if err != nil || !locked {
		t.Fatalf("acquire worker lock: %t %v", locked, err)
	}
	defer release()
	if next, acquired, err := other.AIOLock(ctx, "resource:alldebrid:account:movie:example"); err != nil || acquired {
		if acquired {
			next()
		}
		t.Fatalf("live worker was not excluded: %t %v", acquired, err)
	}
	release()
	release() // Deferred and explicit release must remain safe together.
	next, acquired, err := other.AIOLock(ctx, "resource:alldebrid:account:movie:example")
	if err != nil || !acquired {
		t.Fatalf("released worker still blocks its successor: %t %v", acquired, err)
	}
	next()
}
