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

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/database/dbgen"
	"github.com/PazVz/mini_coder/internal/httpapi"
	"github.com/PazVz/mini_coder/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

const setupToken = "integration-only-setup-token-32-bytes"

var credentials = auth.Credentials{Username: "admin", Password: "integration password"}

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
func setup(t *testing.T, s *auth.Service) auth.User {
	t.Helper()
	u, err := s.Setup(t.Context(), setupToken, credentials)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func login(t *testing.T, s *auth.Service) auth.Session {
	t.Helper()
	session, err := s.Login(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestSetupAuthorizationAndRace(t *testing.T) {
	s, pool := newService(t)
	ctx := t.Context()
	disabled, err := auth.NewService(pool, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.Setup(ctx, "", credentials); !errors.Is(err, auth.ErrSetupDisabled) {
		t.Fatalf("disabled setup: %v", err)
	}
	if _, err := auth.NewService(pool, "short"); err == nil {
		t.Fatal("accepted weak setup token")
	}
	if _, err := s.Setup(ctx, "wrong-token", credentials); !errors.Is(err, auth.ErrSetupForbidden) {
		t.Fatalf("setup token: %v", err)
	}
	if needed, err := s.NeedsSetup(ctx); err != nil || !needed {
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
		wg.Go(func() { <-start; _, err := other.Setup(ctx, setupToken, credentials); results <- err })
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
	if needed, err := s.NeedsSetup(ctx); needed || err != nil {
		t.Fatalf("user deletion reopened setup: %v %v", needed, err)
	}
}

func TestSetupConflictRollsBack(t *testing.T) {
	s, pool := newService(t)
	_, err := dbgen.New(pool).CreateUser(t.Context(), dbgen.CreateUserParams{Username: "admin", PasswordHash: "old hash"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Setup(t.Context(), setupToken, credentials); !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("conflict: %v", err)
	}
	if needed, err := s.NeedsSetup(t.Context()); !needed || err != nil {
		t.Fatalf("failed setup changed state: %v %v", needed, err)
	}
	u, err := s.Setup(t.Context(), setupToken, auth.Credentials{Username: "new-admin", Password: credentials.Password})
	if err != nil || u.Role != "admin" {
		t.Fatalf("retry setup: %+v %v", u, err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s, pool := newService(t)
	u := setup(t, s)
	first, second := login(t, s), login(t, s)
	if first.Token == second.Token || first.User != u {
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
	persisted, err := dbgen.New(pool).GetUserByID(t.Context(), u.ID)
	if err != nil || !strings.HasPrefix(persisted.PasswordHash, "$argon2id$") || strings.Contains(persisted.PasswordHash, credentials.Password) {
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
	if got, err := restarted.Authenticate(t.Context(), first.Token); err != nil || got != u {
		t.Fatalf("session did not persist: %+v %v", got, err)
	}
	for _, c := range []auth.Credentials{{Username: "missing", Password: credentials.Password}, {Username: "admin", Password: "incorrect password"}} {
		if _, err := s.Login(t.Context(), c); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("invalid login: %v", err)
		}
	}
	for _, token := range []string{"", "bad", strings.Repeat("a", 43)} {
		if _, err := s.Authenticate(t.Context(), token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("invalid session: %v", err)
		}
	}
	if err := s.Logout(t.Context(), first.Token); err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(t.Context(), first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(t.Context(), first.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("logged out session accepted")
	}
	if _, err := s.Authenticate(t.Context(), second.Token); err != nil {
		t.Fatal("logout revoked another session")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(t.Context(), second.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("expired session accepted")
	}
	if err := s.DeleteExpiredSessions(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cleanup count=%d err=%v", count, err)
	}
}

func TestDisabledUserRevocationAndCurrentRole(t *testing.T) {
	s, pool := newService(t)
	u := setup(t, s)
	first, second := login(t, s), login(t, s)
	if _, err := pool.Exec(t.Context(), "UPDATE user_roles SET role='member' WHERE user_id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	if current, err := s.Authenticate(t.Context(), first.Token); err != nil || current.Role != "member" {
		t.Fatalf("stale role: %+v %v", current, err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE users SET status='disabled',updated_at=now() WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(t.Context(), credentials); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled login: %v", err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE users SET status='active',updated_at=now() WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	for _, session := range []auth.Session{first, second} {
		if _, err := s.Authenticate(t.Context(), session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("reenabling restored a revoked session")
		}
	}
	login(t, s)
}

func TestLoginWaitsForDisableTransaction(t *testing.T) {
	s, pool := newService(t)
	u := setup(t, s)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), "UPDATE users SET status='disabled',updated_at=now() WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Login(t.Context(), credentials); done <- err }()
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
	s, _ := newService(t)
	server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{Auth: s, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}))
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
	request("GET", "/api/v1/auth/me", "", 401)
	request("POST", "/api/v1/auth/setup", body, 201)
	request("POST", "/api/v1/auth/setup", body, 409)
	request("POST", "/api/v1/auth/login", body, 200)
	request("GET", "/api/v1/auth/me", "", 200)
	request("POST", "/api/v1/auth/logout", "", 204)
	request("GET", "/api/v1/auth/me", "", 401)
	request("POST", "/api/v1/auth/logout", "", 204)
}

func TestSetupRollsBackAfterUserCreation(t *testing.T) {
	s, pool := newService(t)
	ctx := t.Context()
	// Fail the last write, after both the user and role have been created.
	_, err := pool.Exec(ctx, `CREATE FUNCTION fail_setup() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected setup failure'; END; $$;
 CREATE TRIGGER fail_setup BEFORE UPDATE ON auth_setup FOR EACH ROW EXECUTE FUNCTION fail_setup();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Setup(ctx, setupToken, credentials); err == nil {
		t.Fatal("setup ignored database failure")
	}
	var users, roles int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_roles)").Scan(&users, &roles); err != nil {
		t.Fatal(err)
	}
	if users != 0 || roles != 0 {
		t.Fatalf("partial setup persisted: %d users, %d roles", users, roles)
	}
	if needed, err := s.NeedsSetup(ctx); !needed || err != nil {
		t.Fatalf("setup state changed on failure: %v %v", needed, err)
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER fail_setup ON auth_setup; DROP FUNCTION fail_setup()"); err != nil {
		t.Fatal(err)
	}
	setup(t, s)
}
