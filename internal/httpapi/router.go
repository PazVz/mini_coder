// Package httpapi defines the application's HTTP routes.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Authenticator is the business boundary used by HTTP handlers.
type Authenticator interface {
	NeedsSetup(context.Context) (bool, error)
	Setup(context.Context, string, auth.Credentials) (auth.User, error)
	Login(context.Context, auth.Credentials) (auth.Session, error)
	Authenticate(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
}

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
	a := &api{config.Auth, logger, config.CookieSecure}
	r := chi.NewRouter()
	r.Use(a.observe)
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, 403, "csrf_failed", "不允许跨源修改请求")
	}))
	r.Use(protection.Handler)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, 404, "not_found", "路由或资源不存在")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		// Chi's custom handler replaces its default Allow header generation.
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE"} {
			if r.Match(chi.NewRouteContext(), method, req.URL.Path) {
				w.Header().Add("Allow", method)
			}
		}
		writeError(w, req, 405, "method_not_allowed", "请求方法不支持")
	})
	r.Get("/healthz", healthz)
	r.Head("/healthz", healthz)
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Use(noStore)
		limit := newAttemptLimiter(time.Now)
		r.Get("/setup", a.setupStatus)
		r.With(limit.middleware).Post("/setup", a.setup)
		r.With(limit.middleware).Post("/login", a.login)
		r.With(a.requireUser).Get("/me", a.me)
		r.Post("/logout", a.logout)
	})
	return r
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodHead {
		writeJSON(w, 200, map[string]string{"status": "ok"})
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
