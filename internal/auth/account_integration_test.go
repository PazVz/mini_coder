//go:build integration

package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

func TestAccountEmailProvisioning(t *testing.T) {
	service, pool := newService(t)
	input := newUserInput("admin")
	input.Email = ""
	var validation *auth.ValidationError
	if _, err := service.Setup(t.Context(), setupToken, input); !errors.As(err, &validation) {
		t.Fatalf("setup without email = %v, want ValidationError", err)
	}
	input.Email = " Admin@EXAMPLE.com "
	admin, err := service.Setup(t.Context(), setupToken, input)
	if err != nil {
		t.Fatal(err)
	}
	if admin.Email != "Admin@EXAMPLE.com" || admin.EmailVerified {
		t.Fatalf("setup email = %q, verified = %v", admin.Email, admin.EmailVerified)
	}
	session := login(t, service)
	if session.User.Email != admin.Email || session.User.EmailVerified {
		t.Fatalf("login email = %+v", session.User)
	}
	input = newUserInput("member")
	input.Email = "ADMIN@example.COM"
	if _, err := service.CreateUser(t.Context(), admin.ID, input); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("duplicate email = %v, want ErrEmailTaken", err)
	}
	input.Email = ""
	if _, err := service.CreateUser(t.Context(), admin.ID, input); !errors.As(err, &validation) {
		t.Fatalf("creation without email = %v, want ValidationError", err)
	}
	member := createMember(t, service, admin.ID, "member")
	if member.Email != "member@example.com" || member.EmailVerified {
		t.Fatalf("member email = %+v", member)
	}
	// Verification is server-owned; ensure all reads report the persisted state.
	if _, err := pool.Exec(t.Context(), "UPDATE users SET email_verified_at=now() WHERE id=$1", admin.ID); err != nil {
		t.Fatal(err)
	}
	current, err := service.Authenticate(t.Context(), session.Token)
	if err != nil || !current.EmailVerified || current.Email != admin.Email {
		t.Fatalf("current email state = %+v, error=%v", current, err)
	}
	page, err := service.ListUsers(t.Context(), admin.ID, auth.ListUsersOptions{Limit: 10})
	if err != nil || len(page.Users) != 2 || !page.Users[0].EmailVerified || page.Users[1].Email != member.Email {
		t.Fatalf("listed emails = %+v, error=%v", page, err)
	}
}

func TestSetupAndCreationRaceForEmail(t *testing.T) {
	service, pool := newService(t)
	// An earlier data migration can contain an active admin while setup remains open.
	admin, err := dbgen.New(pool).CreateUser(t.Context(), dbgen.CreateUserParams{
		Username: "existing", Email: pgtype.Text{String: "existing@example.com", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbgen.New(pool).SetAdminRole(t.Context(), admin.ID); err != nil {
		t.Fatal(err)
	}
	other, err := auth.NewService(pool, setupToken)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	group.Go(func() {
		<-start
		input := newUserInput("initial")
		input.Email = "Shared@example.com"
		_, err := service.Setup(t.Context(), setupToken, input)
		results <- err
	})
	group.Go(func() {
		<-start
		input := newUserInput("managed")
		input.Email = "shared@EXAMPLE.com"
		_, err := other.CreateUser(t.Context(), admin.ID, input)
		results <- err
	})
	close(start)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, auth.ErrEmailTaken) {
			t.Fatalf("email race = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("email race successes = %d, want 1", successes)
	}
	var users, roles int
	if err := pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_roles)").Scan(&users, &roles); err != nil {
		t.Fatal(err)
	}
	if users != 2 || roles != 2 {
		t.Fatalf("email conflict left partial data: users=%d roles=%d", users, roles)
	}
	// Whether setup won or lost, its singleton state must agree with the committed user.
	initialized, err := dbgen.New(pool).GetSetupState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var initialExists bool
	if err := pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM users WHERE username='initial')").Scan(&initialExists); err != nil {
		t.Fatal(err)
	}
	if initialized != initialExists {
		t.Fatal("setup state did not roll back with the email conflict")
	}
}

func TestPasswordChangeLifecycle(t *testing.T) {
	service, pool := newService(t)
	admin := setup(t, service)
	member := createMember(t, service, admin.ID, "member")
	first, second := login(t, service), login(t, service)
	memberSession, err := service.Login(t.Context(), auth.Credentials{Username: member.Username, Password: credentials.Password})
	if err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(pool)
	before, err := queries.GetUserByID(t.Context(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	change := auth.PasswordChange{CurrentPassword: credentials.Password, NewPassword: "new private password"}
	for _, tc := range []struct {
		name   string
		userID int64
		token  string
		change auth.PasswordChange
		want   error
	}{
		{"wrong current password", admin.ID, first.Token, auth.PasswordChange{CurrentPassword: "wrong password", NewPassword: change.NewPassword}, auth.ErrInvalidCurrentPassword},
		{"another user's session", member.ID, first.Token, change, auth.ErrUnauthenticated},
		{"missing session", admin.ID, "", change, auth.ErrUnauthenticated},
		{"unknown user", member.ID + 100, first.Token, change, auth.ErrUnauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := service.ChangePassword(t.Context(), tc.userID, tc.token, tc.change); !errors.Is(err, tc.want) {
				t.Fatalf("ChangePassword = %v, want %v", err, tc.want)
			}
		})
	}
	for _, invalid := range []auth.PasswordChange{
		{CurrentPassword: "", NewPassword: change.NewPassword},
		{CurrentPassword: strings.Repeat("x", 1025), NewPassword: change.NewPassword},
		{CurrentPassword: credentials.Password, NewPassword: "short"},
		{CurrentPassword: credentials.Password, NewPassword: strings.Repeat("x", 1025)},
		{CurrentPassword: credentials.Password, NewPassword: "invalid UTF8\xff"},
	} {
		var validation *auth.ValidationError
		if err := service.ChangePassword(t.Context(), admin.ID, first.Token, invalid); !errors.As(err, &validation) {
			t.Fatalf("invalid password change = %v", err)
		}
	}
	if after, err := queries.GetUserByID(t.Context(), admin.ID); err != nil || after != before {
		t.Fatalf("failed password change modified account: %+v error=%v", after, err)
	}
	for _, token := range []string{first.Token, second.Token} {
		if _, err := service.Authenticate(t.Context(), token); err != nil {
			t.Fatalf("failed password change revoked a session: %v", err)
		}
	}
	if err := service.ChangePassword(t.Context(), admin.ID, first.Token, change); err != nil {
		t.Fatal(err)
	}
	after, err := queries.GetUserByID(t.Context(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.PasswordHash == before.PasswordHash || !strings.HasPrefix(after.PasswordHash.String, "$argon2id$") {
		t.Fatal("new password was not stored as a fresh Argon2id hash")
	}
	if after.Email != before.Email || after.Status != before.Status || !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("unexpected profile changes: %+v", after)
	}
	for _, token := range []string{first.Token, second.Token} {
		if _, err := service.Authenticate(t.Context(), token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("old session survived password change: %v", err)
		}
	}
	if _, err := service.Authenticate(t.Context(), memberSession.Token); err != nil {
		t.Fatalf("password change revoked another user's session: %v", err)
	}
	if _, err := service.Login(t.Context(), credentials); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password accepted: %v", err)
	}
	if _, err := service.Login(t.Context(), auth.Credentials{Username: admin.Username, Password: change.NewPassword}); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
}

func TestPasswordlessAccountRejectsLocalCredentials(t *testing.T) {
	service, pool := newService(t)
	user, err := dbgen.New(pool).CreateUser(t.Context(), dbgen.CreateUserParams{
		Username: "external", Email: pgtype.Text{String: "external@example.com", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(t.Context(), auth.Credentials{Username: user.Username, Password: credentials.Password}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("passwordless login = %v, want ErrInvalidCredentials", err)
	}
	// Model a future externally authenticated session without adding an OIDC flow.
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if _, err := dbgen.New(pool).CreateSession(t.Context(), dbgen.CreateSessionParams{TokenHash: hash[:], UserID: user.ID}); err != nil {
		t.Fatal(err)
	}
	err = service.ChangePassword(t.Context(), user.ID, token, auth.PasswordChange{
		CurrentPassword: credentials.Password, NewPassword: "new private password",
	})
	if !errors.Is(err, auth.ErrPasswordNotSet) {
		t.Fatalf("passwordless change = %v, want ErrPasswordNotSet", err)
	}
	if after, err := dbgen.New(pool).GetUserByID(t.Context(), user.ID); err != nil || after.PasswordHash.Valid {
		t.Fatalf("passwordless account gained local credentials: %+v error=%v", after, err)
	}
}

func waitForAuthUserLock(t *testing.T, pool *pgxpool.Pool, done <-chan error) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("authentication operation bypassed user lock: %v", err)
		case <-deadline:
			t.Fatal("authentication operation did not wait for the user lock")
		case <-ticker.C:
			var waiting bool
			if err := pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%LockAuthUserByID%')").Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				return
			}
		}
	}
}

func TestQueuedPasswordChangeRechecksSession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"credentials changed", "UPDATE users SET password_hash='replacement hash' WHERE id=$1"},
		{"disabled", "UPDATE users SET status='disabled' WHERE id=$1"},
		{"expired while waiting", "UPDATE sessions SET expires_at=clock_timestamp() WHERE user_id=$1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, pool := newService(t)
			user := setup(t, service)
			session := login(t, service)
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(t.Context(), "SELECT id FROM users WHERE id=$1 FOR UPDATE", user.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				done <- service.ChangePassword(t.Context(), user.ID, session.Token, auth.PasswordChange{
					CurrentPassword: credentials.Password, NewPassword: "new private password",
				})
			}()
			waitForAuthUserLock(t, pool, done)
			if _, err := tx.Exec(t.Context(), tc.query, user.ID); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("queued password change = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestLoginRechecksPasswordAfterWaiting(t *testing.T) {
	service, pool := newService(t)
	user := setup(t, service)
	session := login(t, service)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), "SELECT id FROM users WHERE id=$1 FOR UPDATE", user.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.Login(t.Context(), credentials)
		done <- err
	}()
	waitForAuthUserLock(t, pool, done)
	if _, err := tx.Exec(t.Context(), "UPDATE users SET password_hash=NULL WHERE id=$1", user.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login used old password after change: %v", err)
	}
	if _, err := service.Authenticate(t.Context(), session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("removing password did not revoke session: %v", err)
	}
}

func TestPasswordChangeRollsBackOnSessionRevocationFailure(t *testing.T) {
	service, pool := newService(t)
	user := setup(t, service)
	session := login(t, service)
	before, err := dbgen.New(pool).GetUserByID(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), `CREATE FUNCTION fail_revocation() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected revocation failure'; END; $$;
		CREATE TRIGGER fail_revocation BEFORE DELETE ON sessions FOR EACH ROW EXECUTE FUNCTION fail_revocation();`)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ChangePassword(t.Context(), user.ID, session.Token, auth.PasswordChange{
		CurrentPassword: credentials.Password, NewPassword: "new private password",
	}); err == nil {
		t.Fatalf("revocation failure was ignored: %v", err)
	}
	if after, err := dbgen.New(pool).GetUserByID(t.Context(), user.ID); err != nil || after != before {
		t.Fatalf("failed revocation committed password: %+v error=%v", after, err)
	}
	if _, err := service.Authenticate(t.Context(), session.Token); err != nil {
		t.Fatalf("rollback lost the old session: %v", err)
	}
}
