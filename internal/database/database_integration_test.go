//go:build integration

package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"github.com/PazVz/mini_coder/internal/database/dbgen"
	"github.com/PazVz/mini_coder/internal/testutil"
)

func migrateUp(t *testing.T, provider *goose.Provider) {
	t.Helper()
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrations(t *testing.T) {
	pool, provider := testutil.NewDatabase(t)
	ctx := t.Context()
	for _, state := range []goose.State{goose.StatePending, goose.StateApplied} {
		statuses, err := provider.Status(ctx)
		if err != nil || len(statuses) != 5 {
			t.Fatalf("migration status = %v, error = %v", statuses, err)
		}
		for _, status := range statuses {
			if status.State != state {
				t.Fatalf("state = %s, want %s", status.State, state)
			}
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
	// Roll back primary email, user management, authentication and original users.
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
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
	pool, provider := testutil.NewDatabase(t)
	migrateUp(t, provider)
	ctx := t.Context()
	queries := dbgen.New(pool)
	created, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		Username: "alice", PasswordHash: pgtype.Text{String: "test-only-password-hash", Valid: true},
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
	if err != nil || byID != dbgen.User(created) {
		t.Fatalf("lookup by ID = %+v, error = %v", byID, err)
	}
	byName, err := otherQueries.GetUserByUsername(ctx, "alice")
	if err != nil || byName != dbgen.User(created) {
		t.Fatalf("lookup by username = %+v, error = %v", byName, err)
	}
	if _, err := queries.GetUserByUsername(ctx, "missing"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing username: %v", err)
	}
	if _, err := queries.GetUserByID(ctx, -1); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing ID: %v", err)
	}
	_, err = queries.CreateUser(ctx, dbgen.CreateUserParams{Username: "alice", PasswordHash: pgtype.Text{String: "another-test-hash", Valid: true}})
	assertPostgresError(t, err, "23505", "users_username_key")
	for _, name := range []string{"", "ab", "Alice", "a b", "álî", "用户一", strings.Repeat("a", 33)} {
		_, err := queries.CreateUser(ctx, dbgen.CreateUserParams{Username: name, PasswordHash: pgtype.Text{String: "test-hash", Valid: true}})
		assertPostgresError(t, err, "23514", "users_username_format")
	}
	_, err = queries.CreateUser(ctx, dbgen.CreateUserParams{Username: "bob", PasswordHash: pgtype.Text{String: " ", Valid: true}})
	assertPostgresError(t, err, "23514", "users_password_hash_nonempty")
	_, err = pool.Exec(ctx, "UPDATE users SET status = 'unknown' WHERE id = $1", created.ID)
	assertPostgresError(t, err, "23514", "users_status_check")
}

func TestUserTransactionRollback(t *testing.T) {
	pool, provider := testutil.NewDatabase(t)
	migrateUp(t, provider)
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	queries := dbgen.New(pool)
	_, err = queries.WithTx(tx).CreateUser(ctx, dbgen.CreateUserParams{
		Username: "rolled-back", PasswordHash: pgtype.Text{String: "test-hash", Valid: true},
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

func TestAuthMigrationPreservesExistingUsers(t *testing.T) {
	pool, provider := testutil.NewDatabase(t)
	ctx := t.Context()
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, "INSERT INTO users (username,password_hash) VALUES ('existing','old hash') RETURNING id").Scan(&id); err != nil {
		t.Fatal(err)
	}
	migrateUp(t, provider)
	u, err := dbgen.New(pool).GetAuthUserByUsername(ctx, "existing")
	if err != nil || u.ID != id || u.Role != "member" {
		t.Fatalf("upgrade user: %+v %v", u, err)
	}
	// Remove verification, primary email and the management gate before authentication.
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE id=$1)", id).Scan(&exists); err != nil || !exists {
		t.Fatalf("auth downgrade lost user: exists=%v err=%v", exists, err)
	}
	migrateUp(t, provider)
	if u, err := dbgen.New(pool).GetAuthUserByUsername(ctx, "existing"); err != nil || u.Role != "member" {
		t.Fatalf("reapply auth migration: %+v %v", u, err)
	}
}
