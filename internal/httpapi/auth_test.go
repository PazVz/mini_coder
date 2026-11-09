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

func (f fakeAuth) Setup(_ context.Context, token string, credentials auth.Credentials) (auth.User, error) {
	return f.setup(token, credentials)
}

func (f fakeAuth) Login(_ context.Context, credentials auth.Credentials) (auth.Session, error) {
	return f.login(credentials)
}

func (f fakeAuth) Authenticate(_ context.Context, token string) (auth.User, error) {
	return f.authenticate(token)
}

func (f fakeAuth) Logout(_ context.Context, token string) error { return f.logout(token) }

func newTestHandler(f fakeAuth) http.Handler {
	return NewHandler(Config{
		Auth:         f,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		CookieSecure: true,
	})
}

func perform(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for name, value := range headers {
		r.Header.Set(name, value)
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
	if w.Code != status {
		t.Fatalf("HTTP status = %d, want %d; body=%s", w.Code, status, w.Body.String())
	}
	if response.Code != code {
		t.Fatalf("error code = %q, want %q", response.Code, code)
	}
	if response.Message == "" {
		t.Fatal("error message is empty")
	}
	if response.RequestID == "" {
		t.Fatal("error response is missing request_id")
	}
	if got := w.Header().Get("X-Request-ID"); got != response.RequestID {
		t.Fatalf("X-Request-ID = %q, want %q", got, response.RequestID)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	return response
}

func TestUnifiedRoutingErrors(t *testing.T) {
	h := newTestHandler(fakeAuth{})
	assertError(t, perform(h, http.MethodGet, "/missing", "", nil), http.StatusNotFound, "not_found")
	for _, tc := range []struct {
		path   string
		method string
		allow  string
	}{
		{"/healthz", http.MethodPost, "GET, HEAD"},
		{"/api/v1/auth/login", http.MethodGet, http.MethodPost},
		{"/api/v1/auth/setup", http.MethodPut, "GET, POST"},
		{"/api/v1/auth/me", http.MethodPost, http.MethodGet},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := perform(h, tc.method, tc.path, "", nil)
			assertError(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
			if got := strings.Join(w.Header().Values("Allow"), ", "); got != tc.allow {
				t.Fatalf("Allow = %q, want %q", got, tc.allow)
			}
		})
	}
}

func TestCredentialJSONBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        string
		contentType string
		code        string
		status      int
	}{
		{"missing type", `{}`, "", "unsupported_media_type", http.StatusUnsupportedMediaType},
		{"form", `username=admin`, "application/x-www-form-urlencoded", "unsupported_media_type", http.StatusUnsupportedMediaType},
		{"syntax", `{`, "application/json", "invalid_json", http.StatusBadRequest},
		{"unknown", `{"role":"admin"}`, "application/json", "invalid_json", http.StatusBadRequest},
		{"multiple", `{} {}`, "application/json", "invalid_json", http.StatusBadRequest},
		{"null", `null`, "application/json", "invalid_json", http.StatusBadRequest},
		{"array", `[]`, "application/json", "invalid_json", http.StatusBadRequest},
		{"oversize", `{"password":"` + strings.Repeat("x", maxBodyBytes) + `"}`, "application/json", "request_too_large", http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := perform(newTestHandler(fakeAuth{}), http.MethodPost, "/api/v1/auth/login", tc.body, map[string]string{"Content-Type": tc.contentType})
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
		{auth.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{auth.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
		{auth.ErrAlreadyInitialized, http.StatusConflict, "already_initialized"},
		{auth.ErrUsernameTaken, http.StatusConflict, "username_taken"},
		{auth.ErrSetupDisabled, http.StatusForbidden, "setup_disabled"},
		{auth.ErrSetupForbidden, http.StatusForbidden, "forbidden"},
		{errors.New("database password=secret"), http.StatusInternalServerError, "internal_error"},
		{
			err: &auth.ValidationError{Fields: []auth.FieldError{
				{Field: "password", Code: "invalid_length", Detail: "密码长度不合法"},
			}},
			status: http.StatusBadRequest,
			code:   "validation_failed",
		},
	} {
		t.Run(tc.code, func(t *testing.T) {
			h := newTestHandler(fakeAuth{
				login: func(auth.Credentials) (auth.Session, error) { return auth.Session{}, tc.err },
			})
			w := perform(h, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"private password"}`, map[string]string{"Content-Type": "application/json"})
			response := assertError(t, w, tc.status, tc.code)
			if tc.code == "validation_failed" {
				if len(response.Validations) != 1 {
					t.Fatalf("validations = %+v, want one field error", response.Validations)
				}
				if got := response.Validations[0].Field; got != "password" {
					t.Fatalf("validation field = %q, want password", got)
				}
			}
			if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "private password") {
				t.Fatal("sensitive data in response")
			}
		})
	}
}

func TestCookieLifecycleAndAuthenticatedUser(t *testing.T) {
	user := auth.User{ID: 9007199254740993, Username: "admin", Status: "active", Role: "admin"}
	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	h := newTestHandler(fakeAuth{
		login: func(credentials auth.Credentials) (auth.Session, error) {
			if credentials.Username != "admin" {
				t.Fatal("credentials not decoded")
			}
			return auth.Session{User: user, Token: "opaque-token", ExpiresAt: expires}, nil
		},
		authenticate: func(token string) (auth.User, error) {
			if token != "opaque-token" {
				return auth.User{}, auth.ErrUnauthenticated
			}
			return user, nil
		},
		logout: func(token string) error {
			if token != "opaque-token" {
				t.Fatal("missing logout token")
			}
			return nil
		},
	})
	w := perform(h, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"a long password"}`, map[string]string{"Content-Type": "application/json; charset=utf-8"})
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "opaque-token") {
		t.Fatal("login response exposed the session token")
	}
	if !strings.Contains(w.Body.String(), `"id":"9007199254740993"`) {
		t.Fatalf("login did not encode the bigint ID as a string: %s", w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, sessionCookieName)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie security = %+v, want HttpOnly, Secure and SameSite=Lax", cookie)
	}
	if cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("cookie scope = %+v, want Path=/ and no Domain", cookie)
	}
	if cookie.MaxAge != 86400 || !cookie.Expires.Equal(expires) {
		t.Fatalf("cookie lifetime = %+v, want MaxAge=86400 and Expires=%s", cookie, expires)
	}
	assertError(t, perform(h, http.MethodGet, "/api/v1/auth/me", "", nil), http.StatusUnauthorized, "unauthenticated")
	headers := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}
	w = perform(h, http.MethodGet, "/api/v1/auth/me", "", headers)
	if w.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"role":"admin"`) {
		t.Fatalf("me response is missing the current role: %s", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("me Cache-Control = %q, want no-store", got)
	}
	w = perform(h, http.MethodPost, "/api/v1/auth/logout", "", headers)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	cookie = w.Result().Cookies()[0]
	if cookie.MaxAge != -1 || cookie.Value != "" {
		t.Fatalf("cleared cookie = %+v, want MaxAge=-1 and an empty value", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" {
		t.Fatalf("cleared cookie attributes = %+v, want HttpOnly, Secure and Path=/", cookie)
	}
}

func TestSetupHandlers(t *testing.T) {
	h := newTestHandler(fakeAuth{
		setupStatus: func() (bool, error) { return true, nil },
		setup: func(token string, credentials auth.Credentials) (auth.User, error) {
			if token != "setup-secret" || credentials.Username != "admin" {
				t.Fatal("missing setup parameters")
			}
			return auth.User{ID: 1, Username: credentials.Username, Role: "admin"}, nil
		},
	})
	if w := perform(h, http.MethodGet, "/api/v1/auth/setup", "", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"needs_setup":true`) {
		t.Fatalf("setup status: %s", w.Body.String())
	}
	w := perform(h, http.MethodPost, "/api/v1/auth/setup", `{"username":"admin","password":"a long password"}`, map[string]string{"Content-Type": "application/json", "X-Setup-Token": "setup-secret"})
	if w.Code != http.StatusCreated || len(w.Result().Cookies()) != 0 {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
}

func TestCrossOriginProtection(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/setup", "/api/v1/auth/logout"} {
		for _, tc := range []struct {
			name    string
			headers map[string]string
		}{
			{"foreign origin", map[string]string{"Origin": "https://evil.example"}},
			{"cross site", map[string]string{"Sec-Fetch-Site": "cross-site"}},
			{"same site", map[string]string{"Sec-Fetch-Site": "same-site"}},
			{"null origin", map[string]string{"Origin": "null"}},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				w := perform(newTestHandler(fakeAuth{}), http.MethodPost, path, `{}`, tc.headers)
				assertError(t, w, http.StatusForbidden, "csrf_failed")
			})
		}
	}
	h := newTestHandler(fakeAuth{logout: func(string) error { return nil }})
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"non browser", nil},
		{"matching origin", map[string]string{"Origin": "http://example.com"}},
		{"same origin", map[string]string{"Sec-Fetch-Site": "same-origin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := perform(h, http.MethodPost, "/api/v1/auth/logout", "", tc.headers)
			if w.Code != http.StatusNoContent {
				t.Fatalf("logout status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
			}
		})
	}
	if w := perform(h, http.MethodGet, "/healthz", "", map[string]string{"Origin": "https://evil.example"}); w.Code != http.StatusOK {
		t.Fatal("safe method rejected")
	}
}

func TestInternalErrorLogsAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		panicRequest bool
	}{
		{"returned error", false},
		{"recovered panic", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			h := NewHandler(Config{
				Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
				Auth: fakeAuth{
					setupStatus: func() (bool, error) {
						if tc.panicRequest {
							panic("private panic")
						}
						return false, errors.New("private failure")
					},
				},
			})
			w := perform(h, http.MethodGet, "/api/v1/auth/setup", "", map[string]string{"X-Request-ID": "untrusted"})
			response := assertError(t, w, http.StatusInternalServerError, "internal_error")
			if response.RequestID == "untrusted" {
				t.Fatal("server accepted the client-supplied request ID")
			}
			if strings.Contains(w.Body.String(), "private") {
				t.Fatalf("response exposed internal error details: %s", w.Body.String())
			}
			if !strings.Contains(logs.String(), response.RequestID) {
				t.Fatalf("logs are missing request ID %q: %s", response.RequestID, logs.String())
			}
			if !strings.Contains(logs.String(), "private") {
				t.Fatalf("logs are missing internal error details: %s", logs.String())
			}
		})
	}
}

func TestLogoutFailurePreservesCookie(t *testing.T) {
	h := newTestHandler(fakeAuth{logout: func(string) error { return errors.New("database unavailable") }})
	w := perform(h, http.MethodPost, "/api/v1/auth/logout", "", map[string]string{"Cookie": sessionCookieName + "=session-token"})
	assertError(t, w, http.StatusInternalServerError, "internal_error")
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("cleared cookie without revoking the session")
	}
}
