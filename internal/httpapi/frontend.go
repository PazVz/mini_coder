package httpapi

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/PazVz/mini_coder/web"
)

func registerFrontend(router chi.Router) {
	// Only page routes receive the SPA shell; unknown APIs and assets retain 404s.
	for _, route := range []string{"/", "/auth/setup", "/auth/register", "/auth/login", "/auth/email/confirm", "/account"} {
		router.Get(route, frontendPage)
		router.Head(route, frontendPage)
	}
	router.Get("/assets/*", frontendAsset)
	router.Head("/assets/*", frontendAsset)
}

func frontendPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	serveFrontendFile(w, r, "dist/index.html")
}

func frontendAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !fs.ValidPath(name) {
		writeError(w, r, http.StatusNotFound, "not_found", "资源不存在")
		return
	}
	serveFrontendFile(w, r, "dist/"+name)
}

func serveFrontendFile(w http.ResponseWriter, r *http.Request, name string) {
	content, err := web.Files.ReadFile(name)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "not_found", "资源不存在")
		return
	}
	if strings.HasPrefix(name, "dist/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(content))
}
