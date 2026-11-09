//go:build integration

package database_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"github.com/PazVz/mini_coder/internal/database"
	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

// Every test uses its own schema. Never migrate or truncate the caller's schema.
func testDatabase(t *testing.T) (*pgxpool.Pool, *goose.Provider) {
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

func migrateUp(t *testing.T, provider *goose.Provider) {
	t.Helper()
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrations(t *testing.T) {
	pool, provider := testDatabase(t)
	ctx := t.Context()
	for _, state := range []goose.State{goose.StatePending, goose.StateApplied} {
		statuses, err := provider.Status(ctx)
		if err != nil || len(statuses) != 1 {
			t.Fatalf("migration status = %v, error = %v", statuses, err)
		}
		if statuses[0].State != state {
			t.Fatalf("state = %s, want %s", statuses[0].State, state)
		}
		migrateUp(t, provider)
	}
	results, err := provider.Up(ctx)
	if err != nil || len(results) != 0 {
		t.Fatalf("repeated migration applied %d changes, error = %v", len(results), err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = dbgen.New(pool).GetUserByID(ctx, 1)
	assertPostgresError(t, err, "42P01", "") // undefined_table
	migrateUp(t, provider)
	if _, err := dbgen.New(pool).GetUserByID(ctx, 1); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("query after reapplying migration: %v", err)
	}
}

func TestUserPersistence(t *testing.T) {
	pool, provider := testDatabase(t)
	migrateUp(t, provider)
	ctx := t.Context()
	queries := dbgen.New(pool)
	created, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		Username: "alice", PasswordHash: "test-only-password-hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 || created.Status != "active" || created.CreatedAt.IsZero() || !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("unexpected user defaults: %+v", created)
	}
	// Read through a new pool to verify that the insert was committed.
	otherPool, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer otherPool.Close()
	otherQueries := dbgen.New(otherPool)
	byID, err := otherQueries.GetUserByID(ctx, created.ID)
	if err != nil || byID != created {
		t.Fatalf("lookup by ID = %+v, error = %v", byID, err)
	}
	byName, err := otherQueries.GetUserByUsername(ctx, "alice")
	if err != nil || byName != created {
		t.Fatalf("lookup by username = %+v, error = %v", byName, err)
	}
	if _, err := queries.GetUserByUsername(ctx, "missing"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing username: %v", err)
	}
	if _, err := queries.GetUserByID(ctx, -1); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing ID: %v", err)
	}
	_, err = queries.CreateUser(ctx, dbgen.CreateUserParams{Username: "alice", PasswordHash: "another-test-hash"})
	assertPostgresError(t, err, "23505", "users_username_key")
	for _, name := range []string{"", "ab", "Alice", "a b", "álî", "用户一", strings.Repeat("a", 33)} {
		_, err := queries.CreateUser(ctx, dbgen.CreateUserParams{Username: name, PasswordHash: "test-hash"})
		assertPostgresError(t, err, "23514", "users_username_format")
	}
	_, err = queries.CreateUser(ctx, dbgen.CreateUserParams{Username: "bob", PasswordHash: " "})
	assertPostgresError(t, err, "23514", "users_password_hash_nonempty")
	_, err = pool.Exec(ctx, "UPDATE users SET status = 'unknown' WHERE id = $1", created.ID)
	assertPostgresError(t, err, "23514", "users_status_check")
}

func TestUserTransactionRollback(t *testing.T) {
	pool, provider := testDatabase(t)
	migrateUp(t, provider)
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	queries := dbgen.New(pool)
	_, err = queries.WithTx(tx).CreateUser(ctx, dbgen.CreateUserParams{
		Username: "rolled-back", PasswordHash: "test-hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.GetUserByUsername(ctx, "rolled-back"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("rolled back user is visible: %v", err)
	}
}

func assertPostgresError(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.ConstraintName != constraint {
		t.Fatalf("error = %v, want PostgreSQL %s (%s)", err, code, constraint)
	}
}
