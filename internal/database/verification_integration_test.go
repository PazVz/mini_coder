//go:build integration

package database_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/PazVz/mini_coder/internal/database/dbgen"
	"github.com/PazVz/mini_coder/internal/testutil"
)

func TestEmailVerificationMigrationRoundTrip(t *testing.T) {
	pool, provider := testutil.NewDatabase(t)
	ctx := t.Context()
	if _, err := provider.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(pool)
	before, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		Username: "existing",
		Email:    pgtype.Text{String: "existing@example.com", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	migrateUp(t, provider)
	if after, err := queries.GetUserByID(ctx, before.ID); err != nil || after != dbgen.User(before) {
		t.Fatalf("verification upgrade changed existing account: %+v error=%v", after, err)
	}
	if err := queries.CreateEmailAction(ctx, dbgen.CreateEmailActionParams{
		TokenHash: make([]byte, 32),
		Purpose:   "verify_email",
		UserID:    pgtype.Int8{Int64: before.ID, Valid: true},
		Email:     before.Email.String,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if after, err := queries.GetUserByID(ctx, before.ID); err != nil || after != dbgen.User(before) {
		t.Fatalf("verification downgrade changed existing account: %+v error=%v", after, err)
	}
	migrateUp(t, provider)
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_actions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("verification downgrade restored a stale proof: %d error=%v", count, err)
	}
}
