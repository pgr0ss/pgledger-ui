// Package testdb gives each test its own pgledger schema.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgr0ss/pgledger-visualizer/internal/pgschema"
)

// BaseURL is TEST_DATABASE_URL or the docker compose database.
func BaseURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	return "postgres://pgledger:pgledger@localhost:" + port + "/pgledger_visualizer"
}

// New creates a schema with pgledger installed and returns a URL whose
// connections use it, plus a writable pool for fixtures.
func New(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	base := BaseURL()

	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect %s: %v (start postgres with: mise run postgres-up)", base, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schema := "viz_test_" + hex.EncodeToString(b)
	ident := pgx.Identifier{schema}.Sanitize()

	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			t.Errorf("connect for cleanup: %v", err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})

	if _, err := conn.Exec(ctx, "SET search_path TO "+ident); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := pgschema.Install(ctx, conn.PgConn()); err != nil {
		t.Fatalf("%v", err)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", base, err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	schemaURL := u.String()

	admin, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(admin.Close)

	return schemaURL, admin
}
