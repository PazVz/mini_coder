//go:build integration

package database_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/PazVz/mini_coder/internal/database/dbgen"
	"github.com/PazVz/mini_coder/internal/testutil"
)

func TestEmailMigrationPreservesExistingAccounts(t *testing.T) {
	pool, provider := testutil.NewDatabase(t)
	ctx := t.Context()
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO users (username, password_hash) VALUES ('legacy', 'existing hash');
		INSERT INTO user_roles (user_id, role) SELECT id, 'admin' FROM users;
		INSERT INTO sessions (token_hash, user_id, expires_at)
		SELECT decode(repeat('ab',32),'hex'), id, now() + interval '1 day' FROM users;
		UPDATE auth_setup SET initialized = true`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(pool)
	user, err := queries.GetAuthUserByUsername(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email.Valid || user.EmailKey.Valid || user.EmailVerifiedAt.Valid {
		t.Fatalf("migration invented email information: %+v", user)
	}
	if user.Role != "admin" || user.PasswordHash.String != "existing hash" || user.Status != "active" {
		t.Fatalf("migration changed legacy account: %+v", user)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration changed sessions: count=%d error=%v", count, err)
	}
	if initialized, err := queries.GetSetupState(ctx); err != nil || !initialized {
		t.Fatalf("migration reopened setup: initialized=%v error=%v", initialized, err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	migrateUp(t, provider)
	if after, err := queries.GetAuthUserByUsername(ctx, "legacy"); err != nil || after != user {
		t.Fatalf("email migration round trip: %+v, error=%v", after, err)
	}
}

func TestEmailUniquenessAndCredentialConstraints(t *testing.T) {
	pool, provider := testutil.NewDatabase(t)
	migrateUp(t, provider)
	ctx := t.Context()
	queries := dbgen.New(pool)
	user, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		Username:     "alice",
		PasswordHash: pgtype.Text{String: "test hash", Valid: true},
		Email:        pgtype.Text{String: "Alice+work@EXAMPLE.com", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.EmailKey.String != "alice+work@example.com" || user.Email.String != "Alice+work@EXAMPLE.com" || user.EmailVerifiedAt.Valid {
		t.Fatalf("email representation = %+v", user)
	}
	// Raw SQL must obey the same unique constraint as API writes.
	_, err = pool.Exec(ctx, "INSERT INTO users (username,email) VALUES ('duplicate','ALICE+WORK@example.COM')")
	assertPostgresError(t, err, "23505", "users_email_key")
	_, err = pool.Exec(ctx, "INSERT INTO users (username,email) VALUES ('bob','bob@example.com')")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, "UPDATE users SET email='alice+WORK@example.com' WHERE username='bob'")
	assertPostgresError(t, err, "23505", "users_email_key")
	// Aliases and dots are not folded into a provider-specific identity.
	_, err = pool.Exec(ctx, `INSERT INTO users (username,email) VALUES
		('alias','alice@example.com'), ('dots','al.ice@example.com'), ('missing1',NULL), ('missing2',NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"", " alice@example.com", "alice@example.com ", "alice\n@example.com", "用户@example.com"} {
		_, err := pool.Exec(ctx, "UPDATE users SET email=$1 WHERE id=$2", email, user.ID)
		assertPostgresError(t, err, "23514", "users_email_format")
	}
	_, err = pool.Exec(ctx, "UPDATE users SET email_verified_at=now() WHERE username='missing1'")
	assertPostgresError(t, err, "23514", "users_email_verification")
	_, err = pool.Exec(ctx, "UPDATE users SET password_hash='' WHERE id=$1", user.ID)
	assertPostgresError(t, err, "23514", "users_password_hash_nonempty")
	// Remove the verification migration before testing the primary-email downgrade.
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	// A nullable credential represents an absent password; downgrade must fail atomically.
	if _, err := provider.Down(ctx); err == nil {
		t.Fatal("downgrade accepted passwordless accounts")
	}
	if after, err := queries.GetUserByID(ctx, user.ID); err != nil || after.Email != user.Email {
		t.Fatalf("failed downgrade changed email schema or data: %+v error=%v", after, err)
	}
}
