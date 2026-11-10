// Package pgtest gives each test its own empty schema in the PostgreSQL
// database named by CALOR_TEST_DATABASE_URL, and skips the test when the
// variable is unset so `go test ./...` still runs everything else.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const EnvVar = "CALOR_TEST_DATABASE_URL"

// DSN creates a fresh schema, drops it when the test ends, and returns a
// connection string whose search_path points at it.
func DSN(t testing.TB) string {
	t.Helper()
	base := os.Getenv(EnvVar)
	if base == "" {
		t.Skip(EnvVar + " is not set; skipping PostgreSQL-backed test")
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := "calor_test_" + hex.EncodeToString(b[:])
	exec := func(sql string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Fatalf("connect to %s: %v\n(start one with `make db`, or unset %s to skip the database tests)", EnvVar, err, EnvVar)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec("CREATE SCHEMA " + schema)
	t.Cleanup(func() { exec("DROP SCHEMA " + schema + " CASCADE") })
	dsn, err := withSearchPath(base, schema)
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

func withSearchPath(dsn, schema string) (string, error) {
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema, nil
}
