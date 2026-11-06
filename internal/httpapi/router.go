// Package httpapi defines the application's HTTP routes.
package httpapi

import (
	"io"
	"net/http"
)

// NewHandler creates the HTTP router. GET patterns also support HEAD requests.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, "{\"status\":\"ok\"}\n")
}
