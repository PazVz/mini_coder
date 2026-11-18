package httpapi

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestFrontendRoutesAndAssets(t *testing.T) {
	handler := NewHandler(Config{})
	for _, route := range []string{"/", "/auth/setup", "/auth/login", "/auth/register", "/auth/email/confirm", "/account"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := perform(handler, method, route, "", nil)
			if response.Code != http.StatusOK {
				t.Fatalf("%s %s = %d", method, route, response.Code)
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
				t.Errorf("%s returned an unexpected content type", route)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s cached the SPA shell", route)
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Errorf("HEAD %s returned a body", route)
			}
		}
	}
	page := perform(handler, http.MethodGet, "/account", "", nil)
	match := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindStringSubmatch(page.Body.String())
	if len(match) != 2 {
		t.Fatal("SPA entry does not reference a built JavaScript asset")
	}
	asset := perform(handler, http.MethodGet, match[1], "", nil)
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("built asset response = %d %s", asset.Code, asset.Header().Get("Content-Type"))
	}
	if asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatal("content-hashed asset is missing immutable caching")
	}
	for _, route := range []string{"/api/missing", "/api/v1/missing", "/assets/missing.js", "/assets/", "/assets/../index.html", "/missing", "/auth/missing", "/.env"} {
		assertError(t, perform(handler, http.MethodGet, route, "", nil), http.StatusNotFound, "not_found")
	}
	assertError(t, perform(handler, http.MethodPost, "/account", "", nil), http.StatusMethodNotAllowed, "method_not_allowed")
}
