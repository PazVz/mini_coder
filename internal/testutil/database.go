//go:build integration

// Package testutil provides isolated PostgreSQL schemas for integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"github.com/PazVz/mini_coder/internal/database"
)

// NewDatabase creates an isolated schema and registers cleanup for its pool and migrator.
// Migrations and cleanup affect only this schema, not the caller's existing tables.
func NewDatabase(t *testing.T) (*pgxpool.Pool, *goose.Provider) {
	t.Helper()
	url := os.Getenv("MINICODER_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("MINICODER_TEST_DATABASE_URL is required for integration tests")
	}
	admin, err := database.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "test_" + strings.ToLower(rand.Text())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("clean up test schema: %v", err)
		}
	})
	config := admin.Config()
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	provider, err := database.NewMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Errorf("close migrator: %v", err)
		}
	})
	return pool, provider
}
