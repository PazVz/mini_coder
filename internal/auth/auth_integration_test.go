//go:build integration

package auth_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/database/dbgen"
	"github.com/PazVz/mini_coder/internal/httpapi"
	"github.com/PazVz/mini_coder/internal/testutil"
)

const setupToken = "integration-only-setup-token-32-bytes"

var credentials = auth.Credentials{Username: "admin", Password: "integration password"}

func newUserInput(username string) auth.CreateUserInput {
	return auth.CreateUserInput{Username: username, Email: username + "@example.com", Password: credentials.Password}
}

func newService(t *testing.T) (*auth.Service, *pgxpool.Pool) {
	t.Helper()
	pool, provider := testutil.NewDatabase(t)
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	service, err := auth.NewService(pool, setupToken)
	if err != nil {
		t.Fatal(err)
	}
	return service, pool
}

func setup(t *testing.T, service *auth.Service) auth.User {
	t.Helper()
	user, err := service.Setup(t.Context(), setupToken, newUserInput(credentials.Username))
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func login(t *testing.T, service *auth.Service) auth.Session {
	t.Helper()
	session, err := service.Login(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestSetupAuthorizationAndRace(t *testing.T) {
	service, pool := newService(t)
	ctx := t.Context()
	disabled, err := auth.NewService(pool, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.Setup(ctx, "", newUserInput(credentials.Username)); !errors.Is(err, auth.ErrSetupDisabled) {
		t.Fatalf("disabled setup: %v", err)
	}
	if _, err := auth.NewService(pool, "short"); err == nil {
		t.Fatal("accepted weak setup token")
	}
	if _, err := service.Setup(ctx, "wrong-token", newUserInput(credentials.Username)); !errors.Is(err, auth.ErrSetupForbidden) {
		t.Fatalf("setup token: %v", err)
	}
	if needed, err := service.NeedsSetup(ctx); err != nil || !needed {
		t.Fatalf("initial setup: %v %v", needed, err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 6)
	start := make(chan struct{})
	// Separate service objects exercise the shared database lock.
	other, err := auth.NewService(pool, setupToken)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		wg.Go(func() {
			<-start
			_, err := other.Setup(ctx, setupToken, newUserInput(credentials.Username))
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, auth.ErrAlreadyInitialized) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("setup succeeded %d times", successes)
	}
	var admins, users int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_roles WHERE role='admin'").Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if admins != 1 || users != 1 {
		t.Fatalf("admins/users = %d/%d", admins, users)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users"); err != nil {
		t.Fatal(err)
	}
	if needed, err := service.NeedsSetup(ctx); needed || err != nil {
		t.Fatalf("user deletion reopened setup: %v %v", needed, err)
	}
}

func TestSetupConflictRollsBack(t *testing.T) {
	service, pool := newService(t)
	_, err := dbgen.New(pool).CreateUser(t.Context(), dbgen.CreateUserParams{Username: "admin", PasswordHash: pgtype.Text{String: "old hash", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Setup(t.Context(), setupToken, newUserInput(credentials.Username)); !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("conflict: %v", err)
	}
	if needed, err := service.NeedsSetup(t.Context()); !needed || err != nil {
		t.Fatalf("failed setup changed state: %v %v", needed, err)
	}
	user, err := service.Setup(t.Context(), setupToken, newUserInput("new-admin"))
	if err != nil || user.Role != "admin" {
		t.Fatalf("retry setup: %+v %v", user, err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	service, pool := newService(t)
	user := setup(t, service)
	first, second := login(t, service), login(t, service)
	if first.Token == second.Token || first.User != user {
		t.Fatal("session fixation or wrong user")
	}
	hash := sha256.Sum256([]byte(first.Token))
	var stored []byte
	if err := pool.QueryRow(t.Context(), "SELECT token_hash FROM sessions WHERE token_hash=$1", hash[:]).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 32 || string(stored) == first.Token {
		t.Fatal("session token stored in plaintext")
	}
	persisted, err := dbgen.New(pool).GetUserByID(t.Context(), user.ID)
	if err != nil || !strings.HasPrefix(persisted.PasswordHash.String, "$argon2id$") || strings.Contains(persisted.PasswordHash.String, credentials.Password) {
		t.Fatal("password was not hashed")
	}
	anotherPool, err := pgxpool.NewWithConfig(t.Context(), pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer anotherPool.Close()
	restarted, err := auth.NewService(anotherPool, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.Authenticate(t.Context(), first.Token); err != nil || got != user {
		t.Fatalf("session did not persist: %+v %v", got, err)
	}
	for _, tc := range []struct {
		name        string
		credentials auth.Credentials
	}{
		{"unknown user", auth.Credentials{Username: "missing", Password: credentials.Password}},
		{"wrong password", auth.Credentials{Username: "admin", Password: "incorrect password"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Login(t.Context(), tc.credentials); !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("Login = %v, want %v", err, auth.ErrInvalidCredentials)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"missing token", ""},
		{"short token", "bad"},
		{"noncanonical token", strings.Repeat("a", 43)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Authenticate(t.Context(), tc.token); !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("Authenticate = %v, want %v", err, auth.ErrUnauthenticated)
			}
		})
	}
	if err := service.Logout(t.Context(), first.Token); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(t.Context(), first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(t.Context(), first.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("logged out session accepted")
	}
	if _, err := service.Authenticate(t.Context(), second.Token); err != nil {
		t.Fatal("logout revoked another session")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day'"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(t.Context(), second.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("expired session accepted")
	}
	if err := service.DeleteExpiredSessions(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cleanup count=%d err=%v", count, err)
	}
}

func TestDisabledUserRevocationAndCurrentRole(t *testing.T) {
	service, pool := newService(t)
	user := setup(t, service)
	first, second := login(t, service), login(t, service)
	if _, err := pool.Exec(t.Context(), "UPDATE user_roles SET role='member' WHERE user_id=$1", user.ID); err != nil {
		t.Fatal(err)
	}
	if current, err := service.Authenticate(t.Context(), first.Token); err != nil || current.Role != "member" {
		t.Fatalf("stale role: %+v %v", current, err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE users SET status='disabled',updated_at=now() WHERE id=$1", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(t.Context(), credentials); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled login: %v", err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE users SET status='active',updated_at=now() WHERE id=$1", user.ID); err != nil {
		t.Fatal(err)
	}
	for _, session := range []auth.Session{first, second} {
		if _, err := service.Authenticate(t.Context(), session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("reenabling restored a revoked session")
		}
	}
	login(t, service)
}

func TestLoginWaitsForDisableTransaction(t *testing.T) {
	service, pool := newService(t)
	user := setup(t, service)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), "UPDATE users SET status='disabled',updated_at=now() WHERE id=$1", user.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.Login(t.Context(), credentials)
		done <- err
	}()
	// Observe the database lock rather than relying on timing of password hashing.
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case err := <-done:
			t.Fatalf("login bypassed disable lock: %v", err)
		case <-deadline:
			t.Fatal("login did not wait on the user row")
		case <-ticker.C:
			var waiting bool
			err := pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%LockAuthUserByID%')").Scan(&waiting)
			if err != nil {
				t.Fatal(err)
			}
			if waiting {
				break waiting
			}
		}
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login during disable: %v", err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("sessions after disable: %d %v", count, err)
	}
}

func TestHTTPAuthRoundTrip(t *testing.T) {
	service, _ := newService(t)
	server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Auth:   service,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	request := func(method, path, body string, status int) string {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		req.Header.Set("X-Setup-Token", setupToken)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
		}
		if strings.Contains(string(data), credentials.Password) || strings.Contains(string(data), "password_hash") {
			t.Fatal("HTTP leaked password")
		}
		return string(data)
	}
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, credentials.Username, credentials.Password)
	setupBody := fmt.Sprintf(`{"username":%q,"email":"admin@example.com","password":%q}`, credentials.Username, credentials.Password)
	request(http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized)
	request(http.MethodPost, "/api/v1/auth/setup", setupBody, http.StatusCreated)
	request(http.MethodPost, "/api/v1/auth/setup", setupBody, http.StatusConflict)
	request(http.MethodPost, "/api/v1/auth/login", body, http.StatusOK)
	request(http.MethodGet, "/api/v1/auth/me", "", http.StatusOK)
	otherSession := login(t, service)
	request(http.MethodPost, "/api/v1/auth/password", `{"current_password":"integration password","new_password":"new private password"}`, http.StatusNoContent)
	request(http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized)
	if _, err := service.Authenticate(t.Context(), otherSession.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("HTTP password change did not revoke another session: %v", err)
	}
	request(http.MethodPost, "/api/v1/auth/login", body, http.StatusUnauthorized)
	request(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"new private password"}`, http.StatusOK)
	request(http.MethodPost, "/api/v1/auth/logout", "", http.StatusNoContent)
	request(http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized)
	request(http.MethodPost, "/api/v1/auth/logout", "", http.StatusNoContent)
}

func TestSetupRollsBackAfterUserCreation(t *testing.T) {
	service, pool := newService(t)
	ctx := t.Context()
	// Fail the last write, after both the user and role have been created.
	_, err := pool.Exec(ctx, `CREATE FUNCTION fail_setup() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected setup failure'; END; $$;
 CREATE TRIGGER fail_setup BEFORE UPDATE ON auth_setup FOR EACH ROW EXECUTE FUNCTION fail_setup();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Setup(ctx, setupToken, newUserInput(credentials.Username)); err == nil {
		t.Fatal("setup ignored database failure")
	}
	var users, roles int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_roles)").Scan(&users, &roles); err != nil {
		t.Fatal(err)
	}
	if users != 0 || roles != 0 {
		t.Fatalf("partial setup persisted: %d users, %d roles", users, roles)
	}
	if needed, err := service.NeedsSetup(ctx); !needed || err != nil {
		t.Fatalf("setup state changed on failure: %v %v", needed, err)
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER fail_setup ON auth_setup; DROP FUNCTION fail_setup()"); err != nil {
		t.Fatal(err)
	}
	setup(t, service)
}
