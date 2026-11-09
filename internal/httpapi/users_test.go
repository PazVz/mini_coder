package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/authz"
)

type fakeUsers struct {
	create func(int64, auth.Credentials) (auth.User, error)
	list   func(int64, auth.ListUsersOptions) (auth.UserPage, error)
	status func(int64, int64, string) (auth.User, error)
	role   func(int64, int64, string) (auth.User, error)
}

func (f fakeUsers) CreateUser(_ context.Context, actor int64, credentials auth.Credentials) (auth.User, error) {
	return f.create(actor, credentials)
}

func (f fakeUsers) ListUsers(_ context.Context, actor int64, options auth.ListUsersOptions) (auth.UserPage, error) {
	return f.list(actor, options)
}

func (f fakeUsers) UpdateUserStatus(_ context.Context, actor, id int64, status string) (auth.User, error) {
	return f.status(actor, id, status)
}

func (f fakeUsers) UpdateUserRole(_ context.Context, actor, id int64, role string) (auth.User, error) {
	return f.role(actor, id, role)
}

func newUserTestHandler(users fakeUsers) http.Handler {
	return NewHandler(Config{
		Auth: fakeAuth{authenticate: func(string) (auth.User, error) {
			return auth.User{ID: 7, Role: "admin", Status: "active"}, nil
		}},
		Users:  users,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestUserRequestBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		code   string
		status int
	}{
		{"creation role injection", http.MethodPost, "/api/v1/users", `{"username":"alice","password":"a long password","role":"admin"}`, "invalid_json", http.StatusBadRequest},
		{"status mass assignment", http.MethodPatch, "/api/v1/users/2/status", `{"status":"active","role":"admin"}`, "invalid_json", http.StatusBadRequest},
		{"role wrong type", http.MethodPatch, "/api/v1/users/2/role", `{"role":42}`, "invalid_json", http.StatusBadRequest},
		{"null patch", http.MethodPatch, "/api/v1/users/2/status", `null`, "invalid_json", http.StatusBadRequest},
		{"multiple patches", http.MethodPatch, "/api/v1/users/2/role", `{} {}`, "invalid_json", http.StatusBadRequest},
		{"large patch", http.MethodPatch, "/api/v1/users/2/role", `{"role":"` + strings.Repeat("x", maxBodyBytes) + `"}`, "request_too_large", http.StatusRequestEntityTooLarge},
		{"zero ID", http.MethodPatch, "/api/v1/users/0/status", `{}`, "validation_failed", http.StatusBadRequest},
		{"negative ID", http.MethodPatch, "/api/v1/users/-1/role", `{}`, "validation_failed", http.StatusBadRequest},
		{"overflow ID", http.MethodPatch, "/api/v1/users/9223372036854775808/role", `{}`, "validation_failed", http.StatusBadRequest},
		{"nonnumeric ID", http.MethodPatch, "/api/v1/users/abc/status", `{}`, "validation_failed", http.StatusBadRequest},
		{"empty limit", http.MethodGet, "/api/v1/users?limit=", "", "validation_failed", http.StatusBadRequest},
		{"overflow limit", http.MethodGet, "/api/v1/users?limit=2147483648", "", "validation_failed", http.StatusBadRequest},
		{"nonnumeric cursor", http.MethodGet, "/api/v1/users?after_id=abc", "", "validation_failed", http.StatusBadRequest},
		{"overflow cursor", http.MethodGet, "/api/v1/users?after_id=9223372036854775808", "", "validation_failed", http.StatusBadRequest},
		{"duplicate limit", http.MethodGet, "/api/v1/users?limit=1&limit=2", "", "validation_failed", http.StatusBadRequest},
		{"unknown query", http.MethodGet, "/api/v1/users?role=admin", "", "validation_failed", http.StatusBadRequest},
		{"invalid query encoding", http.MethodGet, "/api/v1/users?limit=%zz", "", "validation_failed", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := perform(newUserTestHandler(fakeUsers{}), tc.method, tc.path, tc.body, map[string]string{"Content-Type": "application/json"})
			assertError(t, w, tc.status, tc.code)
		})
	}
	w := perform(newUserTestHandler(fakeUsers{}), http.MethodPatch, "/api/v1/users/2/role", `{"role":"admin"}`, nil)
	assertError(t, w, http.StatusUnsupportedMediaType, "unsupported_media_type")
}

func TestUserHandlersForwardAuthenticatedActor(t *testing.T) {
	calls := 0
	user := auth.User{
		ID:       9007199254740993,
		Username: "alice",
		Role:     "member",
		Status:   "active",
	}
	handler := newUserTestHandler(fakeUsers{
		create: func(actor int64, credentials auth.Credentials) (auth.User, error) {
			calls++
			if actor != 7 {
				t.Fatalf("creation actor = %d, want 7", actor)
			}
			if credentials.Username != "alice" {
				t.Fatalf("creation username = %q, want alice", credentials.Username)
			}
			if credentials.Password != "a long password" {
				t.Fatal("creation password did not match the request")
			}
			return user, nil
		},
		list: func(actor int64, options auth.ListUsersOptions) (auth.UserPage, error) {
			calls++
			if actor != 7 {
				t.Fatalf("list actor = %d, want 7", actor)
			}
			if options.AfterID != user.ID {
				t.Fatalf("list cursor = %d, want %d", options.AfterID, user.ID)
			}
			if options.Limit != 25 {
				t.Fatalf("list limit = %d, want 25", options.Limit)
			}
			return auth.UserPage{Users: []auth.User{user}}, nil
		},
		status: func(actor, id int64, status string) (auth.User, error) {
			calls++
			if actor != 7 {
				t.Fatalf("status actor = %d, want 7", actor)
			}
			if id != user.ID {
				t.Fatalf("status target = %d, want %d", id, user.ID)
			}
			if status != "disabled" {
				t.Fatalf("requested status = %q, want disabled", status)
			}
			return user, nil
		},
		role: func(actor, id int64, role string) (auth.User, error) {
			calls++
			if actor != 7 {
				t.Fatalf("role actor = %d, want 7", actor)
			}
			if id != user.ID {
				t.Fatalf("role target = %d, want %d", id, user.ID)
			}
			if role != "admin" {
				t.Fatalf("requested role = %q, want admin", role)
			}
			return user, nil
		},
	})
	for _, tc := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/api/v1/users", `{"username":"alice","password":"a long password"}`, http.StatusCreated},
		{http.MethodGet, "/api/v1/users?after_id=9007199254740993&limit=25", "", http.StatusOK},
		{http.MethodPatch, "/api/v1/users/9007199254740993/status", `{"status":"disabled"}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/users/9007199254740993/role", `{"role":"admin"}`, http.StatusOK},
	} {
		w := perform(handler, tc.method, tc.path, tc.body, map[string]string{"Content-Type": "application/json"})
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"id":"9007199254740993"`) {
			t.Fatalf("missing string-encoded user ID: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "password") {
			t.Fatal("public user response exposed a password field")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store on user response")
		}
	}
	if calls != 4 {
		t.Fatalf("service calls = %d, want 4", calls)
	}
}

func TestUserListDefaults(t *testing.T) {
	h := newUserTestHandler(fakeUsers{list: func(_ int64, options auth.ListUsersOptions) (auth.UserPage, error) {
		if options != (auth.ListUsersOptions{Limit: 50}) {
			t.Fatalf("default page = %+v", options)
		}
		return auth.UserPage{Users: []auth.User{}}, nil
	}})
	w := perform(h, http.MethodGet, "/api/v1/users", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != `{"users":[]}` {
		t.Fatalf("empty list body = %s, want {\"users\":[]}", w.Body.String())
	}
}

func TestUserManagementErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{authz.ErrForbidden, http.StatusForbidden, "forbidden"},
		{auth.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
		{auth.ErrUserNotFound, http.StatusNotFound, "not_found"},
		{auth.ErrLastAdmin, http.StatusConflict, "last_admin"},
		{errors.New("private database failure"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			h := newUserTestHandler(fakeUsers{status: func(int64, int64, string) (auth.User, error) { return auth.User{}, tc.err }})
			w := perform(h, http.MethodPatch, "/api/v1/users/1/status", `{"status":"disabled"}`, map[string]string{"Content-Type": "application/json"})
			assertError(t, w, tc.status, tc.code)
			if strings.Contains(w.Body.String(), "private") {
				t.Fatal("leaked internal error")
			}
		})
	}
}

func TestUserMutationCrossOriginProtection(t *testing.T) {
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/users"},
		{http.MethodPatch, "/api/v1/users/1/status"},
		{http.MethodPatch, "/api/v1/users/1/role"},
	} {
		w := perform(newUserTestHandler(fakeUsers{}), tc.method, tc.path, `{}`, map[string]string{"Origin": "https://evil.example"})
		assertError(t, w, http.StatusForbidden, "csrf_failed")
	}
}

func TestUserCreationSharesHashRateLimit(t *testing.T) {
	h := NewHandler(Config{
		Auth: fakeAuth{
			authenticate: func(string) (auth.User, error) {
				return auth.User{ID: 7, Role: "admin", Status: "active"}, nil
			},
			login: func(auth.Credentials) (auth.Session, error) {
				return auth.Session{}, auth.ErrInvalidCredentials
			},
		},
		Users: fakeUsers{create: func(int64, auth.Credentials) (auth.User, error) {
			return auth.User{}, auth.ErrUsernameTaken
		}},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	headers := map[string]string{"Content-Type": "application/json"}
	for range 5 {
		assertError(t, perform(h, http.MethodPost, "/api/v1/auth/login", `{}`, headers), http.StatusUnauthorized, "invalid_credentials")
		assertError(t, perform(h, http.MethodPost, "/api/v1/users", `{}`, headers), http.StatusConflict, "username_taken")
	}
	w := perform(h, http.MethodPost, "/api/v1/users", `{}`, headers)
	assertError(t, w, http.StatusTooManyRequests, "rate_limited")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("rate limit response is missing Retry-After")
	}
}
