package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/PazVz/mini_coder/internal/auth"
)

type fakeEmails struct {
	request      func(string) error
	register     func(auth.RegistrationInput) (auth.User, error)
	verification func(int64, string) error
	change       func(int64, string, auth.EmailChange) error
	confirm      func(int64, string, string) (auth.User, error)
}

func (f fakeEmails) RequestRegistration(_ context.Context, email string) error {
	return f.request(email)
}
func (f fakeEmails) Register(_ context.Context, input auth.RegistrationInput) (auth.User, error) {
	return f.register(input)
}
func (f fakeEmails) RequestEmailVerification(_ context.Context, id int64, session string) error {
	return f.verification(id, session)
}
func (f fakeEmails) RequestEmailChange(_ context.Context, id int64, session string, input auth.EmailChange) error {
	return f.change(id, session, input)
}
func (f fakeEmails) ConfirmEmail(_ context.Context, id int64, session, token string) (auth.User, error) {
	return f.confirm(id, session, token)
}

func emailHandler(emails EmailManager) http.Handler {
	return NewHandler(Config{
		Auth: fakeAuth{authenticate: func(token string) (auth.User, error) {
			if token != "session" {
				return auth.User{}, auth.ErrUnauthenticated
			}
			return auth.User{ID: 7}, nil
		}},
		Emails: emails,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestEmailHandlers(t *testing.T) {
	calls := 0
	assertActor := func(id int64, session string) {
		t.Helper()
		if id != 7 || session != "session" {
			t.Fatal("email operation lost the authenticated actor or session")
		}
	}
	h := emailHandler(fakeEmails{
		request: func(email string) error {
			calls++
			if email != "user@example.com" {
				t.Fatal("registration email not decoded")
			}
			return nil
		},
		register: func(input auth.RegistrationInput) (auth.User, error) {
			calls++
			if input.Token != "private-token" || input.Username != "member" || input.Password != "private password" {
				t.Fatal("registration input not decoded")
			}
			return auth.User{
				ID:            8,
				Email:         "user@example.com",
				EmailVerified: true,
				Role:          "member",
			}, nil
		},
		verification: func(id int64, session string) error {
			calls++
			assertActor(id, session)
			return nil
		},
		change: func(id int64, session string, input auth.EmailChange) error {
			calls++
			assertActor(id, session)
			if input.Email != "new@example.com" || input.CurrentPassword != "private password" {
				t.Fatal("email change input not decoded")
			}
			return nil
		},
		confirm: func(id int64, session, token string) (auth.User, error) {
			calls++
			assertActor(id, session)
			if token != "private-token" {
				t.Fatal("confirmation token not decoded")
			}
			return auth.User{
				ID:            id,
				Email:         "new@example.com",
				EmailVerified: true,
			}, nil
		},
	})
	for _, tc := range []struct {
		path   string
		body   string
		status int
	}{
		{"/api/v1/auth/register/request", `{"email":"user@example.com"}`, http.StatusAccepted},
		{"/api/v1/auth/register", `{"token":"private-token","username":"member","password":"private password"}`, http.StatusCreated},
		{"/api/v1/auth/email/verification", `{}`, http.StatusAccepted},
		{"/api/v1/auth/email/change", `{"email":"new@example.com","current_password":"private password"}`, http.StatusAccepted},
		{"/api/v1/auth/email/confirm", `{"token":"private-token"}`, http.StatusOK},
	} {
		w := perform(h, http.MethodPost, tc.path, tc.body, map[string]string{"Content-Type": "application/json", "Cookie": sessionCookieName + "=session"})
		if w.Code != tc.status {
			t.Fatalf("%s = %d %s", tc.path, w.Code, w.Body.String())
		}
		if len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "private password") {
			t.Fatal("email response leaked a proof or unexpectedly changed authentication")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("email response is cacheable")
		}
	}
	if calls != 5 {
		t.Fatalf("email service calls = %d, want 5", calls)
	}
}

func TestEmailRequestBoundaries(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/register/request", "/api/v1/auth/register", "/api/v1/auth/email/verification", "/api/v1/auth/email/change", "/api/v1/auth/email/confirm"} {
		t.Run(path, func(t *testing.T) {
			h := emailHandler(fakeEmails{})
			assertError(t, perform(h, http.MethodPost, path, `{}`, map[string]string{"Origin": "https://evil.example"}), http.StatusForbidden, "csrf_failed")
			for _, body := range []string{`null`, `{} {}`, `{"user_id":"8"}`, `{"role":"admin","email_verified":true}`} {
				assertError(t, perform(h, http.MethodPost, path, body, map[string]string{"Content-Type": "application/json", "Cookie": sessionCookieName + "=session"}), http.StatusBadRequest, "invalid_json")
			}
			if strings.Contains(path, "/email/") {
				assertError(t, perform(h, http.MethodPost, path, `{}`, map[string]string{"Content-Type": "application/json"}), http.StatusUnauthorized, "unauthenticated")
			}
		})
	}
	assertError(t, perform(emailHandler(nil), http.MethodPost, "/api/v1/auth/register/request", `{}`, nil), http.StatusServiceUnavailable, "email_unavailable")
}

func TestEmailErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrEmailUnavailable, http.StatusServiceUnavailable, "email_unavailable"},
		{auth.ErrEmailDelivery, http.StatusServiceUnavailable, "email_delivery_failed"},
		{auth.ErrRegistrationDisabled, http.StatusForbidden, "registration_disabled"},
		{auth.ErrInvalidEmailToken, http.StatusBadRequest, "invalid_email_token"},
		{auth.ErrEmailAlreadyVerified, http.StatusConflict, "email_already_verified"},
		{auth.ErrEmailMissing, http.StatusConflict, "email_missing"},
		{&auth.EmailRateLimitError{RetryAfter: 3500}, http.StatusTooManyRequests, "rate_limited"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			h := emailHandler(fakeEmails{request: func(string) error { return tc.err }})
			w := perform(h, http.MethodPost, "/api/v1/auth/register/request", `{"email":"a@example.com"}`, map[string]string{"Content-Type": "application/json"})
			assertError(t, w, tc.status, tc.code)
			if tc.code == "rate_limited" && w.Header().Get("Retry-After") != "3500" {
				t.Fatal("email quota lost Retry-After")
			}
		})
	}
}

func TestEmailPagesDoNotConsumeProofs(t *testing.T) {
	for _, path := range []string{"/auth/register", "/auth/email/confirm"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := perform(emailHandler(nil), method, path+"?token=untrusted-secret", "", nil)
			if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "untrusted-secret") {
				t.Fatalf("confirmation page = %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("confirmation page missing security headers")
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatal("HEAD returned a page body")
			}
		}
	}
}
