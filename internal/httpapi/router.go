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
	Setup(context.Context, string, auth.Credentials) (auth.User, error)
	Login(context.Context, auth.Credentials) (auth.Session, error)
	Authenticate(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
}

// Config supplies the authentication service, request logger and cookie security policy.
type Config struct {
	Auth         Authenticator
	Logger       *slog.Logger
	CookieSecure bool
}

type api struct {
	auth         Authenticator
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
	router.Route("/api/v1/auth", func(router chi.Router) {
		router.Use(noStore)
		limit := newAttemptLimiter(time.Now)
		router.Get("/setup", a.setupStatus)
		router.With(limit.middleware).Post("/setup", a.setup)
		router.With(limit.middleware).Post("/login", a.login)
		router.With(a.requireUser).Get("/me", a.me)
		router.Post("/logout", a.logout)
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
