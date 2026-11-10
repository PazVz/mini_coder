package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/PazVz/mini_coder/internal/auth"
)

func TestPasswordChangeHandler(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"success", nil, http.StatusNoContent, ""},
		{"incorrect current password", auth.ErrInvalidCurrentPassword, http.StatusForbidden, "invalid_current_password"},
		{"passwordless account", auth.ErrPasswordNotSet, http.StatusConflict, "password_not_set"},
		{"revoked session", auth.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(fakeAuth{
				authenticate: func(string) (auth.User, error) { return auth.User{ID: 7}, nil },
				changePassword: func(userID int64, token string, change auth.PasswordChange) error {
					if userID != 7 || token != "session-token" {
						t.Fatal("password change did not receive authenticated identity and session")
					}
					if change.CurrentPassword != "old private password" || change.NewPassword != "new private password" {
						t.Fatal("password change did not receive request fields")
					}
					return tc.err
				},
			})
			w := perform(h, http.MethodPost, "/api/v1/auth/password", `{"current_password":"old private password","new_password":"new private password"}`, map[string]string{
				"Content-Type": "application/json", "Cookie": sessionCookieName + "=session-token",
			})
			if tc.err != nil {
				assertError(t, w, tc.status, tc.code)
				if len(w.Result().Cookies()) != 0 {
					t.Fatal("failed password change modified the browser cookie")
				}
			} else {
				if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
					t.Fatalf("successful change = %d %s", w.Code, w.Body.String())
				}
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || cookies[0].Value != "" || cookies[0].MaxAge != -1 {
					t.Fatalf("session cookie was not cleared: %+v", cookies)
				}
				if !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
					t.Fatalf("cleared cookie lost security attributes: %+v", cookies[0])
				}
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store on password response")
			}
			if strings.Contains(w.Body.String(), "private password") || strings.Contains(w.Body.String(), "session-token") {
				t.Fatal("password response exposed a credential")
			}
		})
	}
}

func TestPasswordChangeRequestBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"cannot target another user", `{"user_id":"8","current_password":"old password","new_password":"new password"}`},
		{"unknown field", `{"password":"new password"}`},
		{"null body", `null`},
		{"array", `[]`},
		{"multiple bodies", `{} {}`},
		{"non-string password", `{"new_password":123}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(fakeAuth{authenticate: func(string) (auth.User, error) { return auth.User{ID: 7}, nil }})
			assertError(t, perform(h, http.MethodPost, "/api/v1/auth/password", tc.body, map[string]string{"Content-Type": "application/json"}), http.StatusBadRequest, "invalid_json")
		})
	}
	h := newTestHandler(fakeAuth{authenticate: func(string) (auth.User, error) { return auth.User{}, auth.ErrUnauthenticated }})
	assertError(t, perform(h, http.MethodPost, "/api/v1/auth/password", `{}`, map[string]string{"Content-Type": "application/json"}), http.StatusUnauthorized, "unauthenticated")
	h = newTestHandler(fakeAuth{authenticate: func(string) (auth.User, error) { return auth.User{ID: 7}, nil }})
	assertError(t, perform(h, http.MethodPost, "/api/v1/auth/password", `{}`, nil), http.StatusUnsupportedMediaType, "unsupported_media_type")
	assertError(t, perform(h, http.MethodPost, "/api/v1/auth/password", `{"new_password":"`+strings.Repeat("x", maxBodyBytes)+`"}`, map[string]string{"Content-Type": "application/json"}), http.StatusRequestEntityTooLarge, "request_too_large")
}

func TestPasswordChangeSharesHashRateLimit(t *testing.T) {
	h := newTestHandler(fakeAuth{
		authenticate:   func(string) (auth.User, error) { return auth.User{ID: 7}, nil },
		login:          func(auth.Credentials) (auth.Session, error) { return auth.Session{}, auth.ErrInvalidCredentials },
		changePassword: func(int64, string, auth.PasswordChange) error { return auth.ErrInvalidCurrentPassword },
	})
	headers := map[string]string{"Content-Type": "application/json"}
	for range 5 {
		assertError(t, perform(h, http.MethodPost, "/api/v1/auth/login", `{}`, headers), http.StatusUnauthorized, "invalid_credentials")
		assertError(t, perform(h, http.MethodPost, "/api/v1/auth/password", `{}`, headers), http.StatusForbidden, "invalid_current_password")
	}
	w := perform(h, http.MethodPost, "/api/v1/auth/password", `{}`, headers)
	assertError(t, w, http.StatusTooManyRequests, "rate_limited")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("rate limit response is missing Retry-After")
	}
}
