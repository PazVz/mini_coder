// Package httpapi defines the application's HTTP routes.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/PazVz/mini_coder/internal/auth"
)

// Authenticator is the business boundary used by HTTP handlers.
type Authenticator interface {
	NeedsSetup(context.Context) (bool, error)
	Setup(context.Context, string, auth.CreateUserInput) (auth.User, error)
	Login(context.Context, auth.Credentials) (auth.Session, error)
	Authenticate(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
	ChangePassword(context.Context, int64, string, auth.PasswordChange) error
}

// UserManager is the business boundary for administrator-managed accounts.
type UserManager interface {
	CreateUser(context.Context, int64, auth.CreateUserInput) (auth.User, error)
	ListUsers(context.Context, int64, auth.ListUsersOptions) (auth.UserPage, error)
	UpdateUserStatus(context.Context, int64, int64, string) (auth.User, error)
	UpdateUserRole(context.Context, int64, int64, string) (auth.User, error)
}

// EmailManager owns mailbox proofs, primary address changes and public registration.
type EmailManager interface {
	RequestRegistration(context.Context, string) error
	Register(context.Context, auth.RegistrationInput) (auth.User, error)
	RequestEmailVerification(context.Context, int64, string) error
	RequestEmailChange(context.Context, int64, string, auth.EmailChange) error
	ConfirmEmail(context.Context, int64, string, string) (auth.User, error)
}

// Config supplies the account services, request logger and cookie security policy.
type Config struct {
	Auth         Authenticator
	Users        UserManager
	Emails       EmailManager
	Logger       *slog.Logger
	CookieSecure bool
}

type api struct {
	auth         Authenticator
	users        UserManager
	emails       EmailManager
	logger       *slog.Logger
	cookieSecure bool
}

// NewHandler creates a single Chi router for health checks and API endpoints.
func NewHandler(config Config) http.Handler {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	a := &api{
		auth:         config.Auth,
		users:        config.Users,
		emails:       config.Emails,
		logger:       logger,
		cookieSecure: config.CookieSecure,
	}
	router := chi.NewRouter()
	router.Use(a.observe)
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusForbidden, "csrf_failed", "不允许跨源修改请求")
	}))
	router.Use(protection.Handler)
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "路由或资源不存在")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		// Chi's custom handler replaces its default Allow header generation.
		for _, method := range []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
			http.MethodConnect,
			http.MethodTrace,
		} {
			if router.Match(chi.NewRouteContext(), method, r.URL.Path) {
				w.Header().Add("Allow", method)
			}
		}
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不支持")
	})
	router.Get("/healthz", healthz)
	router.Head("/healthz", healthz)
	router.Get("/auth/register", registrationPage)
	router.Head("/auth/register", registrationPage)
	router.Get("/auth/email/confirm", emailConfirmationPage)
	router.Head("/auth/email/confirm", emailConfirmationPage)
	router.Get("/auth/email.js", emailScript)
	router.Get("/auth/email.css", emailStyle)
	limit := newAttemptLimiter(time.Now)
	router.Route("/api/v1/auth", func(router chi.Router) {
		router.Use(noStore)
		router.Get("/setup", a.setupStatus)
		router.With(limit.middleware).Post("/setup", a.setup)
		router.With(limit.middleware).Post("/login", a.login)
		router.With(a.requireUser).Get("/me", a.me)
		router.With(a.requireUser, limit.middleware).Post("/password", a.changePassword)
		router.Post("/logout", a.logout)
		router.With(a.requireEmails, limit.middleware).Post("/register/request", a.requestRegistration)
		router.With(a.requireEmails, limit.middleware).Post("/register", a.register)
		router.With(a.requireUser, a.requireEmails, limit.middleware).Post("/email/verification", a.requestEmailVerification)
		router.With(a.requireUser, a.requireEmails, limit.middleware).Post("/email/change", a.requestEmailChange)
		router.With(a.requireUser, a.requireEmails, limit.middleware).Post("/email/confirm", a.confirmEmail)
	})
	router.Route("/api/v1/users", func(router chi.Router) {
		router.Use(noStore, a.requireUser)
		router.With(limit.middleware).Post("/", a.createUser)
		router.Get("/", a.listUsers)
		router.Patch("/{id}/status", a.updateUserStatus)
		router.Patch("/{id}/role", a.updateUserRole)
	})
	return router
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodHead {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	} else {
		w.WriteHeader(http.StatusOK)
	}
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
