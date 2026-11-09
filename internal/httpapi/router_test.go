package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PazVz/mini_coder/internal/httpapi"
)

func TestHealthz(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	httpapi.NewHandler(httpapi.Config{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

func TestRouting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"unknown path", http.MethodGet, "/missing", http.StatusNotFound},
		{"health subpath", http.MethodGet, "/healthz/extra", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			httpapi.NewHandler(httpapi.Config{}).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status {
				t.Errorf("status = %d, want %d", response.Code, tc.status)
			}
		})
	}
}

func TestHealthzHEAD(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{}))
	t.Cleanup(server.Close)

	response, err := server.Client().Head(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("HEAD /healthz: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("HEAD response body = %q, want empty", body)
	}
}
