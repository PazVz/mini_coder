package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PazVz/mini_coder/internal/auth"
)

type fakeAuth struct {
	setupStatus  func() (bool, error)
	setup        func(string, auth.Credentials) (auth.User, error)
	login        func(auth.Credentials) (auth.Session, error)
	authenticate func(string) (auth.User, error)
	logout       func(string) error
}

func (f fakeAuth) NeedsSetup(context.Context) (bool, error) { return f.setupStatus() }
func (f fakeAuth) Setup(_ context.Context, t string, c auth.Credentials) (auth.User, error) {
	return f.setup(t, c)
}
func (f fakeAuth) Login(_ context.Context, c auth.Credentials) (auth.Session, error) {
	return f.login(c)
}
func (f fakeAuth) Authenticate(_ context.Context, t string) (auth.User, error) {
	return f.authenticate(t)
}
func (f fakeAuth) Logout(_ context.Context, t string) error { return f.logout(t) }

func newTestHandler(f fakeAuth) http.Handler {
	return NewHandler(Config{Auth: f, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CookieSecure: true})
}
func perform(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func assertError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) ErrorResponse {
	t.Helper()
	var response ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid error JSON: %s (%v)", w.Body.String(), err)
	}
	if w.Code != status || response.Code != code || response.Message == "" || response.RequestID == "" || response.RequestID != w.Header().Get("X-Request-ID") || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response: %d %+v headers=%v", w.Code, response, w.Header())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("error response may be cached")
	}
	return response
}

func TestUnifiedRoutingErrors(t *testing.T) {
	h := newTestHandler(fakeAuth{})
	assertError(t, perform(h, "GET", "/missing", "", nil), 404, "not_found")
	for _, tc := range []struct{ path, method, allow string }{
		{"/healthz", "POST", "GET, HEAD"}, {"/api/v1/auth/login", "GET", "POST"},
		{"/api/v1/auth/setup", "PUT", "GET, POST"}, {"/api/v1/auth/me", "POST", "GET"},
	} {
		w := perform(h, tc.method, tc.path, "", nil)
		assertError(t, w, 405, "method_not_allowed")
		if strings.Join(w.Header().Values("Allow"), ", ") != tc.allow {
			t.Fatalf("Allow = %v for %s", w.Header().Values("Allow"), tc.path)
		}
	}
}

func TestCredentialJSONBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, code string
		status                        int
	}{
		{"missing type", `{}`, "", "unsupported_media_type", 415},
		{"form", `username=admin`, "application/x-www-form-urlencoded", "unsupported_media_type", 415},
		{"syntax", `{`, "application/json", "invalid_json", 400},
		{"unknown", `{"role":"admin"}`, "application/json", "invalid_json", 400},
		{"multiple", `{} {}`, "application/json", "invalid_json", 400},
		{"null", `null`, "application/json", "invalid_json", 400},
		{"array", `[]`, "application/json", "invalid_json", 400},
		{"oversize", `{"password":"` + strings.Repeat("x", maxBodyBytes) + `"}`, "application/json", "request_too_large", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := perform(newTestHandler(fakeAuth{}), "POST", "/api/v1/auth/login", tc.body, map[string]string{"Content-Type": tc.contentType})
			assertError(t, w, tc.status, tc.code)
		})
	}
}

func TestAuthErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrInvalidCredentials, 401, "invalid_credentials"}, {auth.ErrUnauthenticated, 401, "unauthenticated"},
		{auth.ErrAlreadyInitialized, 409, "already_initialized"}, {auth.ErrUsernameTaken, 409, "username_taken"},
		{auth.ErrSetupDisabled, 403, "setup_disabled"}, {auth.ErrSetupForbidden, 403, "forbidden"},
		{errors.New("database password=secret"), 500, "internal_error"},
		{&auth.ValidationError{Fields: []auth.FieldError{{Field: "password", Code: "invalid_length", Detail: "密码长度不合法"}}}, 400, "validation_failed"},
	} {
		h := newTestHandler(fakeAuth{login: func(auth.Credentials) (auth.Session, error) { return auth.Session{}, tc.err }})
		w := perform(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"private password"}`, map[string]string{"Content-Type": "application/json"})
		response := assertError(t, w, tc.status, tc.code)
		if tc.code == "validation_failed" && (len(response.Validations) != 1 || response.Validations[0].Field != "password") {
			t.Fatal("missing field validation")
		}
		if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "private password") {
			t.Fatal("sensitive data in response")
		}
	}
}

func TestCookieLifecycleAndAuthenticatedUser(t *testing.T) {
	u := auth.User{ID: 9007199254740993, Username: "admin", Status: "active", Role: "admin"}
	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	h := newTestHandler(fakeAuth{
		login: func(c auth.Credentials) (auth.Session, error) {
			if c.Username != "admin" {
				t.Fatal("credentials not decoded")
			}
			return auth.Session{User: u, Token: "opaque-token", ExpiresAt: expires}, nil
		},
		authenticate: func(token string) (auth.User, error) {
			if token != "opaque-token" {
				return auth.User{}, auth.ErrUnauthenticated
			}
			return u, nil
		},
		logout: func(token string) error {
			if token != "opaque-token" {
				t.Fatal("missing logout token")
			}
			return nil
		},
	})
	w := perform(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"a long password"}`, map[string]string{"Content-Type": "application/json; charset=utf-8"})
	if w.Code != 200 || strings.Contains(w.Body.String(), "opaque-token") || !strings.Contains(w.Body.String(), `"id":"9007199254740993"`) {
		t.Fatalf("login: %s", w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	c := cookies[0]
	if c.Name != sessionCookieName || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != 86400 || !c.Expires.Equal(expires) {
		t.Fatalf("cookie: %+v", c)
	}
	assertError(t, perform(h, "GET", "/api/v1/auth/me", "", nil), 401, "unauthenticated")
	headers := map[string]string{"Cookie": c.Name + "=" + c.Value}
	w = perform(h, "GET", "/api/v1/auth/me", "", headers)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"admin"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("me: %d %s", w.Code, w.Body.String())
	}
	w = perform(h, "POST", "/api/v1/auth/logout", "", headers)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	c = w.Result().Cookies()[0]
	if c.MaxAge != -1 || c.Value != "" || !c.HttpOnly || !c.Secure || c.Path != "/" {
		t.Fatalf("cleared cookie: %+v", c)
	}
}

func TestSetupHandlers(t *testing.T) {
	h := newTestHandler(fakeAuth{
		setupStatus: func() (bool, error) { return true, nil },
		setup: func(token string, c auth.Credentials) (auth.User, error) {
			if token != "setup-secret" || c.Username != "admin" {
				t.Fatal("missing setup parameters")
			}
			return auth.User{ID: 1, Username: c.Username, Role: "admin"}, nil
		},
	})
	if w := perform(h, "GET", "/api/v1/auth/setup", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"needs_setup":true`) {
		t.Fatalf("setup status: %s", w.Body.String())
	}
	w := perform(h, "POST", "/api/v1/auth/setup", `{"username":"admin","password":"a long password"}`, map[string]string{"Content-Type": "application/json", "X-Setup-Token": "setup-secret"})
	if w.Code != 201 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
}

func TestCrossOriginProtection(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/setup", "/api/v1/auth/logout"} {
		for _, headers := range []map[string]string{
			{"Origin": "https://evil.example"}, {"Sec-Fetch-Site": "cross-site"}, {"Sec-Fetch-Site": "same-site"}, {"Origin": "null"},
		} {
			assertError(t, perform(newTestHandler(fakeAuth{}), "POST", path, `{}`, headers), 403, "csrf_failed")
		}
	}
	h := newTestHandler(fakeAuth{logout: func(string) error { return nil }})
	for _, headers := range []map[string]string{nil, {"Origin": "http://example.com"}, {"Sec-Fetch-Site": "same-origin"}} {
		if w := perform(h, "POST", "/api/v1/auth/logout", "", headers); w.Code != 204 {
			t.Fatalf("same-origin/non-browser blocked: %s", w.Body.String())
		}
	}
	if w := perform(h, "GET", "/healthz", "", map[string]string{"Origin": "https://evil.example"}); w.Code != 200 {
		t.Fatal("safe method rejected")
	}
}

func TestInternalErrorLogsAndRecovery(t *testing.T) {
	for _, panicRequest := range []bool{false, true} {
		var logs bytes.Buffer
		h := NewHandler(Config{Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Auth: fakeAuth{setupStatus: func() (bool, error) {
			if panicRequest {
				panic("private panic")
			}
			return false, errors.New("private failure")
		}}})
		w := perform(h, "GET", "/api/v1/auth/setup", "", map[string]string{"X-Request-ID": "untrusted"})
		response := assertError(t, w, 500, "internal_error")
		if response.RequestID == "untrusted" || strings.Contains(w.Body.String(), "private") || !strings.Contains(logs.String(), response.RequestID) || !strings.Contains(logs.String(), "private") {
			t.Fatalf("unexpected logs/body: %s %s", logs.String(), w.Body.String())
		}
	}
}

func TestLogoutFailurePreservesCookie(t *testing.T) {
	h := newTestHandler(fakeAuth{logout: func(string) error { return errors.New("database unavailable") }})
	w := perform(h, "POST", "/api/v1/auth/logout", "", map[string]string{"Cookie": sessionCookieName + "=session-token"})
	assertError(t, w, 500, "internal_error")
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("cleared cookie without revoking the session")
	}
}
