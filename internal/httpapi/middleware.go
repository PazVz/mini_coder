package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type requestIDKey struct{}
type userKey struct{}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

func (a *api) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := rand.Text()
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			a.logger.InfoContext(r.Context(), "http request",
				"request_id", id,
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.Status(),
				"duration", time.Since(start),
			)
		}()
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					panic(value)
				}
				a.logger.ErrorContext(r.Context(), "http panic",
					"request_id", id,
					"panic", fmt.Sprint(value),
					"stack", string(debug.Stack()),
				)
				if wrapped.Status() != 0 {
					// Headers are already committed, so a JSON error would corrupt the response.
					panic(http.ErrAbortHandler)
				}
				writeError(wrapped, r, http.StatusInternalServerError, "internal_error", "服务暂时无法处理请求")
			}
		}()
		next.ServeHTTP(wrapped, r)
	})
}

func (a *api) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.Authenticate(r.Context(), sessionToken(r))
		if err != nil {
			a.authError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}
