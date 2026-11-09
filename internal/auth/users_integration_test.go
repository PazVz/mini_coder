//go:build integration

package auth_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/authz"
	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

func createMember(t *testing.T, service *auth.Service, adminID int64, username string) auth.User {
	t.Helper()
	user, err := service.CreateUser(t.Context(), adminID, auth.Credentials{
		Username: username,
		Password: credentials.Password,
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func TestUserCreationAndPagination(t *testing.T) {
	service, pool := newService(t)
	admin := setup(t, service)
	alice := createMember(t, service, admin.ID, "alice")
	bob := createMember(t, service, admin.ID, "bob")
	if alice.Role != "member" {
		t.Fatalf("new user role = %q, want member", alice.Role)
	}
	if alice.Status != "active" {
		t.Fatalf("new user status = %q, want active", alice.Status)
	}
	if _, err := service.Login(t.Context(), auth.Credentials{Username: "alice", Password: credentials.Password}); err != nil {
		t.Fatalf("new member cannot log in: %v", err)
	}
	if _, err := service.CreateUser(t.Context(), admin.ID, auth.Credentials{Username: "alice", Password: credentials.Password}); !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("duplicate username = %v, want ErrUsernameTaken", err)
	}
	_, err := service.CreateUser(t.Context(), admin.ID, auth.Credentials{
		Username: "Bad Name",
		Password: "short",
	})
	var validation *auth.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("invalid credentials = %v, want ValidationError", err)
	}
	if len(validation.Fields) != 2 {
		t.Fatalf("credential validation fields = %+v, want username and password errors", validation.Fields)
	}
	first, err := service.ListUsers(t.Context(), admin.ID, auth.ListUsersOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Users) != 2 {
		t.Fatalf("first page length = %d, want 2", len(first.Users))
	}
	if first.Users[0].ID != admin.ID || first.Users[1].ID != alice.ID {
		t.Fatalf("first page IDs = %d, %d; want %d, %d", first.Users[0].ID, first.Users[1].ID, admin.ID, alice.ID)
	}
	if first.NextAfterID != strconv.FormatInt(alice.ID, 10) {
		t.Fatalf("next cursor = %q, want %d", first.NextAfterID, alice.ID)
	}
	second, err := service.ListUsers(t.Context(), admin.ID, auth.ListUsersOptions{Limit: 2, AfterID: alice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Users) != 1 {
		t.Fatalf("last page length = %d, want 1", len(second.Users))
	}
	if second.Users[0].ID != bob.ID {
		t.Fatalf("last page user ID = %d, want %d", second.Users[0].ID, bob.ID)
	}
	if second.NextAfterID != "" {
		t.Fatalf("last page cursor = %q, want none", second.NextAfterID)
	}
	empty, err := service.ListUsers(t.Context(), admin.ID, auth.ListUsersOptions{Limit: 2, AfterID: bob.ID})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Users == nil || len(empty.Users) != 0 {
		t.Fatalf("empty page users = %#v, want a non-nil empty slice", empty.Users)
	}
	if empty.NextAfterID != "" {
		t.Fatalf("empty page cursor = %q, want none", empty.NextAfterID)
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("users after rejected creation = %d, want 3", count)
	}
}

func TestUserManagementPermissionsAndValidation(t *testing.T) {
	service, _ := newService(t)
	admin := setup(t, service)
	member := createMember(t, service, admin.ID, "member")
	for _, tc := range []struct {
		name string
		run  func(int64) error
	}{
		{"create", func(actor int64) error {
			_, err := service.CreateUser(t.Context(), actor, auth.Credentials{Username: "another", Password: credentials.Password})
			return err
		}},
		{"list", func(actor int64) error {
			_, err := service.ListUsers(t.Context(), actor, auth.ListUsersOptions{Limit: 50})
			return err
		}},
		{"status", func(actor int64) error {
			_, err := service.UpdateUserStatus(t.Context(), actor, member.ID, "disabled")
			return err
		}},
		{"role", func(actor int64) error {
			_, err := service.UpdateUserRole(t.Context(), actor, member.ID, "admin")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(member.ID); !errors.Is(err, authz.ErrForbidden) {
				t.Fatalf("member operation = %v, want ErrForbidden", err)
			}
			if err := tc.run(0); !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("unknown actor = %v, want ErrUnauthenticated", err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"unknown status", func() error {
			_, err := service.UpdateUserStatus(t.Context(), admin.ID, member.ID, "pending")
			return err
		}},
		{"empty role", func() error {
			_, err := service.UpdateUserRole(t.Context(), admin.ID, member.ID, "")
			return err
		}},
		{"negative cursor", func() error {
			_, err := service.ListUsers(t.Context(), admin.ID, auth.ListUsersOptions{AfterID: -1, Limit: 50})
			return err
		}},
		{"empty limit", func() error {
			_, err := service.ListUsers(t.Context(), admin.ID, auth.ListUsersOptions{})
			return err
		}},
		{"large limit", func() error {
			_, err := service.ListUsers(t.Context(), admin.ID, auth.ListUsersOptions{Limit: 101})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var validation *auth.ValidationError
			if err := tc.run(); !errors.As(err, &validation) {
				t.Fatalf("invalid input = %v, want ValidationError", err)
			}
		})
	}
	if _, err := service.UpdateUserStatus(t.Context(), admin.ID, member.ID+100, "disabled"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("missing status target: %v", err)
	}
	if _, err := service.UpdateUserRole(t.Context(), admin.ID, member.ID+100, "admin"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("missing role target: %v", err)
	}
}

func TestUserUpdatesAndSessionRevocation(t *testing.T) {
	service, _ := newService(t)
	admin := setup(t, service)
	member := createMember(t, service, admin.ID, "member")
	memberCredentials := auth.Credentials{Username: member.Username, Password: credentials.Password}
	first, err := service.Login(t.Context(), memberCredentials)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(t.Context(), memberCredentials)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := service.UpdateUserRole(t.Context(), admin.ID, member.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Role != "admin" {
		t.Fatalf("promoted role = %q, want admin", promoted.Role)
	}
	if !promoted.UpdatedAt.After(member.UpdatedAt) {
		t.Fatalf("promotion timestamp = %v, want later than %v", promoted.UpdatedAt, member.UpdatedAt)
	}
	current, err := service.Authenticate(t.Context(), first.Token)
	if err != nil {
		t.Fatal(err)
	}
	if current.Role != "admin" {
		t.Fatalf("existing session role = %q, want admin", current.Role)
	}
	if _, err := service.ListUsers(t.Context(), member.ID, auth.ListUsersOptions{Limit: 50}); err != nil {
		t.Fatalf("promoted user denied: %v", err)
	}
	if _, err := service.UpdateUserRole(t.Context(), admin.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListUsers(t.Context(), member.ID, auth.ListUsersOptions{Limit: 50}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("demoted actor still authorized: %v", err)
	}
	if _, err := service.UpdateUserStatus(t.Context(), admin.ID, member.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(t.Context(), memberCredentials); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled login: %v", err)
	}
	if _, err := service.ListUsers(t.Context(), member.ID, auth.ListUsersOptions{Limit: 50}); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("disabled actor: %v", err)
	}
	if _, err := service.UpdateUserStatus(t.Context(), admin.ID, member.ID, "active"); err != nil {
		t.Fatal(err)
	}
	for _, session := range []auth.Session{first, second} {
		if _, err := service.Authenticate(t.Context(), session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("revoked session revived: %v", err)
		}
	}
	if _, err := service.Login(t.Context(), memberCredentials); err != nil {
		t.Fatalf("reenabled member cannot log in: %v", err)
	}
	unchanged, err := service.UpdateUserRole(t.Context(), admin.ID, admin.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != admin {
		t.Fatalf("same role changed user: %+v, want %+v", unchanged, admin)
	}
	unchanged, err = service.UpdateUserStatus(t.Context(), admin.ID, admin.ID, "active")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != admin {
		t.Fatalf("same status changed user: %+v, want %+v", unchanged, admin)
	}
}

func TestLastActiveAdminProtection(t *testing.T) {
	service, pool := newService(t)
	admin := setup(t, service)
	other := createMember(t, service, admin.ID, "other-admin")
	if _, err := service.UpdateUserRole(t.Context(), admin.ID, other.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateUserStatus(t.Context(), admin.ID, other.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	// A disabled administrator cannot satisfy the last-active-admin invariant.
	if _, err := service.UpdateUserRole(t.Context(), admin.ID, admin.ID, "member"); !errors.Is(err, auth.ErrLastAdmin) {
		t.Fatalf("last admin demotion: %v", err)
	}
	if _, err := service.UpdateUserStatus(t.Context(), admin.ID, admin.ID, "disabled"); !errors.Is(err, auth.ErrLastAdmin) {
		t.Fatalf("last admin disable: %v", err)
	}
	count, err := dbgen.New(pool).CountActiveAdmins(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("remaining administrators = %d, want 1", count)
	}
}

func TestConcurrentAdminRemoval(t *testing.T) {
	for _, scenario := range []string{"demote both", "disable both", "demote and disable"} {
		t.Run(scenario, func(t *testing.T) {
			service, pool := newService(t)
			first := setup(t, service)
			second := createMember(t, service, first.ID, "second-admin")
			if _, err := service.UpdateUserRole(t.Context(), first.ID, second.ID, "admin"); err != nil {
				t.Fatal(err)
			}
			other, err := auth.NewService(pool, "")
			if err != nil {
				t.Fatal(err)
			}
			// Queue both requests behind an actual database lock, not a timed sleep.
			gate, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer gate.Rollback(context.Background())
			if _, err := dbgen.New(gate).LockUserManagement(t.Context()); err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			for i, actor := range []auth.User{first, second} {
				go func() {
					var err error
					if scenario == "disable both" || (scenario == "demote and disable" && i == 1) {
						_, err = other.UpdateUserStatus(t.Context(), actor.ID, actor.ID, "disabled")
					} else {
						_, err = other.UpdateUserRole(t.Context(), actor.ID, actor.ID, "member")
					}
					results <- err
				}()
			}
			waitForManagementWaiters(t, pool, 2)
			if err := gate.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			succeeded, protected := 0, 0
			for range 2 {
				err := <-results
				switch {
				case err == nil:
					succeeded++
				case errors.Is(err, auth.ErrLastAdmin):
					protected++
				default:
					t.Fatalf("concurrent removal: %v", err)
				}
			}
			if succeeded != 1 {
				t.Fatalf("successful removals = %d, want 1", succeeded)
			}
			if protected != 1 {
				t.Fatalf("protected removals = %d, want 1", protected)
			}
			count, err := dbgen.New(pool).CountActiveAdmins(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("active administrators = %d, want 1", count)
			}
		})
	}
}

func TestQueuedManagementRechecksActor(t *testing.T) {
	for _, change := range []string{"role", "status"} {
		t.Run(change, func(t *testing.T) {
			service, pool := newService(t)
			admin := setup(t, service)
			actor := createMember(t, service, admin.ID, "queued-admin")
			if _, err := service.UpdateUserRole(t.Context(), admin.ID, actor.ID, "admin"); err != nil {
				t.Fatal(err)
			}
			gate, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer gate.Rollback(context.Background())
			queries := dbgen.New(gate)
			if _, err := queries.LockUserManagement(t.Context()); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := service.CreateUser(t.Context(), actor.ID, auth.Credentials{Username: "should-not-exist", Password: credentials.Password})
				result <- err
			}()
			waitForManagementWaiters(t, pool, 1)
			want := authz.ErrForbidden
			if change == "role" {
				err = queries.UpdateUserRole(t.Context(), dbgen.UpdateUserRoleParams{UserID: actor.ID, Role: "member"})
			} else {
				err = queries.UpdateUserStatus(t.Context(), dbgen.UpdateUserStatusParams{ID: actor.ID, Status: "disabled"})
				want = auth.ErrUnauthenticated
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := gate.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, want) {
				t.Fatalf("queued request = %v, want %v", err, want)
			}
			var count int
			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM users WHERE username='should-not-exist'").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("unauthorized user creation = %d, want 0", count)
			}
		})
	}
}

func waitForManagementWaiters(t *testing.T, pool *pgxpool.Pool, expected int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		// Restrict observations to this test's lock table, even when another test
		// process uses a different schema in the same PostgreSQL database.
		err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity a
WHERE a.datname = current_database() AND a.wait_event_type = 'Lock'
AND a.query LIKE '-- name: LockUserManagement%'
AND EXISTS (
    SELECT 1 FROM pg_locks l
    WHERE l.pid = a.pid AND l.relation = 'user_management_lock'::regclass
)`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count >= expected {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("management waiters = %d, want %d", count, expected)
		case <-ticker.C:
		}
	}
}

func TestUserUpdateFailureRollsBack(t *testing.T) {
	service, pool := newService(t)
	admin := setup(t, service)
	member := createMember(t, service, admin.ID, "member")
	session, err := service.Login(t.Context(), auth.Credentials{Username: member.Username, Password: credentials.Password})
	if err != nil {
		t.Fatal(err)
	}
	// Fail after the session-revocation trigger, so status, role, timestamps and
	// session deletions must all be rolled back with the transaction.
	_, err = pool.Exec(t.Context(), `CREATE FUNCTION fail_user_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'injected user update failure'; END; $$;
CREATE TRIGGER zz_fail_user_update AFTER UPDATE ON users FOR EACH ROW EXECUTE FUNCTION fail_user_update();`)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"role", "status"} {
		if field == "role" {
			_, err = service.UpdateUserRole(t.Context(), admin.ID, member.ID, "admin")
		} else {
			_, err = service.UpdateUserStatus(t.Context(), admin.ID, member.ID, "disabled")
		}
		if err == nil {
			t.Fatalf("%s update ignored database failure", field)
		}
		current, err := service.Authenticate(t.Context(), session.Token)
		if err != nil {
			t.Fatalf("session after failed %s update: %v", field, err)
		}
		if current != member {
			t.Fatalf("%s update partially committed: %+v, want %+v", field, current, member)
		}
	}
	if _, err := pool.Exec(t.Context(), "DROP TRIGGER zz_fail_user_update ON users; DROP FUNCTION fail_user_update()"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateUserRole(t.Context(), admin.ID, member.ID, "admin"); err != nil {
		t.Fatalf("management gate not released after failure: %v", err)
	}
}
